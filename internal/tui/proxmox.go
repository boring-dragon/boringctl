package tui

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/boring-labs/boringctl/internal/proxmox"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"gopkg.in/yaml.v3"
)

const maxTaskLogLines = 80

func (model model) proxmoxMenuItems() []list.Item {
	return []list.Item{
		item{title: "Config and Health", description: "Show resolved config metadata and current cluster status", value: "config"},
		item{title: "Node Metrics", description: "Show one hour of CPU, memory, disk, and network history", value: "metrics"},
		item{title: "Recent Tasks", description: "Browse recent Proxmox tasks and open task logs", value: "tasks"},
		item{title: "Storage Content", description: "Choose a node and storage to inspect ISO, template, backup, and disk volumes", value: "storage"},
		item{title: "Agent Commands", description: "Show stable JSON commands useful for automation and AI agents", value: "agent"},
		item{title: "Refresh Health", description: "Refresh the live Proxmox status panel", value: "refresh"},
	}
}

func (model *model) selectProxmoxMenuItem(value string) (tea.Model, tea.Cmd) {
	switch value {
	case "config":
		model.infoTitle = "Config and Health"
		model.infoBody = model.proxmoxConfigBody()
		model.infoBackScreen = screenProxmoxMenu
		model.screen = screenProxmoxInfo
		return *model, nil
	case "tasks":
		model.screen = screenProxmoxLoading
		model.loadingMessage = "Loading recent Proxmox tasks..."
		return *model, model.loadTasksCommand()
	case "metrics":
		model.metricsNode = ""
		model.nodeMetrics = nil
		model.nodeMetricsErr = ""
		model.screen = screenProxmoxMetricsNode
		model.setList("Choose Metrics Node", model.nodeItems())
		return *model, nil
	case "storage":
		model.storageNode = ""
		model.storageName = ""
		model.storageContent = nil
		model.screen = screenProxmoxStorageNode
		model.setList("Choose Storage Node", model.nodeItems())
		return *model, nil
	case "agent":
		model.infoTitle = "Agent Commands"
		model.infoBody = model.agentCommandBody()
		model.infoBackScreen = screenProxmoxMenu
		model.screen = screenProxmoxInfo
		return *model, nil
	case "refresh":
		model.checkingHealth = true
		return *model, model.healthCommand()
	default:
		return *model, nil
	}
}

func (model model) loadTasksCommand() tea.Cmd {
	service := model.service

	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()

		tasks, err := service.ListTasks(ctx, proxmox.TaskListFilter{Limit: 50})
		return tasksLoadedMsg{tasks: tasks, err: err}
	}
}

func (model model) taskItems() []list.Item {
	if len(model.tasks) == 0 {
		return []list.Item{item{title: "No tasks found", description: "Proxmox did not return recent tasks", value: ""}}
	}

	items := make([]list.Item, 0, len(model.tasks))
	for _, task := range model.tasks {
		title := task.Type
		if task.ID != "" {
			title = strings.TrimSpace(title + " " + task.ID)
		}
		if title == "" {
			title = shortUPID(task.UPID)
		}

		description := strings.Join(cleanParts([]string{
			task.Node,
			task.User,
			taskState(task),
			formatUnixTime(task.StartTime),
		}), " · ")

		items = append(items, item{
			title:       title,
			description: description,
			value:       task.UPID,
			filterValue: strings.Join([]string{task.UPID, task.Node, task.Type, task.ID, task.User, task.Status, task.ExitStatus}, " "),
		})
	}

	return items
}

func (model *model) selectTask(upid string) (tea.Model, tea.Cmd) {
	if upid == "" {
		return *model, nil
	}

	for _, task := range model.tasks {
		if task.UPID == upid {
			model.selectedTask = task
			model.taskLog = nil
			model.screen = screenProxmoxLoading
			model.loadingMessage = "Loading task log..."
			return *model, model.loadTaskLogCommand(task)
		}
	}

	model.error = "task was not found in the current list"
	return *model, nil
}

func (model model) loadTaskLogCommand(task proxmox.Task) tea.Cmd {
	service := model.service

	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()

		logEntries, err := service.TaskLog(ctx, task.UPID)
		return taskLogLoadedMsg{task: task, log: logEntries, err: err}
	}
}

func (model model) taskLogView() string {
	task := model.selectedTask

	var builder strings.Builder
	builder.WriteString(sectionTitleStyle.Render("Task Log"))
	builder.WriteString("\n\n")
	builder.WriteString(fmt.Sprintf("UPID:   %s\n", task.UPID))
	builder.WriteString(fmt.Sprintf("Node:   %s\n", displayValue(task.Node)))
	builder.WriteString(fmt.Sprintf("Type:   %s\n", displayValue(task.Type)))
	builder.WriteString(fmt.Sprintf("ID:     %s\n", displayValue(task.ID)))
	builder.WriteString(fmt.Sprintf("User:   %s\n", displayValue(task.User)))
	builder.WriteString(fmt.Sprintf("Status: %s\n", taskState(task)))
	builder.WriteString(fmt.Sprintf("Start:  %s\n", formatUnixTime(task.StartTime)))
	builder.WriteString(fmt.Sprintf("End:    %s\n", formatUnixTime(task.EndTime)))
	builder.WriteString("\n")

	if len(model.taskLog) == 0 {
		builder.WriteString("No log entries returned.")
	} else {
		logEntries := model.taskLog
		visibleLogLines := model.visibleTaskLogLines()
		if len(logEntries) > visibleLogLines {
			logEntries = logEntries[len(logEntries)-visibleLogLines:]
			builder.WriteString(fmt.Sprintf("Showing last %d log lines.\n\n", visibleLogLines))
		}

		lineWidth := model.contentWidth() - 12
		for _, logEntry := range logEntries {
			builder.WriteString(fmt.Sprintf("%4d  %s\n", logEntry.LineNumber, shortenText(logEntry.Text, lineWidth)))
		}
	}

	return panelStyle.Width(model.contentWidth()).Render(strings.TrimRight(builder.String(), "\n"))
}

func (model model) visibleTaskLogLines() int {
	visibleLines := model.height - 20
	if visibleLines < 8 {
		return 8
	}
	if visibleLines > maxTaskLogLines {
		return maxTaskLogLines
	}
	return visibleLines
}

func (model *model) selectStorageNode(node string) (tea.Model, tea.Cmd) {
	if node == "" {
		return *model, nil
	}

	model.storageNode = node
	model.storageName = ""
	model.storageContent = nil
	model.screen = screenProxmoxStorage
	model.setList("Choose Storage", model.allStorageItemsForNode(node))
	return *model, nil
}

func (model *model) selectStorage(storage string) (tea.Model, tea.Cmd) {
	if storage == "" {
		return *model, nil
	}

	model.storageName = storage
	model.storageContent = nil
	model.screen = screenProxmoxLoading
	model.loadingMessage = fmt.Sprintf("Loading %s/%s content...", model.storageNode, storage)
	return *model, model.loadStorageContentCommand()
}

func (model model) loadStorageContentCommand() tea.Cmd {
	service := model.service
	node := model.storageNode
	storage := model.storageName

	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		content, err := service.ListStorageContent(ctx, node, storage, "", 0)
		return storageContentLoadedMsg{node: node, storage: storage, content: content, err: err}
	}
}

func (model model) storageContentItems() []list.Item {
	if len(model.storageContent) == 0 {
		return []list.Item{item{title: "No storage content found", description: "Proxmox returned an empty content list", value: ""}}
	}

	contentItems := append([]proxmox.StorageContent(nil), model.storageContent...)
	sort.Slice(contentItems, func(leftIndex int, rightIndex int) bool {
		left := contentItems[leftIndex]
		right := contentItems[rightIndex]
		if left.Content != right.Content {
			return left.Content < right.Content
		}
		if left.VMID != right.VMID {
			return left.VMID < right.VMID
		}
		return left.VolumeID < right.VolumeID
	})

	items := make([]list.Item, 0, len(contentItems))
	for _, content := range contentItems {
		title := content.VolumeID
		if title == "" {
			title = content.Name
		}
		if title == "" {
			title = "-"
		}

		description := strings.Join(cleanParts([]string{
			content.Content,
			content.Format,
			formatBytes(content.Size),
			formatVMID(content.VMID),
			formatUnixTime(content.CreationTime),
		}), " · ")

		items = append(items, item{
			title:       title,
			description: description,
			value:       content.VolumeID,
			filterValue: strings.Join([]string{content.VolumeID, content.Name, content.Content, content.Format, strconv.Itoa(content.VMID), content.Notes}, " "),
		})
	}

	return items
}

func (model model) exportGuestSpecCommand() tea.Cmd {
	service := model.service
	guest := model.selectedVM

	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()

		spec, err := service.ExportGuestSpec(ctx, guest.VMID)
		if err != nil {
			return proxmoxInfoMsg{backScreen: screenVMAction, err: err}
		}

		encodedSpec, err := yaml.Marshal(spec)
		if err != nil {
			return proxmoxInfoMsg{backScreen: screenVMAction, err: err}
		}

		body := fmt.Sprintf(
			"Export:\n  boringctl --output text export guest %d > guest-%d.yaml\n\nDry-run apply:\n  boringctl --output json apply --file guest-%d.yaml --dry-run\n\nYAML:\n%s",
			guest.VMID,
			guest.VMID,
			guest.VMID,
			string(encodedSpec),
		)

		return proxmoxInfoMsg{
			title:      fmt.Sprintf("Export %d Spec", guest.VMID),
			body:       body,
			backScreen: screenVMAction,
		}
	}
}

func (model model) infoView() string {
	var builder strings.Builder
	builder.WriteString(sectionTitleStyle.Render(model.infoTitle))
	builder.WriteString("\n\n")
	builder.WriteString(strings.TrimRight(model.infoBody, "\n"))

	return panelStyle.Width(model.contentWidth()).Render(builder.String())
}

func (model *model) restoreInfoBackScreen(backScreen screen) {
	switch backScreen {
	case screenVMAction:
		model.screen = screenVMAction
		model.setList(model.guestActionTitle(), model.vmActionItems())
	case screenProxmoxMenu:
		model.screen = screenProxmoxMenu
		model.setList("Proxmox Ops", model.proxmoxMenuItems())
	default:
		model.screen = screenHome
		model.setList("Home", model.homeItems())
	}
}

func (model model) proxmoxConfigBody() string {
	loadedConfig := model.service.Config
	healthStatus := "not connected"
	if model.health.Connected {
		healthStatus = "connected"
	}
	if model.checkingHealth {
		healthStatus = "checking"
	}

	var builder strings.Builder
	builder.WriteString(fmt.Sprintf("Endpoint:     %s\n", loadedConfig.Cluster.Endpoint))
	builder.WriteString(fmt.Sprintf("TLS:          insecure=%t\n", loadedConfig.Cluster.InsecureTLS))
	builder.WriteString(fmt.Sprintf("Auth envs:    %s / %s\n", loadedConfig.Auth.TokenIDEnv, loadedConfig.Auth.TokenSecretEnv))
	builder.WriteString(fmt.Sprintf("Health:       %s\n", healthStatus))
	if model.health.Error != "" {
		builder.WriteString(fmt.Sprintf("Error:        %s\n", model.health.Error))
	}
	if !model.health.CheckedAt.IsZero() {
		builder.WriteString(fmt.Sprintf("Checked:      %s\n", model.health.CheckedAt.Format(time.RFC3339)))
	}
	builder.WriteString(fmt.Sprintf("Nodes:        %s\n", formatValues(loadedConfig.NodeNames())))
	builder.WriteString(fmt.Sprintf("Storages:     %s\n", formatValues(loadedConfig.StorageNames())))
	builder.WriteString(fmt.Sprintf("VM images:    %s\n", formatValues(loadedConfig.ImageNames())))
	builder.WriteString(fmt.Sprintf("LXC images:   %s\n", formatValues(loadedConfig.LXCImageNames())))
	builder.WriteString(fmt.Sprintf("Plans:        %s\n", formatValues(loadedConfig.PlanNames())))

	if len(model.health.Nodes) > 0 {
		builder.WriteString("\nNodes\n")
		for _, node := range model.health.Nodes {
			builder.WriteString(fmt.Sprintf("  %-12s %-10s CPU %.1f%%/%d RAM %s/%s\n", node.Name, node.Status, node.CPUPercent, node.MaxCPU, formatBytes(node.MemoryBytes), formatBytes(node.MaxMemBytes)))
		}
	}

	if len(model.health.Storages) > 0 {
		builder.WriteString("\nStorages\n")
		for _, storage := range model.health.Storages {
			builder.WriteString(fmt.Sprintf("  %-10s %-16s %s/%s available %s\n", storage.Node, storage.Name, formatBytes(storage.UsedBytes), formatBytes(storage.TotalBytes), formatBytes(storage.AvailableBytes)))
		}
	}

	return builder.String()
}

func (model model) agentCommandBody() string {
	return strings.Join([]string{
		"Schema discovery:",
		"  boringctl --output json schema",
		"  boringctl --output json schema task",
		"",
		"Health and inventory:",
		"  boringctl --output json config check",
		"  boringctl --output json list --fields vmid,name,node,type,status --limit 20",
		"",
		"Tasks and storage:",
		"  boringctl --output json task list --limit 20",
		"  boringctl --output json task log 'UPID:node:...'",
		"  boringctl --output json storage list --node <node> --storage <storage>",
		"",
		"Guest spec drift review:",
		"  boringctl --output text export guest <vmid> > guest-<vmid>.yaml",
		"  boringctl --output json apply --file guest-<vmid>.yaml --dry-run",
	}, "\n")
}

func cleanParts(parts []string) []string {
	cleaned := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" && part != "-" {
			cleaned = append(cleaned, part)
		}
	}
	return cleaned
}

func taskState(task proxmox.Task) string {
	if task.ExitStatus != "" {
		return task.ExitStatus
	}
	return displayValue(task.Status)
}

func displayValue(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "-"
	}
	return value
}

func formatVMID(vmid int) string {
	if vmid <= 0 {
		return ""
	}
	return fmt.Sprintf("VMID %d", vmid)
}

func formatUnixTime(timestamp int64) string {
	if timestamp <= 0 {
		return "-"
	}
	return time.Unix(timestamp, 0).Format("2006-01-02 15:04")
}

func shortUPID(upid string) string {
	if len(upid) <= 28 {
		return upid
	}
	return upid[:28] + "..."
}

func shortenText(value string, maxWidth int) string {
	if maxWidth < 20 {
		maxWidth = 20
	}
	runes := []rune(value)
	if len(runes) <= maxWidth {
		return value
	}
	return string(runes[:maxWidth-3]) + "..."
}
