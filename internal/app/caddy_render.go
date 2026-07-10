package app

import (
	"fmt"
	"net"
	"net/url"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

func renderCaddySite(request CaddySiteRequest) string {
	var builder strings.Builder
	builder.WriteString(request.Domain)
	builder.WriteString(" {\n")
	if request.CommonProxySnippet != "" {
		builder.WriteString("\timport ")
		builder.WriteString(request.CommonProxySnippet)
		builder.WriteString(" ")
		builder.WriteString(request.slug())
		builder.WriteString("\n")
	}

	if request.Visibility == CaddyVisibilityInternal {
		builder.WriteString("\timport ")
		builder.WriteString(request.InternalACLSnippet)
		builder.WriteString("\n")
	}

	builder.WriteString("\n")
	if request.UseWAF || caddyTemplateUsesRoute(request.AppType) {
		builder.WriteString("\troute {\n")
		if request.UseWAF {
			builder.WriteString(renderWAFBlock(request))
		}
		builder.WriteString(indentCaddyDirective(renderCaddyHandler(request), 2))
		builder.WriteString("\t}\n")
	} else {
		builder.WriteString(indentCaddyDirective(renderCaddyHandler(request), 1))
	}

	builder.WriteString("}\n")
	return builder.String()
}

func renderWAFBlock(request CaddySiteRequest) string {
	return fmt.Sprintf(`		waf {
			rule_file /etc/caddy/waf/rules/%s.json
			ip_blacklist_file /etc/caddy/waf/blacklists/ip.txt
			dns_blacklist_file /etc/caddy/waf/blacklists/dns.txt
			anomaly_threshold 10
			log_severity warn
			log_json
			log_path /var/log/caddy/waf-%s.log
		}

`, request.slug(), request.slug())
}

func renderCaddyHandler(request CaddySiteRequest) string {
	switch request.AppType {
	case CaddyAppStatic:
		return fmt.Sprintf("root * %s\nfile_server {\n\thide .env .git\n\tprecompressed zstd gzip\n}\n", request.rootPath())
	case CaddyAppSPA:
		return fmt.Sprintf("root * %s\ntry_files {path} /index.html\nfile_server {\n\thide .env .git\n\tprecompressed zstd gzip\n}\n", request.rootPath())
	case CaddyAppPHPFPM:
		return fmt.Sprintf("root * %s\nphp_fastcgi %s\nfile_server {\n\thide .env .git\n\tprecompressed zstd gzip\n}\n", request.rootPath(), request.upstreamHostPort())
	case CaddyAppRealtime:
		return fmt.Sprintf("reverse_proxy %s {\n\tflush_interval -1\n\tstream_timeout 24h\n\tstream_close_delay 5m\n%s}\n", request.upstreamURL(), renderTransportBlock(request))
	case CaddyAppLargeUpload:
		return fmt.Sprintf("request_body {\n\tmax_size 1GB\n}\nreverse_proxy %s {\n%s}\n", request.upstreamURL(), renderLargeUploadProxyOptions(request))
	}

	if request.UpstreamScheme == "https" && request.InsecureTLSUpstream {
		return fmt.Sprintf("reverse_proxy %s {\n\theader_up Host {host}\n\ttransport http {\n\t\ttls_insecure_skip_verify\n\t}\n}\n", request.upstreamURL())
	}

	return fmt.Sprintf("reverse_proxy %s\n", request.upstreamURL())
}

func renderLargeUploadProxyOptions(request CaddySiteRequest) string {
	var builder strings.Builder
	builder.WriteString("\ttransport http {\n")
	builder.WriteString("\t\tread_timeout 10m\n")
	builder.WriteString("\t\twrite_timeout 10m\n")
	if request.UpstreamScheme == "https" && request.InsecureTLSUpstream {
		builder.WriteString("\t\ttls_insecure_skip_verify\n")
	}
	builder.WriteString("\t}\n")
	return builder.String()
}

func renderTransportBlock(request CaddySiteRequest) string {
	if request.UpstreamScheme != "https" || !request.InsecureTLSUpstream {
		return ""
	}
	return "\ttransport http {\n\t\ttls_insecure_skip_verify\n\t}\n"
}

func renderCaddyWAFRules(request CaddySiteRequest) string {
	return fmt.Sprintf(`[
  {
    "id": "%s-scanner-paths",
    "phase": 1,
    "pattern": "(?i)(^|/)(\\.env|\\.git|wp-admin|phpmyadmin|backup|admin\\.php)",
    "targets": ["PATH"],
    "severity": "HIGH",
    "score": 10,
    "mode": "block",
    "priority": 90,
    "description": "Block common scanner paths on %s."
  }
]
`, request.slug(), request.Domain)
}

func indentCaddyDirective(value string, tabCount int) string {
	prefix := strings.Repeat("\t", tabCount)
	lines := strings.Split(strings.TrimRight(value, "\n"), "\n")
	for lineIndex, line := range lines {
		lines[lineIndex] = prefix + line
	}
	return strings.Join(lines, "\n") + "\n"
}

func (request CaddySiteRequest) slug() string {
	return caddySlugFromDomain(request.Domain)
}

func (request CaddySiteRequest) upstreamURL() string {
	if !caddyTemplateNeedsTarget(request.AppType) {
		return ""
	}
	return fmt.Sprintf("%s://%s:%d", request.UpstreamScheme, request.UpstreamHost, request.UpstreamPort)
}

func (request CaddySiteRequest) upstreamHostPort() string {
	if request.UpstreamHost == "" || request.UpstreamPort == 0 {
		return ""
	}
	return fmt.Sprintf("%s:%d", request.UpstreamHost, request.UpstreamPort)
}

func (request CaddySiteRequest) rootPath() string {
	if request.RootPath != "" {
		return request.RootPath
	}
	template, exists := caddyTemplate(request.AppType)
	if !exists || template.DefaultRoot == "" {
		return ""
	}
	return strings.ReplaceAll(template.DefaultRoot, "{slug}", request.slug())
}

func (request CaddySiteRequest) sitePath(loadedConfig interface{ CaddyRepoPath() (string, error) }) string {
	repoPath, _ := loadedConfig.CaddyRepoPath()
	return filepath.Join(repoPath, "caddy", "sites", request.Visibility, request.slug()+".caddy")
}

func (request CaddySiteRequest) rulePath(loadedConfig interface{ CaddyRepoPath() (string, error) }) string {
	repoPath, _ := loadedConfig.CaddyRepoPath()
	return filepath.Join(repoPath, "caddy", "waf", "rules", request.slug()+".json")
}

func ParseCaddyTarget(target string) (string, string, int, error) {
	target = strings.TrimSpace(target)
	if target == "" {
		return "", "", 0, nil
	}

	if strings.Contains(target, "://") {
		parsedURL, err := url.Parse(target)
		if err != nil {
			return "", "", 0, err
		}
		port, err := strconv.Atoi(parsedURL.Port())
		if err != nil {
			return "", "", 0, fmt.Errorf("target URL must include a numeric port")
		}
		return parsedURL.Scheme, parsedURL.Hostname(), port, nil
	}

	host, portValue, err := net.SplitHostPort(target)
	if err != nil {
		return "", "", 0, fmt.Errorf("target must be host:port or scheme://host:port")
	}
	port, err := strconv.Atoi(portValue)
	if err != nil {
		return "", "", 0, fmt.Errorf("target port must be numeric")
	}

	return "", host, port, nil
}

func normalizeDomain(domain string) string {
	return strings.ToLower(strings.Trim(strings.TrimSpace(domain), "."))
}

func caddySlugFromDomain(domain string) string {
	domain = strings.TrimSuffix(domain, ".")
	parts := strings.Split(domain, ".")
	if len(parts) > 2 {
		parts = parts[:len(parts)-2]
	}
	slug := strings.Join(parts, "-")
	slug = strings.ReplaceAll(slug, "_", "-")
	slug = regexp.MustCompile(`[^a-z0-9-]+`).ReplaceAllString(slug, "-")
	slug = strings.Trim(slug, "-")
	if slug == "" {
		return "site"
	}
	return slug
}
