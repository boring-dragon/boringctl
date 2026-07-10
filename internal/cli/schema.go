package cli

import (
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type cliSchemaPayload struct {
	SchemaVersion int                   `json:"schema_version"`
	CLISpec       string                `json:"clispec"`
	Name          string                `json:"name"`
	Description   string                `json:"description"`
	GlobalArgs    []cliSchemaArgPayload `json:"global_args"`
	Commands      []cliSchemaCommand    `json:"commands"`
	Errors        []cliSchemaError      `json:"errors"`
}

type cliSchemaCommand struct {
	Name        string                `json:"name"`
	Description string                `json:"description,omitempty"`
	Args        []cliSchemaArgPayload `json:"args,omitempty"`
	Mutating    bool                  `json:"mutating"`
	Idempotent  bool                  `json:"idempotent"`
	Dangerous   bool                  `json:"dangerous"`
	Async       bool                  `json:"async_capable"`
	Examples    []string              `json:"examples,omitempty"`
}

type cliSchemaArgPayload struct {
	Name        string   `json:"name"`
	Short       string   `json:"short,omitempty"`
	Type        string   `json:"type"`
	Description string   `json:"description,omitempty"`
	Default     string   `json:"default,omitempty"`
	Enum        []string `json:"enum,omitempty"`
}

type cliSchemaError struct {
	Kind        string `json:"kind"`
	Retryable   bool   `json:"retryable"`
	Description string `json:"description"`
}

type cliSchemaMetadata struct {
	Mutating   bool
	Idempotent bool
	Dangerous  bool
	Async      bool
	Examples   []string
}

func (commandContext *commandContext) schemaCommand(rootCommand *cobra.Command) *cobra.Command {
	return &cobra.Command{
		Use:   "schema [command-prefix]",
		Short: "Print machine-readable CLI schema for agents",
		Args:  cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			prefix := strings.Join(args, " ")
			payload := buildCLISchema(rootCommand, prefix)
			return writeJSON(commandContext.output, payload)
		},
	}
}

func buildCLISchema(rootCommand *cobra.Command, prefix string) cliSchemaPayload {
	commands := make([]cliSchemaCommand, 0)
	walkSchemaCommands(rootCommand, "", strings.TrimSpace(prefix), &commands)

	return cliSchemaPayload{
		SchemaVersion: agentOutputSchemaVersion,
		CLISpec:       "0.1",
		Name:          "boringctl",
		Description:   "Homelab cloud control for Proxmox VMs, LXC containers, Caddy routes, tasks, storage, and backups",
		GlobalArgs: []cliSchemaArgPayload{
			{Name: "--config", Type: "string", Description: "config file path"},
			{Name: "--profile", Type: "string", Description: "config profile name from ~/.config/boringctl/<profile>.yaml"},
			{Name: "--output", Short: "-o", Type: "string", Enum: []string{"auto", "text", "json"}, Default: "auto", Description: "output format"},
			{Name: "--json", Type: "boolean", Description: "force machine-readable JSON output"},
			{Name: "--yes", Short: "-y", Type: "boolean", Description: "skip confirmation prompts for destructive operations"},
		},
		Commands: commands,
		Errors: []cliSchemaError{
			{Kind: "auth", Retryable: false, Description: "Proxmox authentication failed"},
			{Kind: "not_found", Retryable: false, Description: "Requested Proxmox resource was not found"},
			{Kind: "conflict", Retryable: false, Description: "Requested operation conflicts with current state"},
			{Kind: "api", Retryable: true, Description: "Proxmox API request failed"},
			{Kind: "task_failed", Retryable: true, Description: "Proxmox asynchronous task failed"},
			{Kind: "timeout", Retryable: true, Description: "Operation timed out or was cancelled"},
			{Kind: "confirmation_required", Retryable: false, Description: "Mutating command requires --yes in JSON mode"},
			{Kind: "doctor_failed", Retryable: false, Description: "One or more required doctor checks failed"},
			{Kind: "usage", Retryable: false, Description: "Invalid command usage"},
			{Kind: "other", Retryable: false, Description: "General error"},
		},
	}
}

func walkSchemaCommands(command *cobra.Command, prefix string, filter string, commands *[]cliSchemaCommand) {
	for _, child := range command.Commands() {
		if child.Hidden {
			continue
		}

		path := child.Name()
		if prefix != "" {
			path = prefix + " " + path
		}
		if child.HasSubCommands() {
			walkSchemaCommands(child, path, filter, commands)
			continue
		}
		if filter != "" && path != filter && !strings.HasPrefix(path, filter+" ") {
			continue
		}

		metadata := schemaMetadata()[path]
		if !metadata.Mutating && !metadata.Dangerous {
			metadata.Idempotent = true
		}
		*commands = append(*commands, cliSchemaCommand{
			Name:        path,
			Description: child.Short,
			Args:        schemaArgs(child),
			Mutating:    metadata.Mutating,
			Idempotent:  metadata.Idempotent,
			Dangerous:   metadata.Dangerous,
			Async:       metadata.Async,
			Examples:    metadata.Examples,
		})
	}
}

func schemaArgs(command *cobra.Command) []cliSchemaArgPayload {
	args := make([]cliSchemaArgPayload, 0)
	command.NonInheritedFlags().VisitAll(func(flag *pflag.Flag) {
		args = append(args, cliSchemaArgPayload{
			Name:        "--" + flag.Name,
			Short:       shortFlag(flag),
			Type:        schemaFlagType(flag),
			Description: flag.Usage,
			Default:     flag.DefValue,
			Enum:        enumForFlag(flag),
		})
	})
	return args
}

func shortFlag(flag *pflag.Flag) string {
	if flag.Shorthand == "" {
		return ""
	}
	return "-" + flag.Shorthand
}

func schemaFlagType(flag *pflag.Flag) string {
	switch flag.Value.Type() {
	case "bool":
		return "boolean"
	case "int":
		return "integer"
	case "duration":
		return "string"
	case "stringArray", "stringSlice":
		return "array"
	default:
		return "string"
	}
}

func enumForFlag(flag *pflag.Flag) []string {
	switch flag.Name {
	case "output":
		return []string{"auto", "text", "json"}
	case "kind":
		return []string{"qemu", "lxc"}
	case "status":
		return []string{"running", "stopped", "ok", "error"}
	default:
		return nil
	}
}

func schemaMetadata() map[string]cliSchemaMetadata {
	return map[string]cliSchemaMetadata{
		"create":               {Mutating: true, Idempotent: false, Async: true, Examples: []string{"boringctl create --node pve1 --image debian-13 --plan tiny --name api-01 --storage local-lvm --ssh-key default"}},
		"create-lxc":           {Mutating: true, Idempotent: false, Async: true, Examples: []string{"boringctl create-lxc --node pve1 --image ubuntu-24.04 --plan medium --name app-01 --storage local-lvm --docker --dry-run", "boringctl --output json create-lxc --node pve1 --image debian-13 --cores 2 --memory 4096 --disk 25 --name worker-01 --storage local-lvm --feature nesting=1 --dry-run"}},
		"start":                {Mutating: true, Idempotent: true, Async: true},
		"stop":                 {Mutating: true, Idempotent: true, Async: true},
		"reboot":               {Mutating: true, Idempotent: false, Async: true},
		"delete":               {Mutating: true, Idempotent: false, Dangerous: true, Async: true},
		"resize":               {Mutating: true, Idempotent: false, Dangerous: true, Async: true},
		"rename":               {Mutating: true, Idempotent: true},
		"tags":                 {Mutating: true, Idempotent: true},
		"snapshot":             {Mutating: true, Idempotent: false, Dangerous: true, Async: true},
		"api post":             {Mutating: true, Dangerous: true, Examples: []string{"boringctl api post /nodes/pve1/lxc/122/status/start --yes"}},
		"api put":              {Mutating: true, Dangerous: true, Examples: []string{"boringctl api put /nodes/pve1/lxc/122/config --data '{\"features\":\"nesting=1\"}' --yes"}},
		"api delete":           {Mutating: true, Dangerous: true},
		"task wait":            {Async: true},
		"task stop":            {Mutating: true, Dangerous: true},
		"storage upload":       {Mutating: true, Async: true},
		"storage download-url": {Mutating: true, Async: true},
		"backup create":        {Mutating: true, Async: true},
		"backup restore":       {Mutating: true, Dangerous: true, Async: true},
		"apply":                {Mutating: true, Idempotent: true, Async: true},
		"init-config":          {Mutating: true, Idempotent: false, Dangerous: true},
		"ssh":                  {Mutating: true, Idempotent: false, Dangerous: true},
		"ssh-config":           {Mutating: true, Idempotent: true},
		"shell":                {Mutating: true, Idempotent: false, Dangerous: true},
		"caddy add-site":       {Mutating: true},
		"caddy edit-site":      {Mutating: true},
		"caddy remove-site":    {Mutating: true, Dangerous: true},
		"caddy deploy":         {Mutating: true, Dangerous: true, Async: true},
		"caddy check":          {Mutating: true, Idempotent: true},
		"caddy rollback":       {Mutating: true, Dangerous: true, Async: true},
	}
}
