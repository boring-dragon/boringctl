package cli

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/boring-dragon/boringctl/internal/app"

	"github.com/spf13/cobra"
)

type caddySiteResultPayload struct {
	SchemaVersion int      `json:"schema_version"`
	Operation     string   `json:"operation"`
	Steps         []string `json:"steps,omitempty"`
	DryRun        bool     `json:"dry_run"`
	Domain        string   `json:"domain"`
	Slug          string   `json:"slug"`
	SitePath      string   `json:"site_path"`
	RulePath      string   `json:"rule_path,omitempty"`
	Upstream      string   `json:"upstream,omitempty"`
	RootPath      string   `json:"root_path,omitempty"`
	Visibility    string   `json:"visibility"`
	AppType       string   `json:"app_type"`
	Deployed      bool     `json:"deployed"`
	DeploySummary string   `json:"deploy_summary,omitempty"`
	DeployBackup  string   `json:"deploy_backup,omitempty"`
}

type caddyRemoveResultPayload struct {
	SchemaVersion int      `json:"schema_version"`
	Operation     string   `json:"operation"`
	Steps         []string `json:"steps,omitempty"`
	DryRun        bool     `json:"dry_run"`
	Domain        string   `json:"domain"`
	SitePath      string   `json:"site_path"`
	RulePath      string   `json:"rule_path,omitempty"`
	Removed       bool     `json:"removed"`
	Deployed      bool     `json:"deployed"`
	DeploySummary string   `json:"deploy_summary,omitempty"`
	DeployBackup  string   `json:"deploy_backup,omitempty"`
}

type caddySitesPayload struct {
	SchemaVersion int                `json:"schema_version"`
	ManagedOnly   bool               `json:"managed_only"`
	Sites         []caddySitePayload `json:"sites"`
}

type caddySitePayload struct {
	Domain     string `json:"domain"`
	Slug       string `json:"slug"`
	Visibility string `json:"visibility"`
	AppType    string `json:"app_type"`
	Upstream   string `json:"upstream,omitempty"`
	RootPath   string `json:"root_path,omitempty"`
	UseWAF     bool   `json:"use_waf"`
	Managed    bool   `json:"managed"`
	SitePath   string `json:"site_path,omitempty"`
	RulePath   string `json:"rule_path,omitempty"`
}

type caddyTemplatesPayload struct {
	SchemaVersion int                    `json:"schema_version"`
	Templates     []caddyTemplatePayload `json:"templates"`
}

type caddyTemplatePayload struct {
	Name        string `json:"name"`
	Label       string `json:"label"`
	Description string `json:"description"`
	NeedsTarget bool   `json:"needs_target"`
	NeedsRoot   bool   `json:"needs_root"`
	DefaultRoot string `json:"default_root,omitempty"`
	DefaultWAF  bool   `json:"default_waf"`
}

type caddyDeployPayload struct {
	SchemaVersion   int                       `json:"schema_version"`
	Operation       string                    `json:"operation"`
	Steps           []string                  `json:"steps,omitempty"`
	Validated       bool                      `json:"validated"`
	Applied         bool                      `json:"applied"`
	Backup          string                    `json:"backup,omitempty"`
	RolledBack      bool                      `json:"rolled_back"`
	RollbackSummary string                    `json:"rollback_summary,omitempty"`
	Summary         string                    `json:"summary"`
	Smoke           []caddySmokeResultPayload `json:"smoke,omitempty"`
}

type caddySmokeResultPayload struct {
	Domain     string `json:"domain"`
	URL        string `json:"url"`
	StatusCode int    `json:"status_code"`
	Expected   string `json:"expected"`
	Passed     bool   `json:"passed"`
	Error      string `json:"error,omitempty"`
}

type caddyBackupsPayload struct {
	SchemaVersion int      `json:"schema_version"`
	Backups       []string `json:"backups"`
}

type caddyRollbackPayload struct {
	SchemaVersion int      `json:"schema_version"`
	Operation     string   `json:"operation"`
	Steps         []string `json:"steps,omitempty"`
	Backup        string   `json:"backup"`
	Applied       bool     `json:"applied"`
	Summary       string   `json:"summary"`
}

type caddyDeployCommandError struct {
	err    error
	result app.CaddyDeployResult
}

func (err caddyDeployCommandError) Error() string {
	return err.err.Error()
}

func (err caddyDeployCommandError) Unwrap() error {
	return err.err
}

func caddyNestedDeployError(err error, backup string, rolledBack bool, rollbackSummary string) error {
	if backup == "" {
		return err
	}
	return caddyDeployCommandError{
		err: err,
		result: app.CaddyDeployResult{
			Backup:          backup,
			RolledBack:      rolledBack,
			RollbackSummary: rollbackSummary,
		},
	}
}

func (commandContext *commandContext) caddyCommand() *cobra.Command {
	caddyCommand := &cobra.Command{
		Use:   "caddy",
		Short: "Manage Caddy routes from a Git repository",
	}

	caddyCommand.AddCommand(
		commandContext.caddyAddSiteCommand(),
		commandContext.caddyEditSiteCommand(),
		commandContext.caddyRemoveSiteCommand(),
		commandContext.caddyListCommand(),
		commandContext.caddyTemplatesCommand(),
		commandContext.caddyDeployCommand(),
		commandContext.caddyCheckCommand(),
		commandContext.caddyRollbackCommand(),
	)

	return caddyCommand
}

func (commandContext *commandContext) caddyAddSiteCommand() *cobra.Command {
	request := app.CaddySiteRequest{}
	var target string
	var prompt bool
	var noWAF bool

	addSiteCommand := &cobra.Command{
		Use:   "add-site",
		Short: "Generate a Caddy site file and optionally deploy it",
		RunE: func(cmd *cobra.Command, args []string) error {
			loadedConfig, err := commandContext.loadConfig()
			if err != nil {
				return err
			}

			if target != "" {
				scheme, host, port, err := app.ParseCaddyTarget(target)
				if err != nil {
					return err
				}
				if scheme != "" {
					request.UpstreamScheme = scheme
				}
				request.UpstreamHost = host
				request.UpstreamPort = port
			}

			if commandContext.jsonOutput && (prompt || caddySiteNeedsPrompt(request)) {
				return fmt.Errorf("caddy add-site --json requires complete flags and cannot prompt")
			}

			if prompt || caddySiteNeedsPrompt(request) {
				if err := promptForCaddySite(commandContext.output, os.Stdin, loadedConfig.Caddy.DefaultDomain, &request); err != nil {
					return err
				}
			}
			request.WAFExplicit = request.WAFExplicit || cmd.Flags().Changed("waf")
			if noWAF {
				request.UseWAF = false
				request.WAFExplicit = true
			}

			service := app.NewService(loadedConfig, nil)
			var steps []string
			reporter := commandContext.printStep
			if commandContext.jsonOutput {
				reporter = func(message string) {
					steps = append(steps, message)
				}
			}

			result, err := service.AddCaddySite(cmd.Context(), request, reporter)
			if err != nil {
				return caddyNestedDeployError(err, result.DeployBackup, result.DeployRolledBack, result.DeployRollbackSummary)
			}

			if commandContext.jsonOutput {
				return writeJSON(commandContext.output, caddySiteResultJSONPayload("caddy_add_site", result, request.DryRun, steps))
			}

			printCaddySiteResult(commandContext.output, result, request.DryRun)
			return nil
		},
	}

	addSiteCommand.Flags().StringVar(&request.Domain, "domain", "", "domain to serve, for example app.example.com")
	addSiteCommand.Flags().StringVar(&target, "target", "", "upstream target as host:port or scheme://host:port")
	addSiteCommand.Flags().StringVar(&request.UpstreamScheme, "scheme", "http", "upstream scheme: http or https")
	addSiteCommand.Flags().StringVar(&request.UpstreamHost, "host", "", "upstream host or IP")
	addSiteCommand.Flags().IntVar(&request.UpstreamPort, "port", 0, "upstream port")
	addSiteCommand.Flags().StringVar(&request.RootPath, "root", "", "root path for file-serving templates")
	addSiteCommand.Flags().StringVar(&request.Visibility, "visibility", "", "route visibility: internal or public")
	addSiteCommand.Flags().StringVar(&request.AppType, "type", "", "site template; run 'boringctl caddy templates'")
	addSiteCommand.Flags().BoolVar(&request.UseWAF, "waf", false, "enable caddy-waf for this route")
	addSiteCommand.Flags().BoolVar(&noWAF, "no-waf", false, "disable the default WAF on public routes")
	addSiteCommand.Flags().BoolVar(&request.InsecureTLSUpstream, "insecure-upstream-tls", false, "skip TLS verification for HTTPS upstream")
	addSiteCommand.Flags().BoolVar(&request.Deploy, "deploy", false, "deploy Caddy config after writing files")
	addSiteCommand.Flags().BoolVar(&request.DryRun, "dry-run", false, "show generated paths without writing files")
	addSiteCommand.Flags().BoolVar(&request.Force, "force", false, "overwrite existing generated files")
	addSiteCommand.Flags().BoolVar(&prompt, "prompt", false, "prompt for all missing values")

	return addSiteCommand
}

func (commandContext *commandContext) caddyEditSiteCommand() *cobra.Command {
	request := app.CaddySiteRequest{}
	var target string
	var prompt bool
	var noWAF bool

	editSiteCommand := &cobra.Command{
		Use:   "edit-site <domain>",
		Short: "Update a boringctl-managed Caddy site",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			service, err := commandContext.loadCaddyService()
			if err != nil {
				return err
			}

			existingRequest, err := service.CaddySiteRequestForDomain(args[0])
			if err != nil {
				return err
			}

			if cmd.Flags().Changed("visibility") {
				existingRequest.Visibility = request.Visibility
			}
			if cmd.Flags().Changed("type") {
				existingRequest.AppType = request.AppType
			}
			if cmd.Flags().Changed("scheme") {
				existingRequest.UpstreamScheme = request.UpstreamScheme
			}
			if cmd.Flags().Changed("host") {
				existingRequest.UpstreamHost = request.UpstreamHost
			}
			if cmd.Flags().Changed("port") {
				existingRequest.UpstreamPort = request.UpstreamPort
			}
			if cmd.Flags().Changed("root") {
				existingRequest.RootPath = request.RootPath
			}
			if cmd.Flags().Changed("insecure-upstream-tls") {
				existingRequest.InsecureTLSUpstream = request.InsecureTLSUpstream
			}
			if cmd.Flags().Changed("waf") {
				existingRequest.UseWAF = request.UseWAF
				existingRequest.WAFExplicit = true
			}
			if noWAF {
				existingRequest.UseWAF = false
				existingRequest.WAFExplicit = true
			}
			if target != "" {
				scheme, host, port, err := app.ParseCaddyTarget(target)
				if err != nil {
					return err
				}
				if scheme != "" {
					existingRequest.UpstreamScheme = scheme
				}
				existingRequest.UpstreamHost = host
				existingRequest.UpstreamPort = port
			}

			existingRequest.Deploy = request.Deploy
			existingRequest.DryRun = request.DryRun
			existingRequest.Force = true

			if commandContext.jsonOutput && prompt {
				return fmt.Errorf("caddy edit-site --json cannot use --prompt")
			}

			if prompt {
				if err := promptForCaddySiteEdit(commandContext.output, os.Stdin, &existingRequest); err != nil {
					return err
				}
			}

			var steps []string
			reporter := commandContext.printStep
			if commandContext.jsonOutput {
				reporter = func(message string) {
					steps = append(steps, message)
				}
			}

			result, err := service.AddCaddySite(cmd.Context(), existingRequest, reporter)
			if err != nil {
				return caddyNestedDeployError(err, result.DeployBackup, result.DeployRolledBack, result.DeployRollbackSummary)
			}

			if commandContext.jsonOutput {
				return writeJSON(commandContext.output, caddySiteResultJSONPayload("caddy_edit_site", result, existingRequest.DryRun, steps))
			}

			printCaddySiteResult(commandContext.output, result, existingRequest.DryRun)
			return nil
		},
	}

	editSiteCommand.Flags().StringVar(&target, "target", "", "upstream target as host:port or scheme://host:port")
	editSiteCommand.Flags().StringVar(&request.UpstreamScheme, "scheme", "http", "upstream scheme: http or https")
	editSiteCommand.Flags().StringVar(&request.UpstreamHost, "host", "", "upstream host or IP")
	editSiteCommand.Flags().IntVar(&request.UpstreamPort, "port", 0, "upstream port")
	editSiteCommand.Flags().StringVar(&request.RootPath, "root", "", "root path for file-serving templates")
	editSiteCommand.Flags().StringVar(&request.Visibility, "visibility", "", "route visibility: internal or public")
	editSiteCommand.Flags().StringVar(&request.AppType, "type", "", "site template; run 'boringctl caddy templates'")
	editSiteCommand.Flags().BoolVar(&request.UseWAF, "waf", false, "enable caddy-waf for this route")
	editSiteCommand.Flags().BoolVar(&noWAF, "no-waf", false, "disable WAF on this route")
	editSiteCommand.Flags().BoolVar(&request.InsecureTLSUpstream, "insecure-upstream-tls", false, "skip TLS verification for HTTPS upstream")
	editSiteCommand.Flags().BoolVar(&request.Deploy, "deploy", false, "deploy Caddy config after writing files")
	editSiteCommand.Flags().BoolVar(&request.DryRun, "dry-run", false, "show generated paths without writing files")
	editSiteCommand.Flags().BoolVar(&prompt, "prompt", false, "prompt through current values")

	return editSiteCommand
}

func (commandContext *commandContext) caddyRemoveSiteCommand() *cobra.Command {
	var deploy bool
	var dryRun bool
	var yes bool

	removeSiteCommand := &cobra.Command{
		Use:   "remove-site <domain>",
		Short: "Remove a boringctl-managed Caddy site",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			service, err := commandContext.loadCaddyService()
			if err != nil {
				return err
			}

			if !yes && !commandContext.yes && !dryRun {
				confirmed, err := commandContext.confirm(fmt.Sprintf("Remove Caddy site %s?", args[0]))
				if err != nil {
					return err
				}
				if !confirmed {
					if commandContext.jsonOutput {
						return writeJSON(commandContext.output, actionPayload{
							SchemaVersion: agentOutputSchemaVersion,
							Operation:     "caddy_remove_site",
							Status:        "aborted",
							Name:          args[0],
						})
					}
					fmt.Fprintln(commandContext.output, "Aborted.")
					return nil
				}
			}

			var steps []string
			reporter := commandContext.printStep
			if commandContext.jsonOutput {
				reporter = func(message string) {
					steps = append(steps, message)
				}
			}

			result, err := service.RemoveCaddySite(cmd.Context(), args[0], deploy, dryRun, reporter)
			if err != nil {
				return caddyNestedDeployError(err, result.DeployBackup, result.DeployRolledBack, result.DeployRollbackSummary)
			}

			if commandContext.jsonOutput {
				return writeJSON(commandContext.output, caddyRemoveResultJSONPayload(result, dryRun, steps))
			}

			printCaddyRemoveResult(commandContext.output, result, dryRun)
			return nil
		},
	}

	removeSiteCommand.Flags().BoolVar(&deploy, "deploy", false, "deploy Caddy config after removing files")
	removeSiteCommand.Flags().BoolVar(&dryRun, "dry-run", false, "show what would be removed")
	removeSiteCommand.Flags().BoolVar(&yes, "yes", false, "skip confirmation")

	return removeSiteCommand
}

func (commandContext *commandContext) caddyListCommand() *cobra.Command {
	var managedOnly bool

	listCommand := &cobra.Command{
		Use:   "list",
		Short: "List Caddy routes from the configured Git repository",
		RunE: func(cmd *cobra.Command, args []string) error {
			service, err := commandContext.loadCaddyService()
			if err != nil {
				return err
			}

			sites, err := service.ListCaddySites()
			if err != nil {
				return err
			}

			if commandContext.jsonOutput {
				return writeJSON(commandContext.output, caddySitesJSONPayload(sites, managedOnly))
			}

			printCaddySites(commandContext.output, sites, managedOnly)
			return nil
		},
	}

	listCommand.Flags().BoolVar(&managedOnly, "managed", false, "show only boringctl-managed routes")

	return listCommand
}

func (commandContext *commandContext) caddyTemplatesCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "templates",
		Short: "List available Caddy site templates",
		RunE: func(cmd *cobra.Command, args []string) error {
			templates := app.CaddyTemplates()
			if commandContext.jsonOutput {
				return writeJSON(commandContext.output, caddyTemplatesJSONPayload(templates))
			}

			printCaddyTemplates(commandContext.output, templates)
			return nil
		},
	}
}

func (commandContext *commandContext) caddyDeployCommand() *cobra.Command {
	var yes bool
	var noSmoke bool

	deployCommand := &cobra.Command{
		Use:   "deploy",
		Short: "Deploy the git Caddy tree to the Caddy LXC",
		RunE: func(cmd *cobra.Command, args []string) error {
			service, err := commandContext.loadCaddyService()
			if err != nil {
				return err
			}

			if err := commandContext.confirmCaddyGitChanges(cmd, service, yes); err != nil {
				return err
			}

			var steps []string
			reporter := commandContext.printStep
			if commandContext.jsonOutput {
				reporter = func(message string) {
					steps = append(steps, message)
				}
			}

			result, err := service.DeployCaddyWithOptions(cmd.Context(), app.CaddyDeployOptions{Apply: true, Smoke: !noSmoke}, reporter)
			if err != nil {
				if !commandContext.jsonOutput && result.Backup != "" {
					printCaddyDeployResult(commandContext.output, result)
					if len(result.Smoke) > 0 {
						printCaddySmokeResults(commandContext.output, result.Smoke)
					}
				}
				return caddyDeployCommandError{err: err, result: result}
			}

			if !commandContext.jsonOutput {
				printCaddyDeployResult(commandContext.output, result)
				if !noSmoke {
					printCaddySmokeResults(commandContext.output, result.Smoke)
				}
			}
			if commandContext.jsonOutput {
				return writeJSON(commandContext.output, caddyDeployJSONPayload("caddy_deploy", result, steps, result.Smoke))
			}
			return nil
		},
	}

	deployCommand.Flags().BoolVar(&yes, "yes", false, "skip dirty-git confirmation")
	deployCommand.Flags().BoolVar(&noSmoke, "no-smoke", false, "skip post-deploy smoke checks")

	return deployCommand
}

func (commandContext *commandContext) caddyCheckCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "check",
		Short: "Stage and validate the git Caddy tree without applying it",
		RunE: func(cmd *cobra.Command, args []string) error {
			service, err := commandContext.loadCaddyService()
			if err != nil {
				return err
			}

			var steps []string
			reporter := commandContext.printStep
			if commandContext.jsonOutput {
				reporter = func(message string) {
					steps = append(steps, message)
				}
			}

			result, err := service.DeployCaddy(cmd.Context(), false, reporter)
			if err != nil {
				return err
			}

			if commandContext.jsonOutput {
				return writeJSON(commandContext.output, caddyDeployJSONPayload("caddy_check", result, steps, nil))
			}

			printCaddyDeployResult(commandContext.output, result)
			return nil
		},
	}
}

func (commandContext *commandContext) caddyRollbackCommand() *cobra.Command {
	var listBackups bool
	var yes bool

	rollbackCommand := &cobra.Command{
		Use:   "rollback [backup-name]",
		Short: "List or restore Caddy config backups in the Caddy LXC",
		Args: func(cmd *cobra.Command, args []string) error {
			if listBackups {
				if len(args) != 0 {
					return fmt.Errorf("--list does not accept a backup name")
				}
				return nil
			}
			if len(args) != 1 {
				return fmt.Errorf("backup name is required unless --list is used")
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			service, err := commandContext.loadCaddyService()
			if err != nil {
				return err
			}

			if listBackups {
				backups, err := service.ListCaddyBackups(cmd.Context())
				if err != nil {
					return err
				}
				if commandContext.jsonOutput {
					return writeJSON(commandContext.output, caddyBackupsPayload{
						SchemaVersion: agentOutputSchemaVersion,
						Backups:       backups,
					})
				}
				printCaddyBackups(commandContext.output, backups)
				return nil
			}

			if !yes && !commandContext.yes {
				confirmed, err := commandContext.confirm(fmt.Sprintf("Restore /etc/%s into /etc/caddy?", args[0]))
				if err != nil {
					return err
				}
				if !confirmed {
					if commandContext.jsonOutput {
						return writeJSON(commandContext.output, actionPayload{
							SchemaVersion: agentOutputSchemaVersion,
							Operation:     "caddy_rollback",
							Status:        "aborted",
							Name:          args[0],
						})
					}
					fmt.Fprintln(commandContext.output, "Aborted.")
					return nil
				}
			}

			var steps []string
			reporter := commandContext.printStep
			if commandContext.jsonOutput {
				reporter = func(message string) {
					steps = append(steps, message)
				}
			}

			result, err := service.RollbackCaddy(cmd.Context(), args[0], reporter)
			if err != nil {
				return err
			}

			if commandContext.jsonOutput {
				return writeJSON(commandContext.output, caddyRollbackJSONPayload(result, steps))
			}

			printCaddyRollbackResult(commandContext.output, result)
			return nil
		},
	}

	rollbackCommand.Flags().BoolVar(&listBackups, "list", false, "list available Caddy backups")
	rollbackCommand.Flags().BoolVar(&yes, "yes", false, "skip confirmation")

	return rollbackCommand
}

func (commandContext *commandContext) confirmCaddyGitChanges(cmd *cobra.Command, service *app.Service, yes bool) error {
	changes, err := service.CaddyGitChanges(cmd.Context())
	if err != nil {
		return err
	}
	if len(changes) == 0 || yes || commandContext.yes {
		return nil
	}
	if commandContext.jsonOutput {
		return confirmationRequiredError{message: "caddy git changes require --yes when --json is used"}
	}

	fmt.Fprintln(commandContext.output, "Caddy Git changes to deploy:")
	for _, change := range changes {
		fmt.Fprintf(commandContext.output, "  %s\n", change)
	}

	confirmed, err := commandContext.confirm("Deploy these Caddy changes?")
	if err != nil {
		return err
	}
	if !confirmed {
		return fmt.Errorf("deploy aborted")
	}

	return nil
}

func (commandContext *commandContext) loadCaddyService() (*app.Service, error) {
	loadedConfig, err := commandContext.loadConfig()
	if err != nil {
		return nil, err
	}

	return app.NewService(loadedConfig, nil), nil
}

func caddySiteNeedsPrompt(request app.CaddySiteRequest) bool {
	if request.Domain == "" || request.Visibility == "" || request.AppType == "" {
		return true
	}
	template := caddyPromptTemplate(request.AppType)
	if template.NeedsTarget && (request.UpstreamHost == "" || request.UpstreamPort == 0) {
		return true
	}
	if template.NeedsRoot && request.RootPath == "" {
		return true
	}
	return false
}

func promptForCaddySite(output io.Writer, input io.Reader, defaultDomain string, request *app.CaddySiteRequest) error {
	reader := bufio.NewReader(input)

	if request.Domain == "" {
		value, err := promptText(output, reader, "Domain", "app."+defaultDomain)
		if err != nil {
			return err
		}
		request.Domain = value
	}

	if request.Visibility == "" {
		value, err := promptChoice(output, reader, "Visibility", []promptOption{
			{Label: "internal", Description: "LAN and WireGuard only"},
			{Label: "public", Description: "internet-facing"},
		}, "internal")
		if err != nil {
			return err
		}
		request.Visibility = value
	}

	if request.AppType == "" {
		value, err := promptChoice(output, reader, "Site template", caddyTemplatePromptOptions(), "generic")
		if err != nil {
			return err
		}
		request.AppType = value
	}

	template := caddyPromptTemplate(request.AppType)
	if template.NeedsTarget {
		if request.UpstreamScheme == "" {
			value, err := promptChoice(output, reader, "Upstream scheme", []promptOption{
				{Label: "http", Description: "plain HTTP upstream"},
				{Label: "https", Description: "HTTPS upstream"},
			}, "http")
			if err != nil {
				return err
			}
			request.UpstreamScheme = value
		}

		if request.UpstreamHost == "" {
			value, err := promptText(output, reader, "Internal IP or host", "192.0.2.50")
			if err != nil {
				return err
			}
			request.UpstreamHost = value
		}

		if request.UpstreamPort == 0 {
			value, err := promptInt(output, reader, "Internal port", 3000)
			if err != nil {
				return err
			}
			request.UpstreamPort = value
		}

		if request.UpstreamScheme == "https" {
			value, err := promptBool(output, reader, "Skip upstream TLS verification", request.InsecureTLSUpstream)
			if err != nil {
				return err
			}
			request.InsecureTLSUpstream = value
		}
	}

	if template.NeedsRoot && request.RootPath == "" {
		fallback := strings.ReplaceAll(template.DefaultRoot, "{slug}", siteSlugFromDomain(request.Domain))
		value, err := promptText(output, reader, "Root path", fallback)
		if err != nil {
			return err
		}
		request.RootPath = value
	}

	if request.Visibility == app.CaddyVisibilityPublic {
		value, err := promptBool(output, reader, "Enable WAF", true)
		if err != nil {
			return err
		}
		request.UseWAF = value
		request.WAFExplicit = true
	}

	if !request.DryRun {
		value, err := promptBool(output, reader, "Deploy after writing git files", request.Deploy)
		if err != nil {
			return err
		}
		request.Deploy = value
	}

	return nil
}

func promptForCaddySiteEdit(output io.Writer, input io.Reader, request *app.CaddySiteRequest) error {
	reader := bufio.NewReader(input)

	value, err := promptChoice(output, reader, "Visibility", []promptOption{
		{Label: "internal", Description: "LAN and WireGuard only"},
		{Label: "public", Description: "internet-facing"},
	}, request.Visibility)
	if err != nil {
		return err
	}
	request.Visibility = value

	value, err = promptChoice(output, reader, "Site template", caddyTemplatePromptOptions(), request.AppType)
	if err != nil {
		return err
	}
	request.AppType = value

	template := caddyPromptTemplate(request.AppType)
	if template.NeedsTarget {
		value, err = promptChoice(output, reader, "Upstream scheme", []promptOption{
			{Label: "http", Description: "plain HTTP upstream"},
			{Label: "https", Description: "HTTPS upstream"},
		}, request.UpstreamScheme)
		if err != nil {
			return err
		}
		request.UpstreamScheme = value

		value, err = promptText(output, reader, "Internal IP or host", request.UpstreamHost)
		if err != nil {
			return err
		}
		request.UpstreamHost = value

		portValue, err := promptInt(output, reader, "Internal port", request.UpstreamPort)
		if err != nil {
			return err
		}
		request.UpstreamPort = portValue

		if request.UpstreamScheme == "https" {
			tlsValue, err := promptBool(output, reader, "Skip upstream TLS verification", request.InsecureTLSUpstream)
			if err != nil {
				return err
			}
			request.InsecureTLSUpstream = tlsValue
		}
	}

	if template.NeedsRoot {
		fallback := request.RootPath
		if fallback == "" {
			fallback = strings.ReplaceAll(template.DefaultRoot, "{slug}", siteSlugFromDomain(request.Domain))
		}
		value, err = promptText(output, reader, "Root path", fallback)
		if err != nil {
			return err
		}
		request.RootPath = value
	}

	if request.Visibility == app.CaddyVisibilityPublic {
		wafValue, err := promptBool(output, reader, "Enable WAF", request.UseWAF)
		if err != nil {
			return err
		}
		request.UseWAF = wafValue
		request.WAFExplicit = true
	} else {
		request.UseWAF = false
		request.WAFExplicit = true
	}

	if !request.DryRun {
		deployValue, err := promptBool(output, reader, "Deploy after writing git files", request.Deploy)
		if err != nil {
			return err
		}
		request.Deploy = deployValue
	}

	return nil
}

type promptOption struct {
	Label       string
	Description string
}

func promptText(output io.Writer, reader *bufio.Reader, label string, fallback string) (string, error) {
	fmt.Fprintf(output, "%s [%s]: ", label, fallback)
	value, err := reader.ReadString('\n')
	if err != nil {
		return "", err
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback, nil
	}
	return value, nil
}

func promptInt(output io.Writer, reader *bufio.Reader, label string, fallback int) (int, error) {
	for {
		value, err := promptText(output, reader, label, strconv.Itoa(fallback))
		if err != nil {
			return 0, err
		}
		parsedValue, err := strconv.Atoi(value)
		if err == nil && parsedValue > 0 {
			return parsedValue, nil
		}
		fmt.Fprintln(output, "Enter a positive number.")
	}
}

func promptBool(output io.Writer, reader *bufio.Reader, label string, fallback bool) (bool, error) {
	defaultValue := "n"
	if fallback {
		defaultValue = "y"
	}

	for {
		value, err := promptText(output, reader, label+" [y/n]", defaultValue)
		if err != nil {
			return false, err
		}
		switch strings.ToLower(value) {
		case "y", "yes":
			return true, nil
		case "n", "no":
			return false, nil
		default:
			fmt.Fprintln(output, "Enter y or n.")
		}
	}
}

func promptChoice(output io.Writer, reader *bufio.Reader, label string, options []promptOption, fallback string) (string, error) {
	fmt.Fprintf(output, "%s:\n", label)
	for optionIndex, option := range options {
		fmt.Fprintf(output, "  %d. %s - %s\n", optionIndex+1, option.Label, option.Description)
	}

	for {
		value, err := promptText(output, reader, "Choose", fallback)
		if err != nil {
			return "", err
		}
		value = strings.ToLower(strings.TrimSpace(value))
		for optionIndex, option := range options {
			if value == option.Label || value == strconv.Itoa(optionIndex+1) {
				return option.Label, nil
			}
		}
		fmt.Fprintln(output, "Choose one of the listed options.")
	}
}

func caddyTemplatePromptOptions() []promptOption {
	templates := app.CaddyTemplates()
	options := make([]promptOption, 0, len(templates))
	for _, template := range templates {
		options = append(options, promptOption{Label: template.Name, Description: template.Description})
	}
	return options
}

func caddyPromptTemplate(name string) app.CaddyTemplate {
	for _, template := range app.CaddyTemplates() {
		if template.Name == name {
			return template
		}
	}
	return app.CaddyTemplates()[0]
}

func siteSlugFromDomain(domain string) string {
	domain = strings.Trim(strings.ToLower(strings.TrimSpace(domain)), ".")
	parts := strings.Split(domain, ".")
	if len(parts) > 2 {
		parts = parts[:len(parts)-2]
	}
	slug := strings.Join(parts, "-")
	slug = strings.Trim(slug, "-")
	if slug == "" {
		return "site"
	}
	return slug
}

func printCaddySiteResult(output io.Writer, result app.CaddySiteResult, dryRun bool) {
	if dryRun {
		fmt.Fprintln(output, "Caddy site preview")
	} else {
		fmt.Fprintln(output, "Caddy site ready")
	}
	fmt.Fprintf(output, "Domain:     %s\n", result.Domain)
	fmt.Fprintf(output, "Visibility: %s\n", result.Visibility)
	fmt.Fprintf(output, "Type:       %s\n", result.AppType)
	if result.Upstream != "" {
		fmt.Fprintf(output, "Upstream:   %s\n", result.Upstream)
	}
	if result.RootPath != "" {
		fmt.Fprintf(output, "Root:       %s\n", result.RootPath)
	}
	fmt.Fprintf(output, "Site file:  %s\n", result.SitePath)
	if result.RulePath != "" {
		fmt.Fprintf(output, "WAF rules:  %s\n", result.RulePath)
	}
	if result.Deployed {
		fmt.Fprintf(output, "Deploy:     %s\n", result.DeploySummary)
		fmt.Fprintf(output, "Backup:     %s\n", result.DeployBackup)
	}
}

func printCaddyRemoveResult(output io.Writer, result app.CaddyRemoveResult, dryRun bool) {
	if dryRun {
		fmt.Fprintln(output, "Caddy remove preview")
	} else {
		fmt.Fprintln(output, "Caddy site removed")
	}
	fmt.Fprintf(output, "Domain:    %s\n", result.Domain)
	fmt.Fprintf(output, "Site file: %s\n", result.SitePath)
	if result.RulePath != "" {
		fmt.Fprintf(output, "WAF rules: %s\n", result.RulePath)
	}
	if result.Deployed {
		fmt.Fprintf(output, "Deploy:    %s\n", result.DeploySummary)
		fmt.Fprintf(output, "Backup:    %s\n", result.DeployBackup)
	}
}

func printCaddySites(output io.Writer, sites []app.CaddySiteSummary, managedOnly bool) {
	fmt.Fprintln(output, "DOMAIN\tVISIBILITY\tTEMPLATE\tUPSTREAM/ROOT\tWAF\tOWNER")
	for _, site := range sites {
		if managedOnly && !site.Managed {
			continue
		}
		owner := "discovered"
		if site.Managed {
			owner = "boringctl"
		}
		waf := "no"
		if site.UseWAF {
			waf = "yes"
		}
		upstream := site.Upstream
		if upstream == "" && site.RootPath != "" {
			upstream = site.RootPath
		}
		if upstream == "" {
			upstream = "-"
		}
		fmt.Fprintf(output, "%s\t%s\t%s\t%s\t%s\t%s\n", site.Domain, site.Visibility, site.AppType, upstream, waf, owner)
	}
}

func printCaddyTemplates(output io.Writer, templates []app.CaddyTemplate) {
	fmt.Fprintln(output, "TEMPLATE\tTARGET\tROOT\tDESCRIPTION")
	for _, template := range templates {
		target := "no"
		if template.NeedsTarget {
			target = "yes"
		}
		root := "-"
		if template.NeedsRoot {
			root = template.DefaultRoot
		}
		fmt.Fprintf(output, "%s\t%s\t%s\t%s\n", template.Name, target, root, template.Description)
	}
}

func printCaddyDeployResult(output io.Writer, result app.CaddyDeployResult) {
	fmt.Fprintln(output, "Caddy deploy")
	fmt.Fprintf(output, "Validated: %t\n", result.Validated)
	fmt.Fprintf(output, "Applied:   %t\n", result.Applied)
	if result.Backup != "" {
		fmt.Fprintf(output, "Backup:    %s\n", result.Backup)
	}
	if result.RolledBack || result.RollbackSummary != "" {
		fmt.Fprintf(output, "Rolled back: %t\n", result.RolledBack)
		fmt.Fprintf(output, "Rollback:    %s\n", result.RollbackSummary)
	}
	fmt.Fprintf(output, "Summary:   %s\n", result.Summary)
}

func printCaddySmokeResults(output io.Writer, results []app.CaddySmokeResult) {
	if len(results) == 0 {
		fmt.Fprintln(output, "Smoke:     no managed routes to check")
		return
	}

	fmt.Fprintln(output)
	fmt.Fprintln(output, "Smoke checks")
	fmt.Fprintln(output, "DOMAIN\tSTATUS\tEXPECTED\tRESULT")
	for _, result := range results {
		status := "-"
		if result.StatusCode != 0 {
			status = strconv.Itoa(result.StatusCode)
		}
		state := "pass"
		if !result.Passed {
			state = "fail"
			if result.Error != "" {
				state = "fail: " + result.Error
			}
		}
		fmt.Fprintf(output, "%s\t%s\t%s\t%s\n", result.Domain, status, result.Expected, state)
	}
}

func printCaddyBackups(output io.Writer, backups []string) {
	if len(backups) == 0 {
		fmt.Fprintln(output, "No Caddy backups found.")
		return
	}

	fmt.Fprintln(output, "BACKUP")
	for _, backup := range backups {
		fmt.Fprintln(output, backup)
	}
}

func printCaddyRollbackResult(output io.Writer, result app.CaddyRollbackResult) {
	fmt.Fprintln(output, "Caddy rollback")
	fmt.Fprintf(output, "Backup:  %s\n", result.Backup)
	fmt.Fprintf(output, "Applied: %t\n", result.Applied)
	fmt.Fprintf(output, "Summary: %s\n", result.Summary)
}

func caddySiteResultJSONPayload(operation string, result app.CaddySiteResult, dryRun bool, steps []string) caddySiteResultPayload {
	return caddySiteResultPayload{
		SchemaVersion: agentOutputSchemaVersion,
		Operation:     operation,
		Steps:         steps,
		DryRun:        dryRun,
		Domain:        result.Domain,
		Slug:          result.Slug,
		SitePath:      result.SitePath,
		RulePath:      result.RulePath,
		Upstream:      result.Upstream,
		RootPath:      result.RootPath,
		Visibility:    result.Visibility,
		AppType:       result.AppType,
		Deployed:      result.Deployed,
		DeploySummary: result.DeploySummary,
		DeployBackup:  result.DeployBackup,
	}
}

func caddyRemoveResultJSONPayload(result app.CaddyRemoveResult, dryRun bool, steps []string) caddyRemoveResultPayload {
	return caddyRemoveResultPayload{
		SchemaVersion: agentOutputSchemaVersion,
		Operation:     "caddy_remove_site",
		Steps:         steps,
		DryRun:        dryRun,
		Domain:        result.Domain,
		SitePath:      result.SitePath,
		RulePath:      result.RulePath,
		Removed:       result.Removed,
		Deployed:      result.Deployed,
		DeploySummary: result.DeploySummary,
		DeployBackup:  result.DeployBackup,
	}
}

func caddySitesJSONPayload(sites []app.CaddySiteSummary, managedOnly bool) caddySitesPayload {
	payloads := make([]caddySitePayload, 0, len(sites))
	for _, site := range sites {
		if managedOnly && !site.Managed {
			continue
		}
		payloads = append(payloads, caddySitePayload{
			Domain:     site.Domain,
			Slug:       site.Slug,
			Visibility: site.Visibility,
			AppType:    site.AppType,
			Upstream:   site.Upstream,
			RootPath:   site.RootPath,
			UseWAF:     site.UseWAF,
			Managed:    site.Managed,
			SitePath:   site.SitePath,
			RulePath:   site.RulePath,
		})
	}

	return caddySitesPayload{
		SchemaVersion: agentOutputSchemaVersion,
		ManagedOnly:   managedOnly,
		Sites:         payloads,
	}
}

func caddyTemplatesJSONPayload(templates []app.CaddyTemplate) caddyTemplatesPayload {
	payloads := make([]caddyTemplatePayload, 0, len(templates))
	for _, template := range templates {
		payloads = append(payloads, caddyTemplatePayload{
			Name:        template.Name,
			Label:       template.Label,
			Description: template.Description,
			NeedsTarget: template.NeedsTarget,
			NeedsRoot:   template.NeedsRoot,
			DefaultRoot: template.DefaultRoot,
			DefaultWAF:  template.DefaultWAF,
		})
	}

	return caddyTemplatesPayload{
		SchemaVersion: agentOutputSchemaVersion,
		Templates:     payloads,
	}
}

func caddyDeployJSONPayload(operation string, result app.CaddyDeployResult, steps []string, smokeResults []app.CaddySmokeResult) caddyDeployPayload {
	return caddyDeployPayload{
		SchemaVersion:   agentOutputSchemaVersion,
		Operation:       operation,
		Steps:           steps,
		Validated:       result.Validated,
		Applied:         result.Applied,
		Backup:          result.Backup,
		RolledBack:      result.RolledBack,
		RollbackSummary: result.RollbackSummary,
		Summary:         result.Summary,
		Smoke:           caddySmokeJSONPayloads(smokeResults),
	}
}

func caddySmokeJSONPayloads(results []app.CaddySmokeResult) []caddySmokeResultPayload {
	payloads := make([]caddySmokeResultPayload, 0, len(results))
	for _, result := range results {
		payloads = append(payloads, caddySmokeResultPayload{
			Domain:     result.Domain,
			URL:        result.URL,
			StatusCode: result.StatusCode,
			Expected:   result.Expected,
			Passed:     result.Passed,
			Error:      result.Error,
		})
	}
	return payloads
}

func caddyRollbackJSONPayload(result app.CaddyRollbackResult, steps []string) caddyRollbackPayload {
	return caddyRollbackPayload{
		SchemaVersion: agentOutputSchemaVersion,
		Operation:     "caddy_rollback",
		Steps:         steps,
		Backup:        result.Backup,
		Applied:       result.Applied,
		Summary:       result.Summary,
	}
}
