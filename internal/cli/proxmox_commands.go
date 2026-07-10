package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/boring-labs/boringctl/internal/app"
	"github.com/boring-labs/boringctl/internal/config"
	"github.com/boring-labs/boringctl/internal/proxmox"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

type configCheckPayload struct {
	SchemaVersion int      `json:"schema_version"`
	Status        string   `json:"status"`
	Endpoint      string   `json:"endpoint"`
	Nodes         int      `json:"nodes"`
	NodesOnline   int      `json:"nodes_online"`
	CheckedAt     string   `json:"checked_at"`
	Errors        []string `json:"errors,omitempty"`
}

type configShowPayload struct {
	SchemaVersion  int           `json:"schema_version"`
	ConfigPath     string        `json:"config_path"`
	Profile        string        `json:"profile,omitempty"`
	Endpoint       string        `json:"endpoint"`
	InsecureTLS    bool          `json:"insecure_tls"`
	TokenIDEnv     string        `json:"token_id_env"`
	TokenSecretEnv string        `json:"token_secret_env"`
	Nodes          []string      `json:"nodes"`
	NodeDetails    []nodePayload `json:"node_details,omitempty"`
	Storages       []string      `json:"storages"`
	Images         []string      `json:"images"`
	LXCImages      []string      `json:"lxc_images"`
	Plans          []string      `json:"plans"`
}

type taskListPayload struct {
	SchemaVersion int            `json:"schema_version"`
	Tasks         []proxmox.Task `json:"tasks"`
}

type taskStatusPayload struct {
	SchemaVersion int                `json:"schema_version"`
	Task          proxmox.TaskStatus `json:"task"`
}

type taskLogPayload struct {
	SchemaVersion int                    `json:"schema_version"`
	UPID          string                 `json:"upid"`
	Log           []proxmox.TaskLogEntry `json:"log"`
}

type storageContentPayload struct {
	SchemaVersion int                      `json:"schema_version"`
	Node          string                   `json:"node"`
	Storage       string                   `json:"storage"`
	Content       []proxmox.StorageContent `json:"content"`
}

type taskActionPayload struct {
	SchemaVersion int    `json:"schema_version"`
	Operation     string `json:"operation"`
	Status        string `json:"status"`
	UPID          string `json:"upid,omitempty"`
	Node          string `json:"node,omitempty"`
}

type guestSpecPayload struct {
	SchemaVersion int           `json:"schema_version"`
	Spec          app.GuestSpec `json:"spec"`
}

type guestSpecApplyPayload struct {
	SchemaVersion int                      `json:"schema_version"`
	Result        app.GuestSpecApplyResult `json:"result"`
}

func (commandContext *commandContext) configCommand() *cobra.Command {
	configCommand := &cobra.Command{
		Use:   "config",
		Short: "Inspect boringctl configuration and Proxmox connectivity",
	}

	configCommand.AddCommand(commandContext.configCheckCommand(), commandContext.configShowCommand())
	return configCommand
}

func (commandContext *commandContext) configCheckCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "check",
		Short: "Verify configuration and Proxmox connectivity",
		RunE: func(cmd *cobra.Command, args []string) error {
			service, err := commandContext.loadHealthAwareService()
			if err != nil {
				return err
			}

			health := service.Health(cmd.Context())
			payload := configCheckPayload{
				SchemaVersion: agentOutputSchemaVersion,
				Status:        "ok",
				Endpoint:      health.Endpoint,
				Nodes:         len(health.Nodes),
				CheckedAt:     health.CheckedAt.Format(time.RFC3339),
			}
			for _, node := range health.Nodes {
				if strings.EqualFold(node.Status, "online") {
					payload.NodesOnline++
				}
			}
			if !health.Connected {
				payload.Status = "error"
				if health.Error != "" {
					payload.Errors = append(payload.Errors, health.Error)
				}
			}

			if commandContext.jsonOutput {
				return writeJSON(commandContext.output, payload)
			}
			if payload.Status != "ok" {
				return fmt.Errorf("config check failed: %s", strings.Join(payload.Errors, "; "))
			}
			fmt.Fprintf(commandContext.output, "Connection OK: %s (%d/%d nodes online)\n", payload.Endpoint, payload.NodesOnline, payload.Nodes)
			return nil
		},
	}
}

func (commandContext *commandContext) configShowCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "show",
		Short: "Show resolved boringctl config metadata without secrets",
		RunE: func(cmd *cobra.Command, args []string) error {
			loadedConfig, configPath, err := config.LoadProfile(commandContext.configPath, commandContext.profile)
			if err != nil {
				return err
			}

			payload := configShowPayload{
				SchemaVersion:  agentOutputSchemaVersion,
				ConfigPath:     configPath,
				Profile:        commandContext.profile,
				Endpoint:       loadedConfig.Cluster.Endpoint,
				InsecureTLS:    loadedConfig.Cluster.InsecureTLS,
				TokenIDEnv:     loadedConfig.Auth.TokenIDEnv,
				TokenSecretEnv: loadedConfig.Auth.TokenSecretEnv,
				Nodes:          loadedConfig.NodeNames(),
				NodeDetails:    nodesJSONPayload(loadedConfig).Nodes,
				Storages:       loadedConfig.StorageNames(),
				Images:         loadedConfig.ImageNames(),
				LXCImages:      loadedConfig.LXCImageNames(),
				Plans:          loadedConfig.PlanNames(),
			}

			if commandContext.jsonOutput {
				return writeJSON(commandContext.output, payload)
			}
			fmt.Fprintf(commandContext.output, "Config:   %s\n", payload.ConfigPath)
			fmt.Fprintf(commandContext.output, "Endpoint: %s\n", payload.Endpoint)
			fmt.Fprintf(commandContext.output, "TLS:      insecure=%t\n", payload.InsecureTLS)
			fmt.Fprintf(commandContext.output, "Auth:     %s / %s\n", payload.TokenIDEnv, payload.TokenSecretEnv)
			fmt.Fprintf(commandContext.output, "Nodes:    %s\n", formatList(payload.Nodes))
			fmt.Fprintf(commandContext.output, "Storages: %s\n", formatList(payload.Storages))
			fmt.Fprintf(commandContext.output, "Images:   %d VM, %d LXC\n", len(payload.Images), len(payload.LXCImages))
			return nil
		},
	}
}

func (commandContext *commandContext) apiCommand() *cobra.Command {
	apiCommand := &cobra.Command{
		Use:   "api",
		Short: "Send raw requests to the Proxmox API",
		Long: `Send raw requests to the Proxmox API.

The API path is positional on method subcommands. Do not pass --path.
Use --query key=value for query parameters. For POST/PUT, --data accepts either a flat JSON object or form-style key=value&key=value pairs. Comma-separated key=value is also accepted for simple values, but JSON or & form is safer for Proxmox values containing commas such as net0.
Mutating methods require confirmation unless --yes is set.`,
		Example: `  boringctl api get /version
  boringctl api get /nodes/pve1/storage/local/content --query content=vztmpl
  boringctl api post /nodes/pve1/lxc --data '{"vmid":122,"hostname":"demo","ostemplate":"local:vztmpl/ubuntu-24.04-standard_24.04-2_amd64.tar.zst","rootfs":"local-lvm:20","net0":"name=eth0,bridge=vmbr0,type=veth,ip=dhcp"}' --yes
  boringctl api post /nodes/pve1/lxc/122/status/start --yes`,
	}
	for _, method := range []string{"get", "post", "put", "delete"} {
		apiCommand.AddCommand(commandContext.apiMethodCommand(method))
	}
	return apiCommand
}

func (commandContext *commandContext) apiMethodCommand(method string) *cobra.Command {
	var query []string
	var data string
	var rawResponse bool
	example := apiMethodExample(method)

	apiMethodCommand := &cobra.Command{
		Use:   method + " <path>",
		Short: "Raw Proxmox API " + strings.ToUpper(method),
		Long: fmt.Sprintf(`Raw Proxmox API %s.

<path> is positional and may be written with or without a leading slash.
Use --query key=value for query parameters. Use --data for POST/PUT form values; JSON must be a flat object because Proxmox expects form fields. For non-JSON data, prefer key=value&key=value when values contain commas.`, strings.ToUpper(method)),
		Example: example,
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if method != "get" {
				confirmed, err := commandContext.confirm(strings.ToUpper(method) + " " + ensureAPIPath(args[0]) + "?")
				if err != nil {
					return err
				}
				if !confirmed {
					return writeActionOrAborted(commandContext, "api_"+method, args[0])
				}
			}

			loadedConfig, err := commandContext.loadConfig()
			if err != nil {
				return err
			}
			client, err := app.NewClientFromConfig(loadedConfig)
			if err != nil {
				return err
			}

			values, err := rawAPIValues(query, data)
			if err != nil {
				return err
			}
			rawJSON, err := client.RawRequest(cmd.Context(), method, ensureAPIPath(args[0]), values, rawResponse)
			if err != nil {
				return err
			}
			_, err = commandContext.output.Write(append(rawJSON, '\n'))
			return err
		},
	}

	apiMethodCommand.Flags().StringArrayVar(&query, "query", nil, "query parameter as key=value; may be repeated")
	apiMethodCommand.Flags().StringVar(&data, "data", "", "flat JSON object or key=value&key=value form data; comma form is accepted for simple values")
	apiMethodCommand.Flags().BoolVar(&rawResponse, "raw-response", false, "return full Proxmox response envelope")
	return apiMethodCommand
}

func apiMethodExample(method string) string {
	switch method {
	case "get":
		return `  boringctl api get /version
  boringctl api get /nodes/pve1/storage/local/content --query content=vztmpl
  boringctl api get /nodes/pve1/lxc/122/config`
	case "post":
		return `  boringctl api post /nodes/pve1/lxc --data '{"vmid":122,"hostname":"demo","ostemplate":"local:vztmpl/ubuntu-24.04-standard_24.04-2_amd64.tar.zst","rootfs":"local-lvm:20","net0":"name=eth0,bridge=vmbr0,type=veth,ip=dhcp"}' --yes
  boringctl api post /nodes/pve1/lxc --data 'vmid=122&hostname=demo&ostemplate=local:vztmpl/ubuntu-24.04-standard_24.04-2_amd64.tar.zst&rootfs=local-lvm:20&net0=name=eth0,bridge=vmbr0,type=veth,ip=dhcp' --yes
  boringctl api post /nodes/pve1/lxc/122/status/start --yes`
	case "put":
		return `  boringctl api put /nodes/pve1/lxc/122/config --data '{"features":"nesting=1"}' --yes
  boringctl api put /nodes/pve1/lxc/122/config --data '{"tags":"fizzy;prod"}' --yes`
	case "delete":
		return `  boringctl api delete /nodes/pve1/lxc/122 --yes
  boringctl api delete /nodes/pve1/tasks/UPID:pve1:... --yes`
	default:
		return "  boringctl api " + method + " /version"
	}
}

func (commandContext *commandContext) taskCommand() *cobra.Command {
	taskCommand := &cobra.Command{
		Use:   "task",
		Short: "Inspect and wait for Proxmox tasks",
	}
	taskCommand.AddCommand(
		commandContext.taskListCommand(),
		commandContext.taskStatusCommand(),
		commandContext.taskLogCommand(),
		commandContext.taskWaitCommand(),
		commandContext.taskStopCommand(),
	)
	return taskCommand
}

func (commandContext *commandContext) taskListCommand() *cobra.Command {
	var node string
	var source string
	var status string
	var limit int

	listCommand := &cobra.Command{
		Use:   "list",
		Short: "List recent Proxmox tasks",
		RunE: func(cmd *cobra.Command, args []string) error {
			service, err := commandContext.loadService()
			if err != nil {
				return err
			}
			tasks, err := service.ListTasks(cmd.Context(), proxmox.TaskListFilter{
				Node:   node,
				Source: source,
				Status: strings.ToLower(strings.TrimSpace(status)),
				Limit:  limit,
			})
			if err != nil {
				return err
			}
			if commandContext.jsonOutput {
				return writeJSON(commandContext.output, taskListPayload{SchemaVersion: agentOutputSchemaVersion, Tasks: tasks})
			}
			printTasks(commandContext.output, tasks)
			return nil
		},
	}
	listCommand.Flags().StringVar(&node, "node", "", "filter by node")
	listCommand.Flags().StringVar(&source, "source", "", "filter by source")
	listCommand.Flags().StringVar(&status, "status", "", "filter by status: running, ok, or error")
	listCommand.Flags().IntVar(&limit, "limit", 50, "maximum tasks to return")
	return listCommand
}

func (commandContext *commandContext) taskStatusCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "status <upid>",
		Short: "Show Proxmox task status",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			service, err := commandContext.loadService()
			if err != nil {
				return err
			}
			status, err := service.TaskStatus(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			if commandContext.jsonOutput {
				return writeJSON(commandContext.output, taskStatusPayload{SchemaVersion: agentOutputSchemaVersion, Task: status})
			}
			printTaskStatus(commandContext.output, args[0], status)
			return nil
		},
	}
}

func (commandContext *commandContext) taskLogCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "log <upid>",
		Short: "Show Proxmox task log",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			service, err := commandContext.loadService()
			if err != nil {
				return err
			}
			logEntries, err := service.TaskLog(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			if commandContext.jsonOutput {
				return writeJSON(commandContext.output, taskLogPayload{SchemaVersion: agentOutputSchemaVersion, UPID: args[0], Log: logEntries})
			}
			for _, entry := range logEntries {
				fmt.Fprintln(commandContext.output, entry.Text)
			}
			return nil
		},
	}
}

func (commandContext *commandContext) taskWaitCommand() *cobra.Command {
	var timeout time.Duration

	waitCommand := &cobra.Command{
		Use:   "wait <upid>",
		Short: "Wait for a Proxmox task to finish",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			service, err := commandContext.loadService()
			if err != nil {
				return err
			}
			status, err := service.WaitForTaskStatus(cmd.Context(), args[0], timeout)
			if err != nil {
				return err
			}
			if commandContext.jsonOutput {
				return writeJSON(commandContext.output, taskStatusPayload{SchemaVersion: agentOutputSchemaVersion, Task: status})
			}
			printTaskStatus(commandContext.output, args[0], status)
			return nil
		},
	}
	waitCommand.Flags().DurationVar(&timeout, "timeout", 5*time.Minute, "maximum time to wait")
	return waitCommand
}

func (commandContext *commandContext) taskStopCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "stop <upid>",
		Short: "Stop a running Proxmox task",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			confirmed, err := commandContext.confirm("Stop Proxmox task " + args[0] + "?")
			if err != nil {
				return err
			}
			if !confirmed {
				return writeActionOrAborted(commandContext, "task_stop", args[0])
			}
			service, err := commandContext.loadService()
			if err != nil {
				return err
			}
			if err := service.StopTask(cmd.Context(), args[0]); err != nil {
				return err
			}
			node, _ := proxmox.ParseUPIDNode(args[0])
			if commandContext.jsonOutput {
				return writeJSON(commandContext.output, taskActionPayload{SchemaVersion: agentOutputSchemaVersion, Operation: "task_stop", Status: "completed", UPID: args[0], Node: node})
			}
			fmt.Fprintf(commandContext.output, "Stopped task %s\n", args[0])
			return nil
		},
	}
}

func (commandContext *commandContext) storageCommand() *cobra.Command {
	storageCommand := &cobra.Command{
		Use:   "storage",
		Short: "Inspect and manage Proxmox storage content",
	}
	storageCommand.AddCommand(commandContext.storageListCommand(), commandContext.storageUploadCommand(), commandContext.storageDownloadURLCommand())
	return storageCommand
}

func (commandContext *commandContext) storageListCommand() *cobra.Command {
	var node string
	var storage string
	var content string
	var vmid int

	listCommand := &cobra.Command{
		Use:   "list",
		Short: "List content on a Proxmox storage",
		RunE: func(cmd *cobra.Command, args []string) error {
			if node == "" || storage == "" {
				return fmt.Errorf("--node and --storage are required")
			}
			service, err := commandContext.loadService()
			if err != nil {
				return err
			}
			contentItems, err := service.ListStorageContent(cmd.Context(), node, storage, content, vmid)
			if err != nil {
				return err
			}
			if commandContext.jsonOutput {
				return writeJSON(commandContext.output, storageContentPayload{SchemaVersion: agentOutputSchemaVersion, Node: node, Storage: storage, Content: contentItems})
			}
			printStorageContent(commandContext.output, contentItems)
			return nil
		},
	}
	listCommand.Flags().StringVar(&node, "node", "", "Proxmox node")
	listCommand.Flags().StringVar(&storage, "storage", "", "storage name")
	listCommand.Flags().StringVar(&content, "content", "", "content type filter such as iso, vztmpl, backup, images")
	listCommand.Flags().IntVar(&vmid, "vmid", 0, "filter by VMID")
	return listCommand
}

func (commandContext *commandContext) storageUploadCommand() *cobra.Command {
	request := proxmox.UploadRequest{}

	uploadCommand := &cobra.Command{
		Use:   "upload",
		Short: "Upload ISO or LXC template content to Proxmox storage",
		RunE: func(cmd *cobra.Command, args []string) error {
			if request.Node == "" || request.Storage == "" || request.Content == "" || request.FilePath == "" {
				return fmt.Errorf("--node, --storage, --content, and --file are required")
			}
			service, err := commandContext.loadService()
			if err != nil {
				return err
			}
			var steps []string
			reporter := commandContext.reporter(&steps)
			upid, err := service.UploadStorageContent(cmd.Context(), request, reporter)
			if err != nil {
				return err
			}
			if commandContext.jsonOutput {
				return writeJSON(commandContext.output, taskActionPayload{SchemaVersion: agentOutputSchemaVersion, Operation: "storage_upload", Status: "completed", UPID: upid, Node: request.Node})
			}
			fmt.Fprintf(commandContext.output, "Uploaded %s\n", request.FilePath)
			return nil
		},
	}
	uploadCommand.Flags().StringVar(&request.Node, "node", "", "Proxmox node")
	uploadCommand.Flags().StringVar(&request.Storage, "storage", "", "storage name")
	uploadCommand.Flags().StringVar(&request.Content, "content", "", "content type: iso or vztmpl")
	uploadCommand.Flags().StringVar(&request.FilePath, "file", "", "local file to upload")
	uploadCommand.Flags().StringVar(&request.Filename, "filename", "", "remote filename override")
	return uploadCommand
}

func (commandContext *commandContext) storageDownloadURLCommand() *cobra.Command {
	request := proxmox.DownloadURLRequest{VerifyCertificates: true}

	downloadCommand := &cobra.Command{
		Use:   "download-url",
		Short: "Ask Proxmox to download storage content from a URL",
		RunE: func(cmd *cobra.Command, args []string) error {
			if request.Node == "" || request.Storage == "" || request.Content == "" || request.URL == "" || request.Filename == "" {
				return fmt.Errorf("--node, --storage, --content, --url, and --filename are required")
			}
			service, err := commandContext.loadService()
			if err != nil {
				return err
			}
			var steps []string
			reporter := commandContext.reporter(&steps)
			upid, err := service.DownloadStorageContentFromURL(cmd.Context(), request, reporter)
			if err != nil {
				return err
			}
			if commandContext.jsonOutput {
				return writeJSON(commandContext.output, taskActionPayload{SchemaVersion: agentOutputSchemaVersion, Operation: "storage_download_url", Status: "completed", UPID: upid, Node: request.Node})
			}
			fmt.Fprintf(commandContext.output, "Downloaded %s\n", request.URL)
			return nil
		},
	}
	downloadCommand.Flags().StringVar(&request.Node, "node", "", "Proxmox node")
	downloadCommand.Flags().StringVar(&request.Storage, "storage", "", "storage name")
	downloadCommand.Flags().StringVar(&request.Content, "content", "", "content type: iso or vztmpl")
	downloadCommand.Flags().StringVar(&request.URL, "url", "", "source URL")
	downloadCommand.Flags().StringVar(&request.Filename, "filename", "", "remote filename")
	downloadCommand.Flags().StringVar(&request.Checksum, "checksum", "", "expected checksum")
	downloadCommand.Flags().StringVar(&request.ChecksumAlgorithm, "checksum-algorithm", "", "checksum algorithm")
	downloadCommand.Flags().BoolVar(&request.VerifyCertificates, "verify-certificates", true, "verify source URL TLS certificates")
	return downloadCommand
}

func (commandContext *commandContext) backupCommand() *cobra.Command {
	backupCommand := &cobra.Command{
		Use:   "backup",
		Short: "Create or restore Proxmox guest backups",
	}
	backupCommand.AddCommand(commandContext.backupCreateCommand(), commandContext.backupRestoreCommand())
	return backupCommand
}

func (commandContext *commandContext) backupCreateCommand() *cobra.Command {
	request := proxmox.BackupRequest{Mode: "snapshot", Compress: "zstd"}

	createCommand := &cobra.Command{
		Use:   "create <vmid>",
		Short: "Create a Proxmox vzdump backup for a VM or LXC container",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			vmid, err := parseVMID(args[0])
			if err != nil {
				return err
			}
			request.VMID = vmid
			service, err := commandContext.loadService()
			if err != nil {
				return err
			}
			var steps []string
			upid, guest, err := service.CreateGuestBackup(cmd.Context(), request, commandContext.reporter(&steps))
			if err != nil {
				return err
			}
			if commandContext.jsonOutput {
				return writeJSON(commandContext.output, taskActionPayload{SchemaVersion: agentOutputSchemaVersion, Operation: "backup_create", Status: "completed", UPID: upid, Node: guest.Node})
			}
			fmt.Fprintf(commandContext.output, "Created backup for %s %d\n", guestKindLabel(guest), vmid)
			return nil
		},
	}
	createCommand.Flags().StringVar(&request.Node, "node", "", "override backup node")
	createCommand.Flags().StringVar(&request.Storage, "storage", "", "target backup storage")
	createCommand.Flags().StringVar(&request.Mode, "mode", "snapshot", "backup mode")
	createCommand.Flags().StringVar(&request.Compress, "compress", "zstd", "compression")
	createCommand.Flags().StringVar(&request.Notes, "notes", "", "notes template")
	return createCommand
}

func (commandContext *commandContext) backupRestoreCommand() *cobra.Command {
	request := proxmox.RestoreRequest{Kind: proxmox.GuestTypeQEMU}

	restoreCommand := &cobra.Command{
		Use:   "restore",
		Short: "Restore a Proxmox backup archive as a VM or LXC container",
		RunE: func(cmd *cobra.Command, args []string) error {
			if request.Node == "" || request.VMID <= 0 || request.Archive == "" {
				return fmt.Errorf("--node, --vmid, and --archive are required")
			}
			confirmed, err := commandContext.confirm(fmt.Sprintf("Restore %s as %s %d on %s?", request.Archive, request.Kind, request.VMID, request.Node))
			if err != nil {
				return err
			}
			if !confirmed {
				return writeActionOrAborted(commandContext, "backup_restore", request.Archive)
			}
			service, err := commandContext.loadService()
			if err != nil {
				return err
			}
			var steps []string
			upid, err := service.RestoreGuestBackup(cmd.Context(), request, commandContext.reporter(&steps))
			if err != nil {
				return err
			}
			if commandContext.jsonOutput {
				return writeJSON(commandContext.output, taskActionPayload{SchemaVersion: agentOutputSchemaVersion, Operation: "backup_restore", Status: "completed", UPID: upid, Node: request.Node})
			}
			fmt.Fprintf(commandContext.output, "Restored backup as %s %d\n", request.Kind, request.VMID)
			return nil
		},
	}
	restoreCommand.Flags().StringVar(&request.Node, "node", "", "target node")
	restoreCommand.Flags().IntVar(&request.VMID, "vmid", 0, "new VMID")
	restoreCommand.Flags().StringVar(&request.Kind, "kind", proxmox.GuestTypeQEMU, "guest kind: qemu or lxc")
	restoreCommand.Flags().StringVar(&request.Archive, "archive", "", "backup volume ID")
	restoreCommand.Flags().StringVar(&request.Storage, "storage", "", "target storage")
	restoreCommand.Flags().BoolVar(&request.Force, "force", false, "overwrite existing VMID if Proxmox allows it")
	return restoreCommand
}

func (commandContext *commandContext) exportCommand() *cobra.Command {
	exportCommand := &cobra.Command{
		Use:   "export",
		Short: "Export boringctl-managed specs from Proxmox",
	}
	exportCommand.AddCommand(commandContext.exportGuestCommand())
	return exportCommand
}

func (commandContext *commandContext) exportGuestCommand() *cobra.Command {
	var format string

	guestCommand := &cobra.Command{
		Use:   "guest <vmid>",
		Short: "Export a deterministic VM/LXC guest spec",
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
			spec, err := service.ExportGuestSpec(cmd.Context(), vmid)
			if err != nil {
				return err
			}
			if commandContext.jsonOutput || format == "json" {
				return writeJSON(commandContext.output, guestSpecPayload{SchemaVersion: agentOutputSchemaVersion, Spec: spec})
			}
			output, err := yaml.Marshal(spec)
			if err != nil {
				return err
			}
			_, err = commandContext.output.Write(output)
			return err
		},
	}
	guestCommand.Flags().StringVar(&format, "format", "yaml", "output format: yaml or json")
	return guestCommand
}

func (commandContext *commandContext) applyCommand() *cobra.Command {
	var filePath string
	var dryRun bool

	applyCommand := &cobra.Command{
		Use:   "apply",
		Short: "Apply a narrow boringctl guest spec to an existing VM/LXC",
		RunE: func(cmd *cobra.Command, args []string) error {
			if filePath == "" {
				return fmt.Errorf("--file is required")
			}
			specBytes, err := os.ReadFile(filePath)
			if err != nil {
				return err
			}
			var spec app.GuestSpec
			if err := yaml.Unmarshal(specBytes, &spec); err != nil {
				return err
			}
			if !dryRun {
				confirmed, err := commandContext.confirm("Apply guest spec from " + filePath + "?")
				if err != nil {
					return err
				}
				if !confirmed {
					return writeActionOrAborted(commandContext, "apply", filePath)
				}
			}

			service, err := commandContext.loadService()
			if err != nil {
				return err
			}
			var steps []string
			result, err := service.ApplyGuestSpec(cmd.Context(), spec, dryRun, commandContext.reporter(&steps))
			if err != nil {
				return err
			}
			if commandContext.jsonOutput {
				return writeJSON(commandContext.output, guestSpecApplyPayload{SchemaVersion: agentOutputSchemaVersion, Result: result})
			}
			printApplyResult(commandContext.output, result)
			return nil
		},
	}
	applyCommand.Flags().StringVarP(&filePath, "file", "f", "", "guest spec YAML file")
	applyCommand.Flags().BoolVar(&dryRun, "dry-run", false, "show changes without applying them")
	return applyCommand
}

func rawAPIValues(query []string, data string) (url.Values, error) {
	values := url.Values{}
	for _, pair := range query {
		key, value, ok := strings.Cut(pair, "=")
		if !ok || strings.TrimSpace(key) == "" {
			return nil, fmt.Errorf("invalid parameter %q; expected key=value", pair)
		}
		values.Add(strings.TrimSpace(key), strings.TrimSpace(value))
	}
	if strings.TrimSpace(data) == "" {
		return values, nil
	}

	if strings.HasPrefix(strings.TrimSpace(data), "{") {
		dataValues := map[string]any{}
		if err := json.Unmarshal([]byte(data), &dataValues); err != nil {
			return nil, err
		}
		for key, value := range dataValues {
			values.Set(key, fmt.Sprint(value))
		}
		return values, nil
	}

	separator := ","
	if strings.Contains(data, "&") {
		separator = "&"
	}
	for _, pair := range strings.Split(data, separator) {
		key, value, ok := strings.Cut(pair, "=")
		if !ok || strings.TrimSpace(key) == "" {
			return nil, fmt.Errorf("invalid data parameter %q; expected key=value", pair)
		}
		values.Set(strings.TrimSpace(key), strings.TrimSpace(value))
	}
	return values, nil
}

func ensureAPIPath(path string) string {
	if strings.HasPrefix(path, "/") {
		return path
	}
	return "/" + path
}

func (commandContext *commandContext) reporter(steps *[]string) app.Reporter {
	if commandContext.jsonOutput {
		return func(message string) {
			*steps = append(*steps, message)
		}
	}
	return commandContext.printStep
}

func writeActionOrAborted(commandContext *commandContext, operation string, name string) error {
	if commandContext.jsonOutput {
		return writeJSON(commandContext.output, actionPayload{
			SchemaVersion: agentOutputSchemaVersion,
			Operation:     operation,
			Status:        "aborted",
			Name:          name,
		})
	}
	fmt.Fprintln(commandContext.output, "Aborted.")
	return nil
}

func printTasks(output io.Writer, tasks []proxmox.Task) {
	fmt.Fprintln(output, "NODE\tTYPE\tID\tSTATUS\tEXIT\tUSER")
	for _, task := range tasks {
		fmt.Fprintf(output, "%s\t%s\t%s\t%s\t%s\t%s\n", task.Node, task.Type, task.ID, task.Status, emptyDash(task.ExitStatus), task.User)
	}
}

func printTaskStatus(output io.Writer, upid string, status proxmox.TaskStatus) {
	fmt.Fprintf(output, "Task: %s\n", upid)
	fmt.Fprintf(output, "Status: %s\n", status.Status)
	fmt.Fprintf(output, "Exit:   %s\n", emptyDash(status.ExitStatus))
	fmt.Fprintf(output, "Type:   %s\n", emptyDash(status.Type))
}

func printStorageContent(output io.Writer, content []proxmox.StorageContent) {
	sort.Slice(content, func(leftIndex int, rightIndex int) bool {
		return content[leftIndex].VolumeID < content[rightIndex].VolumeID
	})
	fmt.Fprintln(output, "VOLUME\tCONTENT\tSIZE\tFORMAT")
	for _, item := range content {
		fmt.Fprintf(output, "%s\t%s\t%s\t%s\n", item.VolumeID, item.Content, formatBytes(item.Size), emptyDash(item.Format))
	}
}

func printApplyResult(output io.Writer, result app.GuestSpecApplyResult) {
	fmt.Fprintf(output, "%s %d on %s: %s\n", result.Kind, result.VMID, result.Node, result.Action)
	if len(result.Changes) == 0 {
		return
	}
	for _, change := range result.Changes {
		fmt.Fprintf(output, "  ~ %s: %s -> %s\n", change.Key, emptyDash(change.From), change.To)
	}
}

func emptyDash(value string) string {
	if strings.TrimSpace(value) == "" {
		return "-"
	}
	return value
}
