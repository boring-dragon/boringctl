package app

import "strings"

func CaddyTemplates() []CaddyTemplate {
	return []CaddyTemplate{
		{Name: CaddyAppGeneric, Label: "Generic proxy", Description: "Simple reverse proxy for a normal HTTP app or service.", NeedsTarget: true},
		{Name: CaddyAppPHP, Label: "PHP/Laravel HTTP", Description: "Reverse proxy to a PHP/Laravel app already served by FrankenPHP, Nginx, Apache, or another internal HTTP server.", NeedsTarget: true},
		{Name: CaddyAppRealtime, Label: "Realtime proxy", Description: "Reverse proxy tuned for WebSocket/SSE-style long-lived streams.", NeedsTarget: true},
		{Name: CaddyAppLargeUpload, Label: "Large upload proxy", Description: "Reverse proxy with a larger request body limit and longer upstream timeouts.", NeedsTarget: true},
		{Name: CaddyAppStatic, Label: "Static files", Description: "Serve static files directly from Caddy under /srv/caddy/<site>.", NeedsRoot: true, DefaultRoot: "/srv/caddy/{slug}"},
		{Name: CaddyAppSPA, Label: "Single-page app", Description: "Serve a static SPA and fall back to /index.html for client-side routes.", NeedsRoot: true, DefaultRoot: "/srv/caddy/{slug}"},
		{Name: CaddyAppPHPFPM, Label: "PHP-FPM/Laravel", Description: "Serve a PHP app from disk with php_fastcgi and file_server. Use only when Caddy can read the app files.", NeedsTarget: true, NeedsRoot: true, DefaultRoot: "/srv/caddy/{slug}/public"},
	}
}

func caddyTemplate(name string) (CaddyTemplate, bool) {
	name = normalizeCaddyTemplateName(name)
	for _, template := range CaddyTemplates() {
		if template.Name == name {
			return template, true
		}
	}
	return CaddyTemplate{}, false
}

func normalizeCaddyTemplateName(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	switch name {
	case "", "proxy":
		return CaddyAppGeneric
	case "websocket", "ws", "sse", "streaming":
		return CaddyAppRealtime
	case "upload", "uploads", "large":
		return CaddyAppLargeUpload
	case "laravel", "frankenphp":
		return CaddyAppPHP
	case "phpfpm", "laravel-fpm", "fpm":
		return CaddyAppPHPFPM
	default:
		return name
	}
}

func caddyTemplateNeedsTarget(name string) bool {
	template, exists := caddyTemplate(name)
	return exists && template.NeedsTarget
}

func caddyTemplateUsesRoute(name string) bool {
	switch normalizeCaddyTemplateName(name) {
	case CaddyAppStatic, CaddyAppSPA, CaddyAppPHPFPM, CaddyAppRealtime, CaddyAppLargeUpload:
		return true
	default:
		return false
	}
}
