package tui

import (
	"context"
	"fmt"
	"io"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/boring-dragon/boringctl/internal/app"
	"github.com/boring-dragon/boringctl/internal/proxmox"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

type screen int

const (
	screenHome screen = iota
	screenNode
	screenImage
	screenPlan
	screenCustomCores
	screenCustomMemory
	screenCustomDisk
	screenStorage
	screenSSHKey
	screenPasteSSHKeys
	screenNetwork
	screenStaticIP
	screenStaticGateway
	screenStaticDNS
	screenName
	screenConfirm
	screenCreating
	screenDone
	screenContainerNode
	screenContainerImage
	screenContainerStorage
	screenContainerPlan
	screenContainerSSHKey
	screenContainerNetwork
	screenContainerName
	screenContainerConfirm
	screenContainerCreating
	screenContainerDone
	screenManageLoading
	screenManage
	screenGuestLoading
	screenGuestDetail
	screenVMAction
	screenActionConfirm
	screenDeleteName
	screenActionRunning
	screenSnapshots
	screenCaddyMenu
	screenCaddyLoading
	screenCaddySites
	screenCaddyRouteAction
	screenCaddyDomain
	screenCaddyVisibility
	screenCaddyType
	screenCaddyScheme
	screenCaddyHost
	screenCaddyPort
	screenCaddyRoot
	screenCaddyWAF
	screenCaddyDeploy
	screenCaddyConfirm
	screenCaddyWorking
	screenCaddyDone
	screenSSHAlias
	screenProxmoxMenu
	screenProxmoxLoading
	screenProxmoxTasks
	screenProxmoxTaskDetail
	screenProxmoxStorageNode
	screenProxmoxStorage
	screenProxmoxStorageContent
	screenProxmoxInfo
	screenProxmoxMetricsNode
	screenProxmoxMetrics
)

const suggestedPlanPrefix = "suggested:"

const (
	minimumTerminalWidth  = 80
	minimumTerminalHeight = 24
)

const (
	manageKindVM        = "vm"
	manageKindContainer = "container"
)

type item struct {
	title       string
	description string
	value       string
	filterValue string
	guest       *proxmox.VMResource
}

func (item item) Title() string {
	return item.title
}

func (item item) Description() string {
	return item.description
}

func (item item) FilterValue() string {
	if item.filterValue != "" {
		return item.filterValue
	}

	return item.title + " " + item.description + " " + item.value
}

type model struct {
	service                 *app.Service
	screen                  screen
	list                    list.Model
	input                   textinput.Model
	textarea                textarea.Model
	request                 app.CreateRequest
	containerRequest        app.CreateContainerRequest
	width                   int
	height                  int
	error                   string
	steps                   []string
	result                  app.CreateResult
	containerResult         app.CreateContainerResult
	health                  app.ClusterHealth
	checkingHealth          bool
	vms                     []proxmox.VMResource
	selectedVM              proxmox.VMResource
	guestDetail             app.GuestDetail
	selectedAction          string
	actionMessage           string
	sshAlias                string
	sshConfigStatus         string
	snapshots               []proxmox.Snapshot
	partialError            *app.PartialCreateError
	partialCleaned          bool
	selectedSSHKeys         map[string]bool
	manualResources         bool
	manageKind              string
	caddyRequest            app.CaddySiteRequest
	caddySites              []app.CaddySiteSummary
	selectedCaddy           app.CaddySiteSummary
	caddyAction             string
	caddyMessage            string
	caddySteps              []string
	tasks                   []proxmox.Task
	selectedTask            proxmox.Task
	taskLog                 []proxmox.TaskLogEntry
	storageNode             string
	storageName             string
	storageContent          []proxmox.StorageContent
	infoTitle               string
	infoBody                string
	infoBackScreen          screen
	loadingMessage          string
	guestMetrics            []proxmox.RRDDataPoint
	guestMetricsErr         string
	metricsNode             string
	nodeMetrics             []proxmox.RRDDataPoint
	nodeMetricsErr          string
	dashboardGuests         []proxmox.VMResource
	dashboardGuestErr       string
	dashboardNodeMetrics    map[string][]proxmox.RRDDataPoint
	dashboardNodeMetricErrs map[string]string
}

type createFinishedMsg struct {
	result app.CreateResult
	steps  []string
	err    error
}

type createContainerFinishedMsg struct {
	result app.CreateContainerResult
	steps  []string
	err    error
}

type healthCheckedMsg struct {
	health app.ClusterHealth
}

type vmsLoadedMsg struct {
	vms []proxmox.VMResource
	err error
}

type guestDetailLoadedMsg struct {
	vmid   int
	detail app.GuestDetail
	err    error
}

type actionFinishedMsg struct {
	message string
	err     error
}

type sshConfigMsg struct {
	result     app.SSHConfigResult
	err        error
	fromCreate bool
}

type snapshotsLoadedMsg struct {
	snapshots []proxmox.Snapshot
	err       error
}

type cleanupFinishedMsg struct {
	err error
}

type caddySitesLoadedMsg struct {
	sites []app.CaddySiteSummary
	err   error
}

type caddyFinishedMsg struct {
	message string
	steps   []string
	err     error
}

type proxmoxInfoMsg struct {
	title      string
	body       string
	backScreen screen
	err        error
}

type tasksLoadedMsg struct {
	tasks []proxmox.Task
	err   error
}

type taskLogLoadedMsg struct {
	task proxmox.Task
	log  []proxmox.TaskLogEntry
	err  error
}

type storageContentLoadedMsg struct {
	node    string
	storage string
	content []proxmox.StorageContent
	err     error
}

var (
	appTitleStyle       = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("75"))
	sectionTitleStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("75"))
	itemTitleStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	itemDescStyle       = lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
	selectedTitleStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("208"))
	selectedMarkerStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("208"))
	primaryStyle        = lipgloss.NewStyle().Foreground(lipgloss.Color("75"))
	helpStyle           = lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
	errorStyle          = lipgloss.NewStyle().Foreground(lipgloss.Color("203"))
	successStyle        = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	warnStyle           = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
	mutedStyle          = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	borderStyle         = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	panelStyle          = lipgloss.NewStyle().Padding(1, 2).Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("238"))
)

func Run(service *app.Service) error {
	input := textinput.New()
	input.Focus()

	initialModel := model{
		service:        service,
		screen:         screenHome,
		input:          input,
		width:          80,
		height:         24,
		checkingHealth: true,
		selectedSSHKeys: map[string]bool{
			service.Config.Defaults.SSHKey: true,
		},
		dashboardNodeMetrics:    make(map[string][]proxmox.RRDDataPoint),
		dashboardNodeMetricErrs: make(map[string]string),
	}
	initialModel.setList("Home", initialModel.homeItems())

	_, err := tea.NewProgram(initialModel, tea.WithAltScreen()).Run()
	return err
}

func (model model) Init() tea.Cmd {
	return tea.Batch(
		model.healthCommand(),
		model.loadDashboardGuestsCommand(),
		scheduleDashboardRefresh(),
	)
}

func (model model) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch typedMessage := message.(type) {
	case tea.WindowSizeMsg:
		model.width = typedMessage.Width
		model.height = typedMessage.Height
		model.list.SetSize(model.listWidth(), model.listHeight())
		return model, nil
	case createFinishedMsg:
		model.screen = screenDone
		model.result = typedMessage.result
		model.steps = typedMessage.steps
		model.partialError = nil
		model.partialCleaned = false
		model.sshConfigStatus = ""
		if typedMessage.err != nil {
			model.error = typedMessage.err.Error()
			if partialError, ok := typedMessage.err.(*app.PartialCreateError); ok {
				model.partialError = partialError
			}
		}
		return model, nil
	case createContainerFinishedMsg:
		model.screen = screenContainerDone
		model.containerResult = typedMessage.result
		model.steps = typedMessage.steps
		if typedMessage.err != nil {
			model.error = typedMessage.err.Error()
		} else {
			model.error = ""
		}
		return model, nil
	case sshConfigMsg:
		if typedMessage.fromCreate {
			model.screen = screenDone
			model.actionMessage = ""
			if typedMessage.err != nil {
				model.error = typedMessage.err.Error()
				model.sshConfigStatus = ""
				return model, nil
			}

			model.error = ""
			if typedMessage.result.AlreadyExists {
				model.sshConfigStatus = fmt.Sprintf("SSH config already exists for %s (%s)", typedMessage.result.Alias, typedMessage.result.ConfigPath)
			} else {
				model.sshConfigStatus = fmt.Sprintf("Added SSH config entry %s (%s)", typedMessage.result.Alias, typedMessage.result.ConfigPath)
			}

			return model, nil
		}

		if typedMessage.err != nil {
			model.screen = screenVMAction
			model.error = typedMessage.err.Error()
			model.setList(model.guestActionTitle(), model.vmActionItems())
			model.actionMessage = ""
			return model, nil
		}

		if typedMessage.result.AlreadyExists {
			model.actionMessage = fmt.Sprintf("SSH config already present for %s", typedMessage.result.Alias)
		} else {
			model.actionMessage = fmt.Sprintf("SSH config added for %s", typedMessage.result.Alias)
		}

		model.screen = screenVMAction
		model.setList(model.guestActionTitle(), model.vmActionItems())
		return model, nil
	case healthCheckedMsg:
		model.health = typedMessage.health
		model.checkingHealth = false
		model.list.SetSize(model.listWidth(), model.listHeight())
		if model.screen == screenHome {
			return model, model.loadDashboardNodeMetricsCommand()
		}
		return model, nil
	case dashboardGuestsLoadedMsg:
		model.dashboardGuests = typedMessage.guests
		model.dashboardGuestErr = ""
		if typedMessage.err != nil {
			model.dashboardGuestErr = typedMessage.err.Error()
		}
		model.list.SetSize(model.listWidth(), model.listHeight())
		return model, nil
	case dashboardNodeMetricsLoadedMsg:
		model.dashboardNodeMetrics = typedMessage.metrics
		model.dashboardNodeMetricErrs = typedMessage.errors
		model.list.SetSize(model.listWidth(), model.listHeight())
		return model, nil
	case manageGuestsRefreshedMsg:
		if model.screen != screenManage || model.manageKind != typedMessage.kind {
			return model, nil
		}
		if typedMessage.err != nil {
			model.error = typedMessage.err.Error()
			return model, nil
		}
		selectedValue := ""
		if selectedItem, ok := model.list.SelectedItem().(item); ok {
			selectedValue = selectedItem.value
		}
		filterValue := model.list.FilterValue()
		filterState := model.list.FilterState()
		model.vms = typedMessage.guests
		model.setList(model.manageListTitle(), model.vmItems())
		if filterValue != "" {
			model.list.SetFilterText(filterValue)
			model.list.SetFilterState(filterState)
		}
		for index, listItem := range model.list.VisibleItems() {
			if currentItem, ok := listItem.(item); ok && currentItem.value == selectedValue {
				model.list.Select(index)
				break
			}
		}
		model.error = ""
		return model, nil
	case dashboardRefreshTickMsg:
		commands := []tea.Cmd{scheduleDashboardRefresh()}
		switch model.screen {
		case screenHome:
			model.checkingHealth = true
			commands = append(commands, model.healthCommand(), model.loadDashboardGuestsCommand())
		case screenManage:
			commands = append(commands, model.refreshManageGuestsCommand())
		case screenGuestDetail:
			commands = append(commands, model.loadGuestDetailCommand(), model.loadGuestMetricsCommand())
		case screenProxmoxMetrics:
			commands = append(commands, model.loadNodeMetricsCommand(model.metricsNode, false))
		}
		return model, tea.Batch(commands...)
	case vmsLoadedMsg:
		if typedMessage.err != nil {
			model.error = typedMessage.err.Error()
			model.screen = screenHome
			model.setList("Home", model.homeItems())
			return model, nil
		}
		model.vms = typedMessage.vms
		model.screen = screenManage
		model.setList(model.manageListTitle(), model.vmItems())
		return model, nil
	case guestDetailLoadedMsg:
		if model.screen != screenGuestLoading && model.screen != screenGuestDetail {
			return model, nil
		}
		if typedMessage.vmid != model.selectedVM.VMID {
			return model, nil
		}
		refreshingDetail := model.screen == screenGuestDetail
		if typedMessage.err != nil {
			if refreshingDetail {
				model.error = typedMessage.err.Error()
				return model, nil
			}
			model.error = typedMessage.err.Error()
			model.screen = screenManage
			model.setList(model.manageListTitle(), model.vmItems())
			return model, nil
		}
		model.guestDetail = typedMessage.detail
		model.selectedVM = typedMessage.detail.Guest
		if !refreshingDetail {
			model.screen = screenGuestDetail
		}
		model.error = ""
		return model, nil
	case guestMetricsLoadedMsg:
		if typedMessage.guest.VMID != model.selectedVM.VMID {
			return model, nil
		}
		model.guestMetrics = typedMessage.metrics
		model.guestMetricsErr = ""
		if typedMessage.err != nil {
			model.guestMetricsErr = typedMessage.err.Error()
		}
		return model, nil
	case nodeMetricsLoadedMsg:
		if typedMessage.show && model.screen != screenProxmoxLoading {
			return model, nil
		}
		if !typedMessage.show && model.screen != screenProxmoxMetrics {
			return model, nil
		}
		if typedMessage.node != model.metricsNode {
			return model, nil
		}
		model.nodeMetrics = typedMessage.metrics
		model.nodeMetricsErr = ""
		if typedMessage.err != nil {
			model.nodeMetricsErr = typedMessage.err.Error()
		}
		if typedMessage.show {
			model.screen = screenProxmoxMetrics
		}
		model.error = ""
		return model, nil
	case actionFinishedMsg:
		if typedMessage.err != nil {
			model.screen = screenVMAction
			model.error = typedMessage.err.Error()
			model.setList(model.guestActionTitle(), model.vmActionItems())
			return model, nil
		}

		if model.selectedAction == "delete" {
			model.screen = screenManageLoading
			model.error = ""
			model.actionMessage = typedMessage.message
			return model, tea.Batch(model.loadVMsCommand(), model.healthCommand(), model.loadDashboardGuestsCommand())
		}

		model.screen = screenVMAction
		model.error = ""
		model.actionMessage = typedMessage.message
		model.setList(model.guestActionTitle(), model.vmActionItems())
		return model, tea.Batch(model.healthCommand(), model.loadDashboardGuestsCommand())
	case cleanupFinishedMsg:
		model.screen = screenDone
		if typedMessage.err != nil {
			model.error = typedMessage.err.Error()
		} else {
			model.partialCleaned = true
		}
		return model, nil
	case snapshotsLoadedMsg:
		model.screen = screenSnapshots
		model.snapshots = typedMessage.snapshots
		if typedMessage.err != nil {
			model.error = typedMessage.err.Error()
		} else {
			model.error = ""
		}
		return model, nil
	case caddySitesLoadedMsg:
		if typedMessage.err != nil {
			model.error = typedMessage.err.Error()
			model.screen = screenCaddyMenu
			model.setList("Caddy Routes", model.caddyMenuItems())
			return model, nil
		}
		model.caddySites = typedMessage.sites
		model.screen = screenCaddySites
		model.setList("Caddy Sites", model.caddySiteItems())
		return model, nil
	case caddyFinishedMsg:
		model.screen = screenCaddyDone
		model.caddyMessage = typedMessage.message
		model.caddySteps = typedMessage.steps
		if typedMessage.err != nil {
			model.error = typedMessage.err.Error()
		} else {
			model.error = ""
		}
		return model, nil
	case proxmoxInfoMsg:
		if typedMessage.err != nil {
			model.error = typedMessage.err.Error()
			model.restoreInfoBackScreen(typedMessage.backScreen)
			return model, nil
		}
		model.infoTitle = typedMessage.title
		model.infoBody = typedMessage.body
		model.infoBackScreen = typedMessage.backScreen
		model.screen = screenProxmoxInfo
		model.error = ""
		return model, nil
	case tasksLoadedMsg:
		if typedMessage.err != nil {
			model.error = typedMessage.err.Error()
			model.screen = screenProxmoxMenu
			model.setList("Proxmox Ops", model.proxmoxMenuItems())
			return model, nil
		}
		model.tasks = typedMessage.tasks
		model.screen = screenProxmoxTasks
		model.error = ""
		model.setList("Recent Proxmox Tasks", model.taskItems())
		return model, nil
	case taskLogLoadedMsg:
		if typedMessage.err != nil {
			model.error = typedMessage.err.Error()
			model.screen = screenProxmoxTasks
			model.setList("Recent Proxmox Tasks", model.taskItems())
			return model, nil
		}
		model.selectedTask = typedMessage.task
		model.taskLog = typedMessage.log
		model.screen = screenProxmoxTaskDetail
		model.error = ""
		return model, nil
	case storageContentLoadedMsg:
		if typedMessage.err != nil {
			model.error = typedMessage.err.Error()
			model.screen = screenProxmoxStorage
			model.setList("Choose Storage", model.allStorageItemsForNode(model.storageNode))
			return model, nil
		}
		model.storageNode = typedMessage.node
		model.storageName = typedMessage.storage
		model.storageContent = typedMessage.content
		model.screen = screenProxmoxStorageContent
		model.error = ""
		model.setList("Storage Content", model.storageContentItems())
		return model, nil
	case tea.KeyMsg:
		key := typedMessage.String()
		if key == "ctrl+c" {
			return model, tea.Quit
		}
		if model.terminalTooSmall() {
			if key == "q" {
				return model, tea.Quit
			}
			return model, nil
		}

		if model.isListScreen() && model.list.SettingFilter() {
			var command tea.Cmd
			model.list, command = model.list.Update(typedMessage)
			return model, command
		}

		if key == "q" {
			return model, tea.Quit
		}
		if key == "r" && model.screen == screenHome {
			model.checkingHealth = true
			return model, tea.Batch(model.healthCommand(), model.loadDashboardGuestsCommand())
		}

		if key == "esc" && (model.screen == screenDone || model.screen == screenContainerDone) {
			return model, tea.Quit
		}

		if key == "esc" && model.isListScreen() && model.list.FilterState() == list.FilterApplied {
			var command tea.Cmd
			model.list, command = model.list.Update(typedMessage)
			return model, command
		}

		if key == "esc" && model.screen != screenCreating && model.screen != screenContainerCreating {
			model.previous()
			return model, nil
		}

		if model.isListScreen() {
			return model.updateList(key, typedMessage)
		}

		if model.isTextareaScreen() {
			return model.updateTextarea(key, typedMessage)
		}

		if model.isInputScreen() {
			return model.updateInput(key, typedMessage)
		}

		if model.screen == screenConfirm {
			if key == "enter" {
				model.screen = screenCreating
				model.error = ""
				return model, model.createCommand()
			}
			return model, nil
		}

		if model.screen == screenContainerConfirm {
			if key == "enter" {
				model.screen = screenContainerCreating
				model.error = ""
				return model, model.createContainerCommand()
			}
			return model, nil
		}

		if model.screen == screenDone && key == "d" && model.partialError != nil && !model.partialCleaned {
			model.screen = screenActionRunning
			return model, model.cleanupPartialCommand()
		}

		if model.screen == screenDone && key == "a" && model.error == "" && model.result.IP != "" {
			model.screen = screenActionRunning
			model.sshConfigStatus = ""
			return model, model.createSSHConfigCommand(model.result.VMID, model.result.Name, true)
		}

		if model.screen == screenDone && key == "enter" {
			return model, tea.Quit
		}

		if model.screen == screenContainerDone && key == "enter" {
			return model, tea.Quit
		}

		if model.screen == screenActionConfirm && key == "enter" {
			model.screen = screenActionRunning
			return model, model.vmActionCommand()
		}

		if model.screen == screenSnapshots && key == "r" {
			return model, model.loadSnapshotsCommand()
		}

		if model.screen == screenProxmoxTaskDetail && key == "r" {
			model.screen = screenProxmoxLoading
			model.loadingMessage = "Reloading task log..."
			return model, model.loadTaskLogCommand(model.selectedTask)
		}

		if model.screen == screenProxmoxMetrics && key == "r" {
			model.screen = screenProxmoxLoading
			model.loadingMessage = "Reloading node metrics..."
			return model, model.loadNodeMetricsCommand(model.metricsNode, true)
		}

		if model.screen == screenProxmoxInfo && key == "enter" {
			model.previous()
			return model, nil
		}

		if model.screen == screenGuestDetail && key == "enter" {
			model.screen = screenVMAction
			model.setList(model.guestActionTitle(), model.vmActionItems())
			return model, nil
		}

		if model.screen == screenCaddyConfirm && key == "enter" {
			model.screen = screenCaddyWorking
			return model, model.caddyCommand()
		}

		if model.screen == screenCaddyDone && key == "enter" {
			model.screen = screenCaddyMenu
			model.setList("Caddy Routes", model.caddyMenuItems())
			return model, nil
		}
	}

	var command tea.Cmd
	if model.isListScreen() {
		model.list, command = model.list.Update(message)
		return model, command
	}

	if model.isInputScreen() {
		model.input, command = model.input.Update(message)
		return model, command
	}

	if model.isTextareaScreen() {
		model.textarea, command = model.textarea.Update(message)
		return model, command
	}

	return model, nil
}

func (model model) View() string {
	if model.terminalTooSmall() {
		return model.smallTerminalView()
	}

	var builder strings.Builder
	builder.WriteString(model.headerView())
	builder.WriteString("\n")
	if model.screen == screenHome {
		builder.WriteString(model.clusterDashboardView())
		builder.WriteString("\n")
	}

	if model.error != "" && model.screen != screenDone {
		builder.WriteString(errorStyle.Render(model.error))
		builder.WriteString("\n\n")
	}

	switch model.screen {
	case screenConfirm:
		builder.WriteString(model.confirmView())
	case screenCreating:
		builder.WriteString("Creating VM...\n\n")
		builder.WriteString(helpStyle.Render("This can take a few minutes while Proxmox clones, resizes, configures, starts, and waits for guest agent IP."))
	case screenDone:
		builder.WriteString(model.doneView())
	case screenContainerConfirm:
		builder.WriteString(model.containerConfirmView())
	case screenContainerCreating:
		builder.WriteString("Creating LXC container...\n\n")
		builder.WriteString(helpStyle.Render("This can take a few minutes while Proxmox extracts the template, injects SSH keys, starts the container, and waits for an IP."))
	case screenContainerDone:
		builder.WriteString(model.containerDoneView())
	case screenManageLoading:
		builder.WriteString(panelStyle.Width(model.contentWidth()).Render("Loading " + model.manageKindPlural() + "..."))
	case screenGuestLoading:
		builder.WriteString(panelStyle.Width(model.contentWidth()).Render("Loading " + model.manageKindSingular() + " details..."))
	case screenGuestDetail:
		builder.WriteString(model.guestDetailView())
	case screenActionConfirm:
		builder.WriteString(model.actionConfirmView())
	case screenActionRunning:
		builder.WriteString(panelStyle.Width(model.contentWidth()).Render("Working..."))
	case screenSnapshots:
		builder.WriteString(model.snapshotsView())
	case screenProxmoxLoading:
		builder.WriteString(panelStyle.Width(model.contentWidth()).Render(model.loadingMessage))
	case screenProxmoxTaskDetail:
		builder.WriteString(model.taskLogView())
	case screenProxmoxInfo:
		builder.WriteString(model.infoView())
	case screenProxmoxMetrics:
		builder.WriteString(model.nodeMetricsView())
	case screenCaddyLoading:
		builder.WriteString(panelStyle.Width(model.contentWidth()).Render("Loading Caddy routes..."))
	case screenCaddyWorking:
		builder.WriteString(panelStyle.Width(model.contentWidth()).Render("Working on Caddy config..."))
	case screenCaddyConfirm:
		builder.WriteString(model.caddyConfirmView())
	case screenCaddyDone:
		builder.WriteString(model.caddyDoneView())
	default:
		if model.isListScreen() {
			builder.WriteString(model.listView())
		}
		if model.isInputScreen() {
			builder.WriteString(model.inputView())
		}
		if model.isTextareaScreen() {
			builder.WriteString(model.textareaView())
		}
	}

	builder.WriteString("\n\n")
	builder.WriteString(helpStyle.Render(model.helpText()))

	return builder.String()
}

func (model *model) updateList(key string, message tea.KeyMsg) (tea.Model, tea.Cmd) {
	if (model.screen == screenSSHKey || model.screen == screenContainerSSHKey) && key == " " {
		selectedItem, ok := model.list.SelectedItem().(item)
		if !ok {
			return *model, nil
		}
		model.selectedSSHKeys[selectedItem.value] = !model.selectedSSHKeys[selectedItem.value]
		model.setList(model.screenTitle(), model.sshKeyItems())
		return *model, nil
	}

	if key == "enter" {
		selectedItem, ok := model.list.SelectedItem().(item)
		if !ok {
			return *model, nil
		}

		model.error = ""
		if model.screen == screenHome {
			return model.selectHomeItem(selectedItem.value)
		}
		if model.screen == screenManage {
			return model.selectVM(selectedItem.value)
		}
		if model.screen == screenVMAction {
			return model.selectVMAction(selectedItem.value)
		}
		if model.screen == screenCaddyMenu {
			return model.selectCaddyMenuItem(selectedItem.value)
		}
		if model.screen == screenCaddySites {
			return model.selectCaddySite(selectedItem.value)
		}
		if model.screen == screenCaddyRouteAction {
			return model.selectCaddyRouteAction(selectedItem.value)
		}
		if model.screen == screenProxmoxMenu {
			return model.selectProxmoxMenuItem(selectedItem.value)
		}
		if model.screen == screenProxmoxTasks {
			return model.selectTask(selectedItem.value)
		}
		if model.screen == screenProxmoxStorageNode {
			return model.selectStorageNode(selectedItem.value)
		}
		if model.screen == screenProxmoxStorage {
			return model.selectStorage(selectedItem.value)
		}
		if model.screen == screenProxmoxStorageContent {
			return *model, nil
		}
		if model.screen == screenProxmoxMetricsNode {
			return model.selectMetricsNode(selectedItem.value)
		}
		if model.screen == screenSSHKey || model.screen == screenContainerSSHKey {
			if model.screen == screenContainerSSHKey {
				model.containerRequest.SSHKeys = model.selectedSSHKeyNames()
			} else {
				model.request.SSHKeys = model.selectedSSHKeyNames()
			}
			model.next()
			return *model, nil
		}
		if selectedItem.value == "" {
			return *model, nil
		}
		model.applySelection(selectedItem.value)
		model.next()
		return *model, nil
	}

	var command tea.Cmd
	model.list, command = model.list.Update(message)
	return *model, command
}

func (model *model) updateInput(key string, message tea.KeyMsg) (tea.Model, tea.Cmd) {
	if key == "enter" {
		if model.screen == screenSSHAlias {
			if err := model.applyInput(model.input.Value()); err != nil {
				model.error = err.Error()
				return *model, nil
			}
			model.sshAlias = strings.TrimSpace(model.input.Value())
			model.screen = screenActionRunning
			model.error = ""
			return *model, model.vmActionCommand()
		}

		if err := model.applyInput(model.input.Value()); err != nil {
			model.error = err.Error()
			return *model, nil
		}

		model.error = ""
		if model.screen == screenDeleteName {
			model.screen = screenActionRunning
			return *model, model.vmActionCommand()
		}
		model.next()
		return *model, nil
	}

	var command tea.Cmd
	model.input, command = model.input.Update(message)
	return *model, command
}

func (model *model) updateTextarea(key string, message tea.KeyMsg) (tea.Model, tea.Cmd) {
	if key == "ctrl+s" {
		model.request.SSHPublicKeys = publicKeysFromTextarea(model.textarea.Value())
		model.next()
		return *model, nil
	}

	var command tea.Cmd
	model.textarea, command = model.textarea.Update(message)
	return *model, command
}

func (model *model) applySelection(value string) {
	switch model.screen {
	case screenNode:
		model.request.Node = value
	case screenImage:
		model.request.Image = value
	case screenPlan:
		if strings.HasPrefix(value, suggestedPlanPrefix) {
			if suggestedPlan, exists := model.suggestedPlan(value); exists {
				model.request.Plan = "custom"
				model.request.Cores = suggestedPlan.Cores
				model.request.MemoryMB = suggestedPlan.MemoryMB
				model.request.DiskGB = suggestedPlan.DiskGB
				model.manualResources = false
			}
			return
		}

		model.request.Plan = value
		model.manualResources = value == "custom"
		if value == "custom" {
			model.request.Cores = 0
			model.request.MemoryMB = 0
			model.request.DiskGB = 0
		} else {
			model.request.Cores = 0
			model.request.MemoryMB = 0
			model.request.DiskGB = 0
		}
	case screenStorage:
		model.request.Storage = value
	case screenSSHKey:
		model.request.SSHKey = value
	case screenNetwork:
		model.request.NetworkMode = value
	case screenContainerNode:
		model.containerRequest.Node = value
	case screenContainerImage:
		model.containerRequest.Image = value
	case screenContainerStorage:
		model.containerRequest.Storage = value
	case screenContainerPlan:
		model.containerRequest.Plan = value
	case screenContainerNetwork:
		model.containerRequest.NetworkMode = value
	case screenCaddyVisibility:
		model.caddyRequest.Visibility = value
		if value == app.CaddyVisibilityPublic && !model.caddyRequest.WAFExplicit {
			model.caddyRequest.UseWAF = true
		}
		if value == app.CaddyVisibilityInternal && !model.caddyRequest.WAFExplicit {
			model.caddyRequest.UseWAF = false
		}
	case screenCaddyType:
		model.caddyRequest.AppType = value
	case screenCaddyScheme:
		model.caddyRequest.UpstreamScheme = value
	case screenCaddyWAF:
		model.caddyRequest.UseWAF = value == "yes"
		model.caddyRequest.WAFExplicit = true
	case screenCaddyDeploy:
		model.caddyRequest.Deploy = value == "yes"
	}
}

func (model *model) applyInput(value string) error {
	value = strings.TrimSpace(value)
	if value == "" && model.screen != screenStaticDNS && model.screen != screenSSHAlias {
		return fmt.Errorf("value is required")
	}

	switch model.screen {
	case screenCustomCores:
		cores, err := strconv.Atoi(value)
		if err != nil || cores <= 0 {
			return fmt.Errorf("enter a positive core count")
		}
		model.request.Cores = cores
	case screenCustomMemory:
		memoryMB, err := strconv.Atoi(value)
		if err != nil || memoryMB <= 0 {
			return fmt.Errorf("enter memory in MB")
		}
		model.request.MemoryMB = memoryMB
	case screenCustomDisk:
		diskGB, err := strconv.Atoi(value)
		if err != nil || diskGB <= 0 {
			return fmt.Errorf("enter disk size in GB")
		}
		model.request.DiskGB = diskGB
	case screenStaticIP:
		model.request.IPAddress = value
	case screenStaticGateway:
		model.request.Gateway = value
	case screenStaticDNS:
		model.request.DNS = value
	case screenName:
		model.request.Name = value
	case screenContainerName:
		model.containerRequest.Name = value
	case screenSSHAlias:
		model.sshAlias = value
	case screenDeleteName:
		if value != model.selectedVM.Name {
			return fmt.Errorf("typed name must match %q", model.selectedVM.Name)
		}
	case screenCaddyDomain:
		model.caddyRequest.Domain = value
	case screenCaddyHost:
		model.caddyRequest.UpstreamHost = value
	case screenCaddyPort:
		port, err := strconv.Atoi(value)
		if err != nil || port <= 0 || port > 65535 {
			return fmt.Errorf("enter a port between 1 and 65535")
		}
		model.caddyRequest.UpstreamPort = port
	case screenCaddyRoot:
		model.caddyRequest.RootPath = value
	}

	return nil
}

func (model *model) next() {
	switch model.screen {
	case screenNode:
		model.screen = screenImage
		model.setList("Choose Image", model.imageItems())
	case screenImage:
		model.screen = screenStorage
		model.setList("Choose Storage", model.storageItems())
	case screenStorage:
		model.screen = screenPlan
		model.setList("Choose Size", model.planItems())
	case screenPlan:
		if model.request.Plan == "custom" && model.manualResources {
			model.screen = screenCustomCores
			model.setInput("Custom CPU cores", "4")
			return
		}
		model.screen = screenSSHKey
		model.setList("Choose SSH Keys", model.sshKeyItems())
	case screenCustomCores:
		model.screen = screenCustomMemory
		model.setInput("Custom memory in MB", "8192")
	case screenCustomMemory:
		model.screen = screenCustomDisk
		model.setInput("Custom disk in GB", "80")
	case screenCustomDisk:
		model.screen = screenSSHKey
		model.setList("Choose SSH Keys", model.sshKeyItems())
	case screenSSHKey:
		model.request.SSHKeys = model.selectedSSHKeyNames()
		model.screen = screenPasteSSHKeys
		model.setTextarea("Paste additional public keys", strings.Join(model.request.SSHPublicKeys, "\n"))
	case screenPasteSSHKeys:
		model.request.SSHPublicKeys = publicKeysFromTextarea(model.textarea.Value())
		model.screen = screenNetwork
		model.setList("Choose Network", model.networkItems())
	case screenNetwork:
		if model.request.NetworkMode == "static" {
			model.screen = screenStaticIP
			model.setInput("Static IP/CIDR", "192.0.2.50/24")
			return
		}
		model.screen = screenName
		model.setInput("VM name", "test-api-01")
	case screenStaticIP:
		model.screen = screenStaticGateway
		gateway := strings.TrimSpace(model.service.Config.Defaults.StaticGateway)
		if gateway == "" {
			gateway = "192.0.2.1"
		}
		model.setInput("Gateway", gateway)
	case screenStaticGateway:
		model.screen = screenStaticDNS
		dns := strings.TrimSpace(model.service.Config.Defaults.StaticDNS)
		if dns == "" {
			dns = "192.0.2.1"
		}
		model.setInput("DNS server", dns)
	case screenStaticDNS:
		model.screen = screenName
		model.setInput("VM name", "test-api-01")
	case screenName:
		model.screen = screenConfirm
	case screenContainerNode:
		model.screen = screenContainerImage
		model.setList("Choose LXC Image", model.lxcImageItems())
	case screenContainerImage:
		model.screen = screenContainerStorage
		model.setList("Choose Rootfs Storage", model.storageItemsForNode(model.containerRequest.Node))
	case screenContainerStorage:
		model.screen = screenContainerPlan
		model.setList("Choose Size", model.configuredPlanItems())
	case screenContainerPlan:
		model.screen = screenContainerSSHKey
		model.setList("Choose SSH Keys", model.sshKeyItems())
	case screenContainerSSHKey:
		model.containerRequest.SSHKeys = model.selectedSSHKeyNames()
		model.containerRequest.NetworkMode = "dhcp"
		model.screen = screenContainerName
		model.setInput("Container hostname", "test-lxc-01")
	case screenContainerName:
		model.screen = screenContainerConfirm
	case screenCaddyDomain:
		model.screen = screenCaddyVisibility
		model.setList("Caddy Visibility", model.caddyVisibilityItems())
	case screenCaddyVisibility:
		model.screen = screenCaddyType
		model.setList("Caddy Site Type", model.caddyTypeItems())
	case screenCaddyType:
		template := caddyTUITemplate(model.caddyRequest.AppType)
		if !template.NeedsTarget {
			if template.NeedsRoot {
				model.screen = screenCaddyRoot
				model.setInput("Root path", model.caddyDefaultRoot())
				return
			}
			model.screen = screenCaddyWAF
			model.setList("Caddy WAF", model.caddyWAFItems())
			return
		}
		model.screen = screenCaddyScheme
		model.setList("Caddy Upstream Scheme", model.caddySchemeItems())
	case screenCaddyScheme:
		model.screen = screenCaddyHost
		model.setInput("Internal IP or host", caddyFallbackHost(model.caddyRequest.UpstreamHost))
	case screenCaddyHost:
		model.screen = screenCaddyPort
		model.setInput("Internal port", caddyFallbackPort(model.caddyRequest.UpstreamPort))
	case screenCaddyPort:
		if caddyTUITemplate(model.caddyRequest.AppType).NeedsRoot {
			model.screen = screenCaddyRoot
			model.setInput("Root path", model.caddyDefaultRoot())
			return
		}
		model.screen = screenCaddyWAF
		model.setList("Caddy WAF", model.caddyWAFItems())
	case screenCaddyRoot:
		model.screen = screenCaddyWAF
		model.setList("Caddy WAF", model.caddyWAFItems())
	case screenCaddyWAF:
		model.screen = screenCaddyDeploy
		model.setList("Deploy Caddy", model.caddyDeployItems())
	case screenCaddyDeploy:
		model.screen = screenCaddyConfirm
	}
}

func (model *model) previous() {
	switch model.screen {
	case screenHome:
		model.screen = screenHome
		model.setList("Home", model.homeItems())
	case screenNode:
		model.screen = screenHome
		model.setList("Home", model.homeItems())
	case screenImage:
		model.screen = screenNode
		model.setList("Choose Node", model.nodeItems())
	case screenStorage:
		model.screen = screenImage
		model.setList("Choose Image", model.imageItems())
	case screenPlan:
		model.screen = screenStorage
		model.setList("Choose Storage", model.storageItems())
	case screenCustomCores:
		model.screen = screenPlan
		model.setList("Choose Size", model.planItems())
	case screenCustomMemory:
		model.screen = screenCustomCores
		model.setInput("Custom CPU cores", strconv.Itoa(model.request.Cores))
	case screenCustomDisk:
		model.screen = screenCustomMemory
		model.setInput("Custom memory in MB", strconv.Itoa(model.request.MemoryMB))
	case screenSSHKey:
		if model.manualResources {
			model.screen = screenCustomDisk
			model.setInput("Custom disk in GB", strconv.Itoa(model.request.DiskGB))
			return
		}
		model.screen = screenPlan
		model.setList("Choose Size", model.planItems())
	case screenPasteSSHKeys:
		model.screen = screenSSHKey
		model.setList("Choose SSH Keys", model.sshKeyItems())
	case screenNetwork:
		model.screen = screenPasteSSHKeys
		model.setTextarea("Paste additional public keys", strings.Join(model.request.SSHPublicKeys, "\n"))
	case screenStaticIP:
		model.screen = screenNetwork
		model.setList("Choose Network", model.networkItems())
	case screenStaticGateway:
		model.screen = screenStaticIP
		model.setInput("Static IP/CIDR", model.request.IPAddress)
	case screenStaticDNS:
		model.screen = screenStaticGateway
		model.setInput("Gateway", model.request.Gateway)
	case screenName:
		if model.request.NetworkMode == "static" {
			model.screen = screenStaticDNS
			model.setInput("DNS server", model.request.DNS)
			return
		}
		model.screen = screenNetwork
		model.setList("Choose Network", model.networkItems())
	case screenConfirm:
		model.screen = screenName
		model.setInput("VM name", model.request.Name)
	case screenContainerNode:
		model.screen = screenHome
		model.setList("Home", model.homeItems())
	case screenContainerImage:
		model.screen = screenContainerNode
		model.setList("Choose Node", model.nodeItems())
	case screenContainerStorage:
		model.screen = screenContainerImage
		model.setList("Choose LXC Image", model.lxcImageItems())
	case screenContainerPlan:
		model.screen = screenContainerStorage
		model.setList("Choose Rootfs Storage", model.storageItemsForNode(model.containerRequest.Node))
	case screenContainerSSHKey:
		model.screen = screenContainerPlan
		model.setList("Choose Size", model.configuredPlanItems())
	case screenContainerNetwork:
		model.screen = screenContainerSSHKey
		model.setList("Choose SSH Keys", model.sshKeyItems())
	case screenContainerName:
		model.screen = screenContainerSSHKey
		model.setList("Choose SSH Keys", model.sshKeyItems())
	case screenContainerConfirm:
		model.screen = screenContainerName
		model.setInput("Container hostname", model.containerRequest.Name)
	case screenManage:
		model.screen = screenHome
		model.setList("Home", model.homeItems())
	case screenGuestDetail:
		model.screen = screenManage
		model.setList(model.manageListTitle(), model.vmItems())
	case screenVMAction:
		model.screen = screenGuestDetail
	case screenActionConfirm, screenSnapshots:
		model.screen = screenVMAction
		model.setList(model.guestActionTitle(), model.vmActionItems())
	case screenDeleteName:
		model.screen = screenVMAction
		model.setList(model.guestActionTitle(), model.vmActionItems())
	case screenSSHAlias:
		model.screen = screenVMAction
		model.setList(model.guestActionTitle(), model.vmActionItems())
	case screenCaddyMenu:
		model.screen = screenHome
		model.setList("Home", model.homeItems())
	case screenCaddySites:
		model.screen = screenCaddyMenu
		model.setList("Caddy Routes", model.caddyMenuItems())
	case screenCaddyRouteAction:
		model.screen = screenCaddySites
		model.setList("Caddy Sites", model.caddySiteItems())
	case screenCaddyDomain:
		model.screen = screenCaddyMenu
		model.setList("Caddy Routes", model.caddyMenuItems())
	case screenCaddyVisibility:
		if model.caddyRequest.Domain == model.selectedCaddy.Domain && model.caddyRequest.Domain != "" {
			model.screen = screenCaddyRouteAction
			model.setList("Caddy Route Actions", model.caddyRouteActionItems())
			return
		}
		model.screen = screenCaddyDomain
		model.setInput("Domain", "app."+model.service.Config.Caddy.DefaultDomain)
	case screenCaddyType:
		model.screen = screenCaddyVisibility
		model.setList("Caddy Visibility", model.caddyVisibilityItems())
	case screenCaddyScheme:
		model.screen = screenCaddyType
		model.setList("Caddy Site Type", model.caddyTypeItems())
	case screenCaddyHost:
		model.screen = screenCaddyScheme
		model.setList("Caddy Upstream Scheme", model.caddySchemeItems())
	case screenCaddyPort:
		model.screen = screenCaddyHost
		model.setInput("Internal IP or host", model.caddyRequest.UpstreamHost)
	case screenCaddyRoot:
		if caddyTUITemplate(model.caddyRequest.AppType).NeedsTarget {
			model.screen = screenCaddyPort
			model.setInput("Internal port", caddyFallbackPort(model.caddyRequest.UpstreamPort))
			return
		}
		model.screen = screenCaddyType
		model.setList("Caddy Site Type", model.caddyTypeItems())
	case screenCaddyWAF:
		template := caddyTUITemplate(model.caddyRequest.AppType)
		if template.NeedsRoot {
			model.screen = screenCaddyRoot
			model.setInput("Root path", model.caddyDefaultRoot())
			return
		}
		if !template.NeedsTarget {
			model.screen = screenCaddyType
			model.setList("Caddy Site Type", model.caddyTypeItems())
			return
		}
		model.screen = screenCaddyPort
		model.setInput("Internal port", caddyFallbackPort(model.caddyRequest.UpstreamPort))
	case screenCaddyDeploy:
		model.screen = screenCaddyWAF
		model.setList("Caddy WAF", model.caddyWAFItems())
	case screenCaddyConfirm:
		model.screen = screenCaddyDeploy
		model.setList("Deploy Caddy", model.caddyDeployItems())
	case screenCaddyDone:
		model.screen = screenCaddyMenu
		model.setList("Caddy Routes", model.caddyMenuItems())
	case screenProxmoxMenu:
		model.screen = screenHome
		model.setList("Home", model.homeItems())
	case screenProxmoxTasks:
		model.screen = screenProxmoxMenu
		model.setList("Proxmox Ops", model.proxmoxMenuItems())
	case screenProxmoxTaskDetail:
		model.screen = screenProxmoxTasks
		model.setList("Recent Proxmox Tasks", model.taskItems())
	case screenProxmoxStorageNode:
		model.screen = screenProxmoxMenu
		model.setList("Proxmox Ops", model.proxmoxMenuItems())
	case screenProxmoxStorage:
		model.screen = screenProxmoxStorageNode
		model.setList("Choose Storage Node", model.nodeItems())
	case screenProxmoxStorageContent:
		model.screen = screenProxmoxStorage
		model.setList("Choose Storage", model.allStorageItemsForNode(model.storageNode))
	case screenProxmoxInfo:
		model.restoreInfoBackScreen(model.infoBackScreen)
	case screenProxmoxMetricsNode:
		model.screen = screenProxmoxMenu
		model.setList("Proxmox Ops", model.proxmoxMenuItems())
	case screenProxmoxMetrics:
		model.screen = screenProxmoxMetricsNode
		model.setList("Choose Metrics Node", model.nodeItems())
	}
}

func (model *model) setList(title string, items []list.Item) {
	var delegate list.ItemDelegate = itemDelegate{}
	if model.screen == screenHome {
		delegate = homeItemDelegate{}
	} else if model.screen == screenManage {
		delegate = guestItemDelegate{}
	}
	model.list = list.New(items, delegate, model.listWidth(), model.listHeight())
	model.list.Title = ""
	model.list.SetShowTitle(false)
	model.list.SetShowStatusBar(false)
	model.list.SetFilteringEnabled(true)
	model.list.SetShowHelp(false)
	model.list.SetShowPagination(false)
	if model.screen == screenManage {
		model.list.FilterInput.Prompt = "Search: "
		model.list.KeyMap.Filter.SetHelp("/", "search")
		model.list.SetSize(model.listWidth(), model.listHeight())
	}
}

func (model *model) setInput(prompt string, placeholder string) {
	model.input = textinput.New()
	model.input.Placeholder = placeholder
	model.input.Prompt = prompt + ": "
	model.input.Focus()
}

func (model *model) setTextarea(prompt string, value string) {
	model.textarea = textarea.New()
	model.textarea.Placeholder = "Optional: paste one public key per line"
	model.textarea.Prompt = "│ "
	model.textarea.ShowLineNumbers = false
	model.textarea.SetWidth(model.contentWidth() - 8)
	model.textarea.SetHeight(6)
	model.textarea.SetValue(value)
	model.textarea.Focus()
}

func (model model) isListScreen() bool {
	switch model.screen {
	case screenHome, screenNode, screenImage, screenPlan, screenStorage, screenSSHKey, screenNetwork, screenContainerNode, screenContainerImage, screenContainerStorage, screenContainerPlan, screenContainerSSHKey, screenContainerNetwork, screenManage, screenVMAction, screenCaddyMenu, screenCaddySites, screenCaddyRouteAction, screenCaddyVisibility, screenCaddyType, screenCaddyScheme, screenCaddyWAF, screenCaddyDeploy, screenProxmoxMenu, screenProxmoxTasks, screenProxmoxStorageNode, screenProxmoxStorage, screenProxmoxStorageContent, screenProxmoxMetricsNode:
		return true
	default:
		return false
	}
}

func (model model) isInputScreen() bool {
	switch model.screen {
	case screenCustomCores, screenCustomMemory, screenCustomDisk, screenStaticIP, screenStaticGateway, screenStaticDNS, screenName, screenContainerName, screenDeleteName, screenCaddyDomain, screenCaddyHost, screenCaddyPort, screenCaddyRoot:
		return true
	case screenSSHAlias:
		return true
	default:
		return false
	}
}

func (model model) isTextareaScreen() bool {
	return model.screen == screenPasteSSHKeys
}

func (model model) homeItems() []list.Item {
	return []list.Item{
		item{title: "Create VM", description: "Launch a new VM from a prepared template", value: "create"},
		item{title: "Create LXC", description: "Launch an LXC container with your SSH key injected", value: "create-lxc"},
		item{title: "Manage VMs", description: "Browse QEMU VMs and run lifecycle actions", value: "manage-vms"},
		item{title: "Manage LXC", description: "Browse LXC containers and run lifecycle actions", value: "manage-containers"},
		item{title: "Caddy Routes", description: "Add, list, validate, and deploy reverse proxy routes", value: "caddy"},
		item{title: "Proxmox Ops", description: "Inspect config, metrics, recent tasks, storage content, and agent commands", value: "proxmox"},
		item{title: "Refresh Dashboard", description: "Refresh cluster, guest, storage, and RRD data", value: "refresh"},
	}
}

func (model model) vmItems() []list.Item {
	items := make([]list.Item, 0, len(model.vms))
	for _, vm := range model.vms {
		guest := vm
		description := fmt.Sprintf("%s · %s · RAM %s · disk %s", vm.Node, vm.Status, formatBytes(vm.MaxMem), formatBytes(vm.MaxDisk))
		if vm.Tags != "" {
			description += " · tags " + formatValues(strings.Split(vm.Tags, ";"))
		}
		guestKind := "vm"
		if vm.IsContainer() {
			guestKind = "container lxc"
		}
		items = append(items, item{
			title:       fmt.Sprintf("%d  %s", vm.VMID, vm.Name),
			description: description,
			value:       strconv.Itoa(vm.VMID),
			guest:       &guest,
			filterValue: strings.Join([]string{
				strconv.Itoa(vm.VMID),
				vm.Name,
				vm.Node,
				vm.Status,
				vm.Tags,
				vm.GuestType(),
				guestKind,
			}, " "),
		})
	}
	if len(items) == 0 {
		items = append(items, item{title: "No " + model.manageKindPlural() + " found", description: "No non-template guests of this type were returned", value: ""})
	}

	return items
}

func (model model) vmActionItems() []list.Item {
	kind := model.selectedGuestKind()
	items := []list.Item{
		item{title: "Start", description: "Start this " + kind, value: "start"},
		item{title: "Stop", description: "Shutdown this " + kind, value: "stop"},
		item{title: "Reboot", description: "Reboot this " + kind, value: "reboot"},
	}
	if !model.selectedVM.IsContainer() {
		items = append(items, item{title: "SSH Command", description: "Resolve guest agent IP and show SSH command", value: "ssh"})
		items = append(items, item{title: "SSH Config", description: "Persist a host alias in ~/.ssh/config", value: "ssh-config"})
	} else {
		items = append(items, item{title: "Shell Command", description: "Show pct exec shell command through the Proxmox host", value: "shell"})
	}
	items = append(
		items,
		item{title: "Snapshots", description: "List snapshots for this " + kind, value: "snapshots"},
		item{title: "Export Spec", description: "Show deterministic YAML for drift review and apply", value: "export-spec"},
		item{title: "Delete", description: "Destroy this " + kind, value: "delete"},
	)

	return items
}

func (model model) nodeItems() []list.Item {
	items := make([]list.Item, 0, len(model.service.Config.Nodes))
	for _, nodeName := range model.service.Config.NodeNames() {
		node := model.service.Config.Nodes[nodeName]
		items = append(items, item{
			title:       nodeName,
			description: node.Label,
			value:       nodeName,
		})
	}

	return items
}

func (model model) imageItems() []list.Item {
	items := make([]list.Item, 0, len(model.service.Config.Images))
	for _, imageName := range model.service.Config.ImageNames() {
		image := model.service.Config.Images[imageName]
		if image.Templates[model.request.Node] == 0 {
			continue
		}
		description := image.Family + " · user " + image.DefaultUser
		if image.Recommended {
			description += " · recommended"
		}
		items = append(items, item{
			title:       image.Label,
			description: description,
			value:       imageName,
		})
	}
	if len(items) == 0 {
		items = append(items, item{
			title:       "No templates on this node",
			description: "Create or map templates before creating VMs here",
			value:       "",
		})
	}

	return items
}

func (model model) lxcImageItems() []list.Item {
	items := make([]list.Item, 0, len(model.service.Config.LXCImages))
	for _, imageName := range model.service.Config.LXCImageNames() {
		image := model.service.Config.LXCImages[imageName]
		if image.Templates[model.containerRequest.Node] == "" {
			continue
		}
		description := image.Family + " · user " + image.DefaultUser
		if image.Recommended {
			description += " · recommended"
		}
		items = append(items, item{
			title:       image.Label,
			description: description,
			value:       imageName,
		})
	}
	if len(items) == 0 {
		items = append(items, item{
			title:       "No LXC templates on this node",
			description: "Download or map LXC templates before creating containers here",
			value:       "",
		})
	}

	return items
}

func (model model) planItems() []list.Item {
	suggestedPlans := model.service.SuggestedPlans(model.health, model.request.Node, model.request.Storage)
	items := make([]list.Item, 0, len(suggestedPlans)+1)
	for _, plan := range suggestedPlans {
		items = append(items, item{
			title:       plan.Label,
			description: fmt.Sprintf("%d vCPU · %s RAM · %d GB disk · %s", plan.Cores, formatMemoryMB(plan.MemoryMB), plan.DiskGB, plan.Reason),
			value:       suggestedPlanPrefix + plan.Name,
		})
	}

	items = append(items, item{
		title:       "Custom",
		description: "Choose CPU, RAM, and disk manually",
		value:       "custom",
	})

	return items
}

func (model model) configuredPlanItems() []list.Item {
	items := make([]list.Item, 0, len(model.service.Config.Plans))
	for _, planName := range model.service.Config.PlanNames() {
		plan := model.service.Config.Plans[planName]
		items = append(items, item{
			title:       plan.Label,
			description: fmt.Sprintf("%d vCPU · %s RAM · %d GB rootfs", plan.Cores, formatMemoryMB(plan.MemoryMB), plan.DiskGB),
			value:       planName,
		})
	}

	return items
}

func (model model) storageItems() []list.Item {
	storageNames := model.service.Config.StorageNames()
	if node, exists := model.service.Config.Nodes[model.request.Node]; exists && len(node.Storages) > 0 {
		storageNames = node.Storages
	}

	return model.storageItemsFromNames(model.request.Node, storageNames)
}

func (model model) storageItemsForNode(nodeName string) []list.Item {
	storageNames := model.service.Config.StorageNames()
	if node, exists := model.service.Config.Nodes[nodeName]; exists && len(node.Storages) > 0 {
		storageNames = node.Storages
	}

	return model.storageItemsFromNames(nodeName, storageNames)
}

func (model model) allStorageItemsForNode(nodeName string) []list.Item {
	return model.storageItemsFromNames(nodeName, model.service.Config.StorageNames())
}

func (model model) storageItemsFromNames(nodeName string, storageNames []string) []list.Item {
	items := make([]list.Item, 0, len(storageNames))
	for _, storageName := range storageNames {
		storage := model.service.Config.Storages[storageName]
		description := storage.Label
		if storageHealth, exists := model.storageHealthForNode(nodeName, storageName); exists {
			description = fmt.Sprintf("%s · %s available", storage.Label, formatBytes(storageHealth.AvailableBytes))
		}
		items = append(items, item{
			title:       storageName,
			description: description,
			value:       storageName,
		})
	}

	return items
}

func (model model) sshKeyItems() []list.Item {
	items := make([]list.Item, 0, len(model.service.Config.SSHKeys))
	for _, keyName := range model.service.Config.SSHKeyNames() {
		key := model.service.Config.SSHKeys[keyName]
		marker := "[ ]"
		if model.selectedSSHKeys[keyName] {
			marker = "[x]"
		}
		items = append(items, item{
			title:       marker + " " + keyName,
			description: key.Path,
			value:       keyName,
		})
	}

	return items
}

func (model model) networkItems() []list.Item {
	return []list.Item{
		item{title: "DHCP", description: "Use cloud-init DHCP and wait for guest agent IP", value: "dhcp"},
		item{title: "Static", description: "Prompt for IP/CIDR, gateway, and DNS", value: "static"},
	}
}

func (model model) inputView() string {
	return panelStyle.Width(model.contentWidth()).Render(model.input.View())
}

func (model model) textareaView() string {
	var builder strings.Builder
	builder.WriteString(sectionTitleStyle.Render("Paste Public Keys"))
	builder.WriteString("\n\n")
	builder.WriteString(model.textarea.View())
	builder.WriteString("\n")
	builder.WriteString(helpStyle.Render("Optional. One public key per line. Press ctrl+s to continue."))
	return panelStyle.Width(model.contentWidth()).Render(builder.String())
}

func (model model) confirmView() string {
	preview, err := model.service.CreatePreview(model.request)
	if err != nil {
		return panelStyle.Width(model.contentWidth()).Render(errorStyle.Render(err.Error()))
	}

	var builder strings.Builder
	builder.WriteString("Create VM\n\n")
	builder.WriteString(fmt.Sprintf("Name:       %s\n", preview.Name))
	builder.WriteString(fmt.Sprintf("Node:       %s\n", preview.Node))
	builder.WriteString(fmt.Sprintf("Template:   %d\n", preview.TemplateID))
	builder.WriteString(fmt.Sprintf("Storage:    %s\n", preview.Storage))
	builder.WriteString(fmt.Sprintf("Network:    %s\n", model.networkSummary()))
	builder.WriteString("\nProxmox operations\n")
	builder.WriteString(fmt.Sprintf("POST /nodes/%s/qemu/%d/clone %s\n", preview.Node, preview.TemplateID, inlineParams(preview.CloneParams)))
	builder.WriteString(fmt.Sprintf("PUT  /nodes/%s/qemu/<nextid>/resize %s\n", preview.Node, inlineParams(preview.ResizeParams)))
	builder.WriteString(fmt.Sprintf("PUT  /nodes/%s/qemu/<nextid>/config %s\n", preview.Node, inlineParams(preview.ConfigParams)))
	builder.WriteString(fmt.Sprintf("POST %s\n", preview.StartPath))
	builder.WriteString("\nPress enter to create.")

	return panelStyle.Width(model.contentWidth()).Render(builder.String())
}

func (model model) doneView() string {
	if model.error != "" {
		var builder strings.Builder
		builder.WriteString(errorStyle.Render("Create failed"))
		builder.WriteString("\n\n")
		builder.WriteString(lipgloss.NewStyle().Width(model.contentWidth() - 6).Render(model.error))
		if len(model.steps) > 0 {
			builder.WriteString("\n\n")
			for _, step := range model.steps {
				builder.WriteString(mutedStyle.Render(step))
				builder.WriteString("\n")
			}
		}
		if model.partialError != nil && !model.partialCleaned {
			builder.WriteString("\n\n")
			builder.WriteString(warnStyle.Render(fmt.Sprintf("Partial VM %d exists on %s. Press d to destroy it.", model.partialError.VMID, model.partialError.Node)))
		}
		if model.partialCleaned {
			builder.WriteString("\n\n")
			builder.WriteString(successStyle.Render("Partial VM cleanup completed."))
		}

		return panelStyle.Width(model.contentWidth()).Render(strings.TrimRight(builder.String(), "\n"))
	}

	var builder strings.Builder
	builder.WriteString(successStyle.Render("VM ready"))
	builder.WriteString("\n\n")
	for _, step := range model.steps {
		builder.WriteString("✓ ")
		builder.WriteString(step)
		builder.WriteString("\n")
	}
	builder.WriteString("\n")
	builder.WriteString(fmt.Sprintf("Name: %s\n", model.result.Name))
	builder.WriteString(fmt.Sprintf("VMID: %d\n", model.result.VMID))
	builder.WriteString(fmt.Sprintf("Node: %s\n", model.result.Node))
	if model.result.Plan != "" {
		builder.WriteString(fmt.Sprintf("Plan: %s\n", model.result.Plan))
	}
	if model.result.StaticIP {
		builder.WriteString("Network: static from DHCP lease\n")
	} else {
		builder.WriteString(fmt.Sprintf("Network: %s\n", model.result.NetworkMode))
	}
	if model.sshConfigStatus != "" {
		builder.WriteString(fmt.Sprintf("SSH config: %s\n", model.sshConfigStatus))
	}
	if model.result.Warning != "" {
		builder.WriteString("\n")
		builder.WriteString(warnStyle.Render(model.result.Warning))
		builder.WriteString("\n")
	}
	if model.result.IP != "" {
		builder.WriteString(fmt.Sprintf("IP:   %s\n", model.result.IP))
		builder.WriteString(fmt.Sprintf("SSH:  %s\n", model.result.SSHCommand))
		builder.WriteString(fmt.Sprintf("Fix:  %s  # if host key changed\n", model.result.SSHKeygen))
		if model.sshConfigStatus == "" {
			builder.WriteString("Add:  press a to persist this VM in ~/.ssh/config\n")
		}
	} else {
		builder.WriteString("IP:   not available from guest agent yet\n")
	}
	builder.WriteString("\nPress enter to exit.")

	return builder.String()
}

func (model model) containerConfirmView() string {
	preview, err := model.service.CreateContainerPreview(model.containerRequest)
	if err != nil {
		return panelStyle.Width(model.contentWidth()).Render(errorStyle.Render(err.Error()))
	}

	var builder strings.Builder
	builder.WriteString("Create LXC container\n\n")
	builder.WriteString(fmt.Sprintf("Name:       %s\n", preview.Name))
	builder.WriteString(fmt.Sprintf("Node:       %s\n", preview.Node))
	builder.WriteString(fmt.Sprintf("Template:   %s\n", preview.Template))
	builder.WriteString(fmt.Sprintf("Storage:    %s\n", preview.Storage))
	builder.WriteString(fmt.Sprintf("Network:    %s\n", model.containerNetworkSummary()))
	builder.WriteString("\nProxmox operation\n")
	builder.WriteString(fmt.Sprintf("POST %s %s\n", preview.CreatePath, inlineParams(preview.Params)))
	builder.WriteString("\nPress enter to create.")

	return panelStyle.Width(model.contentWidth()).Render(builder.String())
}

func (model model) containerDoneView() string {
	if model.error != "" {
		var builder strings.Builder
		builder.WriteString(errorStyle.Render("LXC create failed"))
		builder.WriteString("\n\n")
		builder.WriteString(lipgloss.NewStyle().Width(model.contentWidth() - 6).Render(model.error))
		if len(model.steps) > 0 {
			builder.WriteString("\n\n")
			for _, step := range model.steps {
				builder.WriteString(mutedStyle.Render(step))
				builder.WriteString("\n")
			}
		}

		return panelStyle.Width(model.contentWidth()).Render(strings.TrimRight(builder.String(), "\n"))
	}

	var builder strings.Builder
	builder.WriteString(successStyle.Render("LXC container ready"))
	builder.WriteString("\n\n")
	for _, step := range model.steps {
		builder.WriteString("✓ ")
		builder.WriteString(step)
		builder.WriteString("\n")
	}
	builder.WriteString("\n")
	builder.WriteString(fmt.Sprintf("Name: %s\n", model.containerResult.Name))
	builder.WriteString(fmt.Sprintf("VMID: %d\n", model.containerResult.VMID))
	builder.WriteString(fmt.Sprintf("Node: %s\n", model.containerResult.Node))
	if model.containerResult.Plan != "" {
		builder.WriteString(fmt.Sprintf("Plan: %s\n", model.containerResult.Plan))
	}
	builder.WriteString(fmt.Sprintf("Network: %s\n", model.containerResult.NetworkMode))
	if model.containerResult.Warning != "" {
		builder.WriteString("\n")
		builder.WriteString(warnStyle.Render(model.containerResult.Warning))
		builder.WriteString("\n")
	}
	if model.containerResult.IP != "" {
		builder.WriteString(fmt.Sprintf("IP:   %s\n", model.containerResult.IP))
		builder.WriteString(fmt.Sprintf("SSH:  %s\n", model.containerResult.SSHCommand))
		builder.WriteString(fmt.Sprintf("Fix:  %s  # if host key changed\n", model.containerResult.SSHKeygen))
	} else if model.containerResult.Started {
		builder.WriteString("IP:   not available from LXC interfaces yet\n")
	} else {
		builder.WriteString("IP:   container was not started\n")
	}
	builder.WriteString("\nPress enter to exit.")

	return builder.String()
}

func (model model) actionConfirmView() string {
	return panelStyle.Width(model.contentWidth()).Render(fmt.Sprintf(
		"%s %s %d (%s) on %s?\n\nPress enter to confirm or esc to go back.",
		strings.ToUpper(model.selectedAction[:1])+model.selectedAction[1:],
		model.selectedGuestKind(),
		model.selectedVM.VMID,
		model.selectedVM.Name,
		model.selectedVM.Node,
	))
}

func (model model) guestDetailView() string {
	detail := model.guestDetail
	guest := detail.Guest

	var builder strings.Builder
	status := successStyle.Render("● running")
	if guest.Status != "running" {
		status = mutedStyle.Render("● " + guest.Status)
	}
	title := sectionTitleStyle.Render(fmt.Sprintf("%d · %s", guest.VMID, guest.Name)) + "  " + status
	builder.WriteString(ansi.Truncate(title, model.contentWidth(), "…"))
	builder.WriteString("\n")
	metadata := mutedStyle.Render(model.selectedGuestTypeLabel() + " · " + guest.Node + " · uptime " + formatUptime(guest.Uptime))
	builder.WriteString(ansi.Truncate(metadata, model.contentWidth(), "…"))
	builder.WriteString("\n")
	diskUsage := formatBytes(guest.Disk)
	diskCapacity := formatBytes(guest.MaxDisk)
	if guest.Disk <= 0 && guest.MaxDisk > 0 {
		diskUsage = "n/a"
		diskCapacity += " provisioned"
	}
	resources := fmt.Sprintf(
		"CPU %5.1f%%   RAM %s / %s   Disk %s / %s",
		guest.CPU*100,
		formatBytes(guest.Mem),
		formatBytes(guest.MaxMem),
		diskUsage,
		diskCapacity,
	)
	builder.WriteString(ansi.Truncate(resources, model.contentWidth(), "…"))
	builder.WriteString(model.guestMetricsView())

	if model.height >= 30 {
		builder.WriteString("\n")
		detailLine := fmt.Sprintf("IPs %s   Tags %s   Snapshots %d", formatValues(detail.IPAddresses), formatValues(detail.Tags), detail.SnapshotCount)
		builder.WriteString(ansi.Truncate(detailLine, model.contentWidth(), "…"))
		if detail.ShellCommand != "" {
			builder.WriteString("\n")
			shellLine := mutedStyle.Render("Shell  ") + detail.ShellCommand
			builder.WriteString(ansi.Truncate(shellLine, model.contentWidth(), "…"))
		}
	}

	return builder.String()
}

func (model model) snapshotsView() string {
	var builder strings.Builder
	builder.WriteString(fmt.Sprintf("Snapshots for %s %d %s\n\n", model.selectedGuestKind(), model.selectedVM.VMID, model.selectedVM.Name))
	if model.error != "" {
		builder.WriteString(errorStyle.Render(model.error))
		return panelStyle.Width(model.contentWidth()).Render(builder.String())
	}
	if len(model.snapshots) == 0 {
		builder.WriteString("No snapshots found.")
	} else {
		for _, snapshot := range model.snapshots {
			builder.WriteString(fmt.Sprintf("%-24s %s\n", snapshot.Name, snapshot.Description))
		}
	}
	builder.WriteString("\nPress r to refresh or esc to go back.")
	return panelStyle.Width(model.contentWidth()).Render(builder.String())
}

func (model model) summaryResources() (int, int, int) {
	if model.request.Plan == "custom" {
		return model.request.Cores, model.request.MemoryMB, model.request.DiskGB
	}

	plan := model.service.Config.Plans[model.request.Plan]
	return plan.Cores, plan.MemoryMB, plan.DiskGB
}

func (model model) suggestedPlan(value string) (app.SuggestedPlan, bool) {
	planName := strings.TrimPrefix(value, suggestedPlanPrefix)
	for _, suggestedPlan := range model.service.SuggestedPlans(model.health, model.request.Node, model.request.Storage) {
		if suggestedPlan.Name == planName {
			return suggestedPlan, true
		}
	}

	return app.SuggestedPlan{}, false
}

func (model model) storageHealth(storageName string) (app.StorageHealth, bool) {
	return model.storageHealthForNode(model.request.Node, storageName)
}

func (model model) storageHealthForNode(nodeName string, storageName string) (app.StorageHealth, bool) {
	for _, storage := range model.health.Storages {
		if storage.Node == nodeName && storage.Name == storageName {
			return storage, true
		}
	}

	return app.StorageHealth{}, false
}

func (model model) networkSummary() string {
	if model.request.NetworkMode == "static" {
		return fmt.Sprintf("static %s gateway %s DNS %s", model.request.IPAddress, model.request.Gateway, model.request.DNS)
	}

	return "DHCP"
}

func (model model) containerNetworkSummary() string {
	if model.containerRequest.NetworkMode == "static" {
		return fmt.Sprintf("static %s gateway %s DNS %s", model.containerRequest.IPAddress, model.containerRequest.Gateway, model.containerRequest.DNS)
	}

	return "DHCP"
}

func (model model) headerView() string {
	return model.titleRule()
}

func (model model) listView() string {
	if model.screen == screenManage && model.width >= 110 {
		return model.manageSplitView()
	}

	var builder strings.Builder
	builder.WriteString(sectionTitleStyle.Render(model.screenTitle()))
	builder.WriteString("\n\n")
	if model.screen == screenVMAction && model.actionMessage != "" {
		builder.WriteString(successStyle.Render(model.actionMessage))
		builder.WriteString("\n\n")
	}
	builder.WriteString(model.list.View())

	return panelStyle.Width(model.contentWidth()).Render(builder.String())
}

func (model model) manageListTitle() string {
	if model.manageKind == manageKindContainer {
		return "Manage LXC Containers"
	}
	return "Manage VMs"
}

func (model model) manageKindSingular() string {
	if model.manageKind == manageKindContainer {
		return "container"
	}
	return "VM"
}

func (model model) manageKindPlural() string {
	if model.manageKind == manageKindContainer {
		return "containers"
	}
	return "VMs"
}

func (model model) selectedGuestKind() string {
	if model.selectedVM.IsContainer() {
		return "container"
	}
	return "VM"
}

func (model model) selectedGuestTypeLabel() string {
	if model.selectedVM.IsContainer() {
		return "LXC container"
	}
	return "QEMU VM"
}

func (model model) guestActionTitle() string {
	return fmt.Sprintf("%s %d Actions", strings.ToUpper(model.selectedGuestKind()[:1])+model.selectedGuestKind()[1:], model.selectedVM.VMID)
}

func (model model) screenTitle() string {
	switch model.screen {
	case screenHome:
		return "Home"
	case screenManage:
		return model.manageListTitle()
	case screenGuestDetail:
		return fmt.Sprintf("%s %d Details", strings.ToUpper(model.selectedGuestKind()[:1])+model.selectedGuestKind()[1:], model.selectedVM.VMID)
	case screenVMAction:
		return model.guestActionTitle()
	case screenDeleteName:
		return "Confirm Delete"
	case screenCaddyMenu:
		return "Caddy Routes"
	case screenCaddySites:
		return "Caddy Sites"
	case screenCaddyRouteAction:
		return "Caddy Route Actions"
	case screenCaddyVisibility:
		return "Caddy Visibility"
	case screenCaddyType:
		return "Caddy Site Type"
	case screenCaddyScheme:
		return "Caddy Upstream Scheme"
	case screenCaddyRoot:
		return "Caddy Root Path"
	case screenCaddyWAF:
		return "Caddy WAF"
	case screenCaddyDeploy:
		return "Deploy Caddy"
	case screenSSHAlias:
		return "SSH Alias"
	case screenProxmoxMenu:
		return "Proxmox Ops"
	case screenProxmoxTasks:
		return "Recent Proxmox Tasks"
	case screenProxmoxStorageNode:
		return "Choose Storage Node"
	case screenProxmoxStorage:
		return "Choose Storage"
	case screenProxmoxStorageContent:
		return fmt.Sprintf("%s/%s Content", model.storageNode, model.storageName)
	case screenProxmoxMetricsNode:
		return "Choose Metrics Node"
	case screenNode:
		return "Choose Node"
	case screenImage:
		return "Choose Image"
	case screenPlan:
		return "Choose Size"
	case screenStorage:
		return "Choose Storage"
	case screenSSHKey:
		return "Choose SSH Keys"
	case screenPasteSSHKeys:
		return "Paste Public Keys"
	case screenNetwork:
		return "Choose Network"
	case screenContainerNode:
		return "Choose Node"
	case screenContainerImage:
		return "Choose LXC Image"
	case screenContainerStorage:
		return "Choose Rootfs Storage"
	case screenContainerPlan:
		return "Choose Size"
	case screenContainerSSHKey:
		return "Choose SSH Keys"
	case screenContainerNetwork:
		return "Choose Network"
	case screenContainerName:
		return "Container Hostname"
	default:
		return "Create VM"
	}
}

func (model *model) selectHomeItem(value string) (tea.Model, tea.Cmd) {
	switch value {
	case "create":
		model.screen = screenNode
		model.setList("Choose Node", model.nodeItems())
		return *model, nil
	case "create-lxc":
		model.containerRequest = app.CreateContainerRequest{Start: true, Unprivileged: true, NetworkMode: "dhcp"}
		model.screen = screenContainerNode
		model.setList("Choose Node", model.nodeItems())
		return *model, nil
	case "manage-vms":
		model.manageKind = manageKindVM
		model.screen = screenManageLoading
		return *model, model.loadVMsCommand()
	case "manage-containers":
		model.manageKind = manageKindContainer
		model.screen = screenManageLoading
		return *model, model.loadVMsCommand()
	case "caddy":
		model.screen = screenCaddyMenu
		model.setList("Caddy Routes", model.caddyMenuItems())
		return *model, nil
	case "proxmox":
		model.screen = screenProxmoxMenu
		model.setList("Proxmox Ops", model.proxmoxMenuItems())
		return *model, nil
	case "refresh":
		model.checkingHealth = true
		return *model, tea.Batch(model.healthCommand(), model.loadDashboardGuestsCommand())
	default:
		return *model, nil
	}
}

func (model *model) selectVM(value string) (tea.Model, tea.Cmd) {
	if value == "" {
		return *model, nil
	}
	vmid, err := strconv.Atoi(value)
	if err != nil {
		model.error = err.Error()
		return *model, nil
	}
	for _, vm := range model.vms {
		if vm.VMID == vmid {
			model.selectedVM = vm
			model.guestMetrics = nil
			model.guestMetricsErr = ""
			model.screen = screenGuestLoading
			return *model, tea.Batch(model.loadGuestDetailCommand(), model.loadGuestMetricsCommand())
		}
	}
	model.error = fmt.Sprintf("%s %d was not found", model.manageKindSingular(), vmid)
	return *model, nil
}

func (model *model) selectVMAction(value string) (tea.Model, tea.Cmd) {
	model.selectedAction = value
	model.actionMessage = ""
	switch value {
	case "delete":
		model.screen = screenDeleteName
		model.setInput("Type "+model.selectedVM.Name+" to delete", "")
		return *model, nil
	case "ssh-config":
		model.sshAlias = model.selectedVM.Name
		model.screen = screenSSHAlias
		model.setInput("SSH alias", model.sshAlias)
		return *model, nil
	case "stop", "reboot":
		model.screen = screenActionConfirm
		return *model, nil
	case "snapshots":
		model.screen = screenActionRunning
		return *model, model.loadSnapshotsCommand()
	case "export-spec":
		model.screen = screenActionRunning
		return *model, model.exportGuestSpecCommand()
	default:
		model.screen = screenActionRunning
		return *model, model.vmActionCommand()
	}
}

func (model model) createCommand() tea.Cmd {
	request := model.request
	service := model.service

	return func() tea.Msg {
		var steps []string
		result, err := service.CreateVM(context.Background(), request, func(message string) {
			steps = append(steps, message)
		})

		return createFinishedMsg{
			result: result,
			steps:  steps,
			err:    err,
		}
	}
}

func (model model) createContainerCommand() tea.Cmd {
	request := model.containerRequest
	service := model.service

	return func() tea.Msg {
		var steps []string
		result, err := service.CreateContainer(context.Background(), request, func(message string) {
			steps = append(steps, message)
		})

		return createContainerFinishedMsg{
			result: result,
			steps:  steps,
			err:    err,
		}
	}
}

func (model model) createSSHConfigCommand(vmid int, alias string, fromCreate bool) tea.Cmd {
	service := model.service
	return func() tea.Msg {
		result, err := service.EnsureSSHConfig(context.Background(), vmid, alias, "", false)
		return sshConfigMsg{result: result, err: err, fromCreate: fromCreate}
	}
}

func (model model) loadVMsCommand() tea.Cmd {
	service := model.service
	manageKind := model.manageKind
	return func() tea.Msg {
		var (
			vms []proxmox.VMResource
			err error
		)
		if manageKind == manageKindContainer {
			vms, err = service.ListContainers(context.Background())
		} else {
			vms, err = service.ListVMs(context.Background())
		}
		return vmsLoadedMsg{vms: vms, err: err}
	}
}

func (model model) loadGuestDetailCommand() tea.Cmd {
	service := model.service
	vmid := model.selectedVM.VMID
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()

		detail, err := service.GuestDetail(ctx, vmid)
		return guestDetailLoadedMsg{vmid: vmid, detail: detail, err: err}
	}
}

func (model model) vmActionCommand() tea.Cmd {
	service := model.service
	vm := model.selectedVM
	action := model.selectedAction
	return func() tea.Msg {
		ctx := context.Background()
		switch action {
		case "ssh":
			sshCommand, err := service.SSHCommand(ctx, vm.VMID, "")
			return actionFinishedMsg{message: sshCommand, err: err}
		case "ssh-config":
			alias := strings.TrimSpace(model.sshAlias)
			if alias == "" {
				alias = vm.Name
			}
			result, err := service.EnsureSSHConfig(ctx, vm.VMID, alias, "", false)
			return sshConfigMsg{result: result, err: err, fromCreate: false}
		case "shell":
			shellCommand, err := service.ContainerShellCommand(ctx, vm.VMID, nil)
			return actionFinishedMsg{message: shellCommand, err: err}
		case "start", "stop", "reboot", "delete":
			err := service.Lifecycle(ctx, vm.VMID, action, nil)
			return actionFinishedMsg{message: fmt.Sprintf("%s completed for %s %d", action, model.selectedGuestKind(), vm.VMID), err: err}
		default:
			return actionFinishedMsg{err: fmt.Errorf("unsupported action %s", action)}
		}
	}
}

func (model model) loadSnapshotsCommand() tea.Cmd {
	service := model.service
	vmid := model.selectedVM.VMID
	return func() tea.Msg {
		snapshots, err := service.ListSnapshots(context.Background(), vmid)
		return snapshotsLoadedMsg{snapshots: snapshots, err: err}
	}
}

func (model model) cleanupPartialCommand() tea.Cmd {
	service := model.service
	partialError := model.partialError
	return func() tea.Msg {
		err := service.CleanupPartialVM(context.Background(), partialError, nil)
		return cleanupFinishedMsg{err: err}
	}
}

func (model model) healthCommand() tea.Cmd {
	service := model.service

	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()

		return healthCheckedMsg{
			health: service.Health(ctx),
		}
	}
}

func inlineParams(params map[string]string) string {
	keys := make([]string, 0, len(params))
	for key := range params {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	var parts []string
	for _, key := range keys {
		parts = append(parts, key+"="+params[key])
	}

	return strings.Join(parts, " ")
}

func (model model) selectedSSHKeyNames() []string {
	var selectedNames []string
	for keyName, selected := range model.selectedSSHKeys {
		if selected {
			selectedNames = append(selectedNames, keyName)
		}
	}
	sort.Strings(selectedNames)
	return selectedNames
}

func publicKeysFromTextarea(value string) []string {
	var publicKeys []string
	for _, line := range strings.Split(value, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			publicKeys = append(publicKeys, line)
		}
	}

	return publicKeys
}

func (model model) helpText() string {
	switch model.screen {
	case screenHome:
		return "enter select · r refresh · / filter · q quit"
	case screenSSHKey, screenContainerSSHKey:
		return "space toggle · enter continue · esc back · / filter · q quit"
	case screenManage:
		switch model.list.FilterState() {
		case list.Filtering:
			return "type search · enter apply · esc cancel · ctrl+c quit"
		case list.FilterApplied:
			return "enter open · / edit search · esc clear search · q quit"
		default:
			return "enter open · / search by name, id, node, status, tag · esc back · q quit"
		}
	case screenDone:
		if model.error == "" && model.result.IP != "" {
			if model.sshConfigStatus == "" {
				return "a add to ~/.ssh/config · enter exit · esc exit · q quit"
			}
			return "enter exit · esc exit · q quit"
		}
		return "enter exit · esc exit · q quit"
	case screenContainerDone:
		return "enter exit · esc exit · q quit"
	case screenPasteSSHKeys:
		return "ctrl+s continue · enter newline · esc back · q quit"
	case screenSSHAlias:
		return "enter confirm alias · esc back · q quit"
	case screenCaddyDone:
		return "enter continue · q quit"
	case screenProxmoxTaskDetail:
		return "r reload log · esc back · q quit"
	case screenProxmoxMetrics:
		return "r refresh · esc back · q quit"
	case screenGuestDetail:
		return "enter actions · esc back · q quit"
	case screenProxmoxStorageContent:
		return "/ filter · esc back · q quit"
	case screenProxmoxInfo:
		return "enter continue · esc back · q quit"
	default:
		return "enter select · esc back · / filter · q quit"
	}
}

func (model model) contentWidth() int {
	width := model.width - 4
	if width < 60 {
		return 60
	}

	return width
}

func (model model) terminalTooSmall() bool {
	return model.width < minimumTerminalWidth || model.height < minimumTerminalHeight
}

func (model model) smallTerminalView() string {
	return fmt.Sprintf(
		"%s\n\n%s\n\nCurrent terminal:  %d × %d\nRequired minimum: %d × %d\n\nResize the terminal to continue. Press q to quit.",
		appTitleStyle.Render("boringctl"),
		warnStyle.Render("Terminal too small"),
		model.width,
		model.height,
		minimumTerminalWidth,
		minimumTerminalHeight,
	)
}

func (model model) listHeight() int {
	height := model.height - 12
	if model.screen == screenHome {
		if model.width >= 110 && model.height >= 34 {
			height -= 13 + model.nodeOverviewRowCount()
		} else {
			height -= 4
		}
	}
	if height < 8 {
		return 8
	}

	return height
}

type itemDelegate struct{}

func (itemDelegate) Height() int {
	return 2
}

func (itemDelegate) Spacing() int {
	return 1
}

func (itemDelegate) Update(_ tea.Msg, _ *list.Model) tea.Cmd {
	return nil
}

func (itemDelegate) Render(output io.Writer, listModel list.Model, index int, listItem list.Item) {
	currentItem, ok := listItem.(item)
	if !ok {
		return
	}

	marker := "  "
	titleStyle := itemTitleStyle
	descriptionStyle := itemDescStyle

	if index == listModel.Index() {
		marker = selectedMarkerStyle.Render("▌ ")
		titleStyle = selectedTitleStyle
		descriptionStyle = itemDescStyle.Foreground(lipgloss.Color("250"))
	}

	fmt.Fprintf(output, "%s%s\n", marker, titleStyle.Render(currentItem.title))
	if currentItem.description != "" {
		fmt.Fprintf(output, "  %s", descriptionStyle.Render(currentItem.description))
	}
}

type homeItemDelegate struct{}

func (homeItemDelegate) Height() int {
	return 1
}

func (homeItemDelegate) Spacing() int {
	return 0
}

func (homeItemDelegate) Update(_ tea.Msg, _ *list.Model) tea.Cmd {
	return nil
}

func (homeItemDelegate) Render(output io.Writer, listModel list.Model, index int, listItem list.Item) {
	currentItem, ok := listItem.(item)
	if !ok {
		return
	}

	marker := "  "
	titleStyle := itemTitleStyle
	if index == listModel.Index() {
		marker = selectedMarkerStyle.Render("▐ ")
		titleStyle = selectedTitleStyle
	}
	descriptionWidth := max(listModel.Width()-lipgloss.Width(currentItem.title)-10, 0)
	description := ""
	if descriptionWidth > 0 {
		description = plainTruncate(currentItem.description, descriptionWidth)
	}
	fmt.Fprintf(output, "%s%s  %s", marker, titleStyle.Render(currentItem.title), itemDescStyle.Render(description))
}

type guestItemDelegate struct{}

func (guestItemDelegate) Height() int {
	return 3
}

func (guestItemDelegate) Spacing() int {
	return 1
}

func (guestItemDelegate) Update(_ tea.Msg, _ *list.Model) tea.Cmd {
	return nil
}

func (guestItemDelegate) Render(output io.Writer, listModel list.Model, index int, listItem list.Item) {
	currentItem, ok := listItem.(item)
	if !ok || currentItem.guest == nil {
		itemDelegate{}.Render(output, listModel, index, listItem)
		return
	}

	guest := *currentItem.guest
	selected := index == listModel.Index()
	marker := "  "
	if selected {
		marker = selectedMarkerStyle.Render("▐ ")
	}

	status := successStyle.Render("●")
	if guest.Status != "running" {
		status = mutedStyle.Render("●")
	}
	nameStyle := itemTitleStyle
	if selected {
		nameStyle = selectedTitleStyle
	}
	lineWidth := max(listModel.Width()-lipgloss.Width(marker)-2, 20)
	lineStyle := lipgloss.NewStyle().Width(lineWidth)

	memoryRatio := dashboardRatio(float64(guest.Mem), float64(guest.MaxMem))
	diskRatio := dashboardRatio(float64(guest.Disk), float64(guest.MaxDisk))
	identityText := plainTruncate(currentItem.title, max(lineWidth-4, 12))
	identity := fmt.Sprintf("%s %s", status, nameStyle.Render(identityText))
	diskLabel := fmt.Sprintf("%5.1f%%", diskRatio*100)
	if guest.Disk <= 0 && guest.MaxDisk > 0 {
		diskLabel = "  n/a"
	}
	resources := fmt.Sprintf("CPU %5.1f%%  RAM %5.1f%%  DISK %s", guest.CPU*100, memoryRatio*100, diskLabel)
	if listModel.Width() >= 72 {
		resources = fmt.Sprintf(
			"CPU %5.1f%% %s  RAM %5.1f%% %s  DISK %s",
			guest.CPU*100,
			dashboardUsageBar(guest.CPU, 5),
			memoryRatio*100,
			dashboardUsageBar(memoryRatio, 5),
			diskLabel,
		)
	}
	details := mutedStyle.Render("uptime ") + formatUptime(guest.Uptime)
	if guest.Tags != "" {
		details += mutedStyle.Render(fmt.Sprintf("  ·  %d tags", len(strings.Split(guest.Tags, ";"))))
	}

	fmt.Fprintf(output, "%s%s\n", marker, lineStyle.Render(identity))
	fmt.Fprintf(output, "%s%s\n", strings.Repeat(" ", lipgloss.Width(marker)), lineStyle.Render(resources))
	fmt.Fprintf(output, "%s%s", strings.Repeat(" ", lipgloss.Width(marker)), lineStyle.Render(details))
}

func plainTruncate(value string, width int) string {
	runes := []rune(value)
	if len(runes) <= width {
		return value
	}
	if width <= 1 {
		return "…"
	}
	return string(runes[:width-1]) + "…"
}

func formatMemoryMB(memoryMB int) string {
	if memoryMB%1024 == 0 {
		return fmt.Sprintf("%d GiB", memoryMB/1024)
	}

	return fmt.Sprintf("%d MiB", memoryMB)
}

func formatUptime(seconds int64) string {
	if seconds <= 0 {
		return "-"
	}

	duration := time.Duration(seconds) * time.Second
	days := duration / (24 * time.Hour)
	duration -= days * 24 * time.Hour
	hours := duration / time.Hour
	duration -= hours * time.Hour
	minutes := duration / time.Minute

	if days > 0 {
		return fmt.Sprintf("%dd %dh", days, hours)
	}
	if hours > 0 {
		return fmt.Sprintf("%dh %dm", hours, minutes)
	}
	return fmt.Sprintf("%dm", minutes)
}

func formatValues(values []string) string {
	cleanValues := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			cleanValues = append(cleanValues, value)
		}
	}
	if len(cleanValues) == 0 {
		return "-"
	}
	return strings.Join(cleanValues, ", ")
}

func formatBytes(bytes int64) string {
	if bytes <= 0 {
		return "-"
	}

	const mebibyte = 1024 * 1024
	const gibibyte = 1024 * mebibyte
	const tebibyte = 1024 * gibibyte

	if bytes >= tebibyte {
		return fmt.Sprintf("%.1f TiB", math.Round(float64(bytes)/tebibyte*10)/10)
	}
	if bytes >= gibibyte {
		return fmt.Sprintf("%.1f GiB", math.Round(float64(bytes)/gibibyte*10)/10)
	}

	return fmt.Sprintf("%.0f MiB", float64(bytes)/mebibyte)
}
