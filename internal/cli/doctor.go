package cli

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/boring-labs/boringctl/internal/app"
	"github.com/boring-labs/boringctl/internal/config"

	"github.com/spf13/cobra"
)

type doctorPayload struct {
	SchemaVersion int               `json:"schema_version"`
	Status        string            `json:"status"`
	CheckedAt     time.Time         `json:"checked_at"`
	ConfigPath    string            `json:"config_path"`
	Endpoint      string            `json:"endpoint"`
	Checks        []app.DoctorCheck `json:"checks"`
}

type doctorFailureError struct {
	count int
}

func (err doctorFailureError) Error() string {
	return fmt.Sprintf("doctor found %d required check failures", err.count)
}

func (commandContext *commandContext) doctorCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Check local configuration and Proxmox integration health",
		Long: `Run read-only checks for credentials, TLS, Proxmox inventory drift, node SSH,
QEMU guest agents, and the optional local Caddy tree. Warnings are reported without
failing the command; required check failures return a non-zero exit status.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			loadedConfig, configPath, err := config.LoadProfile(commandContext.configPath, commandContext.profile)
			if err != nil {
				return err
			}

			client, clientSetupError := app.NewHealthAwareClientFromConfig(loadedConfig)
			report := app.NewService(loadedConfig, client).Doctor(cmd.Context(), configPath, clientSetupError)
			if commandContext.jsonOutput {
				err = writeJSON(commandContext.output, doctorJSONPayload(report))
			} else {
				err = printDoctorReport(commandContext.output, report)
			}
			if err != nil {
				return err
			}
			if failureCount := report.FailureCount(); failureCount > 0 {
				return doctorFailureError{count: failureCount}
			}
			return nil
		},
	}
}

func doctorJSONPayload(report app.DoctorReport) doctorPayload {
	return doctorPayload{
		SchemaVersion: agentOutputSchemaVersion,
		Status:        report.Status,
		CheckedAt:     report.CheckedAt,
		ConfigPath:    report.ConfigPath,
		Endpoint:      report.Endpoint,
		Checks:        report.Checks,
	}
}

func printDoctorReport(output io.Writer, report app.DoctorReport) error {
	if _, err := fmt.Fprintf(output, "boringctl doctor: %s\n", strings.ToUpper(report.Status)); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(output, "Config:   %s\n", report.ConfigPath); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(output, "Endpoint: %s\n\n", report.Endpoint); err != nil {
		return err
	}

	for _, check := range report.Checks {
		if _, err := fmt.Fprintf(output, "%-4s %-16s %s\n", strings.ToUpper(check.Status), check.Name, check.Message); err != nil {
			return err
		}
		for _, detail := range check.Details {
			if _, err := fmt.Fprintf(output, "     %s\n", detail); err != nil {
				return err
			}
		}
	}

	return nil
}
