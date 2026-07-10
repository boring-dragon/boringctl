package cli

import (
	"fmt"

	"github.com/boring-dragon/boringctl/internal/version"

	"github.com/spf13/cobra"
)

type versionPayload struct {
	SchemaVersion int `json:"schema_version"`
	version.Info
}

func (commandContext *commandContext) versionCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Show boringctl build information",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			info := version.Current()
			if commandContext.jsonOutput {
				return writeJSON(commandContext.output, versionPayload{
					SchemaVersion: agentOutputSchemaVersion,
					Info:          info,
				})
			}

			fmt.Fprintf(commandContext.output, "boringctl %s\n", info.Version)
			fmt.Fprintf(commandContext.output, "commit: %s\n", info.Commit)
			fmt.Fprintf(commandContext.output, "built:  %s\n", info.BuildDate)
			fmt.Fprintf(commandContext.output, "go:     %s\n", info.GoVersion)
			fmt.Fprintf(commandContext.output, "target: %s/%s\n", info.OS, info.Arch)
			return nil
		},
	}
}
