package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

const maxOperationJournalEntries = 200

type OperationJournalEntry struct {
	Operation  string    `json:"operation"`
	Status     string    `json:"status"`
	Target     string    `json:"target,omitempty"`
	Summary    string    `json:"summary,omitempty"`
	OccurredAt time.Time `json:"occurred_at"`
}

func AppendOperationJournal(entry OperationJournalEntry) (resultErr error) {
	journalPath, err := DefaultOperationJournalPath()
	if err != nil {
		return err
	}

	journalDirectory := filepath.Dir(journalPath)
	if err := os.MkdirAll(journalDirectory, 0o700); err != nil {
		return err
	}
	if err := os.Chmod(journalDirectory, 0o700); err != nil {
		return err
	}
	journalLock, err := acquireLocalFileLock(filepath.Join(journalDirectory, "operations.lock"))
	if err != nil {
		return err
	}
	defer func() {
		if err := journalLock.Close(); err != nil && resultErr == nil {
			resultErr = fmt.Errorf("release operation journal lock: %w", err)
		}
	}()

	entries, err := LoadOperationJournal()
	if err != nil {
		return err
	}

	if entry.OccurredAt.IsZero() {
		entry.OccurredAt = time.Now()
	}
	entries = append(entries, entry)
	if len(entries) > maxOperationJournalEntries {
		entries = entries[len(entries)-maxOperationJournalEntries:]
	}

	fileContents, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return err
	}

	temporaryFile, err := os.CreateTemp(journalDirectory, ".operations-*.json")
	if err != nil {
		return err
	}
	temporaryPath := temporaryFile.Name()
	defer os.Remove(temporaryPath)

	if err := temporaryFile.Chmod(0o600); err != nil {
		temporaryFile.Close()
		return err
	}
	if _, err := temporaryFile.Write(append(fileContents, '\n')); err != nil {
		temporaryFile.Close()
		return err
	}
	if err := temporaryFile.Sync(); err != nil {
		temporaryFile.Close()
		return err
	}
	if err := temporaryFile.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporaryPath, journalPath); err != nil {
		return err
	}

	return nil
}

func LoadOperationJournal() ([]OperationJournalEntry, error) {
	journalPath, err := DefaultOperationJournalPath()
	if err != nil {
		return nil, err
	}

	fileContents, err := os.ReadFile(journalPath)
	if errors.Is(err, os.ErrNotExist) {
		return []OperationJournalEntry{}, nil
	}
	if err != nil {
		return nil, err
	}

	var entries []OperationJournalEntry
	if err := json.Unmarshal(fileContents, &entries); err != nil {
		return nil, fmt.Errorf("parse operation journal: %w", err)
	}

	sort.SliceStable(entries, func(leftIndex int, rightIndex int) bool {
		return entries[leftIndex].OccurredAt.Before(entries[rightIndex].OccurredAt)
	})

	return entries, nil
}

func DefaultOperationJournalPath() (string, error) {
	homeDirectory, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}

	return filepath.Join(homeDirectory, ".config", "boringctl", "operations.json"), nil
}

func recordOperation(reporter Reporter, entry OperationJournalEntry) {
	if err := AppendOperationJournal(entry); err != nil {
		report(reporter, fmt.Sprintf("Warning: operation journal could not be updated: %v", err))
	}
}
