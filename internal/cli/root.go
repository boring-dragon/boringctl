package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/boring-labs/boringctl/internal/app"
	"github.com/boring-labs/boringctl/internal/config"
	"github.com/boring-labs/boringctl/internal/proxmox"
	"github.com/boring-labs/boringctl/internal/tui"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

const agentOutputSchemaVersion = 1

type commandContext struct {
	configPath string
	profile    string
	outputMode string
	jsonOutput bool
	yes        bool
	output     io.Writer
}

type errorPayload struct {
	SchemaVersion int              `json:"schema_version"`
	Error         errorJSONPayload `json:"error"`
}

type errorJSONPayload struct {
	Kind            string `json:"kind"`
	Message         string `json:"message"`
	Backup          string `json:"backup,omitempty"`
	RolledBack      *bool  `json:"rolled_back,omitempty"`
	RollbackSummary string `json:"rollback_summary,omitempty"`
}

type formattedError string

func (err formattedError) Error() string {
	return string(err)
}

type confirmationRequiredError struct {
	message string
}

func (err confirmationRequiredError) Error() string {
	return err.message
}

type nodesPayload struct {
	SchemaVersion int           `json:"schema_version"`
	Nodes         []nodePayload `json:"nodes"`
}

type nodePayload struct {
	Name     string   `json:"name"`
	Label    string   `json:"label"`
	Storages []string `json:"storages"`
	SSHHost  string   `json:"ssh_host,omitempty"`
}

type imagesPayload struct {
	SchemaVersion int            `json:"schema_version"`
	Nodes         []string       `json:"nodes"`
	Images        []imagePayload `json:"images"`
}

type imagePayload struct {
	Name        string         `json:"name"`
	Label       string         `json:"label"`
	Family      string         `json:"family"`
	DefaultUser string         `json:"default_user"`
	Recommended bool           `json:"recommended"`
	Templates   map[string]int `json:"templates"`
}

type lxcImagesPayload struct {
	SchemaVersion int               `json:"schema_version"`
	Nodes         []string          `json:"nodes"`
	Images        []lxcImagePayload `json:"images"`
}

type lxcImagePayload struct {
	Name        string            `json:"name"`
	Label       string            `json:"label"`
	Family      string            `json:"family"`
	OSType      string            `json:"ostype"`
	DefaultUser string            `json:"default_user"`
	Recommended bool              `json:"recommended"`
	Templates   map[string]string `json:"templates"`
}

type plansPayload struct {
	SchemaVersion int           `json:"schema_version"`
	Plans         []planPayload `json:"plans"`
}

type planPayload struct {
	Name     string `json:"name"`
	Label    string `json:"label"`
	Cores    int    `json:"cores"`
	MemoryMB int    `json:"memory_mb"`
	DiskGB   int    `json:"disk_gb"`
	Source   string `json:"source,omitempty"`
}

type createPreviewPayload struct {
	SchemaVersion int              `json:"schema_version"`
	Operation     string           `json:"operation"`
	Node          string           `json:"node"`
	Name          string           `json:"name"`
	TemplateID    int              `json:"template_id"`
	Storage       string           `json:"storage"`
	Steps         []proxmoxRequest `json:"requests"`
}

type lxcCreatePreviewPayload struct {
	SchemaVersion int               `json:"schema_version"`
	Operation     string            `json:"operation"`
	Node          string            `json:"node"`
	Name          string            `json:"name"`
	Template      string            `json:"template"`
	Storage       string            `json:"storage"`
	Method        string            `json:"method"`
	Path          string            `json:"path"`
	Params        map[string]string `json:"params"`
}

type proxmoxRequest struct {
	Method string            `json:"method"`
	Path   string            `json:"path"`
	Params map[string]string `json:"params,omitempty"`
}

type createResultPayload struct {
	SchemaVersion int      `json:"schema_version"`
	Operation     string   `json:"operation"`
	Steps         []string `json:"steps"`
	VMID          int      `json:"vmid"`
	Name          string   `json:"name"`
	Node          string   `json:"node"`
	Image         string   `json:"image"`
	ImageLabel    string   `json:"image_label"`
	Plan          string   `json:"plan,omitempty"`
	TemplateID    int      `json:"template_id"`
	Storage       string   `json:"storage"`
	Cores         int      `json:"cores"`
	MemoryMB      int      `json:"memory_mb"`
	DiskGB        int      `json:"disk_gb"`
	User          string   `json:"user"`
	IP            string   `json:"ip,omitempty"`
	SSHCommand    string   `json:"ssh_command,omitempty"`
	SSHKeygen     string   `json:"ssh_keygen,omitempty"`
	NetworkMode   string   `json:"network_mode"`
	StaticIP      bool     `json:"static_ip"`
	Warning       string   `json:"warning,omitempty"`
}

type lxcCreateResultPayload struct {
	SchemaVersion int      `json:"schema_version"`
	Operation     string   `json:"operation"`
	Steps         []string `json:"steps"`
	VMID          int      `json:"vmid"`
	Name          string   `json:"name"`
	Node          string   `json:"node"`
	Image         string   `json:"image"`
	ImageLabel    string   `json:"image_label"`
	Plan          string   `json:"plan,omitempty"`
	Template      string   `json:"template"`
	Storage       string   `json:"storage"`
	Cores         int      `json:"cores"`
	MemoryMB      int      `json:"memory_mb"`
	DiskGB        int      `json:"disk_gb"`
	SwapMB        int      `json:"swap_mb"`
	User          string   `json:"user"`
	IP            string   `json:"ip,omitempty"`
	SSHCommand    string   `json:"ssh_command,omitempty"`
	SSHKeygen     string   `json:"ssh_keygen,omitempty"`
	NetworkMode   string   `json:"network_mode"`
	Tags          []string `json:"tags,omitempty"`
	Features      []string `json:"features,omitempty"`
	Started       bool     `json:"started"`
	Warning       string   `json:"warning,omitempty"`
}

type guestListPayload struct {
	SchemaVersion int            `json:"schema_version"`
	TagFilter     string         `json:"tag_filter,omitempty"`
	NodeFilter    string         `json:"node_filter,omitempty"`
	StatusFilter  string         `json:"status_filter,omitempty"`
	KindFilter    string         `json:"kind_filter,omitempty"`
	NameFilter    string         `json:"name_filter,omitempty"`
	Limit         int            `json:"limit,omitempty"`
	Total         int            `json:"total"`
	VMs           []guestPayload `json:"vms"`
	Containers    []guestPayload `json:"containers"`
}

type guestPayload struct {
	VMID      int      `json:"vmid"`
	Name      string   `json:"name"`
	Node      string   `json:"node"`
	Type      string   `json:"type"`
	Kind      string   `json:"kind"`
	Status    string   `json:"status"`
	CPU       float64  `json:"cpu"`
	MemoryMax int64    `json:"memory_max_bytes"`
	Memory    int64    `json:"memory_bytes"`
	DiskMax   int64    `json:"disk_max_bytes"`
	Disk      int64    `json:"disk_bytes"`
	Uptime    int64    `json:"uptime_seconds"`
	Template  bool     `json:"template"`
	Tags      []string `json:"tags"`
}

type guestDetailPayload struct {
	SchemaVersion int            `json:"schema_version"`
	Guest         guestPayload   `json:"guest"`
	IPAddresses   []string       `json:"ip_addresses"`
	SnapshotCount int            `json:"snapshot_count"`
	ShellCommand  string         `json:"shell_command,omitempty"`
	Config        map[string]any `json:"config"`
}

type historyPayload struct {
	SchemaVersion int                      `json:"schema_version"`
	Limit         int                      `json:"limit"`
	Entries       []app.CreateHistoryEntry `json:"entries"`
}

type commandPayload struct {
	SchemaVersion int      `json:"schema_version"`
	Operation     string   `json:"operation"`
	VMID          int      `json:"vmid,omitempty"`
	Name          string   `json:"name,omitempty"`
	Kind          string   `json:"kind,omitempty"`
	Node          string   `json:"node,omitempty"`
	GuestType     string   `json:"guest_type,omitempty"`
	SSHHost       string   `json:"ssh_host,omitempty"`
	IPAddress     string   `json:"ip_address,omitempty"`
	User          string   `json:"user,omitempty"`
	Command       string   `json:"command"`
	Args          []string `json:"args,omitempty"`
}

type sshConfigPayload struct {
	SchemaVersion int    `json:"schema_version"`
	Operation     string `json:"operation"`
	Alias         string `json:"alias"`
	IPAddress     string `json:"ip_address"`
	User          string `json:"user"`
	Command       string `json:"command"`
	ConfigPath    string `json:"config_path"`
	Added         bool   `json:"added"`
	AlreadyExists bool   `json:"already_exists"`
	PrintOnly     bool   `json:"print_only"`
}

type tagsPayload struct {
	SchemaVersion int      `json:"schema_version"`
	Operation     string   `json:"operation"`
	VMID          int      `json:"vmid"`
	Tags          []string `json:"tags"`
}

type snapshotsPayload struct {
	SchemaVersion int               `json:"schema_version"`
	VMID          int               `json:"vmid"`
	GuestKind     string            `json:"guest_kind"`
	Snapshots     []snapshotPayload `json:"snapshots"`
}

type snapshotPayload struct {
	Name         string `json:"name"`
	Description  string `json:"description"`
	SnapTime     int64  `json:"snap_time"`
	SnapTimeText string `json:"snap_time_text"`
}

type actionPayload struct {
	SchemaVersion int      `json:"schema_version"`
	Operation     string   `json:"operation"`
	Status        string   `json:"status"`
	VMID          int      `json:"vmid,omitempty"`
	GuestKind     string   `json:"guest_kind,omitempty"`
	Name          string   `json:"name,omitempty"`
	Node          string   `json:"node,omitempty"`
	Steps         []string `json:"steps,omitempty"`
}

func Execute() error {
	commandContext := &commandContext{
		output: os.Stdout,
	}

	rootCommand := &cobra.Command{
		Use:   "boringctl",
		Short: "Homelab cloud control for Proxmox VMs and LXC containers",
		Long: `boringctl manages this homelab through the Proxmox API and a Git-managed Caddy config.

Agent notes:
  - Use --output json for stable machine-readable responses.
  - Use schema or schema <command> to discover commands and flags.
  - Use create-lxc --dry-run before creating containers; it prints the exact Proxmox request.
  - Use task list/log/wait after commands return a Proxmox UPID.
  - Use caddy add-site --dry-run before deploying reverse-proxy changes.`,
		Example: `  boringctl --output json schema create-lxc
  boringctl --output json config check
  boringctl create-lxc --node pve1 --image ubuntu-24.04 --plan medium --storage local-lvm --name app-01 --docker --dry-run
  boringctl task list --limit 10
  boringctl caddy add-site --domain app.example.com --target 192.0.2.50:3000 --visibility internal --type generic --dry-run`,
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			return commandContext.configureOutput()
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if !canRunInteractiveTUI() {
				return cmd.Help()
			}
			return commandContext.runTUI()
		},
	}
	rootCommand.PersistentFlags().StringVar(&commandContext.configPath, "config", "", "config file path")
	rootCommand.PersistentFlags().StringVar(&commandContext.profile, "profile", "", "config profile name from ~/.config/boringctl/<profile>.yaml")
	rootCommand.PersistentFlags().StringVarP(&commandContext.outputMode, "output", "o", "auto", "output format: auto, text, or json")
	rootCommand.PersistentFlags().BoolVar(&commandContext.jsonOutput, "json", false, "print machine-readable JSON for supported commands")
	rootCommand.PersistentFlags().BoolVarP(&commandContext.yes, "yes", "y", false, "skip confirmation prompts for destructive operations")

	rootCommand.AddCommand(
		commandContext.versionCommand(),
		commandContext.doctorCommand(),
		commandContext.nodesCommand(),
		commandContext.imagesCommand(),
		commandContext.lxcImagesCommand(),
		commandContext.plansCommand(),
		commandContext.configCommand(),
		commandContext.initConfigCommand(),
		commandContext.tuiCommand(),
		commandContext.createCommand(),
		commandContext.createContainerCommand(),
		commandContext.caddyCommand(),
		commandContext.apiCommand(),
		commandContext.taskCommand(),
		commandContext.storageCommand(),
		commandContext.backupCommand(),
		commandContext.exportCommand(),
		commandContext.applyCommand(),
		commandContext.schemaCommand(rootCommand),
		commandContext.historyCommand(),
		commandContext.journalCommand(),
		commandContext.listCommand(),
		commandContext.showCommand(),
		commandContext.sshConfigCommand(),
		commandContext.lifecycleCommand("start"),
		commandContext.lifecycleCommand("stop"),
		commandContext.lifecycleCommand("reboot"),
		commandContext.lifecycleCommand("delete"),
		commandContext.sshCommand(),
		commandContext.shellCommand(),
		commandContext.resizeCommand(),
		commandContext.renameCommand(),
		commandContext.tagsCommand(),
		commandContext.snapshotCommand(),
	)

	if err := rootCommand.Execute(); err != nil {
		if commandContext.jsonOutput {
			errorDetails := errorJSONPayload{
				Kind:    errorKind(err),
				Message: err.Error(),
			}
			var deployErr caddyDeployCommandError
			if errors.As(err, &deployErr) {
				errorDetails.Backup = deployErr.result.Backup
				errorDetails.RolledBack = &deployErr.result.RolledBack
				errorDetails.RollbackSummary = deployErr.result.RollbackSummary
			}
			_ = writeJSON(os.Stderr, errorPayload{
				SchemaVersion: agentOutputSchemaVersion,
				Error:         errorDetails,
			})
			return formattedError("")
		}
		return err
	}

	return nil
}

func (commandContext *commandContext) configureOutput() error {
	switch commandContext.outputMode {
	case "", "auto":
		if !stdoutIsTerminal() {
			commandContext.jsonOutput = true
		}
	case "text":
		commandContext.jsonOutput = false
	case "json":
		commandContext.jsonOutput = true
	default:
		return fmt.Errorf("output must be auto, text, or json")
	}

	return nil
}

func stdoutIsTerminal() bool {
	stat, err := os.Stdout.Stat()
	if err != nil {
		return true
	}

	return (stat.Mode() & os.ModeCharDevice) != 0
}

func stdinIsTerminal() bool {
	stat, err := os.Stdin.Stat()
	if err != nil {
		return true
	}

	return (stat.Mode() & os.ModeCharDevice) != 0
}

func canRunInteractiveTUI() bool {
	return stdinIsTerminal() && stdoutIsTerminal()
}

func (commandContext *commandContext) initConfigCommand() *cobra.Command {
	var outputPath string
	var force bool

	initConfigCommand := &cobra.Command{
		Use:   "init-config",
		Short: "Discover Proxmox inventory and print a starter config",
		RunE: func(cmd *cobra.Command, args []string) error {
			service, err := commandContext.loadService()
			if err != nil {
				return err
			}

			discoveredConfig, err := service.DiscoverConfig(cmd.Context())
			if err != nil {
				return err
			}

			yamlBytes, err := yaml.Marshal(discoveredConfig)
			if err != nil {
				return err
			}

			if outputPath == "" {
				_, err = commandContext.output.Write(yamlBytes)
				return err
			}

			if !force {
				if _, err := os.Stat(outputPath); err == nil {
					return fmt.Errorf("%s already exists; pass --force to overwrite", outputPath)
				} else if !os.IsNotExist(err) {
					return err
				}
			}

			if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
				return err
			}

			if err := os.WriteFile(outputPath, yamlBytes, 0o644); err != nil {
				return err
			}

			fmt.Fprintf(commandContext.output, "Wrote discovered config to %s\n", outputPath)
			return nil
		},
	}

	initConfigCommand.Flags().StringVar(&outputPath, "output", "", "write discovered config to this path")
	initConfigCommand.Flags().BoolVar(&force, "force", false, "overwrite output path if it exists")

	return initConfigCommand
}

func (commandContext *commandContext) loadConfig() (*config.Config, error) {
	loadedConfig, _, err := config.LoadProfile(commandContext.configPath, commandContext.profile)
	if err != nil {
		return nil, err
	}

	return loadedConfig, nil
}

func (commandContext *commandContext) loadService() (*app.Service, error) {
	loadedConfig, err := commandContext.loadConfig()
	if err != nil {
		return nil, err
	}

	client, err := app.NewClientFromConfig(loadedConfig)
	if err != nil {
		return nil, err
	}

	return app.NewService(loadedConfig, client), nil
}

func (commandContext *commandContext) loadHealthAwareService() (*app.Service, error) {
	loadedConfig, err := commandContext.loadConfig()
	if err != nil {
		return nil, err
	}

	client, err := app.NewHealthAwareClientFromConfig(loadedConfig)
	if err != nil {
		return nil, err
	}

	return app.NewService(loadedConfig, client), nil
}

func (commandContext *commandContext) nodesCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "nodes",
		Short: "List configured Proxmox nodes",
		RunE: func(cmd *cobra.Command, args []string) error {
			loadedConfig, err := commandContext.loadConfig()
			if err != nil {
				return err
			}

			if commandContext.jsonOutput {
				return writeJSON(commandContext.output, nodesJSONPayload(loadedConfig))
			}

			fmt.Fprintln(commandContext.output, "NODE\tLABEL\tSTORAGES")
			for _, nodeName := range loadedConfig.NodeNames() {
				node := loadedConfig.Nodes[nodeName]
				fmt.Fprintf(commandContext.output, "%s\t%s\t%s\n", nodeName, node.Label, strings.Join(node.Storages, ", "))
			}

			return nil
		},
	}
}

func (commandContext *commandContext) imagesCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "images",
		Short: "List configured VM images",
		RunE: func(cmd *cobra.Command, args []string) error {
			loadedConfig, err := commandContext.loadConfig()
			if err != nil {
				return err
			}

			nodeNames := loadedConfig.NodeNames()
			if commandContext.jsonOutput {
				return writeJSON(commandContext.output, imagesJSONPayload(loadedConfig, nodeNames))
			}

			fmt.Fprintf(commandContext.output, "IMAGE\tUSER")
			for _, nodeName := range nodeNames {
				fmt.Fprintf(commandContext.output, "\t%s", strings.ToUpper(nodeName))
			}
			fmt.Fprintln(commandContext.output)

			for _, imageName := range loadedConfig.ImageNames() {
				image := loadedConfig.Images[imageName]
				fmt.Fprintf(commandContext.output, "%s\t%s", imageName, image.DefaultUser)
				for _, nodeName := range nodeNames {
					fmt.Fprintf(commandContext.output, "\t%d", image.Templates[nodeName])
				}
				fmt.Fprintln(commandContext.output)
			}

			return nil
		},
	}
}

func (commandContext *commandContext) lxcImagesCommand() *cobra.Command {
	lxcImagesCommand := &cobra.Command{
		Use:   "lxc-images",
		Short: "List configured LXC images",
		RunE: func(cmd *cobra.Command, args []string) error {
			loadedConfig, err := commandContext.loadConfig()
			if err != nil {
				return err
			}

			nodeNames := loadedConfig.NodeNames()
			if commandContext.jsonOutput {
				return writeJSON(commandContext.output, lxcImagesJSONPayload(loadedConfig, nodeNames))
			}

			fmt.Fprintf(commandContext.output, "IMAGE\tUSER\tOSTYPE")
			for _, nodeName := range nodeNames {
				fmt.Fprintf(commandContext.output, "\t%s", strings.ToUpper(nodeName))
			}
			fmt.Fprintln(commandContext.output)

			for _, imageName := range loadedConfig.LXCImageNames() {
				image := loadedConfig.LXCImages[imageName]
				fmt.Fprintf(commandContext.output, "%s\t%s\t%s", imageName, image.DefaultUser, image.OSType)
				for _, nodeName := range nodeNames {
					fmt.Fprintf(commandContext.output, "\t%s", image.Templates[nodeName])
				}
				fmt.Fprintln(commandContext.output)
			}

			return nil
		},
	}

	return lxcImagesCommand
}

func (commandContext *commandContext) plansCommand() *cobra.Command {
	var nodeName string
	var storageName string

	plansCommand := &cobra.Command{
		Use:   "plans",
		Short: "List configured VM sizes",
		RunE: func(cmd *cobra.Command, args []string) error {
			if nodeName != "" || storageName != "" {
				if nodeName == "" || storageName == "" {
					return errors.New("--node and --storage must be used together")
				}

				service, err := commandContext.loadHealthAwareService()
				if err != nil {
					return err
				}

				health := service.Health(cmd.Context())
				if !health.Connected && health.Error != "" {
					if !commandContext.jsonOutput {
						fmt.Fprintf(commandContext.output, "warning: %s; showing config fallback\n\n", health.Error)
					}
				}

				if commandContext.jsonOutput {
					return writeJSON(commandContext.output, suggestedPlansJSONPayload(service.SuggestedPlans(health, nodeName, storageName)))
				}

				fmt.Fprintln(commandContext.output, "PLAN\tCPU\tRAM\tDISK\tSOURCE")
				for _, plan := range service.SuggestedPlans(health, nodeName, storageName) {
					fmt.Fprintf(commandContext.output, "%s\t%d\t%s\t%d GB\t%s\n", plan.Name, plan.Cores, formatMemory(plan.MemoryMB), plan.DiskGB, plan.Reason)
				}

				return nil
			}

			loadedConfig, err := commandContext.loadConfig()
			if err != nil {
				return err
			}

			if commandContext.jsonOutput {
				return writeJSON(commandContext.output, plansJSONPayload(loadedConfig))
			}

			fmt.Fprintln(commandContext.output, "PLAN\tCPU\tRAM\tDISK")
			for _, planName := range loadedConfig.PlanNames() {
				plan := loadedConfig.Plans[planName]
				fmt.Fprintf(commandContext.output, "%s\t%d\t%s\t%d GB\n", planName, plan.Cores, formatMemory(plan.MemoryMB), plan.DiskGB)
			}

			return nil
		},
	}

	plansCommand.Flags().StringVar(&nodeName, "node", "", "target node for Proxmox-aware suggestions")
	plansCommand.Flags().StringVar(&storageName, "storage", "", "target storage for Proxmox-aware suggestions")

	return plansCommand
}

func (commandContext *commandContext) tuiCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "tui",
		Short: "Open the interactive homelab TUI",
		RunE: func(cmd *cobra.Command, args []string) error {
			return commandContext.runTUI()
		},
	}
}

func (commandContext *commandContext) runTUI() error {
	service, err := commandContext.loadHealthAwareService()
	if err != nil {
		return err
	}

	return tui.Run(service)
}

func (commandContext *commandContext) createCommand() *cobra.Command {
	request := app.CreateRequest{}
	var dryRun bool

	createCommand := &cobra.Command{
		Use:   "create",
		Short: "Create a VM from a configured template",
		RunE: func(cmd *cobra.Command, args []string) error {
			if dryRun {
				loadedConfig, err := commandContext.loadConfig()
				if err != nil {
					return err
				}
				service := app.NewService(loadedConfig, nil)
				preview, err := service.CreatePreview(request)
				if err != nil {
					return err
				}
				if commandContext.jsonOutput {
					return writeJSON(commandContext.output, createPreviewJSONPayload(preview))
				}
				printCreatePreview(commandContext.output, preview)
				return nil
			}

			service, err := commandContext.loadService()
			if err != nil {
				return err
			}

			if commandContext.jsonOutput {
				var steps []string
				result, err := service.CreateVM(cmd.Context(), request, func(message string) {
					steps = append(steps, message)
				})
				if err != nil {
					return err
				}
				return writeJSON(commandContext.output, createResultJSONPayload(result, steps))
			}

			result, err := service.CreateVM(cmd.Context(), request, commandContext.printStep)
			if err != nil {
				return commandContext.handleCreateError(cmd.Context(), service, err)
			}

			printCreateResult(commandContext.output, result)
			return nil
		},
	}

	createCommand.Flags().StringVar(&request.Node, "node", "", "target node")
	createCommand.Flags().StringVar(&request.Image, "image", "", "image name")
	createCommand.Flags().StringVar(&request.Plan, "plan", "", "plan name")
	createCommand.Flags().StringVar(&request.Name, "name", "", "VM name")
	createCommand.Flags().StringVar(&request.Storage, "storage", "", "target storage")
	createCommand.Flags().StringArrayVar(&request.SSHKeys, "ssh-key", nil, "configured SSH key name; may be repeated")
	createCommand.Flags().StringArrayVar(&request.SSHPublicKeys, "ssh-public-key", nil, "literal public key to inject; may be repeated")
	createCommand.Flags().IntVar(&request.Cores, "cores", 0, "CPU cores override")
	createCommand.Flags().IntVar(&request.MemoryMB, "memory", 0, "memory override in MB")
	createCommand.Flags().IntVar(&request.DiskGB, "disk", 0, "disk override in GB")
	createCommand.Flags().StringVar(&request.NetworkMode, "network", "", "network mode: dhcp or static")
	createCommand.Flags().StringVar(&request.IPAddress, "ip", "", "static IP/CIDR")
	createCommand.Flags().StringVar(&request.Gateway, "gateway", "", "static gateway")
	createCommand.Flags().StringVar(&request.DNS, "dns", "", "DNS server")
	createCommand.Flags().BoolVar(&dryRun, "dry-run", false, "show Proxmox operations without creating a VM")

	return createCommand
}

func (commandContext *commandContext) createContainerCommand() *cobra.Command {
	request := app.CreateContainerRequest{Start: true, Unprivileged: true}
	var dryRun bool

	createCommand := &cobra.Command{
		Use:   "create-lxc",
		Short: "Create an LXC container from a configured OS template",
		Long: `Create an LXC container from a configured Proxmox template volume.

The --image value must come from boringctl lxc-images. Each configured LXC image maps every node to a Proxmox template volume such as local:vztmpl/ubuntu-24.04-standard_24.04-2_amd64.tar.zst.

For Docker inside LXC, pass --docker. It sets the token-safe Proxmox LXC feature needed by Docker-in-LXC: nesting=1. keyctl=1 is sometimes useful, but Proxmox only allows changing keyctl as plain root@pam; API tokens usually reject it. Use --keyctl or --feature keyctl=1 only when you know the current auth can set it.

Agent workflow:
  1. Run lxc-images, nodes, and plans with --output json.
  2. Run create-lxc with --dry-run --output json and inspect params. The ostemplate param must be storage:vztmpl/file, not a VMID.
  3. Run the real create-lxc command.
  4. If a Proxmox UPID appears in an error or raw API response, use task log <upid> and task wait <upid>.`,
		Example: `  boringctl create-lxc --node pve1 --image ubuntu-24.04 --plan medium --storage local-lvm --name fizzy --docker --tag fizzy,prod --dry-run
  boringctl --output json create-lxc --node pve1 --image debian-13 --cores 2 --memory 4096 --disk 25 --storage local-lvm --name worker-01 --feature nesting=1 --dry-run
  boringctl create-lxc --node pve1 --image alpine-3.22 --plan tiny --storage local-lvm --name small-tool --nest
  boringctl storage list --node pve1 --storage local --content vztmpl`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if dryRun {
				loadedConfig, err := commandContext.loadConfig()
				if err != nil {
					return err
				}
				service := app.NewService(loadedConfig, nil)
				preview, err := service.CreateContainerPreview(request)
				if err != nil {
					return err
				}
				if commandContext.jsonOutput {
					return writeJSON(commandContext.output, lxcCreatePreviewJSONPayload(preview))
				}
				printCreateContainerPreview(commandContext.output, preview)
				return nil
			}

			service, err := commandContext.loadService()
			if err != nil {
				return err
			}

			if commandContext.jsonOutput {
				var steps []string
				result, err := service.CreateContainer(cmd.Context(), request, func(message string) {
					steps = append(steps, message)
				})
				if err != nil {
					return err
				}
				return writeJSON(commandContext.output, lxcCreateResultJSONPayload(result, steps))
			}

			result, err := service.CreateContainer(cmd.Context(), request, commandContext.printStep)
			if err != nil {
				return err
			}

			printCreateContainerResult(commandContext.output, result)
			return nil
		},
	}

	createCommand.Flags().StringVar(&request.Node, "node", "", "target node")
	createCommand.Flags().StringVar(&request.Image, "image", "", "LXC image name")
	createCommand.Flags().StringVar(&request.Plan, "plan", "", "plan name")
	createCommand.Flags().StringVar(&request.Name, "name", "", "container hostname")
	createCommand.Flags().StringVar(&request.Storage, "storage", "", "target rootfs storage")
	createCommand.Flags().StringArrayVar(&request.SSHKeys, "ssh-key", nil, "configured SSH key name; may be repeated")
	createCommand.Flags().StringArrayVar(&request.SSHPublicKeys, "ssh-public-key", nil, "literal public key to inject; may be repeated")
	createCommand.Flags().IntVar(&request.Cores, "cores", 0, "CPU cores override")
	createCommand.Flags().IntVar(&request.MemoryMB, "memory", 0, "memory override in MB")
	createCommand.Flags().IntVar(&request.DiskGB, "disk", 0, "rootfs disk override in GB")
	createCommand.Flags().IntVar(&request.SwapMB, "swap", 512, "swap in MB")
	createCommand.Flags().StringVar(&request.NetworkMode, "network", "", "network mode: dhcp or static")
	createCommand.Flags().StringVar(&request.IPAddress, "ip", "", "static IP/CIDR")
	createCommand.Flags().StringVar(&request.Gateway, "gateway", "", "static gateway")
	createCommand.Flags().StringVar(&request.DNS, "dns", "", "DNS server")
	createCommand.Flags().StringArrayVar(&request.Tags, "tag", nil, "Proxmox tag; comma-separated or repeated")
	createCommand.Flags().BoolVar(&request.Start, "start", true, "start container after creation")
	createCommand.Flags().BoolVar(&request.Unprivileged, "unprivileged", true, "create an unprivileged container")
	createCommand.Flags().BoolVar(&request.Docker, "docker", false, "enable token-safe Docker-in-LXC feature: nesting=1")
	createCommand.Flags().BoolVar(&request.Nesting, "nest", false, "enable LXC nesting feature")
	createCommand.Flags().BoolVar(&request.Keyctl, "keyctl", false, "enable LXC keyctl feature; Proxmox usually requires plain root@pam, not an API token")
	createCommand.Flags().StringArrayVar(&request.Features, "feature", nil, "LXC feature such as nesting=1 or fuse=1; may be repeated or comma-separated")
	createCommand.Flags().BoolVar(&dryRun, "dry-run", false, "show Proxmox operations without creating a container")

	return createCommand
}

func (commandContext *commandContext) handleCreateError(ctx context.Context, service *app.Service, createError error) error {
	var partialError *app.PartialCreateError
	if !errors.As(createError, &partialError) {
		return createError
	}

	fmt.Fprintf(commandContext.output, "Create failed after VM %d was cloned on %s: %v\n", partialError.VMID, partialError.Node, partialError.Err)
	confirmed, err := commandContext.confirm(fmt.Sprintf("Destroy partial VM %d now?", partialError.VMID))
	if err != nil {
		return err
	}
	if !confirmed {
		return createError
	}

	if err := service.CleanupPartialVM(ctx, partialError, commandContext.printStep); err != nil {
		return err
	}

	return createError
}

func (commandContext *commandContext) listCommand() *cobra.Command {
	var tagFilter string
	var nodeFilter string
	var statusFilter string
	var kindFilter string
	var nameFilter string
	var fields string
	var limit int

	listCommand := &cobra.Command{
		Use:   "list",
		Short: "List non-template VMs and LXC containers across the cluster",
		RunE: func(cmd *cobra.Command, args []string) error {
			service, err := commandContext.loadService()
			if err != nil {
				return err
			}

			guests, err := service.ListGuests(cmd.Context())
			if err != nil {
				return err
			}

			filteredGuests, err := filterGuests(guests, guestListFilters{
				Tag:    tagFilter,
				Node:   nodeFilter,
				Status: statusFilter,
				Kind:   kindFilter,
				Name:   nameFilter,
			})
			if err != nil {
				return err
			}
			totalGuests := len(filteredGuests)
			filteredGuests = limitGuests(filteredGuests, limit)
			vms, containers := splitGuestsByKind(filteredGuests)

			if commandContext.jsonOutput {
				return writeJSON(commandContext.output, guestListJSONPayload(guestListFilters{
					Tag:    tagFilter,
					Node:   nodeFilter,
					Status: statusFilter,
					Kind:   kindFilter,
					Name:   nameFilter,
					Fields: fields,
					Limit:  limit,
				}, totalGuests, vms, containers))
			}

			printGuestTable(commandContext.output, "Virtual machines", vms)
			fmt.Fprintln(commandContext.output)
			printGuestTable(commandContext.output, "LXC containers", containers)

			return nil
		},
	}

	listCommand.Flags().StringVar(&tagFilter, "tag", "", "show only guests with this Proxmox tag")
	listCommand.Flags().StringVar(&nodeFilter, "node", "", "show only guests on this node")
	listCommand.Flags().StringVar(&statusFilter, "status", "", "show only guests with this status")
	listCommand.Flags().StringVar(&kindFilter, "kind", "", "show only qemu/vm or lxc/container guests")
	listCommand.Flags().StringVar(&nameFilter, "name", "", "show only guests whose name contains this value")
	listCommand.Flags().StringVar(&fields, "fields", "", "comma-separated JSON guest fields to include")
	listCommand.Flags().IntVar(&limit, "limit", 0, "maximum number of guests to return")

	return listCommand
}

func (commandContext *commandContext) historyCommand() *cobra.Command {
	var limit int

	historyCommand := &cobra.Command{
		Use:   "history",
		Short: "Show recent boringctl creates",
		RunE: func(cmd *cobra.Command, args []string) error {
			entries, err := app.LoadCreateHistory()
			if err != nil {
				return err
			}

			if limit <= 0 {
				limit = 20
			}
			if len(entries) > limit {
				entries = entries[len(entries)-limit:]
			}

			if commandContext.jsonOutput {
				return writeJSON(commandContext.output, historyPayload{
					SchemaVersion: agentOutputSchemaVersion,
					Limit:         limit,
					Entries:       newestHistoryEntries(entries),
				})
			}

			fmt.Fprintln(commandContext.output, "CREATED_AT\tVMID\tNAME\tNODE\tIMAGE\tPLAN\tIP\tSSH")
			for entryIndex := len(entries) - 1; entryIndex >= 0; entryIndex-- {
				entry := entries[entryIndex]
				fmt.Fprintf(
					commandContext.output,
					"%s\t%d\t%s\t%s\t%s\t%s\t%s\t%s\n",
					entry.CreatedAt.Format("2006-01-02 15:04:05"),
					entry.VMID,
					entry.Name,
					entry.Node,
					entry.Image,
					entry.Plan,
					entry.IP,
					entry.SSHCommand,
				)
			}

			return nil
		},
	}

	historyCommand.Flags().IntVar(&limit, "limit", 20, "number of recent creates to show")

	return historyCommand
}

func (commandContext *commandContext) showCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "show <vmid|name>",
		Short: "Show VM or LXC container details",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			service, err := commandContext.loadService()
			if err != nil {
				return err
			}

			guest, err := service.ResolveGuestRef(cmd.Context(), args[0])
			if err != nil {
				return err
			}

			detail, err := service.GuestDetail(cmd.Context(), guest.VMID)
			if err != nil {
				return err
			}
			vm := detail.Guest
			vmConfig := detail.Config

			if commandContext.jsonOutput {
				return writeJSON(commandContext.output, guestDetailJSONPayload(detail))
			}

			fmt.Fprintf(commandContext.output, "VMID:   %d\n", vm.VMID)
			fmt.Fprintf(commandContext.output, "Name:   %s\n", vm.Name)
			fmt.Fprintf(commandContext.output, "Type:   %s\n", guestTypeLabel(vm))
			fmt.Fprintf(commandContext.output, "Node:   %s\n", vm.Node)
			fmt.Fprintf(commandContext.output, "Status: %s\n", vm.Status)
			fmt.Fprintf(commandContext.output, "CPU:    %.2f\n", vm.CPU)
			fmt.Fprintf(commandContext.output, "RAM:    %s\n", formatBytes(vm.MaxMem))
			fmt.Fprintf(commandContext.output, "Disk:   %s\n", formatBytes(vm.MaxDisk))
			fmt.Fprintf(commandContext.output, "Uptime: %s\n", formatDuration(vm.Uptime))
			fmt.Fprintf(commandContext.output, "IPs:    %s\n", formatList(detail.IPAddresses))
			fmt.Fprintf(commandContext.output, "Tags:   %s\n", formatList(detail.Tags))
			fmt.Fprintf(commandContext.output, "Snaps:  %d\n", detail.SnapshotCount)

			keys := make([]string, 0, len(vmConfig))
			for key := range vmConfig {
				keys = append(keys, key)
			}
			sort.Strings(keys)

			fmt.Fprintln(commandContext.output)
			fmt.Fprintln(commandContext.output, "Config:")
			for _, key := range keys {
				fmt.Fprintf(commandContext.output, "%s: %v\n", key, vmConfig[key])
			}

			return nil
		},
	}
}

func (commandContext *commandContext) lifecycleCommand(action string) *cobra.Command {
	command := &cobra.Command{
		Use:   action + " <vmid>",
		Short: titleAction(action) + " a VM or LXC container",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			vmid, err := parseVMID(args[0])
			if err != nil {
				return err
			}

			service, err := commandContext.loadService()
			if err != nil {
				return err
			}

			guest, err := service.FindGuest(cmd.Context(), vmid)
			if err != nil {
				return err
			}

			if action == "delete" {
				confirmed, err := commandContext.confirmGuestDelete(cmd.Context(), service, guest)
				if err != nil {
					return err
				}
				if !confirmed {
					if commandContext.jsonOutput {
						return writeJSON(commandContext.output, actionJSONPayload(action, "aborted", guest, nil))
					}
					return nil
				}
			} else if requiresLifecycleConfirmation(action) {
				confirmed, err := commandContext.confirm(lifecycleConfirmationPrompt(action, guest))
				if err != nil {
					return err
				}
				if !confirmed {
					if commandContext.jsonOutput {
						return writeJSON(commandContext.output, actionJSONPayload(action, "aborted", guest, nil))
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

			if err := service.Lifecycle(cmd.Context(), vmid, action, reporter); err != nil {
				return err
			}

			if commandContext.jsonOutput {
				return writeJSON(commandContext.output, actionJSONPayload(action, "completed", guest, steps))
			}

			fmt.Fprintf(commandContext.output, "Done: %s %s %d\n", action, guestKindLabel(guest), vmid)
			return nil
		},
	}

	return command
}

func (commandContext *commandContext) confirmGuestDelete(ctx context.Context, service *app.Service, guest proxmox.VMResource) (bool, error) {
	if commandContext.yes {
		return true, nil
	}
	if commandContext.jsonOutput {
		return false, confirmationRequiredError{message: fmt.Sprintf("delete %s %d requires --yes in JSON mode", guestKindLabel(guest), guest.VMID)}
	}

	detail, err := service.GuestDetail(ctx, guest.VMID)
	if err != nil {
		return false, err
	}

	fmt.Fprintf(commandContext.output, "Delete %s %d\n", guestKindLabel(guest), guest.VMID)
	fmt.Fprintf(commandContext.output, "Name:      %s\n", guest.Name)
	fmt.Fprintf(commandContext.output, "Type:      %s\n", guestTypeLabel(guest))
	fmt.Fprintf(commandContext.output, "Node:      %s\n", guest.Node)
	fmt.Fprintf(commandContext.output, "Status:    %s\n", guest.Status)
	fmt.Fprintf(commandContext.output, "Disk:      %s\n", formatBytes(guest.MaxDisk))
	fmt.Fprintf(commandContext.output, "IPs:       %s\n", formatList(detail.IPAddresses))
	fmt.Fprintf(commandContext.output, "Tags:      %s\n", formatList(detail.Tags))
	fmt.Fprintf(commandContext.output, "Snapshots: %d\n", detail.SnapshotCount)
	fmt.Fprintln(commandContext.output)

	typedName, err := promptText(commandContext.output, bufio.NewReader(os.Stdin), fmt.Sprintf("Type %q to permanently delete", guest.Name), "")
	if err != nil {
		return false, err
	}
	if typedName != guest.Name {
		fmt.Fprintln(commandContext.output, "Aborted.")
		return false, nil
	}

	return true, nil
}

func (commandContext *commandContext) sshCommand() *cobra.Command {
	var user string
	var printOnly bool

	sshCommand := &cobra.Command{
		Use:   "ssh <vmid>",
		Short: "SSH into a VM using its guest agent IP",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			vmid, err := parseVMID(args[0])
			if err != nil {
				return err
			}

			service, err := commandContext.loadService()
			if err != nil {
				return err
			}

			sshCommandText, err := service.SSHCommand(cmd.Context(), vmid, user)
			if err != nil {
				return err
			}

			if commandContext.jsonOutput {
				return writeJSON(commandContext.output, commandPayload{
					SchemaVersion: agentOutputSchemaVersion,
					Operation:     "ssh_command",
					VMID:          vmid,
					Command:       sshCommandText,
				})
			}

			if printOnly {
				fmt.Fprintln(commandContext.output, sshCommandText)
				return nil
			}

			parts := strings.Fields(sshCommandText)
			if len(parts) != 2 || parts[0] != "ssh" {
				return fmt.Errorf("unexpected SSH command %q", sshCommandText)
			}

			sshProcess := exec.CommandContext(cmd.Context(), "ssh", parts[1])
			sshProcess.Stdin = os.Stdin
			sshProcess.Stdout = os.Stdout
			sshProcess.Stderr = os.Stderr

			return sshProcess.Run()
		},
	}

	sshCommand.Flags().StringVar(&user, "user", "", "SSH user override")
	sshCommand.Flags().BoolVar(&printOnly, "print", false, "print the SSH command instead of running it")

	return sshCommand
}

func (commandContext *commandContext) sshConfigCommand() *cobra.Command {
	var alias string
	var user string
	var printOnly bool

	sshConfigCommand := &cobra.Command{
		Use:   "ssh-config <vmid>",
		Short: "Ensure ~/.ssh/config has a host entry for a VM",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			vmid, err := parseVMID(args[0])
			if err != nil {
				return err
			}

			service, err := commandContext.loadService()
			if err != nil {
				return err
			}

			result, err := service.EnsureSSHConfig(cmd.Context(), vmid, alias, user, printOnly)
			if err != nil {
				return err
			}

			if commandContext.jsonOutput {
				return writeJSON(commandContext.output, sshConfigJSONPayload(result, printOnly))
			}

			if result.AlreadyExists {
				fmt.Fprintf(commandContext.output, "SSH config already present: %s (%s)\n", result.Alias, result.ConfigPath)
				return nil
			}

			if printOnly {
				fmt.Fprintf(commandContext.output, "Host %s\n", result.Alias)
				fmt.Fprintf(commandContext.output, "  HostName %s\n", result.IPAddress)
				fmt.Fprintf(commandContext.output, "  User %s\n", result.User)
				fmt.Fprintf(commandContext.output, "  IdentityAgent none\n")
				fmt.Fprintln(commandContext.output, "Would write to "+result.ConfigPath)
				return nil
			}

			fmt.Fprintf(commandContext.output, "Added SSH config entry %s in %s\n", result.Alias, result.ConfigPath)
			fmt.Fprintf(commandContext.output, "Use: %s\n", result.Command)
			return nil
		},
	}

	sshConfigCommand.Flags().StringVar(&alias, "alias", "", "SSH host alias (defaults to VM name)")
	sshConfigCommand.Flags().StringVar(&user, "user", "", "SSH user override")
	sshConfigCommand.Flags().BoolVar(&printOnly, "print", false, "show planned SSH config entry without writing")

	return sshConfigCommand
}

func (commandContext *commandContext) shellCommand() *cobra.Command {
	var printOnly bool
	var targetKind string
	var user string

	shellCommand := &cobra.Command{
		Use:   "shell <node|vmid|name|node:node|lxc:guest|vm:guest> [command...]",
		Short: "Open a shell on a Proxmox node, LXC container, or VM",
		Long: `Open shell access anywhere in the Proxmox cluster.

Target resolution:
  - A node name such as "pve1" opens SSH to that Proxmox node.
  - A VMID or guest name such as "121" or "koel" opens the matching VM/LXC.
  - Prefix targets with node:, guest:, lxc:, or vm: to make intent explicit.

Execution:
  - Node targets run SSH against nodes.<node>.ssh_host.
  - LXC targets run pct on the LXC's owning Proxmox node, so containers on other nodes work.
  - VM targets use the QEMU guest agent IP and SSH into the VM.
  - Pass a command for non-interactive agent usage. Without a command, a real TTY is required.
  - Use --print to inspect the exact SSH argv. With -o json, --print returns target metadata and argv.`,
		Example: `  boringctl shell pve1
  boringctl shell node:pve1 -- pvecm status
  boringctl shell koel -- php artisan config:show app.url
  boringctl shell lxc:121 -- grep APP_URL /opt/koel/.env
  boringctl shell vm:web-01 --user ubuntu -- uptime
  boringctl shell koel --print -o json`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			service, err := commandContext.loadService()
			if err != nil {
				return err
			}

			if !validShellTargetKind(targetKind) {
				return fmt.Errorf("target must be auto, node, guest, lxc, or vm")
			}

			targetRef := args[0]
			if targetKind != "" && targetKind != "auto" {
				targetRef = targetKind + ":" + targetRef
			}
			containerCommand := args[1:]
			plan, err := service.ShellPlan(cmd.Context(), targetRef, containerCommand, user)
			if err != nil {
				return err
			}

			if printOnly {
				if commandContext.jsonOutput {
					return writeJSON(commandContext.output, commandPayload{
						SchemaVersion: agentOutputSchemaVersion,
						Operation:     "shell_command",
						VMID:          plan.VMID,
						Name:          plan.Name,
						Kind:          string(plan.Kind),
						Node:          plan.Node,
						GuestType:     plan.GuestType,
						SSHHost:       plan.SSHHost,
						IPAddress:     plan.IPAddress,
						User:          plan.User,
						Command:       plan.Command,
						Args:          append([]string{"ssh"}, plan.Args...),
					})
				}
				fmt.Fprintln(commandContext.output, plan.Command)
				return nil
			}

			if len(containerCommand) == 0 && !canRunInteractiveTUI() {
				return errors.New("interactive shell requires a TTY; pass a command after -- or use --print")
			}

			sshProcess := exec.CommandContext(cmd.Context(), "ssh", plan.Args...)
			sshProcess.Stdin = os.Stdin
			sshProcess.Stdout = os.Stdout
			sshProcess.Stderr = os.Stderr

			return sshProcess.Run()
		},
	}

	shellCommand.Flags().BoolVar(&printOnly, "print", false, "print the shell command instead of running it")
	shellCommand.Flags().StringVar(&targetKind, "target", "auto", "target kind: auto, node, guest, lxc, or vm")
	shellCommand.Flags().StringVar(&user, "user", "", "SSH user override for VM targets")

	return shellCommand
}

func (commandContext *commandContext) resizeCommand() *cobra.Command {
	var disk string

	resizeCommand := &cobra.Command{
		Use:   "resize <vmid> <size>",
		Short: "Resize a VM disk",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			vmid, err := parseVMID(args[0])
			if err != nil {
				return err
			}

			confirmed, err := commandContext.confirm(fmt.Sprintf("Resize %s on VM %d to %s?", disk, vmid, args[1]))
			if err != nil {
				return err
			}
			if !confirmed {
				if commandContext.jsonOutput {
					return writeJSON(commandContext.output, actionPayload{
						SchemaVersion: agentOutputSchemaVersion,
						Operation:     "resize",
						Status:        "aborted",
						VMID:          vmid,
					})
				}
				fmt.Fprintln(commandContext.output, "Aborted.")
				return nil
			}

			service, err := commandContext.loadService()
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

			if err := service.ResizeVM(cmd.Context(), vmid, disk, args[1], reporter); err != nil {
				return err
			}

			if commandContext.jsonOutput {
				return writeJSON(commandContext.output, actionPayload{
					SchemaVersion: agentOutputSchemaVersion,
					Operation:     "resize",
					Status:        "completed",
					VMID:          vmid,
					Steps:         steps,
				})
			}

			fmt.Fprintf(commandContext.output, "Done: resized VM %d\n", vmid)
			return nil
		},
	}

	resizeCommand.Flags().StringVar(&disk, "disk", app.DefaultDisk, "disk device")

	return resizeCommand
}

func (commandContext *commandContext) renameCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "rename <vmid> <name>",
		Short: "Rename a VM or LXC container",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			vmid, err := parseVMID(args[0])
			if err != nil {
				return err
			}

			service, err := commandContext.loadService()
			if err != nil {
				return err
			}

			if err := service.RenameVM(cmd.Context(), vmid, args[1]); err != nil {
				return err
			}

			if commandContext.jsonOutput {
				return writeJSON(commandContext.output, actionPayload{
					SchemaVersion: agentOutputSchemaVersion,
					Operation:     "rename",
					Status:        "completed",
					VMID:          vmid,
					Name:          args[1],
				})
			}

			fmt.Fprintf(commandContext.output, "Done: renamed VM %d to %s\n", vmid, args[1])
			return nil
		},
	}
}

func (commandContext *commandContext) tagsCommand() *cobra.Command {
	var setTags []string
	var addTags []string
	var removeTags []string
	var clearTags bool

	tagsCommand := &cobra.Command{
		Use:   "tags <vmid>",
		Short: "Show or update Proxmox guest tags",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			vmid, err := parseVMID(args[0])
			if err != nil {
				return err
			}

			service, err := commandContext.loadService()
			if err != nil {
				return err
			}

			mutationCount := 0
			for _, active := range []bool{len(setTags) > 0, len(addTags) > 0, len(removeTags) > 0, clearTags} {
				if active {
					mutationCount++
				}
			}
			if mutationCount > 1 {
				return fmt.Errorf("use only one of --set, --add, --remove, or --clear")
			}

			var tags []string
			switch {
			case clearTags:
				tags, err = service.SetGuestTags(cmd.Context(), vmid, nil)
			case len(setTags) > 0:
				tags, err = service.SetGuestTags(cmd.Context(), vmid, setTags)
			case len(addTags) > 0:
				tags, err = service.AddGuestTags(cmd.Context(), vmid, addTags)
			case len(removeTags) > 0:
				tags, err = service.RemoveGuestTags(cmd.Context(), vmid, removeTags)
			default:
				detail, err := service.GuestDetail(cmd.Context(), vmid)
				if err != nil {
					return err
				}
				tags = detail.Tags
			}
			if err != nil {
				return err
			}

			if commandContext.jsonOutput {
				return writeJSON(commandContext.output, tagsPayload{
					SchemaVersion: agentOutputSchemaVersion,
					Operation:     "tags",
					VMID:          vmid,
					Tags:          tags,
				})
			}

			fmt.Fprintf(commandContext.output, "Tags: %s\n", formatList(tags))
			return nil
		},
	}

	tagsCommand.Flags().StringSliceVar(&setTags, "set", nil, "replace tags with comma-separated values")
	tagsCommand.Flags().StringSliceVar(&addTags, "add", nil, "add comma-separated tags")
	tagsCommand.Flags().StringSliceVar(&removeTags, "remove", nil, "remove comma-separated tags")
	tagsCommand.Flags().BoolVar(&clearTags, "clear", false, "remove all tags")

	return tagsCommand
}

func (commandContext *commandContext) snapshotCommand() *cobra.Command {
	var listSnapshots bool
	var deleteSnapshot string
	var rollbackSnapshot string
	var description string

	snapshotCommand := &cobra.Command{
		Use:   "snapshot <vmid> [snapshot-name]",
		Short: "List, create, or delete VM or LXC container snapshots",
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) < 1 || len(args) > 2 {
				return fmt.Errorf("accepts 1 or 2 args, received %d", len(args))
			}
			if !listSnapshots && deleteSnapshot == "" && rollbackSnapshot == "" && len(args) != 2 {
				return errorsForSnapshotName()
			}
			activeMutations := 0
			for _, active := range []bool{deleteSnapshot != "", rollbackSnapshot != "", !listSnapshots && len(args) == 2} {
				if active {
					activeMutations++
				}
			}
			if activeMutations > 1 {
				return fmt.Errorf("use only one of snapshot create, --delete, or --rollback")
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			vmid, err := parseVMID(args[0])
			if err != nil {
				return err
			}

			service, err := commandContext.loadService()
			if err != nil {
				return err
			}
			guest, err := service.FindGuest(cmd.Context(), vmid)
			if err != nil {
				return err
			}

			if deleteSnapshot != "" || rollbackSnapshot != "" {
				actionName := "Delete"
				targetSnapshot := deleteSnapshot
				if rollbackSnapshot != "" {
					actionName = "Rollback"
					targetSnapshot = rollbackSnapshot
				}
				confirmed, err := commandContext.confirm(fmt.Sprintf("%s snapshot %s on %s %d?", actionName, targetSnapshot, guestKindLabel(guest), vmid))
				if err != nil {
					return err
				}
				if !confirmed {
					if commandContext.jsonOutput {
						return writeJSON(commandContext.output, actionPayload{
							SchemaVersion: agentOutputSchemaVersion,
							Operation:     strings.ToLower(actionName) + "_snapshot",
							Status:        "aborted",
							VMID:          vmid,
							GuestKind:     guestKindLabel(guest),
							Name:          targetSnapshot,
							Node:          guest.Node,
						})
					}
					fmt.Fprintln(commandContext.output, "Aborted.")
					return nil
				}
			}

			if listSnapshots {
				snapshots, err := service.ListSnapshots(cmd.Context(), vmid)
				if err != nil {
					return err
				}

				if commandContext.jsonOutput {
					return writeJSON(commandContext.output, snapshotsJSONPayload(vmid, guest, snapshots))
				}

				fmt.Fprintln(commandContext.output, "NAME\tTIME\tDESCRIPTION")
				for _, snapshot := range snapshots {
					fmt.Fprintf(commandContext.output, "%s\t%s\t%s\n", snapshot.Name, formatUnix(snapshot.SnapTime), snapshot.Description)
				}
				return nil
			}

			if deleteSnapshot != "" {
				var steps []string
				reporter := commandContext.printStep
				if commandContext.jsonOutput {
					reporter = func(message string) {
						steps = append(steps, message)
					}
				}

				if err := service.DeleteSnapshot(cmd.Context(), vmid, deleteSnapshot, reporter); err != nil {
					return err
				}
				if commandContext.jsonOutput {
					return writeJSON(commandContext.output, actionPayload{
						SchemaVersion: agentOutputSchemaVersion,
						Operation:     "delete_snapshot",
						Status:        "completed",
						VMID:          vmid,
						GuestKind:     guestKindLabel(guest),
						Name:          deleteSnapshot,
						Node:          guest.Node,
						Steps:         steps,
					})
				}
				fmt.Fprintf(commandContext.output, "Done: deleted snapshot %s from %s %d\n", deleteSnapshot, guestKindLabel(guest), vmid)
				return nil
			}

			if rollbackSnapshot != "" {
				var steps []string
				reporter := commandContext.printStep
				if commandContext.jsonOutput {
					reporter = func(message string) {
						steps = append(steps, message)
					}
				}

				if err := service.RollbackSnapshot(cmd.Context(), vmid, rollbackSnapshot, reporter); err != nil {
					return err
				}
				if commandContext.jsonOutput {
					return writeJSON(commandContext.output, actionPayload{
						SchemaVersion: agentOutputSchemaVersion,
						Operation:     "rollback_snapshot",
						Status:        "completed",
						VMID:          vmid,
						GuestKind:     guestKindLabel(guest),
						Name:          rollbackSnapshot,
						Node:          guest.Node,
						Steps:         steps,
					})
				}
				fmt.Fprintf(commandContext.output, "Done: rolled back snapshot %s on %s %d\n", rollbackSnapshot, guestKindLabel(guest), vmid)
				return nil
			}

			var steps []string
			reporter := commandContext.printStep
			if commandContext.jsonOutput {
				reporter = func(message string) {
					steps = append(steps, message)
				}
			}

			if err := service.CreateSnapshot(cmd.Context(), vmid, args[1], description, reporter); err != nil {
				return err
			}

			if commandContext.jsonOutput {
				return writeJSON(commandContext.output, actionPayload{
					SchemaVersion: agentOutputSchemaVersion,
					Operation:     "create_snapshot",
					Status:        "completed",
					VMID:          vmid,
					GuestKind:     guestKindLabel(guest),
					Name:          args[1],
					Node:          guest.Node,
					Steps:         steps,
				})
			}

			fmt.Fprintf(commandContext.output, "Done: created snapshot %s on %s %d\n", args[1], guestKindLabel(guest), vmid)
			return nil
		},
	}

	snapshotCommand.Flags().BoolVar(&listSnapshots, "list", false, "list snapshots")
	snapshotCommand.Flags().StringVar(&deleteSnapshot, "delete", "", "delete snapshot by name")
	snapshotCommand.Flags().StringVar(&rollbackSnapshot, "rollback", "", "rollback snapshot by name")
	snapshotCommand.Flags().StringVar(&description, "description", "", "snapshot description")

	return snapshotCommand
}

func (commandContext *commandContext) printStep(message string) {
	fmt.Fprintf(commandContext.output, "✓ %s\n", message)
}

func printCreateResult(output io.Writer, result app.CreateResult) {
	fmt.Fprintln(output)
	fmt.Fprintln(output, "VM ready")
	fmt.Fprintf(output, "Name:     %s\n", result.Name)
	fmt.Fprintf(output, "VMID:     %d\n", result.VMID)
	fmt.Fprintf(output, "Node:     %s\n", result.Node)
	fmt.Fprintf(output, "Image:    %s\n", result.ImageLabel)
	if result.Plan != "" {
		fmt.Fprintf(output, "Plan:     %s\n", result.Plan)
	}
	fmt.Fprintf(output, "Template: %d\n", result.TemplateID)
	fmt.Fprintf(output, "Storage:  %s\n", result.Storage)
	fmt.Fprintf(output, "CPU:      %d cores\n", result.Cores)
	fmt.Fprintf(output, "RAM:      %s\n", formatMemory(result.MemoryMB))
	fmt.Fprintf(output, "Disk:     %d GB\n", result.DiskGB)
	fmt.Fprintf(output, "User:     %s\n", result.User)
	if result.StaticIP {
		fmt.Fprintln(output, "Network:  static from DHCP lease")
	} else {
		fmt.Fprintf(output, "Network:  %s\n", result.NetworkMode)
	}
	if result.Warning != "" {
		fmt.Fprintf(output, "Warning:  %s\n", result.Warning)
	}
	if result.IP != "" {
		fmt.Fprintf(output, "IP:       %s\n", result.IP)
		fmt.Fprintf(output, "SSH:      %s\n", result.SSHCommand)
		fmt.Fprintf(output, "SSH fix:  %s  # if host key changed\n", result.SSHKeygen)
	} else {
		fmt.Fprintln(output, "IP:       not available from guest agent yet")
	}
}

func printCreatePreview(output io.Writer, preview app.CreatePreview) {
	fmt.Fprintln(output, "Create preview")
	fmt.Fprintf(output, "Node:     %s\n", preview.Node)
	fmt.Fprintf(output, "Name:     %s\n", preview.Name)
	fmt.Fprintf(output, "Template: %d\n", preview.TemplateID)
	fmt.Fprintf(output, "Storage:  %s\n", preview.Storage)
	fmt.Fprintln(output)
	fmt.Fprintf(output, "POST /nodes/%s/qemu/%d/clone\n", preview.Node, preview.TemplateID)
	printParams(output, preview.CloneParams)
	fmt.Fprintf(output, "PUT /nodes/%s/qemu/<nextid>/resize\n", preview.Node)
	printParams(output, preview.ResizeParams)
	fmt.Fprintf(output, "PUT /nodes/%s/qemu/<nextid>/config\n", preview.Node)
	printParams(output, preview.ConfigParams)
	fmt.Fprintf(output, "POST %s\n", preview.StartPath)
}

func printCreateContainerResult(output io.Writer, result app.CreateContainerResult) {
	fmt.Fprintln(output)
	fmt.Fprintln(output, "LXC container ready")
	fmt.Fprintf(output, "Name:     %s\n", result.Name)
	fmt.Fprintf(output, "VMID:     %d\n", result.VMID)
	fmt.Fprintf(output, "Node:     %s\n", result.Node)
	fmt.Fprintf(output, "Image:    %s\n", result.ImageLabel)
	if result.Plan != "" {
		fmt.Fprintf(output, "Plan:     %s\n", result.Plan)
	}
	fmt.Fprintf(output, "Template: %s\n", result.Template)
	fmt.Fprintf(output, "Storage:  %s\n", result.Storage)
	fmt.Fprintf(output, "CPU:      %d cores\n", result.Cores)
	fmt.Fprintf(output, "RAM:      %s\n", formatMemory(result.MemoryMB))
	fmt.Fprintf(output, "Swap:     %s\n", formatMemory(result.SwapMB))
	fmt.Fprintf(output, "Rootfs:   %d GB\n", result.DiskGB)
	fmt.Fprintf(output, "User:     %s\n", result.User)
	fmt.Fprintf(output, "Network:  %s\n", result.NetworkMode)
	fmt.Fprintf(output, "Tags:     %s\n", formatList(result.Tags))
	if len(result.Features) > 0 {
		fmt.Fprintf(output, "Features: %s\n", formatList(result.Features))
	}
	if result.Warning != "" {
		fmt.Fprintf(output, "Warning:  %s\n", result.Warning)
	}
	if result.IP != "" {
		fmt.Fprintf(output, "IP:       %s\n", result.IP)
		fmt.Fprintf(output, "SSH:      %s\n", result.SSHCommand)
		fmt.Fprintf(output, "SSH fix:  %s  # if host key changed\n", result.SSHKeygen)
	} else if result.Started {
		fmt.Fprintln(output, "IP:       not available from LXC interfaces yet")
	} else {
		fmt.Fprintln(output, "IP:       container was not started")
	}
}

func printCreateContainerPreview(output io.Writer, preview app.CreateContainerPreview) {
	fmt.Fprintln(output, "Create LXC preview")
	fmt.Fprintf(output, "Node:     %s\n", preview.Node)
	fmt.Fprintf(output, "Name:     %s\n", preview.Name)
	fmt.Fprintf(output, "Template: %s\n", preview.Template)
	fmt.Fprintf(output, "Storage:  %s\n", preview.Storage)
	fmt.Fprintln(output)
	fmt.Fprintf(output, "POST %s\n", preview.CreatePath)
	printParams(output, preview.Params)
}

func nodesJSONPayload(loadedConfig *config.Config) nodesPayload {
	nodes := make([]nodePayload, 0, len(loadedConfig.Nodes))
	for _, nodeName := range loadedConfig.NodeNames() {
		node := loadedConfig.Nodes[nodeName]
		nodes = append(nodes, nodePayload{
			Name:     nodeName,
			Label:    node.Label,
			Storages: append([]string{}, node.Storages...),
			SSHHost:  node.SSHHost,
		})
	}

	return nodesPayload{
		SchemaVersion: agentOutputSchemaVersion,
		Nodes:         nodes,
	}
}

func imagesJSONPayload(loadedConfig *config.Config, nodeNames []string) imagesPayload {
	images := make([]imagePayload, 0, len(loadedConfig.Images))
	for _, imageName := range loadedConfig.ImageNames() {
		image := loadedConfig.Images[imageName]
		templates := make(map[string]int, len(nodeNames))
		for _, nodeName := range nodeNames {
			templates[nodeName] = image.Templates[nodeName]
		}

		images = append(images, imagePayload{
			Name:        imageName,
			Label:       image.Label,
			Family:      image.Family,
			DefaultUser: image.DefaultUser,
			Recommended: image.Recommended,
			Templates:   templates,
		})
	}

	return imagesPayload{
		SchemaVersion: agentOutputSchemaVersion,
		Nodes:         nodeNames,
		Images:        images,
	}
}

func lxcImagesJSONPayload(loadedConfig *config.Config, nodeNames []string) lxcImagesPayload {
	images := make([]lxcImagePayload, 0, len(loadedConfig.LXCImages))
	for _, imageName := range loadedConfig.LXCImageNames() {
		image := loadedConfig.LXCImages[imageName]
		templates := make(map[string]string, len(nodeNames))
		for _, nodeName := range nodeNames {
			templates[nodeName] = image.Templates[nodeName]
		}

		images = append(images, lxcImagePayload{
			Name:        imageName,
			Label:       image.Label,
			Family:      image.Family,
			OSType:      image.OSType,
			DefaultUser: image.DefaultUser,
			Recommended: image.Recommended,
			Templates:   templates,
		})
	}

	return lxcImagesPayload{
		SchemaVersion: agentOutputSchemaVersion,
		Nodes:         nodeNames,
		Images:        images,
	}
}

func plansJSONPayload(loadedConfig *config.Config) plansPayload {
	plans := make([]planPayload, 0, len(loadedConfig.Plans))
	for _, planName := range loadedConfig.PlanNames() {
		plan := loadedConfig.Plans[planName]
		plans = append(plans, planPayload{
			Name:     planName,
			Label:    plan.Label,
			Cores:    plan.Cores,
			MemoryMB: plan.MemoryMB,
			DiskGB:   plan.DiskGB,
		})
	}

	return plansPayload{
		SchemaVersion: agentOutputSchemaVersion,
		Plans:         plans,
	}
}

func suggestedPlansJSONPayload(suggestedPlans []app.SuggestedPlan) plansPayload {
	plans := make([]planPayload, 0, len(suggestedPlans))
	for _, plan := range suggestedPlans {
		plans = append(plans, planPayload{
			Name:     plan.Name,
			Label:    plan.Label,
			Cores:    plan.Cores,
			MemoryMB: plan.MemoryMB,
			DiskGB:   plan.DiskGB,
			Source:   plan.Reason,
		})
	}

	return plansPayload{
		SchemaVersion: agentOutputSchemaVersion,
		Plans:         plans,
	}
}

func createPreviewJSONPayload(preview app.CreatePreview) createPreviewPayload {
	return createPreviewPayload{
		SchemaVersion: agentOutputSchemaVersion,
		Operation:     "create_vm_preview",
		Node:          preview.Node,
		Name:          preview.Name,
		TemplateID:    preview.TemplateID,
		Storage:       preview.Storage,
		Steps: []proxmoxRequest{
			{Method: "POST", Path: fmt.Sprintf("/nodes/%s/qemu/%d/clone", preview.Node, preview.TemplateID), Params: preview.CloneParams},
			{Method: "PUT", Path: fmt.Sprintf("/nodes/%s/qemu/<nextid>/resize", preview.Node), Params: preview.ResizeParams},
			{Method: "PUT", Path: fmt.Sprintf("/nodes/%s/qemu/<nextid>/config", preview.Node), Params: preview.ConfigParams},
			{Method: "POST", Path: preview.StartPath},
		},
	}
}

func lxcCreatePreviewJSONPayload(preview app.CreateContainerPreview) lxcCreatePreviewPayload {
	return lxcCreatePreviewPayload{
		SchemaVersion: agentOutputSchemaVersion,
		Operation:     "create_lxc_preview",
		Node:          preview.Node,
		Name:          preview.Name,
		Template:      preview.Template,
		Storage:       preview.Storage,
		Method:        "POST",
		Path:          preview.CreatePath,
		Params:        preview.Params,
	}
}

func createResultJSONPayload(result app.CreateResult, steps []string) createResultPayload {
	return createResultPayload{
		SchemaVersion: agentOutputSchemaVersion,
		Operation:     "create_vm",
		Steps:         steps,
		VMID:          result.VMID,
		Name:          result.Name,
		Node:          result.Node,
		Image:         result.Image,
		ImageLabel:    result.ImageLabel,
		Plan:          result.Plan,
		TemplateID:    result.TemplateID,
		Storage:       result.Storage,
		Cores:         result.Cores,
		MemoryMB:      result.MemoryMB,
		DiskGB:        result.DiskGB,
		User:          result.User,
		IP:            result.IP,
		SSHCommand:    result.SSHCommand,
		SSHKeygen:     result.SSHKeygen,
		NetworkMode:   result.NetworkMode,
		StaticIP:      result.StaticIP,
		Warning:       result.Warning,
	}
}

func lxcCreateResultJSONPayload(result app.CreateContainerResult, steps []string) lxcCreateResultPayload {
	return lxcCreateResultPayload{
		SchemaVersion: agentOutputSchemaVersion,
		Operation:     "create_lxc",
		Steps:         steps,
		VMID:          result.VMID,
		Name:          result.Name,
		Node:          result.Node,
		Image:         result.Image,
		ImageLabel:    result.ImageLabel,
		Plan:          result.Plan,
		Template:      result.Template,
		Storage:       result.Storage,
		Cores:         result.Cores,
		MemoryMB:      result.MemoryMB,
		DiskGB:        result.DiskGB,
		SwapMB:        result.SwapMB,
		User:          result.User,
		IP:            result.IP,
		SSHCommand:    result.SSHCommand,
		SSHKeygen:     result.SSHKeygen,
		NetworkMode:   result.NetworkMode,
		Tags:          result.Tags,
		Features:      result.Features,
		Started:       result.Started,
		Warning:       result.Warning,
	}
}

type guestListFilters struct {
	Tag    string
	Node   string
	Status string
	Kind   string
	Name   string
	Fields string
	Limit  int
}

func guestListJSONPayload(filters guestListFilters, total int, vms []proxmox.VMResource, containers []proxmox.VMResource) any {
	payload := guestListPayload{
		SchemaVersion: agentOutputSchemaVersion,
		TagFilter:     strings.TrimSpace(filters.Tag),
		NodeFilter:    strings.TrimSpace(filters.Node),
		StatusFilter:  strings.TrimSpace(filters.Status),
		KindFilter:    strings.TrimSpace(filters.Kind),
		NameFilter:    strings.TrimSpace(filters.Name),
		Limit:         filters.Limit,
		Total:         total,
		VMs:           guestPayloads(vms),
		Containers:    guestPayloads(containers),
	}

	fields := cleanValues(strings.Split(filters.Fields, ","))
	if len(fields) == 0 {
		return payload
	}

	return map[string]any{
		"schema_version": agentOutputSchemaVersion,
		"tag_filter":     strings.TrimSpace(filters.Tag),
		"node_filter":    strings.TrimSpace(filters.Node),
		"status_filter":  strings.TrimSpace(filters.Status),
		"kind_filter":    strings.TrimSpace(filters.Kind),
		"name_filter":    strings.TrimSpace(filters.Name),
		"fields":         fields,
		"limit":          filters.Limit,
		"total":          total,
		"vms":            guestPayloadMaps(payload.VMs, fields),
		"containers":     guestPayloadMaps(payload.Containers, fields),
	}
}

func guestDetailJSONPayload(detail app.GuestDetail) guestDetailPayload {
	return guestDetailPayload{
		SchemaVersion: agentOutputSchemaVersion,
		Guest:         guestJSONPayload(detail.Guest),
		IPAddresses:   append([]string{}, detail.IPAddresses...),
		SnapshotCount: detail.SnapshotCount,
		ShellCommand:  detail.ShellCommand,
		Config:        detail.Config,
	}
}

func guestPayloads(guests []proxmox.VMResource) []guestPayload {
	sort.Slice(guests, func(leftIndex int, rightIndex int) bool {
		return guests[leftIndex].VMID < guests[rightIndex].VMID
	})

	payloads := make([]guestPayload, 0, len(guests))
	for _, guest := range guests {
		payloads = append(payloads, guestJSONPayload(guest))
	}
	return payloads
}

func guestJSONPayload(guest proxmox.VMResource) guestPayload {
	return guestPayload{
		VMID:      guest.VMID,
		Name:      guest.Name,
		Node:      guest.Node,
		Type:      guest.GuestType(),
		Kind:      guestKindLabel(guest),
		Status:    guest.Status,
		CPU:       guest.CPU,
		MemoryMax: guest.MaxMem,
		Memory:    guest.Mem,
		DiskMax:   guest.MaxDisk,
		Disk:      guest.Disk,
		Uptime:    guest.Uptime,
		Template:  guest.Template == 1,
		Tags:      splitTags(guest.Tags),
	}
}

func newestHistoryEntries(entries []app.CreateHistoryEntry) []app.CreateHistoryEntry {
	newestEntries := make([]app.CreateHistoryEntry, 0, len(entries))
	for entryIndex := len(entries) - 1; entryIndex >= 0; entryIndex-- {
		newestEntries = append(newestEntries, entries[entryIndex])
	}
	return newestEntries
}

func sshConfigJSONPayload(result app.SSHConfigResult, printOnly bool) sshConfigPayload {
	return sshConfigPayload{
		SchemaVersion: agentOutputSchemaVersion,
		Operation:     "ssh_config",
		Alias:         result.Alias,
		IPAddress:     result.IPAddress,
		User:          result.User,
		Command:       result.Command,
		ConfigPath:    result.ConfigPath,
		Added:         result.Added,
		AlreadyExists: result.AlreadyExists,
		PrintOnly:     printOnly,
	}
}

func snapshotsJSONPayload(vmid int, guest proxmox.VMResource, snapshots []proxmox.Snapshot) snapshotsPayload {
	payloads := make([]snapshotPayload, 0, len(snapshots))
	for _, snapshot := range snapshots {
		payloads = append(payloads, snapshotPayload{
			Name:         snapshot.Name,
			Description:  snapshot.Description,
			SnapTime:     snapshot.SnapTime,
			SnapTimeText: formatUnix(snapshot.SnapTime),
		})
	}

	return snapshotsPayload{
		SchemaVersion: agentOutputSchemaVersion,
		VMID:          vmid,
		GuestKind:     guestKindLabel(guest),
		Snapshots:     payloads,
	}
}

func actionJSONPayload(operation string, status string, guest proxmox.VMResource, steps []string) actionPayload {
	return actionPayload{
		SchemaVersion: agentOutputSchemaVersion,
		Operation:     operation,
		Status:        status,
		VMID:          guest.VMID,
		GuestKind:     guestKindLabel(guest),
		Name:          guest.Name,
		Node:          guest.Node,
		Steps:         steps,
	}
}

func writeJSON(output io.Writer, value any) error {
	encoder := json.NewEncoder(output)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}

func errorKind(err error) string {
	var apiError *proxmox.APIError
	if errors.As(err, &apiError) {
		return apiError.Kind()
	}

	var taskError *proxmox.TaskError
	if errors.As(err, &taskError) {
		return taskError.Kind()
	}

	var timeoutError *proxmox.TimeoutError
	if errors.As(err, &timeoutError) {
		return timeoutError.Kind()
	}

	var confirmationError confirmationRequiredError
	if errors.As(err, &confirmationError) {
		return proxmox.ErrorKindConfirm
	}

	var doctorError doctorFailureError
	if errors.As(err, &doctorError) {
		return "doctor_failed"
	}

	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return proxmox.ErrorKindTimeout
	}

	return proxmox.ErrorKindOther
}

func printParams(output io.Writer, params map[string]string) {
	keys := make([]string, 0, len(params))
	for key := range params {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	for _, key := range keys {
		fmt.Fprintf(output, "  %s=%s\n", key, params[key])
	}
}

func parseVMID(value string) (int, error) {
	vmid, err := strconv.Atoi(value)
	if err != nil || vmid <= 0 {
		return 0, fmt.Errorf("invalid VMID %q", value)
	}

	return vmid, nil
}

func validShellTargetKind(targetKind string) bool {
	switch targetKind {
	case "", "auto", "node", "guest", "lxc", "vm":
		return true
	default:
		return false
	}
}

func confirm(output io.Writer, input io.Reader, prompt string) (bool, error) {
	fmt.Fprintf(output, "%s [y/N] ", prompt)

	var answer string
	if _, err := fmt.Fscanln(input, &answer); err != nil {
		return false, nil
	}

	answer = strings.ToLower(strings.TrimSpace(answer))
	return answer == "y" || answer == "yes", nil
}

func (commandContext *commandContext) confirm(prompt string) (bool, error) {
	if commandContext.yes {
		return true, nil
	}
	if commandContext.jsonOutput {
		return false, confirmationRequiredError{message: prompt + " requires --yes in JSON mode"}
	}

	return confirm(commandContext.output, os.Stdin, prompt)
}

func formatMemory(memoryMB int) string {
	if memoryMB%1024 == 0 {
		return fmt.Sprintf("%d GB", memoryMB/1024)
	}

	return fmt.Sprintf("%d MB", memoryMB)
}

func formatBytes(bytes int64) string {
	if bytes <= 0 {
		return "-"
	}

	const gibibyte = 1024 * 1024 * 1024
	const mebibyte = 1024 * 1024

	if bytes >= gibibyte {
		return fmt.Sprintf("%.1f GB", float64(bytes)/gibibyte)
	}

	return fmt.Sprintf("%.0f MB", float64(bytes)/mebibyte)
}

func formatUnix(timestamp int64) string {
	if timestamp == 0 {
		return "-"
	}

	return time.Unix(timestamp, 0).Format(time.RFC3339)
}

func formatDuration(seconds int64) string {
	if seconds <= 0 {
		return "-"
	}

	duration := time.Duration(seconds) * time.Second
	days := duration / (24 * time.Hour)
	duration -= days * 24 * time.Hour
	hours := duration / time.Hour
	duration -= hours * time.Hour
	minutes := duration / time.Minute

	if days > 0 {
		return fmt.Sprintf("%dd %dh", days, hours)
	}
	if hours > 0 {
		return fmt.Sprintf("%dh %dm", hours, minutes)
	}
	return fmt.Sprintf("%dm", minutes)
}

func formatList(values []string) string {
	cleanValues := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			cleanValues = append(cleanValues, value)
		}
	}
	if len(cleanValues) == 0 {
		return "-"
	}
	return strings.Join(cleanValues, ", ")
}

func splitTags(tags string) []string {
	return cleanValues(strings.Split(tags, ";"))
}

func cleanValues(values []string) []string {
	cleanValues := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			cleanValues = append(cleanValues, value)
		}
	}
	return cleanValues
}

func errorsForSnapshotName() error {
	return fmt.Errorf("snapshot name is required unless --list or --delete is used")
}

func printGuestTable(output io.Writer, title string, guests []proxmox.VMResource) {
	sort.Slice(guests, func(leftIndex int, rightIndex int) bool {
		return guests[leftIndex].VMID < guests[rightIndex].VMID
	})

	fmt.Fprintln(output, title)
	if len(guests) == 0 {
		fmt.Fprintln(output, "  none")
		return
	}

	fmt.Fprintln(output, "VMID\tNAME\tNODE\tSTATUS\tCPU\tRAM\tDISK\tTAGS")
	for _, guest := range guests {
		fmt.Fprintf(output, "%d\t%s\t%s\t%s\t%.2f\t%s\t%s\t%s\n", guest.VMID, guest.Name, guest.Node, guest.Status, guest.CPU, formatBytes(guest.MaxMem), formatBytes(guest.MaxDisk), formatList(strings.Split(guest.Tags, ";")))
	}
}

func filterGuests(guests []proxmox.VMResource, filters guestListFilters) ([]proxmox.VMResource, error) {
	kindFilter := strings.ToLower(strings.TrimSpace(filters.Kind))
	switch kindFilter {
	case "", "all":
	case "vm", "qemu":
		kindFilter = proxmox.GuestTypeQEMU
	case "container", "lxc":
		kindFilter = proxmox.GuestTypeLXC
	default:
		return nil, fmt.Errorf("kind must be vm/qemu or lxc/container, got %q", filters.Kind)
	}

	tagFilter := strings.ToLower(strings.TrimSpace(filters.Tag))
	nodeFilter := strings.ToLower(strings.TrimSpace(filters.Node))
	statusFilter := strings.ToLower(strings.TrimSpace(filters.Status))
	nameFilter := strings.ToLower(strings.TrimSpace(filters.Name))

	filteredGuests := make([]proxmox.VMResource, 0, len(guests))
	for _, guest := range guests {
		if kindFilter != "" && guest.GuestType() != kindFilter {
			continue
		}
		if nodeFilter != "" && strings.ToLower(guest.Node) != nodeFilter {
			continue
		}
		if statusFilter != "" && strings.ToLower(guest.Status) != statusFilter {
			continue
		}
		if nameFilter != "" && !strings.Contains(strings.ToLower(guest.Name), nameFilter) {
			continue
		}
		if tagFilter != "" && !guestHasTag(guest, tagFilter) {
			continue
		}
		filteredGuests = append(filteredGuests, guest)
	}

	sort.Slice(filteredGuests, func(leftIndex int, rightIndex int) bool {
		return filteredGuests[leftIndex].VMID < filteredGuests[rightIndex].VMID
	})

	return filteredGuests, nil
}

func limitGuests(guests []proxmox.VMResource, limit int) []proxmox.VMResource {
	if limit <= 0 || len(guests) <= limit {
		return guests
	}

	return guests[:limit]
}

func splitGuestsByKind(guests []proxmox.VMResource) ([]proxmox.VMResource, []proxmox.VMResource) {
	vms := make([]proxmox.VMResource, 0, len(guests))
	containers := make([]proxmox.VMResource, 0, len(guests))
	for _, guest := range guests {
		if guest.IsContainer() {
			containers = append(containers, guest)
			continue
		}
		vms = append(vms, guest)
	}

	return vms, containers
}

func guestHasTag(guest proxmox.VMResource, tagFilter string) bool {
	for _, tag := range strings.Split(guest.Tags, ";") {
		if strings.ToLower(strings.TrimSpace(tag)) == tagFilter {
			return true
		}
	}

	return false
}

func filterGuestsByTag(guests []proxmox.VMResource, tagFilter string) []proxmox.VMResource {
	tagFilter = strings.ToLower(strings.TrimSpace(tagFilter))
	if tagFilter == "" {
		return guests
	}

	filteredGuests := make([]proxmox.VMResource, 0, len(guests))
	for _, guest := range guests {
		for _, tag := range strings.Split(guest.Tags, ";") {
			if strings.ToLower(strings.TrimSpace(tag)) == tagFilter {
				filteredGuests = append(filteredGuests, guest)
				break
			}
		}
	}

	return filteredGuests
}

func guestPayloadMaps(guests []guestPayload, fields []string) []map[string]any {
	fieldSet := make(map[string]bool, len(fields))
	for _, field := range fields {
		fieldSet[field] = true
	}

	rows := make([]map[string]any, 0, len(guests))
	for _, guest := range guests {
		row := map[string]any{}
		values := map[string]any{
			"vmid":             guest.VMID,
			"name":             guest.Name,
			"node":             guest.Node,
			"type":             guest.Type,
			"kind":             guest.Kind,
			"status":           guest.Status,
			"cpu":              guest.CPU,
			"memory_max_bytes": guest.MemoryMax,
			"memory_bytes":     guest.Memory,
			"disk_max_bytes":   guest.DiskMax,
			"disk_bytes":       guest.Disk,
			"uptime_seconds":   guest.Uptime,
			"template":         guest.Template,
			"tags":             guest.Tags,
		}
		for key, value := range values {
			if fieldSet[key] {
				row[key] = value
			}
		}
		rows = append(rows, row)
	}

	return rows
}

func guestKindLabel(guest proxmox.VMResource) string {
	if guest.IsContainer() {
		return "container"
	}
	return "VM"
}

func guestTypeLabel(guest proxmox.VMResource) string {
	if guest.IsContainer() {
		return "LXC container"
	}
	return "QEMU VM"
}

func titleAction(action string) string {
	if action == "" {
		return action
	}

	return strings.ToUpper(action[:1]) + action[1:]
}

func requiresLifecycleConfirmation(action string) bool {
	switch action {
	case "stop", "reboot", "delete":
		return true
	default:
		return false
	}
}

func lifecycleConfirmationPrompt(action string, guest proxmox.VMResource) string {
	kind := guestKindLabel(guest)
	switch action {
	case "delete":
		if guest.IsContainer() {
			return fmt.Sprintf("Delete container %d and purge its config?", guest.VMID)
		}
		return fmt.Sprintf("Delete VM %d and destroy its disks?", guest.VMID)
	case "stop":
		return fmt.Sprintf("Stop %s %d?", kind, guest.VMID)
	case "reboot":
		return fmt.Sprintf("Reboot %s %d?", kind, guest.VMID)
	default:
		return fmt.Sprintf("%s %s %d?", titleAction(action), kind, guest.VMID)
	}
}
