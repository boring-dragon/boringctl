package app

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"time"
)

const maxHistoryEntries = 200

type CreateHistoryEntry struct {
	VMID       int       `json:"vmid"`
	Name       string    `json:"name"`
	Node       string    `json:"node"`
	Image      string    `json:"image"`
	ImageLabel string    `json:"image_label"`
	Plan       string    `json:"plan"`
	Storage    string    `json:"storage"`
	IP         string    `json:"ip,omitempty"`
	SSHCommand string    `json:"ssh_command,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
}

func AppendCreateHistory(result CreateResult) error {
	historyPath, err := DefaultHistoryPath()
	if err != nil {
		return err
	}

	entries, err := LoadCreateHistory()
	if err != nil {
		return err
	}

	entries = append(entries, CreateHistoryEntry{
		VMID:       result.VMID,
		Name:       result.Name,
		Node:       result.Node,
		Image:      result.Image,
		ImageLabel: result.ImageLabel,
		Plan:       result.Plan,
		Storage:    result.Storage,
		IP:         result.IP,
		SSHCommand: result.SSHCommand,
		CreatedAt:  time.Now(),
	})

	if len(entries) > maxHistoryEntries {
		entries = entries[len(entries)-maxHistoryEntries:]
	}

	if err := os.MkdirAll(filepath.Dir(historyPath), 0o700); err != nil {
		return err
	}

	fileContents, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(historyPath, append(fileContents, '\n'), 0o600)
}

func LoadCreateHistory() ([]CreateHistoryEntry, error) {
	historyPath, err := DefaultHistoryPath()
	if err != nil {
		return nil, err
	}

	fileContents, err := os.ReadFile(historyPath)
	if errors.Is(err, os.ErrNotExist) {
		return []CreateHistoryEntry{}, nil
	}
	if err != nil {
		return nil, err
	}

	var entries []CreateHistoryEntry
	if err := json.Unmarshal(fileContents, &entries); err != nil {
		return nil, err
	}

	sort.SliceStable(entries, func(leftIndex int, rightIndex int) bool {
		return entries[leftIndex].CreatedAt.Before(entries[rightIndex].CreatedAt)
	})

	return entries, nil
}

func DefaultHistoryPath() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}

	return filepath.Join(homeDir, ".config", "boringctl", "history.json"), nil
}
