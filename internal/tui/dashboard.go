package tui

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/boring-labs/boringctl/internal/proxmox"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

const dashboardRefreshInterval = time.Minute

type dashboardGuestsLoadedMsg struct {
	guests []proxmox.VMResource
	err    error
}

type dashboardNodeMetricsLoadedMsg struct {
	metrics map[string][]proxmox.RRDDataPoint
	errors  map[string]string
}

type dashboardRefreshTickMsg struct{}

type manageGuestsRefreshedMsg struct {
	guests []proxmox.VMResource
	kind   string
	err    error
}

type clusterSummary struct {
	nodesOnline     int
	nodesTotal      int
	coresTotal      int
	cpuRatio        float64
	memoryUsed      int64
	memoryTotal     int64
	storageUsed     int64
	storageTotal    int64
	runningGuests   int
	inactiveGuests  int
	virtualMachines int
	containers      int
	guestsByNode    map[string]int
	runningByNode   map[string]int
}

func (model model) loadDashboardGuestsCommand() tea.Cmd {
	service := model.service
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()

		guests, err := service.ListGuests(ctx)
		return dashboardGuestsLoadedMsg{guests: guests, err: err}
	}
}

func (model model) loadDashboardNodeMetricsCommand() tea.Cmd {
	service := model.service
	nodeNames := make([]string, 0, len(model.health.Nodes))
	for _, node := range model.health.Nodes {
		nodeNames = append(nodeNames, node.Name)
	}
	if len(nodeNames) == 0 {
		nodeNames = service.Config.NodeNames()
	}

	return func() tea.Msg {
		type nodeMetricsResult struct {
			node    string
			metrics []proxmox.RRDDataPoint
			err     error
		}

		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()

		results := make(chan nodeMetricsResult, len(nodeNames))
		semaphore := make(chan struct{}, max(min(len(nodeNames), 4), 1))
		for _, nodeName := range nodeNames {
			go func(node string) {
				select {
				case semaphore <- struct{}{}:
					defer func() { <-semaphore }()
				case <-ctx.Done():
					results <- nodeMetricsResult{node: node, err: ctx.Err()}
					return
				}
				metrics, err := service.NodeMetrics(ctx, node)
				results <- nodeMetricsResult{node: node, metrics: metrics, err: err}
			}(nodeName)
		}

		metricsByNode := make(map[string][]proxmox.RRDDataPoint, len(nodeNames))
		errorsByNode := make(map[string]string)
		for range nodeNames {
			result := <-results
			metricsByNode[result.node] = result.metrics
			if result.err != nil {
				errorsByNode[result.node] = result.err.Error()
			}
		}

		return dashboardNodeMetricsLoadedMsg{metrics: metricsByNode, errors: errorsByNode}
	}
}

func (model model) refreshManageGuestsCommand() tea.Cmd {
	service := model.service
	manageKind := model.manageKind
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()

		var (
			guests []proxmox.VMResource
			err    error
		)
		if manageKind == manageKindContainer {
			guests, err = service.ListContainers(ctx)
		} else {
			guests, err = service.ListVMs(ctx)
		}
		return manageGuestsRefreshedMsg{guests: guests, kind: manageKind, err: err}
	}
}

func scheduleDashboardRefresh() tea.Cmd {
	return tea.Tick(dashboardRefreshInterval, func(time.Time) tea.Msg {
		return dashboardRefreshTickMsg{}
	})
}

func (model model) titleRule() string {
	clusterName := "cluster"
	if endpoint, err := url.Parse(model.service.Config.Cluster.Endpoint); err == nil && endpoint.Hostname() != "" {
		clusterName = endpoint.Hostname()
	}

	state := "checking"
	if !model.checkingHealth {
		if model.health.Connected {
			state = "connected"
		} else {
			state = "disconnected"
		}
	}

	label := " BORINGCTL · " + clusterName + " · " + state + " "
	if model.width >= 110 && !model.health.CheckedAt.IsZero() {
		label = " BORINGCTL · " + clusterName + " · " + state + " · checked " + model.health.CheckedAt.Format("15:04") + " "
	}
	lineWidth := max(model.width-2, 1)
	label = ansi.Truncate(label, lineWidth, "…")
	leftWidth := (lineWidth - lipgloss.Width(label)) / 2
	rightWidth := lineWidth - lipgloss.Width(label) - leftWidth
	return borderStyle.Render("╶" + strings.Repeat("─", leftWidth) + label + strings.Repeat("─", rightWidth) + "╴")
}

func (model model) clusterDashboardView() string {
	if model.checkingHealth && len(model.health.Nodes) == 0 {
		return dashboardBox("Cluster", []string{warnStyle.Render("Checking Proxmox resources…")}, model.contentWidth())
	}

	if !model.health.Connected {
		message := "Proxmox is unavailable"
		if model.health.Error != "" && model.width >= 110 {
			message += " · " + firstDashboardLine(model.health.Error)
		}
		return dashboardBox("Cluster", []string{errorStyle.Render(message)}, model.contentWidth())
	}

	summary := model.clusterSummary()
	if model.width < 110 || model.height < 34 {
		return model.compactClusterSummary(summary)
	}

	availableWidth := model.contentWidth()
	cardGap := 1
	cardWidths := distributeDashboardWidths(availableWidth-cardGap*2, 3)
	cpuHistory := model.weightedNodeMetricHistory(func(point proxmox.RRDDataPoint) float64 { return point.CPUUsage }, false)
	memoryHistory := model.weightedNodeMetricHistory(func(point proxmox.RRDDataPoint) float64 { return point.MemoryUsage }, true)
	historyLoaded, historyExpected := model.dashboardHistoryCoverage()
	historyLabel := "cluster 1h"
	if historyLoaded < historyExpected {
		historyLabel = fmt.Sprintf("cluster 1h %d/%d", historyLoaded, historyExpected)
	}
	cpuHistoryLine := warnStyle.Render("history unavailable")
	cpuPeakLine := mutedStyle.Render("peak —")
	if len(cpuHistory) > 0 {
		cpuHistoryLine = mutedStyle.Render(historyLabel+"  ") + sparkline(cpuHistory, max(cardWidths[0]-lipgloss.Width(historyLabel)-5, 8), 1)
		cpuPeakLine = mutedStyle.Render("peak ") + formatDashboardPercent(max(summary.cpuRatio, maxDashboardValue(cpuHistory)))
	}
	memoryHistoryLine := warnStyle.Render("history unavailable")
	memoryPeakLine := mutedStyle.Render("peak —")
	if len(memoryHistory) > 0 {
		memoryHistoryLine = mutedStyle.Render(historyLabel+"  ") + sparkline(memoryHistory, max(cardWidths[1]-lipgloss.Width(historyLabel)-5, 8), 1)
		memoryPeakLine = mutedStyle.Render("peak ") + formatDashboardPercent(max(dashboardRatio(float64(summary.memoryUsed), float64(summary.memoryTotal)), maxDashboardValue(memoryHistory)))
	}

	cpuCard := dashboardBox("CPU · "+fmt.Sprintf("%d cores", summary.coresTotal), []string{
		dashboardValueLine(formatDashboardPercent(summary.cpuRatio), "cluster weighted", cardWidths[0]-2),
		dashboardUsageBar(summary.cpuRatio, cardWidths[0]-6),
		cpuHistoryLine,
		cpuPeakLine,
	}, cardWidths[0])

	memoryRatio := dashboardRatio(float64(summary.memoryUsed), float64(summary.memoryTotal))
	memoryCard := dashboardBox("Memory · "+formatBytes(summary.memoryTotal), []string{
		dashboardValueLine(formatDashboardPercent(memoryRatio), formatBytes(summary.memoryUsed)+" used", cardWidths[1]-2),
		dashboardUsageBar(memoryRatio, cardWidths[1]-6),
		memoryHistoryLine,
		memoryPeakLine,
	}, cardWidths[1])

	storageRatio := dashboardRatio(float64(summary.storageUsed), float64(summary.storageTotal))
	guestLines := []string{
		fmt.Sprintf("%s %d running  %s %d inactive", successStyle.Render("●"), summary.runningGuests, mutedStyle.Render("●"), summary.inactiveGuests),
		fmt.Sprintf("%d VM · %d LXC · nodes %d/%d", summary.virtualMachines, summary.containers, summary.nodesOnline, summary.nodesTotal),
		dashboardUsageBar(storageRatio, cardWidths[2]-6),
		mutedStyle.Render(formatBytes(summary.storageUsed) + " / " + formatBytes(summary.storageTotal) + " configured"),
	}
	if model.dashboardGuestErr != "" {
		guestLines[0] = warnStyle.Render("Guest inventory unavailable")
		guestLines[1] = mutedStyle.Render("press r to retry")
	}
	if len(model.health.StorageErrors) > 0 {
		guestLines[2] = warnStyle.Render("configured storage data is partial")
		guestLines[3] = mutedStyle.Render(fmt.Sprintf("n/a · %d node checks failed", len(model.health.StorageErrors)))
	}
	guestCard := dashboardBox("Guests & managed storage", guestLines, cardWidths[2])

	var builder strings.Builder
	builder.WriteString(lipgloss.JoinHorizontal(lipgloss.Top, cpuCard, " ", memoryCard, " ", guestCard))
	builder.WriteString("\n")
	builder.WriteString(model.nodeOverview(summary))
	return builder.String()
}

func (model model) compactClusterSummary(summary clusterSummary) string {
	memoryRatio := dashboardRatio(float64(summary.memoryUsed), float64(summary.memoryTotal))
	storageRatio := dashboardRatio(float64(summary.storageUsed), float64(summary.storageTotal))
	storageLabel := "managed " + formatDashboardPercent(storageRatio)
	if len(model.health.StorageErrors) > 0 {
		storageLabel = "managed storage partial"
	}
	guestLabel := fmt.Sprintf("guests %d/%d running", summary.runningGuests, summary.runningGuests+summary.inactiveGuests)
	if model.dashboardGuestErr != "" {
		guestLabel = "guests unavailable"
	}
	line := fmt.Sprintf(
		"%s %d/%d nodes   CPU %s   RAM %s   %s   %s",
		successStyle.Render("●"),
		summary.nodesOnline,
		summary.nodesTotal,
		formatDashboardPercent(summary.cpuRatio),
		formatDashboardPercent(memoryRatio),
		guestLabel,
		storageLabel,
	)
	return dashboardBox("Cluster", []string{line}, model.contentWidth())
}

func (model model) clusterSummary() clusterSummary {
	summary := clusterSummary{
		nodesTotal:    len(model.health.Nodes),
		guestsByNode:  make(map[string]int),
		runningByNode: make(map[string]int),
	}

	var usedCPUCapacity float64
	for _, node := range model.health.Nodes {
		if node.Status != "online" {
			continue
		}
		summary.nodesOnline++
		if node.MaxCPU > 0 {
			summary.coresTotal += node.MaxCPU
			usedCPUCapacity += node.CPUPercent / 100 * float64(node.MaxCPU)
		}
		summary.memoryUsed += node.MemoryBytes
		summary.memoryTotal += node.MaxMemBytes
	}
	if summary.coresTotal > 0 {
		summary.cpuRatio = usedCPUCapacity / float64(summary.coresTotal)
	}

	seenStorage := make(map[string]bool)
	for _, storage := range model.health.Storages {
		if !storage.Active || !storage.Enabled {
			continue
		}
		storageKey := storage.Node + "/" + storage.Name
		if storage.Shared {
			storageKey = "shared/" + storage.Name
		}
		if seenStorage[storageKey] {
			continue
		}
		seenStorage[storageKey] = true
		summary.storageUsed += storage.UsedBytes
		summary.storageTotal += storage.TotalBytes
	}

	for _, guest := range model.dashboardGuests {
		summary.guestsByNode[guest.Node]++
		if guest.Status == "running" {
			summary.runningGuests++
			summary.runningByNode[guest.Node]++
		} else {
			summary.inactiveGuests++
		}
		if guest.IsContainer() {
			summary.containers++
		} else {
			summary.virtualMachines++
		}
	}

	return summary
}

func (model model) nodeOverview(summary clusterSummary) string {
	var builder strings.Builder
	builder.WriteString(mutedStyle.Render(" NODES"))
	builder.WriteString("\n")
	rowCount := model.nodeOverviewRowCount()
	nodes := model.health.Nodes
	showMoreRow := len(nodes) > rowCount
	if showMoreRow {
		nodes = nodes[:max(rowCount-1, 0)]
	}
	for _, node := range nodes {
		status := successStyle.Render("●")
		if node.Status != "online" {
			status = errorStyle.Render("●")
		}
		memoryRatio := dashboardRatio(float64(node.MemoryBytes), float64(node.MaxMemBytes))
		history := model.dashboardNodeMetrics[node.Name]
		cpuHistory := make([]float64, 0, len(history))
		for _, point := range history {
			cpuHistory = append(cpuHistory, point.CPUUsage)
		}
		historyView := sparkline(cpuHistory, 10, 1)
		if model.dashboardNodeMetricErrs[node.Name] != "" {
			historyView = mutedStyle.Render("no history")
		}
		guestCounts := fmt.Sprintf("guests %d/%d", summary.runningByNode[node.Name], summary.guestsByNode[node.Name])
		if model.dashboardGuestErr != "" {
			guestCounts = "guests n/a"
		}
		row := fmt.Sprintf(
			" %s %-10s CPU %5.1f%% %s  RAM %5.1f%% %s  %s  1h %s",
			status,
			node.Name,
			node.CPUPercent,
			dashboardUsageBar(node.CPUPercent/100, 8),
			memoryRatio*100,
			dashboardUsageBar(memoryRatio, 8),
			guestCounts,
			historyView,
		)
		builder.WriteString(ansi.Truncate(row, model.contentWidth(), "…"))
		builder.WriteString("\n")
	}
	if showMoreRow {
		builder.WriteString(mutedStyle.Render(fmt.Sprintf("   +%d more nodes", len(model.health.Nodes)-len(nodes))))
		builder.WriteString("\n")
	}
	return strings.TrimRight(builder.String(), "\n")
}

func (model model) nodeOverviewRowCount() int {
	if len(model.health.Nodes) == 0 {
		return 0
	}
	return min(len(model.health.Nodes), max(model.height-25, 1))
}

func (model model) weightedNodeMetricHistory(value func(proxmox.RRDDataPoint) float64, memoryWeight bool) []float64 {
	type weightedHistory struct {
		points []proxmox.RRDDataPoint
		weight float64
	}
	type timestampAggregate struct {
		weightedValue float64
		nodeCount     int
	}

	weights := make(map[string]float64, len(model.health.Nodes))
	for _, node := range model.health.Nodes {
		weight := float64(node.MaxCPU)
		if memoryWeight {
			weight = float64(node.MaxMemBytes)
		}
		weights[node.Name] = weight
	}

	var histories []weightedHistory
	totalWeight := 0.0
	for _, node := range model.health.Nodes {
		if node.Status != "online" {
			continue
		}
		points := model.dashboardNodeMetrics[node.Name]
		weight := weights[node.Name]
		if len(points) == 0 || weight <= 0 {
			continue
		}
		histories = append(histories, weightedHistory{points: points, weight: weight})
		totalWeight += weight
	}
	if len(histories) == 0 || totalWeight <= 0 {
		return nil
	}

	aggregates := make(map[int64]timestampAggregate)
	for _, history := range histories {
		for _, point := range history.points {
			aggregate := aggregates[point.Timestamp]
			aggregate.weightedValue += value(point) * history.weight
			aggregate.nodeCount++
			aggregates[point.Timestamp] = aggregate
		}
	}

	timestamps := make([]int64, 0, len(aggregates))
	for timestamp, aggregate := range aggregates {
		if aggregate.nodeCount == len(histories) {
			timestamps = append(timestamps, timestamp)
		}
	}
	sort.Slice(timestamps, func(leftIndex int, rightIndex int) bool {
		return timestamps[leftIndex] < timestamps[rightIndex]
	})

	average := make([]float64, 0, len(timestamps))
	for _, timestamp := range timestamps {
		average = append(average, aggregates[timestamp].weightedValue/totalWeight)
	}
	return average
}

func (model model) dashboardHistoryCoverage() (int, int) {
	loaded := 0
	expected := 0
	for _, node := range model.health.Nodes {
		if node.Status != "online" {
			continue
		}
		expected++
		if len(model.dashboardNodeMetrics[node.Name]) > 0 && model.dashboardNodeMetricErrs[node.Name] == "" {
			loaded++
		}
	}
	return loaded, expected
}

func (model model) manageSplitView() string {
	leftWidth := model.manageListWidth()
	rightWidth := max(model.contentWidth()-leftWidth-1, 36)
	left := dashboardPanel("Guests", model.list.View(), leftWidth)
	right := model.guestPreviewView(rightWidth)
	return lipgloss.JoinHorizontal(lipgloss.Top, left, " ", right)
}

func (model model) guestPreviewView(width int) string {
	guest, exists := model.selectedGuestFromList()
	if !exists {
		return dashboardBox("Guest", []string{mutedStyle.Render("Select a guest to inspect resources")}, width)
	}

	status := successStyle.Render("● running")
	if guest.Status != "running" {
		status = mutedStyle.Render("● " + guest.Status)
	}
	memoryRatio := dashboardRatio(float64(guest.Mem), float64(guest.MaxMem))
	diskRatio := dashboardRatio(float64(guest.Disk), float64(guest.MaxDisk))
	diskLine := fmt.Sprintf("Disk    %5.1f%%  %s", diskRatio*100, dashboardUsageBar(diskRatio, max(width-22, 10)))
	diskDetail := formatBytes(guest.Disk) + " / " + formatBytes(guest.MaxDisk)
	if guest.Disk <= 0 && guest.MaxDisk > 0 {
		diskLine = "Disk      n/a   " + mutedStyle.Render(strings.Repeat("─", max(width-22, 10)))
		diskDetail = formatBytes(guest.MaxDisk) + " provisioned"
	}
	kind := "QEMU VM"
	if guest.IsContainer() {
		kind = "LXC container"
	}

	lines := []string{
		status + "  " + mutedStyle.Render(kind+" · "+guest.Node),
		fmt.Sprintf("CPU     %5.1f%%  %s", guest.CPU*100, dashboardUsageBar(guest.CPU, max(width-22, 10))),
		fmt.Sprintf("Memory  %5.1f%%  %s", memoryRatio*100, dashboardUsageBar(memoryRatio, max(width-22, 10))),
		mutedStyle.Render("        ") + formatBytes(guest.Mem) + " / " + formatBytes(guest.MaxMem),
		diskLine,
		mutedStyle.Render("        ") + diskDetail,
		"Uptime  " + formatUptime(guest.Uptime),
	}
	if guest.Tags != "" {
		lines = append(lines, mutedStyle.Render("Tags    ")+formatValues(strings.Split(guest.Tags, ";")))
	}
	lines = append(lines, "", helpStyle.Render("enter details · / search · esc back"))
	return dashboardBox(fmt.Sprintf("%d · %s", guest.VMID, guest.Name), lines, width)
}

func (model model) selectedGuestFromList() (proxmox.VMResource, bool) {
	selectedItem, ok := model.list.SelectedItem().(item)
	if !ok || selectedItem.value == "" {
		return proxmox.VMResource{}, false
	}
	for _, guest := range model.vms {
		if fmt.Sprintf("%d", guest.VMID) == selectedItem.value {
			return guest, true
		}
	}
	return proxmox.VMResource{}, false
}

func (model model) manageListWidth() int {
	if model.screen == screenManage && model.width >= 110 {
		return min(max(model.width*44/100, 44), 58)
	}
	return model.contentWidth()
}

func (model model) listWidth() int {
	if model.screen == screenManage && model.width >= 110 {
		return max(model.manageListWidth()-6, 20)
	}
	return model.contentWidth()
}

func dashboardPanel(title string, content string, width int) string {
	var builder strings.Builder
	builder.WriteString(sectionTitleStyle.Render(title))
	builder.WriteString("\n\n")
	builder.WriteString(content)
	return panelStyle.Width(max(width-6, 20)).Render(builder.String())
}

func dashboardBox(title string, lines []string, width int) string {
	width = max(width, 12)
	innerWidth := width - 2
	title = ansi.Truncate(title, max(innerWidth-1, 1), "")
	titleWidth := lipgloss.Width(title)
	topFill := max(innerWidth-titleWidth-1, 0)
	border := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))

	var builder strings.Builder
	builder.WriteString(border.Render("╭─" + title + strings.Repeat("─", topFill) + "╮"))
	for _, line := range lines {
		line = ansi.Truncate(line, innerWidth, "")
		builder.WriteString("\n")
		padding := max(innerWidth-lipgloss.Width(line), 0)
		builder.WriteString(border.Render("│"))
		builder.WriteString(line)
		builder.WriteString(strings.Repeat(" ", padding))
		builder.WriteString(border.Render("│"))
	}
	builder.WriteString("\n")
	builder.WriteString(border.Render("╰" + strings.Repeat("─", innerWidth) + "╯"))
	return builder.String()
}

func dashboardValueLine(value string, label string, width int) string {
	gap := max(width-lipgloss.Width(value)-lipgloss.Width(label), 1)
	return selectedTitleStyle.Render(value) + strings.Repeat(" ", gap) + mutedStyle.Render(label)
}

func dashboardUsageBar(ratio float64, width int) string {
	width = max(width, 1)
	ratio = max(0, min(1, ratio))
	filled := int(ratio * float64(width))
	if ratio > 0 && filled == 0 {
		filled = 1
	}
	color := successStyle
	if ratio >= 0.85 {
		color = errorStyle
	} else if ratio >= 0.60 {
		color = warnStyle
	}
	return color.Render(strings.Repeat("━", filled)) + mutedStyle.Render(strings.Repeat("─", width-filled))
}

func dashboardRatio(used float64, total float64) float64 {
	if total <= 0 {
		return 0
	}
	return max(0, min(1, used/total))
}

func formatDashboardPercent(ratio float64) string {
	return fmt.Sprintf("%.1f%%", ratio*100)
}

func maxDashboardValue(values []float64) float64 {
	maximum := 0.0
	for _, value := range values {
		maximum = max(maximum, value)
	}
	return maximum
}

func distributeDashboardWidths(total int, count int) []int {
	if count <= 0 {
		return nil
	}
	widths := make([]int, count)
	baseWidth := total / count
	for index := range count {
		widths[index] = baseWidth
		if index < total%count {
			widths[index]++
		}
	}
	return widths
}

func firstDashboardLine(value string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(value), "\n")
	return line
}
