package tui

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/boring-labs/boringctl/internal/app"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func (model model) caddyMenuItems() []list.Item {
	return []list.Item{
		item{title: "Add Route", description: "Create a managed Caddy route in the homelab Git repo", value: "add"},
		item{title: "List Routes", description: "Browse managed and discovered Caddy routes", value: "list"},
		item{title: "Check Config", description: "Stage and validate Caddy without applying", value: "check"},
		item{title: "Deploy Config", description: "Deploy the Git Caddy tree to the LXC", value: "deploy"},
	}
}

func (model model) caddySiteItems() []list.Item {
	items := make([]list.Item, 0, len(model.caddySites))
	for _, site := range model.caddySites {
		owner := "discovered"
		if site.Managed {
			owner = "boringctl"
		}

		waf := "WAF off"
		if site.UseWAF {
			waf = "WAF on"
		}

		upstream := site.Upstream
		if upstream == "" && site.RootPath != "" {
			upstream = site.RootPath
		}
		if upstream == "" {
			upstream = "static/respond"
		}

		items = append(items, item{
			title:       site.Domain,
			description: fmt.Sprintf("%s · %s · %s · %s · %s", site.Visibility, site.AppType, upstream, waf, owner),
			value:       site.Domain,
		})
	}
	if len(items) == 0 {
		items = append(items, item{title: "No routes found", description: "Add a managed Caddy route first", value: ""})
	}
	return items
}

func (model model) caddyRouteActionItems() []list.Item {
	items := []list.Item{
		item{title: "Edit Route", description: "Prompt through this managed route and rewrite it", value: "edit"},
		item{title: "Remove Route", description: "Delete this managed route from Git", value: "remove"},
	}
	if !model.selectedCaddy.Managed {
		items = []list.Item{
			item{title: "Discovered route", description: "Only boringctl-managed routes can be edited or removed here", value: ""},
		}
	}
	return items
}

func (model model) caddyVisibilityItems() []list.Item {
	return []list.Item{
		item{title: "Internal", description: "LAN and WireGuard only", value: app.CaddyVisibilityInternal},
		item{title: "Public", description: "Internet-facing route", value: app.CaddyVisibilityPublic},
	}
}

func (model model) caddyTypeItems() []list.Item {
	templates := app.CaddyTemplates()
	items := make([]list.Item, 0, len(templates))
	for _, template := range templates {
		items = append(items, item{
			title:       template.Label,
			description: template.Description,
			value:       template.Name,
		})
	}
	return items
}

func (model model) caddySchemeItems() []list.Item {
	return []list.Item{
		item{title: "HTTP", description: "Plain HTTP upstream", value: "http"},
		item{title: "HTTPS", description: "HTTPS upstream", value: "https"},
	}
}

func (model model) caddyWAFItems() []list.Item {
	defaultValue := model.caddyRequest.Visibility == app.CaddyVisibilityPublic
	if model.caddyRequest.WAFExplicit {
		defaultValue = model.caddyRequest.UseWAF
	}

	items := []list.Item{
		item{title: "Enable WAF", description: "Add caddy-waf scanner path rules", value: "yes"},
		item{title: "No WAF", description: "Do not add WAF to this route", value: "no"},
	}
	if !defaultValue {
		items[0], items[1] = items[1], items[0]
	}
	return items
}

func (model model) caddyDeployItems() []list.Item {
	return []list.Item{
		item{title: "Write Git files only", description: "Do not deploy yet", value: "no"},
		item{title: "Write and deploy", description: "Validate and reload Caddy after writing", value: "yes"},
	}
}

func (model model) caddyConfirmView() string {
	var builder strings.Builder
	switch model.caddyAction {
	case "add", "edit":
		builder.WriteString("Caddy route\n\n")
		builder.WriteString(fmt.Sprintf("Domain:     %s\n", model.caddyRequest.Domain))
		builder.WriteString(fmt.Sprintf("Visibility: %s\n", model.caddyRequest.Visibility))
		builder.WriteString(fmt.Sprintf("Type:       %s\n", model.caddyRequest.AppType))
		if caddyTUITemplate(model.caddyRequest.AppType).NeedsTarget {
			builder.WriteString(fmt.Sprintf("Upstream:   %s://%s:%d\n", model.caddyRequest.UpstreamScheme, model.caddyRequest.UpstreamHost, model.caddyRequest.UpstreamPort))
		}
		if model.caddyRequest.RootPath != "" {
			builder.WriteString(fmt.Sprintf("Root:       %s\n", model.caddyRequest.RootPath))
		}
		builder.WriteString(fmt.Sprintf("WAF:        %t\n", model.caddyRequest.UseWAF))
		builder.WriteString(fmt.Sprintf("Deploy:     %t\n", model.caddyRequest.Deploy))
	case "remove":
		builder.WriteString("Remove Caddy route\n\n")
		builder.WriteString(fmt.Sprintf("Domain: %s\n", model.selectedCaddy.Domain))
	case "check":
		builder.WriteString("Stage and validate Caddy config\n")
	case "deploy":
		builder.WriteString("Deploy Caddy config\n\n")
		builder.WriteString("This validates, backs up /etc/caddy, swaps the staged tree, and reloads Caddy.")
	}
	builder.WriteString("\n\nPress enter to confirm or esc to go back.")
	return panelStyle.Width(model.contentWidth()).Render(builder.String())
}

func (model model) caddyDoneView() string {
	var builder strings.Builder
	if model.error != "" {
		builder.WriteString(errorStyle.Render("Caddy action failed"))
		builder.WriteString("\n\n")
		builder.WriteString(lipgloss.NewStyle().Width(model.contentWidth() - 6).Render(model.error))
	} else {
		builder.WriteString(successStyle.Render("Caddy action complete"))
		builder.WriteString("\n\n")
		builder.WriteString(model.caddyMessage)
	}
	if len(model.caddySteps) > 0 {
		builder.WriteString("\n\n")
		for _, step := range model.caddySteps {
			builder.WriteString("✓ ")
			builder.WriteString(step)
			builder.WriteString("\n")
		}
	}
	builder.WriteString("\nPress enter to continue.")
	return panelStyle.Width(model.contentWidth()).Render(strings.TrimRight(builder.String(), "\n"))
}

func (model *model) selectCaddyMenuItem(value string) (tea.Model, tea.Cmd) {
	model.caddyAction = value
	model.caddyMessage = ""
	model.caddySteps = nil
	model.error = ""
	switch value {
	case "add":
		model.caddyRequest = app.CaddySiteRequest{UpstreamScheme: "http"}
		model.screen = screenCaddyDomain
		model.setInput("Domain", "app."+model.service.Config.Caddy.DefaultDomain)
		return *model, nil
	case "list":
		model.screen = screenCaddyLoading
		return *model, model.loadCaddySitesCommand()
	case "check", "deploy":
		model.screen = screenCaddyConfirm
		return *model, nil
	default:
		return *model, nil
	}
}

func (model *model) selectCaddySite(value string) (tea.Model, tea.Cmd) {
	if value == "" {
		return *model, nil
	}
	for _, site := range model.caddySites {
		if site.Domain == value {
			model.selectedCaddy = site
			model.screen = screenCaddyRouteAction
			model.setList("Caddy Route Actions", model.caddyRouteActionItems())
			return *model, nil
		}
	}
	model.error = fmt.Sprintf("Caddy site %s was not found", value)
	return *model, nil
}

func (model *model) selectCaddyRouteAction(value string) (tea.Model, tea.Cmd) {
	if value == "" {
		return *model, nil
	}

	model.caddyAction = value
	model.caddyMessage = ""
	model.caddySteps = nil
	model.error = ""
	switch value {
	case "edit":
		request, err := model.service.CaddySiteRequestForDomain(model.selectedCaddy.Domain)
		if err != nil {
			model.error = err.Error()
			return *model, nil
		}
		model.caddyRequest = request
		model.screen = screenCaddyVisibility
		model.setList("Caddy Visibility", model.caddyVisibilityItems())
		return *model, nil
	case "remove":
		model.screen = screenCaddyConfirm
		return *model, nil
	default:
		return *model, nil
	}
}

func (model model) loadCaddySitesCommand() tea.Cmd {
	service := model.service
	return func() tea.Msg {
		sites, err := service.ListCaddySites()
		return caddySitesLoadedMsg{sites: sites, err: err}
	}
}

func (model model) caddyCommand() tea.Cmd {
	service := model.service
	action := model.caddyAction
	request := model.caddyRequest
	selectedSite := model.selectedCaddy

	return func() tea.Msg {
		var steps []string
		reporter := func(message string) {
			steps = append(steps, message)
		}

		ctx := context.Background()
		switch action {
		case "add", "edit":
			request.Force = action == "edit"
			result, err := service.AddCaddySite(ctx, request, reporter)
			if err != nil {
				return caddyFinishedMsg{steps: steps, err: err}
			}

			message := fmt.Sprintf("Route %s written to %s", result.Domain, result.SitePath)
			if result.RootPath != "" {
				message += "\nRoot: " + result.RootPath
			}
			if result.Deployed {
				message += "\n" + result.DeploySummary
			}
			return caddyFinishedMsg{message: message, steps: steps}
		case "remove":
			result, err := service.RemoveCaddySite(ctx, selectedSite.Domain, false, false, reporter)
			if err != nil {
				return caddyFinishedMsg{steps: steps, err: err}
			}
			return caddyFinishedMsg{message: fmt.Sprintf("Removed %s", result.Domain), steps: steps}
		case "check":
			result, err := service.DeployCaddy(ctx, false, reporter)
			if err != nil {
				return caddyFinishedMsg{steps: steps, err: err}
			}
			return caddyFinishedMsg{message: result.Summary, steps: steps}
		case "deploy":
			result, err := service.DeployCaddy(ctx, true, reporter)
			if err != nil {
				return caddyFinishedMsg{steps: steps, err: err}
			}
			return caddyFinishedMsg{message: result.Summary, steps: steps}
		default:
			return caddyFinishedMsg{err: fmt.Errorf("unsupported Caddy action %s", action)}
		}
	}
}

func caddyFallbackHost(value string) string {
	if value == "" {
		return "192.0.2.50"
	}
	return value
}

func caddyFallbackPort(value int) string {
	if value <= 0 {
		return "3000"
	}
	return strconv.Itoa(value)
}

func caddyTUITemplate(name string) app.CaddyTemplate {
	for _, template := range app.CaddyTemplates() {
		if template.Name == name {
			return template
		}
	}
	return app.CaddyTemplates()[0]
}

func (model model) caddyDefaultRoot() string {
	if model.caddyRequest.RootPath != "" {
		return model.caddyRequest.RootPath
	}

	template := caddyTUITemplate(model.caddyRequest.AppType)
	slug := strings.Trim(strings.ToLower(model.caddyRequest.Domain), ".")
	parts := strings.Split(slug, ".")
	if len(parts) > 2 {
		parts = parts[:len(parts)-2]
	}
	slug = strings.Join(parts, "-")
	if slug == "" {
		slug = "site"
	}
	return strings.ReplaceAll(template.DefaultRoot, "{slug}", slug)
}
