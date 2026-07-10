package tui

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/boring-dragon/boringctl/internal/proxmox"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type guestMetricsLoadedMsg struct {
	guest   proxmox.VMResource
	metrics []proxmox.RRDDataPoint
	err     error
}

type nodeMetricsLoadedMsg struct {
	node    string
	metrics []proxmox.RRDDataPoint
	show    bool
	err     error
}

func (model model) loadGuestMetricsCommand() tea.Cmd {
	service := model.service
	guest := model.selectedVM

	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()

		metrics, err := service.GuestMetrics(ctx, guest)
		return guestMetricsLoadedMsg{guest: guest, metrics: metrics, err: err}
	}
}

func (model model) loadNodeMetricsCommand(node string, show bool) tea.Cmd {
	service := model.service

	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()

		metrics, err := service.NodeMetrics(ctx, node)
		return nodeMetricsLoadedMsg{node: node, metrics: metrics, show: show, err: err}
	}
}

func (model *model) selectMetricsNode(node string) (tea.Model, tea.Cmd) {
	if node == "" {
		return *model, nil
	}

	model.metricsNode = node
	model.nodeMetrics = nil
	model.nodeMetricsErr = ""
	model.screen = screenProxmoxLoading
	model.loadingMessage = "Loading node metrics..."
	return *model, model.loadNodeMetricsCommand(node, true)
}

func (model model) nodeMetricsView() string {
	var builder strings.Builder
	builder.WriteString(sectionTitleStyle.Render("Node Metrics"))
	builder.WriteString(mutedStyle.Render("  " + model.metricsNode))
	builder.WriteString(model.metricsView(model.nodeMetrics, model.nodeMetricsErr))
	return builder.String()
}

func (model model) guestMetricsView() string {
	return model.metricsView(model.guestMetrics, model.guestMetricsErr)
}

func (model model) metricsView(metrics []proxmox.RRDDataPoint, metricsError string) string {
	if metricsError != "" {
		return "\nMetrics: " + warnStyle.Render("unavailable: "+metricsError)
	}
	if len(metrics) == 0 {
		return "\nMetrics: " + mutedStyle.Render("no RRD data returned for the last hour")
	}

	latest := metrics[len(metrics)-1]
	cpuValues := metricValues(metrics, func(point proxmox.RRDDataPoint) float64 { return point.CPUUsage })
	memoryValues := metricValues(metrics, func(point proxmox.RRDDataPoint) float64 { return point.MemoryUsage })
	diskValues := metricValues(metrics, func(point proxmox.RRDDataPoint) float64 {
		if hasDiskIO(metrics) {
			return point.DiskReadBytesPerSecond + point.DiskWriteBytesPerSecond
		}
		return point.DiskUsage
	})
	networkValues := metricValues(metrics, func(point proxmox.RRDDataPoint) float64 {
		return point.NetworkInBytesPerSecond + point.NetworkOutBytesPerSecond
	})

	diskTitle := "Disk"
	diskScale := 1.0
	diskCurrent := formatPercent(latest.DiskUsage)
	diskPeak := "peak " + formatPercent(maxDashboardValue(diskValues))
	diskHealth := latest.DiskUsage
	diskThresholded := true
	if hasDiskIO(metrics) {
		diskTitle = "Disk I/O"
		diskScale = 0
		diskCurrent = fmt.Sprintf("R %s/s · W %s/s", formatMetricBytes(latest.DiskReadBytesPerSecond), formatMetricBytes(latest.DiskWriteBytesPerSecond))
		diskPeak = "peak " + formatMetricBytes(maxDashboardValue(diskValues)) + "/s"
		diskHealth = 0
		diskThresholded = false
	}

	cards := []metricCard{
		{title: "CPU", values: cpuValues, scaleMaximum: 1, current: formatPercent(latest.CPUUsage), peak: "peak " + formatPercent(maxDashboardValue(cpuValues)), healthRatio: latest.CPUUsage, thresholded: true},
		{title: "Memory", values: memoryValues, scaleMaximum: 1, current: formatPercent(latest.MemoryUsage), peak: "peak " + formatPercent(maxDashboardValue(memoryValues)), healthRatio: latest.MemoryUsage, thresholded: true},
		{title: diskTitle, values: diskValues, scaleMaximum: diskScale, current: diskCurrent, peak: diskPeak, healthRatio: diskHealth, thresholded: diskThresholded},
		{title: "Network", values: networkValues, current: fmt.Sprintf("↓ %s/s · ↑ %s/s", formatMetricBytes(latest.NetworkInBytesPerSecond), formatMetricBytes(latest.NetworkOutBytesPerSecond)), peak: "peak " + formatMetricBytes(maxDashboardValue(networkValues)) + "/s"},
	}

	columns := 2
	if model.width >= 140 {
		columns = 4
	}
	cardWidths := distributeDashboardWidths(model.contentWidth()-(columns-1), columns)

	var rows []string
	for start := 0; start < len(cards); start += columns {
		var views []string
		for column := 0; column < columns && start+column < len(cards); column++ {
			views = append(views, renderMetricCard(cards[start+column], cardWidths[column]))
		}
		rows = append(rows, lipgloss.JoinHorizontal(lipgloss.Top, interleaveDashboardViews(views, " ")...))
	}

	return "\n" + mutedStyle.Render(fmt.Sprintf("Last hour · %s–%s", formatMetricTime(metrics[0].Timestamp), formatMetricTime(latest.Timestamp))) + "\n" + strings.Join(rows, "\n")
}

type metricCard struct {
	title        string
	values       []float64
	scaleMaximum float64
	current      string
	peak         string
	healthRatio  float64
	thresholded  bool
}

func renderMetricCard(card metricCard, width int) string {
	style := primaryStyle
	if card.thresholded {
		style = successStyle
		if card.healthRatio >= 0.85 {
			style = errorStyle
		} else if card.healthRatio >= 0.60 {
			style = warnStyle
		}
	}

	chartLines := brailleChart(card.values, max(width-4, 8), 2, card.scaleMaximum)
	for index, line := range chartLines {
		chartLines[index] = style.Render(line)
	}
	lines := []string{style.Render("● " + card.current)}
	lines = append(lines, chartLines...)
	lines = append(lines, mutedStyle.Render(card.peak))
	return dashboardBox(card.title, lines, width)
}

func metricValues(metrics []proxmox.RRDDataPoint, value func(proxmox.RRDDataPoint) float64) []float64 {
	values := make([]float64, 0, len(metrics))
	for _, point := range metrics {
		values = append(values, value(point))
	}
	return values
}

func interleaveDashboardViews(views []string, separator string) []string {
	result := make([]string, 0, len(views)*2-1)
	for index, view := range views {
		if index > 0 {
			result = append(result, separator)
		}
		result = append(result, view)
	}
	return result
}

func brailleChart(values []float64, width int, height int, scaleMaximum float64) []string {
	if width <= 0 || height <= 0 {
		return nil
	}

	dataPoints := width * 2
	visibleValues := make([]float64, dataPoints)
	if len(values) <= dataPoints {
		copy(visibleValues[dataPoints-len(values):], values)
	} else {
		for index := range dataPoints {
			bucketStart := index * len(values) / dataPoints
			bucketEnd := (index + 1) * len(values) / dataPoints
			visibleValues[index] = values[bucketStart]
			for _, value := range values[bucketStart+1 : bucketEnd] {
				visibleValues[index] = max(visibleValues[index], value)
			}
		}
	}
	if scaleMaximum <= 0 {
		scaleMaximum = maxDashboardValue(visibleValues)
	}
	if scaleMaximum <= 0 {
		scaleMaximum = 1
	}

	leftDots := [4]rune{0x40, 0x04, 0x02, 0x01}
	rightDots := [4]rune{0x80, 0x20, 0x10, 0x08}
	dotHeight := height * 4
	heights := make([]int, len(visibleValues))
	for index, value := range visibleValues {
		heights[index] = int(min(1, max(0, value/scaleMaximum)) * float64(dotHeight))
		if value > 0 && heights[index] == 0 {
			heights[index] = 1
		}
	}

	lines := make([]string, height)
	for row := range height {
		rowBottom := (height - 1 - row) * 4
		var builder strings.Builder
		for column := range width {
			character := rune(0x2800)
			character |= brailleColumn(heights[column*2], rowBottom, leftDots)
			character |= brailleColumn(heights[column*2+1], rowBottom, rightDots)
			builder.WriteRune(character)
		}
		lines[row] = builder.String()
	}
	return lines
}

func brailleColumn(valueHeight int, rowBottom int, dots [4]rune) rune {
	if valueHeight <= rowBottom {
		return 0
	}
	var character rune
	for index := range min(valueHeight-rowBottom, 4) {
		character |= dots[index]
	}
	return character
}

func hasDiskIO(metrics []proxmox.RRDDataPoint) bool {
	for _, point := range metrics {
		if point.DiskReadBytesPerSecond > 0 || point.DiskWriteBytesPerSecond > 0 {
			return true
		}
	}

	return false
}

func sparkline(values []float64, width int, scaleMaximum float64) string {
	if len(values) == 0 || width <= 0 {
		return ""
	}

	levels := []rune("▁▂▃▄▅▆▇█")
	visibleValues := values
	if len(values) > width {
		visibleValues = make([]float64, width)
		for index := range visibleValues {
			bucketStart := index * len(values) / width
			bucketEnd := (index + 1) * len(values) / width
			visibleValues[index] = values[bucketStart]
			for _, value := range values[bucketStart+1 : bucketEnd] {
				if value > visibleValues[index] {
					visibleValues[index] = value
				}
			}
		}
	}
	if scaleMaximum <= 0 {
		for _, value := range visibleValues {
			if value > scaleMaximum {
				scaleMaximum = value
			}
		}
	}

	var builder strings.Builder
	for _, value := range visibleValues {
		normalized := 0.0
		if scaleMaximum > 0 {
			normalized = value / scaleMaximum
		}
		if math.IsNaN(normalized) || math.IsInf(normalized, 0) || normalized < 0 {
			normalized = 0
		}
		if normalized > 1 {
			normalized = 1
		}
		levelIndex := int(math.Round(normalized * float64(len(levels)-1)))
		builder.WriteRune(levels[levelIndex])
	}

	return builder.String()
}

func formatPercent(value float64) string {
	return fmt.Sprintf("%5.1f%%", value*100)
}

func formatMetricTime(timestamp int64) string {
	if timestamp <= 0 {
		return "-"
	}

	return time.Unix(timestamp, 0).Format("15:04")
}

func formatMetricBytes(bytes float64) string {
	if bytes <= 0 {
		return "0 B"
	}

	const (
		kibibyte = 1024
		mebibyte = 1024 * kibibyte
		gibibyte = 1024 * mebibyte
	)

	switch {
	case bytes >= gibibyte:
		return fmt.Sprintf("%.1f GiB", bytes/gibibyte)
	case bytes >= mebibyte:
		return fmt.Sprintf("%.1f MiB", bytes/mebibyte)
	case bytes >= kibibyte:
		return fmt.Sprintf("%.1f KiB", bytes/kibibyte)
	default:
		return fmt.Sprintf("%.0f B", bytes)
	}
}
