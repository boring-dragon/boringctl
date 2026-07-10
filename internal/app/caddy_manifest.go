package app

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type caddyManifest struct {
	Sites []caddyManifestSite `yaml:"sites"`
}

type caddyManifestSite struct {
	Domain              string    `yaml:"domain"`
	Slug                string    `yaml:"slug"`
	Visibility          string    `yaml:"visibility"`
	AppType             string    `yaml:"app_type"`
	UpstreamScheme      string    `yaml:"upstream_scheme,omitempty"`
	UpstreamHost        string    `yaml:"upstream_host,omitempty"`
	UpstreamPort        int       `yaml:"upstream_port,omitempty"`
	RootPath            string    `yaml:"root_path,omitempty"`
	UseWAF              bool      `yaml:"waf"`
	InsecureTLSUpstream bool      `yaml:"insecure_tls_upstream,omitempty"`
	SitePath            string    `yaml:"site_path"`
	RulePath            string    `yaml:"rule_path,omitempty"`
	UpdatedAt           time.Time `yaml:"updated_at"`
}

func (service *Service) managedCaddySite(domain string) (caddyManifestSite, error) {
	domain = normalizeDomain(domain)
	manifest, err := service.loadCaddyManifest()
	if err != nil {
		return caddyManifestSite{}, err
	}

	for _, site := range manifest.Sites {
		if site.Domain == domain {
			return site, nil
		}
	}

	return caddyManifestSite{}, fmt.Errorf("%s is not managed by boringctl", domain)
}

func (service *Service) loadCaddyManifest() (caddyManifest, error) {
	manifestPath, err := service.caddyManifestPath()
	if err != nil {
		return caddyManifest{}, err
	}

	fileContents, err := os.ReadFile(manifestPath)
	if errors.Is(err, os.ErrNotExist) {
		return caddyManifest{}, nil
	}
	if err != nil {
		return caddyManifest{}, err
	}

	var manifest caddyManifest
	if err := yaml.Unmarshal(fileContents, &manifest); err != nil {
		return caddyManifest{}, err
	}

	return manifest, nil
}

func (service *Service) saveCaddyManifest(manifest caddyManifest) error {
	sort.Slice(manifest.Sites, func(leftIndex int, rightIndex int) bool {
		return manifest.Sites[leftIndex].Domain < manifest.Sites[rightIndex].Domain
	})

	manifestPath, err := service.caddyManifestPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(manifestPath), 0o755); err != nil {
		return err
	}

	fileContents, err := yaml.Marshal(manifest)
	if err != nil {
		return err
	}

	return os.WriteFile(manifestPath, fileContents, 0o644)
}

func (service *Service) upsertCaddyManifestSite(request CaddySiteRequest, result CaddySiteResult) error {
	manifest, err := service.loadCaddyManifest()
	if err != nil {
		return err
	}

	site := caddyManifestSite{
		Domain:              result.Domain,
		Slug:                result.Slug,
		Visibility:          result.Visibility,
		AppType:             result.AppType,
		UpstreamScheme:      request.UpstreamScheme,
		UpstreamHost:        request.UpstreamHost,
		UpstreamPort:        request.UpstreamPort,
		RootPath:            request.rootPath(),
		UseWAF:              request.UseWAF,
		InsecureTLSUpstream: request.InsecureTLSUpstream,
		SitePath:            service.relativeCaddyPath(result.SitePath),
		RulePath:            service.relativeCaddyPath(result.RulePath),
		UpdatedAt:           time.Now().UTC(),
	}

	for siteIndex := range manifest.Sites {
		if manifest.Sites[siteIndex].Domain == site.Domain {
			manifest.Sites[siteIndex] = site
			return service.saveCaddyManifest(manifest)
		}
	}

	manifest.Sites = append(manifest.Sites, site)
	return service.saveCaddyManifest(manifest)
}

func (service *Service) removeCaddyManifestSite(domain string) error {
	manifest, err := service.loadCaddyManifest()
	if err != nil {
		return err
	}

	domain = normalizeDomain(domain)
	filteredSites := make([]caddyManifestSite, 0, len(manifest.Sites))
	for _, site := range manifest.Sites {
		if site.Domain != domain {
			filteredSites = append(filteredSites, site)
		}
	}
	manifest.Sites = filteredSites

	return service.saveCaddyManifest(manifest)
}

func (service *Service) caddyManifestPath() (string, error) {
	repoPath, err := service.Config.CaddyRepoPath()
	if err != nil {
		return "", err
	}
	return filepath.Join(repoPath, "caddy", "manifest.yaml"), nil
}

func (service *Service) relativeCaddyPath(path string) string {
	if path == "" {
		return ""
	}

	repoPath, err := service.Config.CaddyRepoPath()
	if err != nil {
		return path
	}

	caddyPath := filepath.Join(repoPath, "caddy")
	relativePath, err := filepath.Rel(caddyPath, path)
	if err != nil || strings.HasPrefix(relativePath, "..") {
		return path
	}

	return filepath.ToSlash(relativePath)
}

func (service *Service) absoluteCaddyPath(path string) string {
	if path == "" || filepath.IsAbs(path) {
		return path
	}

	repoPath, err := service.Config.CaddyRepoPath()
	if err != nil {
		return path
	}

	return filepath.Join(repoPath, "caddy", filepath.FromSlash(path))
}

func manifestSiteUpstream(site caddyManifestSite) string {
	if site.AppType == CaddyAppStatic || site.UpstreamHost == "" || site.UpstreamPort == 0 {
		return ""
	}

	scheme := site.UpstreamScheme
	if scheme == "" {
		scheme = "http"
	}

	return fmt.Sprintf("%s://%s:%d", scheme, site.UpstreamHost, site.UpstreamPort)
}
