package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

var caddyDomainPattern = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?(\.[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?)+$`)

func (service *Service) AddCaddySite(ctx context.Context, request CaddySiteRequest, reporter Reporter) (result CaddySiteResult, resultErr error) {
	resolved, err := service.resolveCaddySiteRequest(request)
	if err != nil {
		return CaddySiteResult{}, err
	}

	result = CaddySiteResult{
		Domain:     resolved.Domain,
		Slug:       resolved.slug(),
		SitePath:   resolved.sitePath(service.Config),
		Visibility: resolved.Visibility,
		AppType:    resolved.AppType,
		Upstream:   resolved.upstreamURL(),
		RootPath:   resolved.rootPath(),
	}
	if resolved.UseWAF {
		result.RulePath = resolved.rulePath(service.Config)
	}
	if request.DryRun {
		return result, nil
	}
	transactionLock, err := service.acquireCaddyTransactionLock()
	if err != nil {
		return CaddySiteResult{}, err
	}
	defer releaseCaddyTransactionLock(transactionLock, reporter)

	operation := "caddy_add_site"
	if _, err := os.Stat(result.SitePath); err == nil {
		operation = "caddy_edit_site"
	}
	defer func() {
		status := "completed"
		summary := "Caddy site files updated"
		if resultErr != nil {
			status = "failed"
			summary = "Caddy site update failed"
		}
		recordOperation(reporter, OperationJournalEntry{
			Operation: operation,
			Status:    status,
			Target:    resolved.Domain,
			Summary:   summary,
		})
	}()

	if err := writeNewFile(result.SitePath, renderCaddySite(resolved), request.Force); err != nil {
		return CaddySiteResult{}, err
	}
	report(reporter, fmt.Sprintf("Wrote %s", result.SitePath))

	if resolved.UseWAF {
		if err := writeNewFile(result.RulePath, renderCaddyWAFRules(resolved), request.Force); err != nil {
			return CaddySiteResult{}, err
		}
		report(reporter, fmt.Sprintf("Wrote %s", result.RulePath))
	} else if err := removeIfExists(resolved.rulePath(service.Config)); err != nil {
		return CaddySiteResult{}, err
	}

	if err := service.upsertCaddyManifestSite(resolved, result); err != nil {
		return CaddySiteResult{}, err
	}
	report(reporter, "Updated caddy manifest")

	if !request.Deploy {
		return result, nil
	}

	deployResult, err := service.deployCaddyWithOptions(ctx, CaddyDeployOptions{Apply: true, Smoke: true}, reporter)
	result.Deployed = deployResult.Applied
	result.DeploySummary = deployResult.Summary
	result.DeployBackup = deployResult.Backup
	result.DeployRolledBack = deployResult.RolledBack
	result.DeployRollbackSummary = deployResult.RollbackSummary
	if err != nil {
		return result, err
	}

	return result, nil
}

func (service *Service) ListCaddySites() ([]CaddySiteSummary, error) {
	manifest, err := service.loadCaddyManifest()
	if err != nil {
		return nil, err
	}

	sitesByDomain := make(map[string]CaddySiteSummary, len(manifest.Sites))
	for _, site := range manifest.Sites {
		sitesByDomain[site.Domain] = CaddySiteSummary{
			Domain:     site.Domain,
			Slug:       site.Slug,
			Visibility: site.Visibility,
			AppType:    site.AppType,
			Upstream:   manifestSiteUpstream(site),
			RootPath:   site.RootPath,
			UseWAF:     site.UseWAF,
			Managed:    true,
			SitePath:   service.absoluteCaddyPath(site.SitePath),
			RulePath:   service.absoluteCaddyPath(site.RulePath),
		}
	}

	discoveredSites, err := service.discoverCaddySites()
	if err != nil {
		return nil, err
	}
	for _, discoveredSite := range discoveredSites {
		if _, exists := sitesByDomain[discoveredSite.Domain]; exists {
			continue
		}
		sitesByDomain[discoveredSite.Domain] = discoveredSite
	}

	sites := make([]CaddySiteSummary, 0, len(sitesByDomain))
	for _, site := range sitesByDomain {
		sites = append(sites, site)
	}
	sort.Slice(sites, func(leftIndex int, rightIndex int) bool {
		if sites[leftIndex].Managed != sites[rightIndex].Managed {
			return sites[leftIndex].Managed
		}
		return sites[leftIndex].Domain < sites[rightIndex].Domain
	})

	return sites, nil
}

func (service *Service) CaddySiteRequestForDomain(domain string) (CaddySiteRequest, error) {
	site, err := service.managedCaddySite(domain)
	if err != nil {
		return CaddySiteRequest{}, err
	}

	return CaddySiteRequest{
		Domain:              site.Domain,
		Visibility:          site.Visibility,
		AppType:             site.AppType,
		UpstreamScheme:      site.UpstreamScheme,
		UpstreamHost:        site.UpstreamHost,
		UpstreamPort:        site.UpstreamPort,
		RootPath:            site.RootPath,
		UseWAF:              site.UseWAF,
		WAFExplicit:         true,
		InsecureTLSUpstream: site.InsecureTLSUpstream,
		Force:               true,
	}, nil
}

func (service *Service) RemoveCaddySite(ctx context.Context, domain string, deploy bool, dryRun bool, reporter Reporter) (result CaddyRemoveResult, resultErr error) {
	site, err := service.managedCaddySite(domain)
	if err != nil {
		return CaddyRemoveResult{}, err
	}

	result = CaddyRemoveResult{
		Domain:   site.Domain,
		SitePath: service.absoluteCaddyPath(site.SitePath),
		RulePath: service.absoluteCaddyPath(site.RulePath),
	}
	if dryRun {
		return result, nil
	}
	transactionLock, err := service.acquireCaddyTransactionLock()
	if err != nil {
		return CaddyRemoveResult{}, err
	}
	defer releaseCaddyTransactionLock(transactionLock, reporter)
	defer func() {
		status := "completed"
		summary := "Caddy site files removed"
		if resultErr != nil {
			status = "failed"
			summary = "Caddy site removal failed"
		}
		recordOperation(reporter, OperationJournalEntry{
			Operation: "caddy_remove_site",
			Status:    status,
			Target:    site.Domain,
			Summary:   summary,
		})
	}()

	if err := removeIfExists(result.SitePath); err != nil {
		return CaddyRemoveResult{}, err
	}
	report(reporter, fmt.Sprintf("Removed %s", result.SitePath))

	if result.RulePath != "" {
		if err := removeIfExists(result.RulePath); err != nil {
			return CaddyRemoveResult{}, err
		}
		report(reporter, fmt.Sprintf("Removed %s", result.RulePath))
	}

	if err := service.removeCaddyManifestSite(site.Domain); err != nil {
		return CaddyRemoveResult{}, err
	}
	report(reporter, "Updated caddy manifest")
	result.Removed = true

	if !deploy {
		return result, nil
	}

	deployResult, err := service.deployCaddyWithOptions(ctx, CaddyDeployOptions{Apply: true, Smoke: true}, reporter)
	result.Deployed = deployResult.Applied
	result.DeploySummary = deployResult.Summary
	result.DeployBackup = deployResult.Backup
	result.DeployRolledBack = deployResult.RolledBack
	result.DeployRollbackSummary = deployResult.RollbackSummary
	if err != nil {
		return result, err
	}

	return result, nil
}

func (service *Service) resolveCaddySiteRequest(request CaddySiteRequest) (CaddySiteRequest, error) {
	request.Domain = normalizeDomain(request.Domain)
	request.Visibility = strings.ToLower(strings.TrimSpace(request.Visibility))
	request.AppType = normalizeCaddyTemplateName(request.AppType)
	request.UpstreamScheme = strings.ToLower(strings.TrimSpace(request.UpstreamScheme))
	request.UpstreamHost = strings.TrimSpace(request.UpstreamHost)
	request.RootPath = strings.TrimSpace(request.RootPath)
	request.CommonProxySnippet = strings.TrimSpace(service.Config.Caddy.CommonProxySnippet)
	request.InternalACLSnippet = strings.TrimSpace(service.Config.Caddy.InternalACLSnippet)

	if request.Domain == "" {
		return request, errors.New("domain is required")
	}
	if !caddyDomainPattern.MatchString(request.Domain) {
		return request, fmt.Errorf("invalid domain %q", request.Domain)
	}

	if request.Visibility == "" {
		request.Visibility = CaddyVisibilityInternal
	}
	if request.Visibility != CaddyVisibilityInternal && request.Visibility != CaddyVisibilityPublic {
		return request, fmt.Errorf("visibility must be %q or %q", CaddyVisibilityInternal, CaddyVisibilityPublic)
	}
	if request.Visibility == CaddyVisibilityInternal && request.InternalACLSnippet == "" {
		return request, errors.New("caddy.internal_acl_snippet is required for internal routes")
	}

	if request.AppType == "" {
		request.AppType = CaddyAppGeneric
	}
	template, exists := caddyTemplate(request.AppType)
	if !exists {
		return request, fmt.Errorf("unknown caddy template %q", request.AppType)
	}

	if request.UpstreamScheme == "" {
		request.UpstreamScheme = "http"
	}
	if request.UpstreamScheme != "http" && request.UpstreamScheme != "https" {
		return request, errors.New("upstream scheme must be http or https")
	}

	if template.NeedsTarget {
		if request.UpstreamHost == "" {
			return request, errors.New("upstream host is required")
		}
		if request.UpstreamPort <= 0 || request.UpstreamPort > 65535 {
			return request, errors.New("upstream port must be between 1 and 65535")
		}
	}
	if template.NeedsRoot && request.RootPath == "" {
		request.RootPath = strings.ReplaceAll(template.DefaultRoot, "{slug}", request.slug())
	}
	if request.Visibility == CaddyVisibilityPublic && !request.WAFExplicit {
		request.UseWAF = service.Config.Caddy.PublicWAFByDefault
	}

	return request, nil
}

func writeNewFile(path string, contents string, force bool) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if !force {
		if _, err := os.Stat(path); err == nil {
			return fmt.Errorf("%s already exists; pass --force to overwrite", path)
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return os.WriteFile(path, []byte(contents), 0o644)
}

func removeIfExists(path string) error {
	if path == "" {
		return nil
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}
