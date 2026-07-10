package app

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

func (service *Service) discoverCaddySites() ([]CaddySiteSummary, error) {
	repoPath, err := service.Config.CaddyRepoPath()
	if err != nil {
		return nil, err
	}

	var sites []CaddySiteSummary
	for _, visibility := range []string{CaddyVisibilityInternal, CaddyVisibilityPublic} {
		sitesPath := filepath.Join(repoPath, "caddy", "sites", visibility)
		entries, err := os.ReadDir(sitesPath)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}

		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".caddy") || strings.HasPrefix(entry.Name(), ".") {
				continue
			}
			sitePath := filepath.Join(sitesPath, entry.Name())
			fileSites, err := parseCaddySiteFile(sitePath, visibility)
			if err != nil {
				return nil, err
			}
			for _, site := range fileSites {
				if site.Domain != "" {
					sites = append(sites, site)
				}
			}
		}
	}

	return sites, nil
}

func parseCaddySiteFile(path string, visibility string) ([]CaddySiteSummary, error) {
	fileContents, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var sites []CaddySiteSummary
	var currentSite *CaddySiteSummary
	braceDepth := 0
	lines := strings.Split(string(fileContents), "\n")
	for _, line := range lines {
		trimmedLine := strings.TrimSpace(line)
		if braceDepth == 0 && strings.HasSuffix(trimmedLine, "{") && !strings.HasPrefix(trimmedLine, "(") {
			domain := strings.TrimSpace(strings.TrimSuffix(trimmedLine, "{"))
			currentSite = &CaddySiteSummary{
				Domain:     domain,
				Slug:       caddySlugFromDomain(domain),
				Visibility: visibility,
				AppType:    CaddyAppGeneric,
				Managed:    false,
				SitePath:   path,
			}
			braceDepth += strings.Count(line, "{") - strings.Count(line, "}")
			if braceDepth == 0 {
				sites = append(sites, *currentSite)
				currentSite = nil
			}
			continue
		}

		if currentSite == nil {
			continue
		}

		updateDiscoveredCaddySite(currentSite, trimmedLine)
		braceDepth += strings.Count(line, "{") - strings.Count(line, "}")
		if braceDepth == 0 {
			sites = append(sites, *currentSite)
			currentSite = nil
		}
	}

	return sites, nil
}

func updateDiscoveredCaddySite(site *CaddySiteSummary, line string) {
	if strings.HasPrefix(line, "reverse_proxy ") {
		site.Upstream = strings.TrimSpace(strings.TrimPrefix(line, "reverse_proxy "))
		site.Upstream = strings.TrimSuffix(site.Upstream, " {")
	}
	if line == "file_server" {
		site.AppType = CaddyAppStatic
	}
	if strings.HasPrefix(line, "try_files ") {
		site.AppType = CaddyAppSPA
	}
	if strings.HasPrefix(line, "php_fastcgi ") {
		site.AppType = CaddyAppPHPFPM
		site.Upstream = strings.TrimSpace(strings.TrimPrefix(line, "php_fastcgi "))
	}
	if strings.HasPrefix(line, "root * ") {
		site.RootPath = strings.TrimSpace(strings.TrimPrefix(line, "root * "))
	}
	if line == "waf {" {
		site.UseWAF = true
	}
	if strings.HasPrefix(line, "rule_file ") {
		site.RulePath = strings.TrimSpace(strings.TrimPrefix(line, "rule_file "))
	}
}
