package app

import (
	"context"
	"net/http"
	"time"
)

func (service *Service) SmokeCaddySites(ctx context.Context) ([]CaddySmokeResult, error) {
	return service.SmokeCaddyDomains(ctx, nil)
}

func (service *Service) SmokeCaddyDomains(ctx context.Context, domains []string) ([]CaddySmokeResult, error) {
	sites, err := service.ListCaddySites()
	if err != nil {
		return nil, err
	}

	selectedDomains := make(map[string]bool, len(domains))
	for _, domain := range domains {
		selectedDomains[normalizeDomain(domain)] = true
	}

	httpClient := &http.Client{Timeout: 10 * time.Second}
	results := make([]CaddySmokeResult, 0, len(sites))
	for _, site := range sites {
		if !site.Managed {
			continue
		}
		if len(selectedDomains) > 0 && !selectedDomains[site.Domain] {
			continue
		}

		normalURL := "https://" + site.Domain + "/"
		results = append(results, smokeCaddyURL(ctx, httpClient, site.Domain, normalURL, "status below 500", func(statusCode int) bool {
			return statusCode > 0 && statusCode < 500
		}))

		if site.UseWAF {
			wafURL := "https://" + site.Domain + "/.env"
			results = append(results, smokeCaddyURL(ctx, httpClient, site.Domain, wafURL, "403 from WAF", func(statusCode int) bool {
				return statusCode == http.StatusForbidden
			}))
		}
	}

	return results, nil
}

func caddySmokeResultsFailed(results []CaddySmokeResult) bool {
	for _, result := range results {
		if !result.Passed {
			return true
		}
	}
	return false
}

func smokeCaddyURL(ctx context.Context, httpClient *http.Client, domain string, url string, expected string, passed func(int) bool) CaddySmokeResult {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return CaddySmokeResult{Domain: domain, URL: url, Expected: expected, Error: err.Error()}
	}

	response, err := httpClient.Do(request)
	if err != nil {
		return CaddySmokeResult{Domain: domain, URL: url, Expected: expected, Error: err.Error()}
	}
	defer response.Body.Close()

	return CaddySmokeResult{
		Domain:     domain,
		URL:        url,
		StatusCode: response.StatusCode,
		Expected:   expected,
		Passed:     passed(response.StatusCode),
	}
}
