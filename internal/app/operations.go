package app

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/boring-dragon/boringctl/internal/proxmox"
)

type GuestSpec struct {
	Kind   string         `json:"kind" yaml:"kind"`
	VMID   int            `json:"vmid,omitempty" yaml:"vmid,omitempty"`
	Name   string         `json:"name,omitempty" yaml:"name,omitempty"`
	Node   string         `json:"node,omitempty" yaml:"node,omitempty"`
	State  string         `json:"state,omitempty" yaml:"state,omitempty"`
	Config map[string]any `json:"config" yaml:"config"`
}

type GuestSpecChange struct {
	Key  string `json:"key" yaml:"key"`
	From string `json:"from,omitempty" yaml:"from,omitempty"`
	To   string `json:"to" yaml:"to"`
}

type GuestSpecApplyResult struct {
	VMID    int               `json:"vmid" yaml:"vmid"`
	Name    string            `json:"name" yaml:"name"`
	Kind    string            `json:"kind" yaml:"kind"`
	Node    string            `json:"node" yaml:"node"`
	DryRun  bool              `json:"dry_run" yaml:"dry_run"`
	Action  string            `json:"action" yaml:"action"`
	Changes []GuestSpecChange `json:"changes" yaml:"changes"`
	Steps   []string          `json:"steps,omitempty" yaml:"steps,omitempty"`
}

func (service *Service) ListTasks(ctx context.Context, filter proxmox.TaskListFilter) ([]proxmox.Task, error) {
	return service.Client.Tasks(ctx, filter)
}

func (service *Service) TaskStatus(ctx context.Context, upid string) (proxmox.TaskStatus, error) {
	node, err := proxmox.ParseUPIDNode(upid)
	if err != nil {
		return proxmox.TaskStatus{}, err
	}

	return service.Client.TaskStatus(ctx, node, upid)
}

func (service *Service) TaskLog(ctx context.Context, upid string) ([]proxmox.TaskLogEntry, error) {
	return service.Client.TaskLog(ctx, upid)
}

func (service *Service) StopTask(ctx context.Context, upid string) error {
	return service.Client.StopTask(ctx, upid)
}

func (service *Service) WaitForTaskStatus(ctx context.Context, upid string, timeout time.Duration) (proxmox.TaskStatus, error) {
	node, err := proxmox.ParseUPIDNode(upid)
	if err != nil {
		return proxmox.TaskStatus{}, err
	}

	return service.Client.WaitForTaskWithTimeout(ctx, node, upid, timeout)
}

func (service *Service) RollbackSnapshot(ctx context.Context, vmid int, name string, reporter Reporter) error {
	guest, err := service.FindGuest(ctx, vmid)
	if err != nil {
		return err
	}

	report(reporter, fmt.Sprintf("Rolling back snapshot %s on %s %d", name, guestKindLabel(guest), vmid))
	task, err := service.Client.RollbackGuestSnapshot(ctx, guest.Node, guest.GuestType(), vmid, name)
	if err != nil {
		return err
	}

	return service.Client.WaitForTask(ctx, guest.Node, task)
}

func (service *Service) ExportGuestSpec(ctx context.Context, vmid int) (GuestSpec, error) {
	guest, config, err := service.ShowGuest(ctx, vmid)
	if err != nil {
		return GuestSpec{}, err
	}

	return GuestSpec{
		Kind:   guest.GuestType(),
		VMID:   guest.VMID,
		Name:   guest.Name,
		Node:   guest.Node,
		State:  guest.Status,
		Config: stableGuestConfig(config),
	}, nil
}

func (service *Service) ApplyGuestSpec(ctx context.Context, spec GuestSpec, dryRun bool, reporter Reporter) (GuestSpecApplyResult, error) {
	guest, currentConfig, err := service.resolveGuestSpecTarget(ctx, spec)
	if err != nil {
		return GuestSpecApplyResult{}, err
	}
	if spec.Kind != "" && spec.Kind != guest.GuestType() {
		return GuestSpecApplyResult{}, fmt.Errorf("spec kind %q does not match existing guest kind %q", spec.Kind, guest.GuestType())
	}

	configChanges := guestSpecChanges(currentConfig, spec.Config)
	result := GuestSpecApplyResult{
		VMID:    guest.VMID,
		Name:    guest.Name,
		Kind:    guest.GuestType(),
		Node:    guest.Node,
		DryRun:  dryRun,
		Action:  "noop",
		Changes: configChanges,
	}

	if len(configChanges) > 0 {
		result.Action = "update"
	}
	desiredState := strings.ToLower(strings.TrimSpace(spec.State))
	if desiredState != "" && desiredState != strings.ToLower(guest.Status) {
		if desiredState != "running" && desiredState != "stopped" {
			return GuestSpecApplyResult{}, fmt.Errorf("state must be running or stopped, got %q", spec.State)
		}
		result.Action = "update"
		result.Changes = append(result.Changes, GuestSpecChange{Key: "state", From: guest.Status, To: desiredState})
	}
	if dryRun {
		return result, nil
	}

	var steps []string
	reportAndRecord := func(message string) {
		steps = append(steps, message)
		report(reporter, message)
	}

	if len(configChanges) > 0 {
		values := url.Values{}
		for _, change := range configChanges {
			values.Set(change.Key, change.To)
		}
		reportAndRecord(fmt.Sprintf("Updating config on %s %d", guestKindLabel(guest), guest.VMID))
		if err := service.Client.SetGuestConfig(ctx, guest.Node, guest.GuestType(), guest.VMID, values); err != nil {
			return GuestSpecApplyResult{}, err
		}
	}

	if desiredState != "" && desiredState != strings.ToLower(guest.Status) {
		switch desiredState {
		case "running":
			if err := service.Lifecycle(ctx, guest.VMID, "start", reportAndRecord); err != nil {
				return GuestSpecApplyResult{}, err
			}
			result.Action = "update"
		case "stopped":
			if err := service.Lifecycle(ctx, guest.VMID, "stop", reportAndRecord); err != nil {
				return GuestSpecApplyResult{}, err
			}
			result.Action = "update"
		default:
			return GuestSpecApplyResult{}, fmt.Errorf("state must be running or stopped, got %q", spec.State)
		}
	}

	result.Steps = steps
	return result, nil
}

func (service *Service) ListStorageContent(ctx context.Context, node string, storage string, content string, vmid int) ([]proxmox.StorageContent, error) {
	return service.Client.StorageContent(ctx, node, storage, proxmox.StorageContentFilter{Content: content, VMID: vmid})
}

func (service *Service) UploadStorageContent(ctx context.Context, request proxmox.UploadRequest, reporter Reporter) (string, error) {
	report(reporter, fmt.Sprintf("Uploading %s to %s/%s", request.FilePath, request.Node, request.Storage))
	task, err := service.Client.UploadStorageContent(ctx, request)
	if err != nil {
		return "", err
	}
	if task == "" {
		return "", nil
	}

	return task, service.Client.WaitForTask(ctx, request.Node, task)
}

func (service *Service) DownloadStorageContentFromURL(ctx context.Context, request proxmox.DownloadURLRequest, reporter Reporter) (string, error) {
	report(reporter, fmt.Sprintf("Downloading %s to %s/%s", request.URL, request.Node, request.Storage))
	task, err := service.Client.DownloadStorageContentFromURL(ctx, request)
	if err != nil {
		return "", err
	}

	return task, service.Client.WaitForTask(ctx, request.Node, task)
}

func (service *Service) CreateGuestBackup(ctx context.Context, request proxmox.BackupRequest, reporter Reporter) (string, proxmox.VMResource, error) {
	guest, err := service.FindGuest(ctx, request.VMID)
	if err != nil {
		return "", proxmox.VMResource{}, err
	}
	if request.Node == "" {
		request.Node = guest.Node
	}

	report(reporter, fmt.Sprintf("Creating backup for %s %d on %s", guestKindLabel(guest), guest.VMID, request.Node))
	task, err := service.Client.CreateBackup(ctx, request)
	if err != nil {
		return "", proxmox.VMResource{}, err
	}

	return task, guest, service.Client.WaitForTask(ctx, request.Node, task)
}

func (service *Service) RestoreGuestBackup(ctx context.Context, request proxmox.RestoreRequest, reporter Reporter) (string, error) {
	report(reporter, fmt.Sprintf("Restoring %s as %s %d on %s", request.Archive, request.Kind, request.VMID, request.Node))
	task, err := service.Client.RestoreBackup(ctx, request)
	if err != nil {
		return "", err
	}

	return task, service.Client.WaitForTask(ctx, request.Node, task)
}

func (service *Service) resolveGuestSpecTarget(ctx context.Context, spec GuestSpec) (proxmox.VMResource, map[string]any, error) {
	if spec.VMID > 0 {
		return service.ShowGuest(ctx, spec.VMID)
	}

	name := strings.TrimSpace(spec.Name)
	if name == "" {
		return proxmox.VMResource{}, nil, fmt.Errorf("spec must include vmid or name")
	}

	guests, err := service.ListGuests(ctx)
	if err != nil {
		return proxmox.VMResource{}, nil, err
	}

	matches := make([]proxmox.VMResource, 0, 1)
	for _, guest := range guests {
		if guest.Name == name {
			matches = append(matches, guest)
		}
	}
	if len(matches) == 0 {
		return proxmox.VMResource{}, nil, fmt.Errorf("guest named %q was not found", name)
	}
	if len(matches) > 1 {
		return proxmox.VMResource{}, nil, fmt.Errorf("guest name %q is ambiguous; use vmid", name)
	}

	config, err := service.Client.GuestConfig(ctx, matches[0].Node, matches[0].GuestType(), matches[0].VMID)
	if err != nil {
		return proxmox.VMResource{}, nil, err
	}

	return matches[0], config, nil
}

func stableGuestConfig(config map[string]any) map[string]any {
	stableConfig := make(map[string]any, len(config))
	for key, value := range config {
		if volatileGuestConfigKey(key) {
			continue
		}
		stableConfig[key] = value
	}
	return stableConfig
}

func volatileGuestConfigKey(key string) bool {
	switch key {
	case "digest", "lock", "pending", "snapshots", "template", "vmgenid":
		return true
	default:
		return strings.HasPrefix(key, "unused")
	}
}

func guestSpecChanges(current map[string]any, desired map[string]any) []GuestSpecChange {
	if len(desired) == 0 {
		return nil
	}

	keys := make([]string, 0, len(desired))
	for key := range desired {
		if volatileGuestConfigKey(key) {
			continue
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)

	changes := make([]GuestSpecChange, 0, len(keys))
	for _, key := range keys {
		desiredValue := scalarString(desired[key])
		currentValue := scalarString(current[key])
		if currentValue == desiredValue {
			continue
		}
		changes = append(changes, GuestSpecChange{Key: key, From: currentValue, To: desiredValue})
	}

	return changes
}

func scalarString(value any) string {
	switch typedValue := value.(type) {
	case nil:
		return ""
	case string:
		return typedValue
	case int:
		return strconv.Itoa(typedValue)
	case int64:
		return strconv.FormatInt(typedValue, 10)
	case float64:
		if typedValue == float64(int64(typedValue)) {
			return strconv.FormatInt(int64(typedValue), 10)
		}
		return strconv.FormatFloat(typedValue, 'f', -1, 64)
	case bool:
		if typedValue {
			return "1"
		}
		return "0"
	default:
		return fmt.Sprint(typedValue)
	}
}
