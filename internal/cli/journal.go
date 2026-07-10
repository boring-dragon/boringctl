package cli

import (
	"fmt"
	"time"

	"github.com/boring-labs/boringctl/internal/app"

	"github.com/spf13/cobra"
)

type operationJournalPayload struct {
	SchemaVersion int                         `json:"schema_version"`
	Limit         int                         `json:"limit"`
	Entries       []app.OperationJournalEntry `json:"entries"`
}

func (commandContext *commandContext) journalCommand() *cobra.Command {
	var limit int

	journalCommand := &cobra.Command{
		Use:   "journal",
		Short: "Show recent local boringctl operations",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			entries, err := app.LoadOperationJournal()
			if err != nil {
				return err
			}

			if limit <= 0 {
				limit = 20
			}
			if len(entries) > limit {
				entries = entries[len(entries)-limit:]
			}
			entries = newestOperationJournalEntries(entries)

			if commandContext.jsonOutput {
				return writeJSON(commandContext.output, operationJournalPayload{
					SchemaVersion: agentOutputSchemaVersion,
					Limit:         limit,
					Entries:       entries,
				})
			}

			fmt.Fprintln(commandContext.output, "OCCURRED_AT\tOPERATION\tSTATUS\tTARGET\tSUMMARY")
			for _, entry := range entries {
				fmt.Fprintf(
					commandContext.output,
					"%s\t%s\t%s\t%s\t%s\n",
					entry.OccurredAt.Local().Format(time.DateTime),
					entry.Operation,
					entry.Status,
					entry.Target,
					entry.Summary,
				)
			}

			return nil
		},
	}

	journalCommand.Flags().IntVar(&limit, "limit", 20, "number of recent operations to show")
	return journalCommand
}

func newestOperationJournalEntries(entries []app.OperationJournalEntry) []app.OperationJournalEntry {
	newestEntries := make([]app.OperationJournalEntry, 0, len(entries))
	for entryIndex := len(entries) - 1; entryIndex >= 0; entryIndex-- {
		newestEntries = append(newestEntries, entries[entryIndex])
	}
	return newestEntries
}
