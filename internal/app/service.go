package app

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/boring-dragon/boringctl/internal/config"
	"github.com/boring-dragon/boringctl/internal/proxmox"

	"golang.org/x/crypto/ssh"
)

const DefaultDisk = "scsi0"

const (
	bytesPerGiB = 1024 * 1024 * 1024
	bytesPerMiB = 1024 * 1024
)

type Reporter func(string)

type ProxmoxAPI interface {
	Nodes(ctx context.Context) ([]proxmox.Node, error)
	Storages(ctx context.Context) ([]proxmox.Storage, error)
	NodeStorages(ctx context.Context, node string) ([]proxmox.StorageStatus, error)
	NextID(ctx context.Context) (int, error)
	VMs(ctx context.Context) ([]proxmox.VMResource, error)
	GuestConfig(ctx context.Context, node string, guestType string, vmid int) (map[string]any, error)
	VMConfig(ctx context.Context, node string, vmid int) (map[string]any, error)
	CloneVM(ctx context.Context, node string, templateID int, newID int, name string, storage string, fullClone bool) (string, error)
	CreateContainer(ctx context.Context, node string, values url.Values) (string, error)
	ResizeDisk(ctx context.Context, node string, vmid int, disk string, size string) (string, error)
	SetGuestConfig(ctx context.Context, node string, guestType string, vmid int, values url.Values) error
	SetVMConfig(ctx context.Context, node string, vmid int, values url.Values) error
	StartGuest(ctx context.Context, node string, guestType string, vmid int) (string, error)
	StartVM(ctx context.Context, node string, vmid int) (string, error)
	ShutdownGuest(ctx context.Context, node string, guestType string, vmid int) (string, error)
	ShutdownVM(ctx context.Context, node string, vmid int) (string, error)
	RebootGuest(ctx context.Context, node string, guestType string, vmid int) (string, error)
	RebootVM(ctx context.Context, node string, vmid int) (string, error)
	DeleteGuest(ctx context.Context, node string, guestType string, vmid int) (string, error)
	DeleteVM(ctx context.Context, node string, vmid int) (string, error)
	RenameGuest(ctx context.Context, node string, guestType string, vmid int, name string) error
	RenameVM(ctx context.Context, node string, vmid int, name string) error
	GuestSnapshots(ctx context.Context, node string, guestType string, vmid int) ([]proxmox.Snapshot, error)
	Snapshots(ctx context.Context, node string, vmid int) ([]proxmox.Snapshot, error)
	CreateGuestSnapshot(ctx context.Context, node string, guestType string, vmid int, name string, description string) (string, error)
	CreateSnapshot(ctx context.Context, node string, vmid int, name string, description string) (string, error)
	DeleteGuestSnapshot(ctx context.Context, node string, guestType string, vmid int, name string) (string, error)
	DeleteSnapshot(ctx context.Context, node string, vmid int, name string) (string, error)
	RollbackGuestSnapshot(ctx context.Context, node string, guestType string, vmid int, name string) (string, error)
	AgentNetworkInterfaces(ctx context.Context, node string, vmid int) ([]proxmox.NetworkInterface, error)
	ContainerInterfaces(ctx context.Context, node string, vmid int) ([]proxmox.ContainerInterface, error)
	FirstRoutableIP(ctx context.Context, node string, vmid int) (string, error)
	WaitForTask(ctx context.Context, node string, upid string) error
	WaitForTaskWithTimeout(ctx context.Context, node string, upid string, timeout time.Duration) (proxmox.TaskStatus, error)
	Tasks(ctx context.Context, filter proxmox.TaskListFilter) ([]proxmox.Task, error)
	TaskStatus(ctx context.Context, node string, upid string) (proxmox.TaskStatus, error)
	TaskLog(ctx context.Context, upid string) ([]proxmox.TaskLogEntry, error)
	StopTask(ctx context.Context, upid string) error
	StorageContent(ctx context.Context, node string, storage string, filter proxmox.StorageContentFilter) ([]proxmox.StorageContent, error)
	UploadStorageContent(ctx context.Context, request proxmox.UploadRequest) (string, error)
	DownloadStorageContentFromURL(ctx context.Context, request proxmox.DownloadURLRequest) (string, error)
	CreateBackup(ctx context.Context, request proxmox.BackupRequest) (string, error)
	RestoreBackup(ctx context.Context, request proxmox.RestoreRequest) (string, error)
}

type Service struct {
	Config *config.Config
	Client ProxmoxAPI
}

type CreateRequest struct {
	Node          string
	Image         string
	Plan          string
	Name          string
	Storage       string
	SSHKey        string
	SSHKeys       []string
	SSHPublicKeys []string
	Cores         int
	MemoryMB      int
	DiskGB        int
	NetworkMode   string
	IPAddress     string
	Gateway       string
	DNS           string
}

type CreateResult struct {
	VMID        int
	Name        string
	Node        string
	Image       string
	ImageLabel  string
	Plan        string
	TemplateID  int
	Storage     string
	Cores       int
	MemoryMB    int
	DiskGB      int
	User        string
	IP          string
	SSHCommand  string
	SSHKeygen   string
	NetworkMode string
	StaticIP    bool
	Warning     string
}

type CreateContainerRequest struct {
	Node          string
	Image         string
	Plan          string
	Name          string
	Storage       string
	SSHKey        string
	SSHKeys       []string
	SSHPublicKeys []string
	Cores         int
	MemoryMB      int
	DiskGB        int
	SwapMB        int
	NetworkMode   string
	IPAddress     string
	Gateway       string
	DNS           string
	Tags          []string
	Start         bool
	Unprivileged  bool
	Docker        bool
	Nesting       bool
	Keyctl        bool
	Features      []string
}

type CreateContainerResult struct {
	VMID        int
	Name        string
	Node        string
	Image       string
	ImageLabel  string
	Plan        string
	Template    string
	Storage     string
	Cores       int
	MemoryMB    int
	DiskGB      int
	SwapMB      int
	User        string
	IP          string
	SSHCommand  string
	SSHKeygen   string
	NetworkMode string
	Tags        []string
	Started     bool
	Warning     string
	Features    []string
}

type CreateContainerPreview struct {
	Node       string
	Name       string
	Template   string
	Storage    string
	CreatePath string
	Params     map[string]string
}

type CreatePreview struct {
	Node         string
	Name         string
	TemplateID   int
	Storage      string
	CloneParams  map[string]string
	ResizeParams map[string]string
	ConfigParams map[string]string
	StartPath    string
}

type GuestDetail struct {
	Guest         proxmox.VMResource
	Config        map[string]any
	IPAddresses   []string
	Tags          []string
	SnapshotCount int
	ShellCommand  string
}

type ShellTargetKind string

const (
	ShellTargetNode      ShellTargetKind = "node"
	ShellTargetContainer ShellTargetKind = "container"
	ShellTargetVM        ShellTargetKind = "vm"
)

type ShellPlan struct {
	Kind      ShellTargetKind
	VMID      int
	Name      string
	Node      string
	GuestType string
	SSHHost   string
	IPAddress string
	User      string
	Args      []string
	Command   string
}

type ClusterHealth struct {
	Connected     bool
	Endpoint      string
	CheckedAt     time.Time
	Nodes         []NodeHealth
	Storages      []StorageHealth
	StorageErrors []string
	Error         string
}

type NodeHealth struct {
	Name        string
	Status      string
	CPUPercent  float64
	MaxCPU      int
	MemoryBytes int64
	MaxMemBytes int64
	Configured  bool
}

type StorageHealth struct {
	Node           string
	Name           string
	Type           string
	Active         bool
	Enabled        bool
	Shared         bool
	UsedBytes      int64
	TotalBytes     int64
	AvailableBytes int64
}

type SuggestedPlan struct {
	Name     string
	Label    string
	Cores    int
	MemoryMB int
	DiskGB   int
	Reason   string
}

type PartialCreateError struct {
	Node string
	VMID int
	Err  error
}

func (err *PartialCreateError) Error() string {
	return err.Err.Error()
}

func (err *PartialCreateError) Unwrap() error {
	return err.Err
}

func NewService(loadedConfig *config.Config, client ProxmoxAPI) *Service {
	return &Service{
		Config: loadedConfig,
		Client: client,
	}
}

func NewClientFromConfig(loadedConfig *config.Config) (*proxmox.Client, error) {
	credentials, err := loadProxmoxCredentials(loadedConfig)
	if err != nil {
		return nil, err
	}
	caFile, err := loadedConfig.ExpandPath(loadedConfig.Cluster.CAFile)
	if err != nil {
		return nil, err
	}

	return proxmox.NewClient(proxmox.Config{
		Endpoint:    loadedConfig.Cluster.Endpoint,
		InsecureTLS: loadedConfig.Cluster.InsecureTLS,
		CAFile:      caFile,
		TokenID:     credentials.TokenID,
		TokenSecret: credentials.TokenSecret,
	})
}

func NewHealthAwareClientFromConfig(loadedConfig *config.Config) (*proxmox.Client, error) {
	credentials, _ := loadProxmoxCredentials(loadedConfig)
	caFile, err := loadedConfig.ExpandPath(loadedConfig.Cluster.CAFile)
	if err != nil {
		return nil, err
	}

	return proxmox.NewClient(proxmox.Config{
		Endpoint:    loadedConfig.Cluster.Endpoint,
		InsecureTLS: loadedConfig.Cluster.InsecureTLS,
		CAFile:      caFile,
		TokenID:     credentials.TokenID,
		TokenSecret: credentials.TokenSecret,
	})
}

type proxmoxCredentials struct {
	TokenID     string
	TokenSecret string
}

func loadProxmoxCredentials(loadedConfig *config.Config) (proxmoxCredentials, error) {
	credentials := proxmoxCredentials{
		TokenID:     os.Getenv(loadedConfig.Auth.TokenIDEnv),
		TokenSecret: os.Getenv(loadedConfig.Auth.TokenSecretEnv),
	}

	if credentials.TokenID != "" && credentials.TokenSecret != "" {
		return credentials, nil
	}

	fileCredentials, err := loadCredentialsFile(loadedConfig)
	if err != nil {
		return credentials, err
	}

	if credentials.TokenID == "" {
		credentials.TokenID = fileCredentials.TokenID
	}

	if credentials.TokenSecret == "" {
		credentials.TokenSecret = fileCredentials.TokenSecret
	}

	if credentials.TokenID == "" {
		return credentials, fmt.Errorf("%s is required", loadedConfig.Auth.TokenIDEnv)
	}

	if credentials.TokenSecret == "" {
		return credentials, fmt.Errorf("%s is required", loadedConfig.Auth.TokenSecretEnv)
	}

	return credentials, nil
}

func loadCredentialsFile(loadedConfig *config.Config) (proxmoxCredentials, error) {
	credentialsPath, err := defaultCredentialsPath()
	if err != nil {
		return proxmoxCredentials{}, err
	}

	fileInfo, err := os.Stat(credentialsPath)
	if errors.Is(err, os.ErrNotExist) {
		return proxmoxCredentials{}, nil
	}
	if err != nil {
		return proxmoxCredentials{}, err
	}
	if fileInfo.Mode().Perm()&0o077 != 0 {
		return proxmoxCredentials{}, fmt.Errorf("credentials file %s has mode %04o; expected 0600 or stricter", credentialsPath, fileInfo.Mode().Perm())
	}

	fileContents, err := os.ReadFile(credentialsPath)
	if err != nil {
		return proxmoxCredentials{}, err
	}

	values := map[string]string{}
	for _, line := range strings.Split(string(fileContents), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		values[strings.TrimSpace(key)] = strings.Trim(strings.TrimSpace(value), `"'`)
	}

	return proxmoxCredentials{
		TokenID:     values[loadedConfig.Auth.TokenIDEnv],
		TokenSecret: values[loadedConfig.Auth.TokenSecretEnv],
	}, nil
}

func defaultCredentialsPath() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}

	return filepath.Join(homeDir, ".config", "boringctl", "credentials.env"), nil
}

func (service *Service) credentialsError() error {
	_, err := loadProxmoxCredentials(service.Config)
	return err
}

func (service *Service) credentialsConfigured() bool {
	return service.credentialsError() == nil
}

func (service *Service) credentialsErrorMessage() string {
	err := service.credentialsError()
	if err == nil {
		return ""
	}

	return err.Error()
}

func (service *Service) Health(ctx context.Context) ClusterHealth {
	health := ClusterHealth{
		Endpoint:  service.Config.Cluster.Endpoint,
		CheckedAt: time.Now(),
	}

	if !service.credentialsConfigured() {
		health.Error = service.credentialsErrorMessage()
		health.Nodes = service.configuredNodeHealth(nil)
		return health
	}

	nodes, err := service.Client.Nodes(ctx)
	if err != nil {
		health.Error = err.Error()
		health.Nodes = service.configuredNodeHealth(nil)
		return health
	}

	health.Connected = true
	health.Nodes = service.configuredNodeHealth(nodes)
	health.Storages, health.StorageErrors = service.configuredStorageHealth(ctx)

	return health
}

func (service *Service) CreateVM(ctx context.Context, request CreateRequest, reporter Reporter) (CreateResult, error) {
	resolved, err := service.resolveCreateRequest(request)
	if err != nil {
		return CreateResult{}, err
	}

	report(reporter, "Validating template")
	templateConfig, err := service.Client.VMConfig(ctx, resolved.Node, resolved.TemplateID)
	if err != nil {
		return CreateResult{}, err
	}

	if _, exists := templateConfig[DefaultDisk]; !exists {
		return CreateResult{}, fmt.Errorf("template %d on %s does not have %s", resolved.TemplateID, resolved.Node, DefaultDisk)
	}

	report(reporter, "Reserving VMID")
	nextID, err := service.Client.NextID(ctx)
	if err != nil {
		return CreateResult{}, err
	}
	resolved.VMID = nextID

	report(reporter, fmt.Sprintf("Cloning template %d into VM %d", resolved.TemplateID, resolved.VMID))
	cloneTask, err := service.Client.CloneVM(ctx, resolved.Node, resolved.TemplateID, resolved.VMID, resolved.Name, resolved.Storage, service.Config.Defaults.FullClone)
	if err != nil {
		return CreateResult{}, err
	}
	if err := service.Client.WaitForTask(ctx, resolved.Node, cloneTask); err != nil {
		return CreateResult{}, err
	}
	partialVM := &PartialCreateError{Node: resolved.Node, VMID: resolved.VMID}

	report(reporter, fmt.Sprintf("Resizing %s to %dG", DefaultDisk, resolved.DiskGB))
	resizeTask, err := service.Client.ResizeDisk(ctx, resolved.Node, resolved.VMID, DefaultDisk, fmt.Sprintf("%dG", resolved.DiskGB))
	if err != nil {
		partialVM.Err = err
		return CreateResult{}, partialVM
	}
	if err := service.Client.WaitForTask(ctx, resolved.Node, resizeTask); err != nil {
		partialVM.Err = err
		return CreateResult{}, partialVM
	}

	report(reporter, "Applying CPU, memory, SSH key, and network")
	if err := service.Client.SetVMConfig(ctx, resolved.Node, resolved.VMID, resolved.proxmoxConfig()); err != nil {
		partialVM.Err = err
		return CreateResult{}, partialVM
	}

	report(reporter, "Starting VM")
	startTask, err := service.Client.StartVM(ctx, resolved.Node, resolved.VMID)
	if err != nil {
		partialVM.Err = err
		return CreateResult{}, partialVM
	}
	if err := service.Client.WaitForTask(ctx, resolved.Node, startTask); err != nil {
		partialVM.Err = err
		return CreateResult{}, partialVM
	}

	report(reporter, "Waiting for guest agent IP")
	ipAddress, warning, err := service.waitForIP(ctx, resolved.Node, resolved.VMID, 3*time.Minute, reporter)
	if err != nil {
		return CreateResult{}, err
	}

	resolved.IP = ipAddress
	resolved.Warning = warning
	if ipAddress != "" {
		if staticWarning, err := service.persistDHCPLeaseAsStatic(ctx, &resolved, reporter); err != nil {
			partialVM.Err = err
			return CreateResult{}, partialVM
		} else if staticWarning != "" {
			resolved.Warning = joinWarnings(resolved.Warning, staticWarning)
		}

		resolved.SSHCommand = fmt.Sprintf("ssh %s@%s", resolved.User, ipAddress)
		resolved.SSHKeygen = fmt.Sprintf("ssh-keygen -R %s", ipAddress)
	}

	if err := AppendCreateHistory(resolved.CreateResult); err != nil {
		resolved.Warning = joinWarnings(resolved.Warning, fmt.Sprintf("VM was created, but history was not saved: %s", err))
	}

	return resolved.CreateResult, nil
}

func (service *Service) CreatePreview(request CreateRequest) (CreatePreview, error) {
	resolved, err := service.resolveCreateRequest(request)
	if err != nil {
		return CreatePreview{}, err
	}

	configValues := resolved.proxmoxConfig()
	configValues.Set("sshkeys", "<"+requestSSHKeyName(request, service.Config)+">")

	return CreatePreview{
		Node:       resolved.Node,
		Name:       resolved.Name,
		TemplateID: resolved.TemplateID,
		Storage:    resolved.Storage,
		CloneParams: map[string]string{
			"newid":   "<nextid>",
			"name":    resolved.Name,
			"target":  resolved.Node,
			"storage": resolved.Storage,
			"full":    boolString(service.Config.Defaults.FullClone),
		},
		ResizeParams: map[string]string{
			"disk": DefaultDisk,
			"size": fmt.Sprintf("%dG", resolved.DiskGB),
		},
		ConfigParams: urlValuesToMap(configValues),
		StartPath:    fmt.Sprintf("/nodes/%s/qemu/<nextid>/status/start", resolved.Node),
	}, nil
}

func (service *Service) CreateContainer(ctx context.Context, request CreateContainerRequest, reporter Reporter) (CreateContainerResult, error) {
	resolved, err := service.resolveCreateContainerRequest(request)
	if err != nil {
		return CreateContainerResult{}, err
	}

	report(reporter, "Reserving VMID")
	nextID, err := service.Client.NextID(ctx)
	if err != nil {
		return CreateContainerResult{}, err
	}
	resolved.VMID = nextID

	report(reporter, fmt.Sprintf("Creating LXC container %d from %s", resolved.VMID, resolved.Template))
	createTask, err := service.Client.CreateContainer(ctx, resolved.Node, resolved.proxmoxConfig())
	if err != nil {
		return CreateContainerResult{}, annotateContainerCreateError(err, resolved)
	}
	if err := service.Client.WaitForTask(ctx, resolved.Node, createTask); err != nil {
		return CreateContainerResult{}, err
	}

	if resolved.Started {
		report(reporter, "Waiting for LXC IP")
		ipAddress, warning, err := service.waitForContainerIP(ctx, resolved.Node, resolved.VMID, 90*time.Second, reporter)
		if err != nil {
			return CreateContainerResult{}, err
		}
		resolved.IP = ipAddress
		resolved.Warning = warning
		if ipAddress != "" {
			resolved.SSHCommand = fmt.Sprintf("ssh %s@%s", resolved.User, ipAddress)
			resolved.SSHKeygen = fmt.Sprintf("ssh-keygen -R %s", ipAddress)
		}
	}

	return resolved.CreateContainerResult, nil
}

func (service *Service) CreateContainerPreview(request CreateContainerRequest) (CreateContainerPreview, error) {
	resolved, err := service.resolveCreateContainerRequest(request)
	if err != nil {
		return CreateContainerPreview{}, err
	}

	values := resolved.proxmoxConfig()
	values.Set("ssh-public-keys", "<"+requestSSHKeyName(CreateRequest{SSHKey: request.SSHKey, SSHKeys: request.SSHKeys}, service.Config)+">")
	values.Set("vmid", "<nextid>")

	return CreateContainerPreview{
		Node:       resolved.Node,
		Name:       resolved.Name,
		Template:   resolved.Template,
		Storage:    resolved.Storage,
		CreatePath: fmt.Sprintf("/nodes/%s/lxc", resolved.Node),
		Params:     urlValuesToMap(values),
	}, nil
}

func (service *Service) persistDHCPLeaseAsStatic(ctx context.Context, resolved *resolvedCreateRequest, reporter Reporter) (string, error) {
	if resolved.NetworkMode != "dhcp" || !service.Config.Defaults.DHCPToStatic || resolved.IP == "" {
		return "", nil
	}

	gateway := strings.TrimSpace(resolved.Gateway)
	if gateway == "" {
		gateway = strings.TrimSpace(service.Config.Defaults.StaticGateway)
	}
	if gateway == "" {
		return "DHCP lease was not converted to static because defaults.static_gateway is not configured.", nil
	}

	dns := strings.TrimSpace(resolved.DNS)
	if dns == "" {
		dns = strings.TrimSpace(service.Config.Defaults.StaticDNS)
	}

	prefixLength := service.Config.Defaults.StaticPrefixLength
	if prefixLength <= 0 {
		prefixLength = 24
	}

	report(reporter, fmt.Sprintf("Persisting %s as static IP", resolved.IP))
	values := url.Values{
		"ipconfig0": {fmt.Sprintf("ip=%s/%d,gw=%s", resolved.IP, prefixLength, gateway)},
	}
	if dns != "" {
		values.Set("nameserver", dns)
	}

	if err := service.Client.SetVMConfig(ctx, resolved.Node, resolved.VMID, values); err != nil {
		return "", err
	}

	report(reporter, "Rebooting VM to apply static IP")
	rebootTask, err := service.Client.RebootVM(ctx, resolved.Node, resolved.VMID)
	if err != nil {
		return "", err
	}
	if err := service.Client.WaitForTask(ctx, resolved.Node, rebootTask); err != nil {
		return "", err
	}

	report(reporter, "Waiting for static IP after reboot")
	ipAddress, warning, err := service.waitForIP(ctx, resolved.Node, resolved.VMID, 3*time.Minute, reporter)
	if err != nil {
		return "", err
	}
	if ipAddress == "" {
		return joinWarnings(warning, fmt.Sprintf("Static IP %s was configured, but guest agent did not report an IP after reboot.", resolved.IP)), nil
	}
	if ipAddress != resolved.IP {
		return joinWarnings(warning, fmt.Sprintf("Static IP %s was configured, but guest agent reported %s after reboot.", resolved.IP, ipAddress)), nil
	}

	resolved.StaticIP = true
	resolved.NetworkMode = "static"
	return warning, nil
}

func (service *Service) CleanupPartialVM(ctx context.Context, partialError *PartialCreateError, reporter Reporter) error {
	if partialError == nil || partialError.VMID == 0 || partialError.Node == "" {
		return errors.New("partial VM reference is required")
	}

	report(reporter, fmt.Sprintf("Destroying partial VM %d on %s", partialError.VMID, partialError.Node))
	task, err := service.Client.DeleteVM(ctx, partialError.Node, partialError.VMID)
	if err != nil {
		return err
	}

	return service.Client.WaitForTask(ctx, partialError.Node, task)
}

func (service *Service) ListVMs(ctx context.Context) ([]proxmox.VMResource, error) {
	return service.listGuestsByType(ctx, proxmox.GuestTypeQEMU)
}

func (service *Service) ListContainers(ctx context.Context) ([]proxmox.VMResource, error) {
	return service.listGuestsByType(ctx, proxmox.GuestTypeLXC)
}

func (service *Service) ListGuests(ctx context.Context) ([]proxmox.VMResource, error) {
	guests, err := service.Client.VMs(ctx)
	if err != nil {
		return nil, err
	}

	filteredGuests := make([]proxmox.VMResource, 0, len(guests))
	for _, guest := range guests {
		if guest.Template == 1 {
			continue
		}

		filteredGuests = append(filteredGuests, guest)
	}

	return filteredGuests, nil
}

func (service *Service) listGuestsByType(ctx context.Context, guestType string) ([]proxmox.VMResource, error) {
	guests, err := service.ListGuests(ctx)
	if err != nil {
		return nil, err
	}

	filteredGuests := make([]proxmox.VMResource, 0, len(guests))
	for _, guest := range guests {
		if guest.GuestType() == guestType {
			filteredGuests = append(filteredGuests, guest)
		}
	}

	return filteredGuests, nil
}

func (service *Service) SuggestedPlans(health ClusterHealth, nodeName string, storageName string) []SuggestedPlan {
	node, nodeExists := findNodeHealth(health.Nodes, nodeName)
	storage, storageExists := findStorageHealth(health.Storages, nodeName, storageName)
	if !health.Connected || !nodeExists || !storageExists || node.MaxCPU <= 0 || node.MaxMemBytes <= 0 || storage.AvailableBytes <= 0 {
		return service.configuredSuggestedPlans()
	}
	if !storage.Active || !storage.Enabled {
		return []SuggestedPlan{}
	}

	safeCores := node.MaxCPU
	if safeCores < 1 {
		safeCores = 1
	}

	totalMemoryMB := int(node.MaxMemBytes / bytesPerMiB)
	freeMemoryMB := int((node.MaxMemBytes - node.MemoryBytes) / bytesPerMiB)
	safeMemoryMB := minInt(freeMemoryMB-1024, totalMemoryMB/2)
	if safeMemoryMB < 1024 {
		safeMemoryMB = minInt(totalMemoryMB/4, 2048)
	}
	safeMemoryMB = roundDownMemoryMB(maxInt(safeMemoryMB, 512))

	availableDiskGB := int(storage.AvailableBytes / bytesPerGiB)
	safeDiskGB := minInt(int(float64(availableDiskGB)*0.75), availableDiskGB-10)
	if safeDiskGB < 10 {
		return []SuggestedPlan{}
	}

	candidates := []SuggestedPlan{
		{Name: "tiny", Label: "Tiny", Cores: 1, MemoryMB: 1024, DiskGB: 20},
		{Name: "small", Label: "Small", Cores: 2, MemoryMB: 2048, DiskGB: 30},
		{Name: "medium", Label: "Medium", Cores: 2, MemoryMB: 4096, DiskGB: 50},
		{Name: "max-safe", Label: "Max Safe", Cores: safeCores, MemoryMB: minInt(safeMemoryMB, 8192), DiskGB: minInt(safeDiskGB, 80)},
	}

	var suggestions []SuggestedPlan
	seen := map[string]bool{}
	for _, candidate := range candidates {
		candidate.Cores = minInt(candidate.Cores, safeCores)
		candidate.MemoryMB = roundDownMemoryMB(minInt(candidate.MemoryMB, safeMemoryMB))
		candidate.DiskGB = minInt(candidate.DiskGB, safeDiskGB)
		if candidate.Cores <= 0 || candidate.MemoryMB < 512 || candidate.DiskGB < 10 {
			continue
		}

		key := fmt.Sprintf("%d:%d:%d", candidate.Cores, candidate.MemoryMB, candidate.DiskGB)
		if seen[key] {
			continue
		}

		seen[key] = true
		if candidate.Name == "max-safe" {
			candidate.Label = fmt.Sprintf("Max %d vCPU", candidate.Cores)
			candidate.Reason = fmt.Sprintf("%s has %d CPU available from Proxmox", nodeName, node.MaxCPU)
		} else {
			candidate.Reason = fmt.Sprintf("auto from %s CPU/RAM and %s free space", nodeName, storageName)
		}
		suggestions = append(suggestions, candidate)
	}

	return suggestions
}

func (service *Service) ShowVM(ctx context.Context, vmid int) (proxmox.VMResource, map[string]any, error) {
	return service.ShowGuest(ctx, vmid)
}

func (service *Service) ShowGuest(ctx context.Context, vmid int) (proxmox.VMResource, map[string]any, error) {
	vm, err := service.FindVM(ctx, vmid)
	if err != nil {
		return proxmox.VMResource{}, nil, err
	}

	vmConfig, err := service.Client.GuestConfig(ctx, vm.Node, vm.GuestType(), vmid)
	if err != nil {
		return proxmox.VMResource{}, nil, err
	}

	return vm, vmConfig, nil
}

func (service *Service) GuestDetail(ctx context.Context, vmid int) (GuestDetail, error) {
	guest, config, err := service.ShowGuest(ctx, vmid)
	if err != nil {
		return GuestDetail{}, err
	}

	ipAddresses, err := service.GuestIPAddresses(ctx, guest, config)
	if err != nil {
		ipAddresses = nil
	}

	snapshots, err := service.Client.GuestSnapshots(ctx, guest.Node, guest.GuestType(), guest.VMID)
	if err != nil {
		snapshots = nil
	}

	shellCommand := ""
	if guest.IsContainer() {
		shellCommand, _ = service.ContainerShellCommand(ctx, guest.VMID, nil)
	}

	return GuestDetail{
		Guest:         guest,
		Config:        config,
		IPAddresses:   ipAddresses,
		Tags:          guestTags(guest, config),
		SnapshotCount: len(snapshots),
		ShellCommand:  shellCommand,
	}, nil
}

func (service *Service) GuestIPAddresses(ctx context.Context, guest proxmox.VMResource, config map[string]any) ([]string, error) {
	if guest.IsContainer() {
		return service.containerIPAddresses(ctx, guest, config)
	}

	interfaces, err := service.Client.AgentNetworkInterfaces(ctx, guest.Node, guest.VMID)
	if err != nil {
		return nil, err
	}

	var ipAddresses []string
	for _, networkInterface := range interfaces {
		for _, ipAddress := range networkInterface.IPAddresses {
			if isUsableGuestAddress(ipAddress.Address) {
				ipAddresses = appendUnique(ipAddresses, ipAddress.Address)
			}
		}
	}

	return ipAddresses, nil
}

func (service *Service) containerIPAddresses(ctx context.Context, guest proxmox.VMResource, config map[string]any) ([]string, error) {
	interfaces, err := service.Client.ContainerInterfaces(ctx, guest.Node, guest.VMID)
	if err == nil {
		var ipAddresses []string
		for _, networkInterface := range interfaces {
			if isUsableGuestAddress(networkInterface.IPv4) {
				ipAddresses = appendUnique(ipAddresses, trimCIDR(networkInterface.IPv4))
			}
			if isUsableGuestAddress(networkInterface.IPv6) {
				ipAddresses = appendUnique(ipAddresses, trimCIDR(networkInterface.IPv6))
			}
		}
		if len(ipAddresses) > 0 {
			return ipAddresses, nil
		}
	}

	return lxcConfigIPAddresses(config), err
}

func (service *Service) FindVM(ctx context.Context, vmid int) (proxmox.VMResource, error) {
	return service.FindGuest(ctx, vmid)
}

func (service *Service) FindGuest(ctx context.Context, vmid int) (proxmox.VMResource, error) {
	guests, err := service.Client.VMs(ctx)
	if err != nil {
		return proxmox.VMResource{}, err
	}

	for _, guest := range guests {
		if guest.VMID == vmid {
			return guest, nil
		}
	}

	return proxmox.VMResource{}, fmt.Errorf("guest %d was not found", vmid)
}

func (service *Service) ResolveGuestRef(ctx context.Context, ref string) (proxmox.VMResource, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return proxmox.VMResource{}, errors.New("guest reference is required")
	}

	vmid, err := strconv.Atoi(ref)
	if err == nil {
		if vmid <= 0 {
			return proxmox.VMResource{}, fmt.Errorf("invalid VMID %q", ref)
		}
		return service.FindGuest(ctx, vmid)
	}

	return service.FindGuestByName(ctx, ref)
}

func (service *Service) FindGuestByName(ctx context.Context, name string) (proxmox.VMResource, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return proxmox.VMResource{}, errors.New("guest name is required")
	}

	guests, err := service.ListGuests(ctx)
	if err != nil {
		return proxmox.VMResource{}, err
	}

	var exactMatches []proxmox.VMResource
	var foldedMatches []proxmox.VMResource
	for _, guest := range guests {
		if guest.Name == name {
			exactMatches = append(exactMatches, guest)
			continue
		}
		if strings.EqualFold(guest.Name, name) {
			foldedMatches = append(foldedMatches, guest)
		}
	}

	matches := exactMatches
	if len(matches) == 0 {
		matches = foldedMatches
	}

	switch len(matches) {
	case 0:
		return proxmox.VMResource{}, fmt.Errorf("no VM or LXC container named %q was found", name)
	case 1:
		return matches[0], nil
	default:
		return proxmox.VMResource{}, fmt.Errorf("guest name %q is ambiguous: %s", name, guestRefs(matches))
	}
}

func (service *Service) Lifecycle(ctx context.Context, vmid int, action string, reporter Reporter) error {
	guest, err := service.FindGuest(ctx, vmid)
	if err != nil {
		return err
	}

	if action == "start" && guest.Status == "running" {
		report(reporter, fmt.Sprintf("%s %d is already running", guestKindLabel(guest), vmid))
		return nil
	}
	if action == "stop" && guest.Status == "stopped" {
		report(reporter, fmt.Sprintf("%s %d is already stopped", guestKindLabel(guest), vmid))
		return nil
	}

	report(reporter, fmt.Sprintf("%s %s %d on %s", titleAction(action), guestKindLabel(guest), vmid, guest.Node))

	var task string
	switch action {
	case "start":
		task, err = service.Client.StartGuest(ctx, guest.Node, guest.GuestType(), vmid)
	case "stop":
		task, err = service.Client.ShutdownGuest(ctx, guest.Node, guest.GuestType(), vmid)
	case "reboot":
		task, err = service.Client.RebootGuest(ctx, guest.Node, guest.GuestType(), vmid)
	case "delete":
		task, err = service.Client.DeleteGuest(ctx, guest.Node, guest.GuestType(), vmid)
	default:
		err = fmt.Errorf("unsupported lifecycle action %q", action)
	}
	if err != nil {
		return err
	}

	return service.Client.WaitForTask(ctx, guest.Node, task)
}

func (service *Service) RenameVM(ctx context.Context, vmid int, name string) error {
	guest, err := service.FindGuest(ctx, vmid)
	if err != nil {
		return err
	}

	return service.Client.RenameGuest(ctx, guest.Node, guest.GuestType(), vmid, name)
}

func (service *Service) ResizeVM(ctx context.Context, vmid int, disk string, size string, reporter Reporter) error {
	vm, err := service.FindVM(ctx, vmid)
	if err != nil {
		return err
	}
	if vm.IsContainer() {
		return fmt.Errorf("resize is only supported for QEMU VMs; %d is an LXC container", vmid)
	}

	if disk == "" {
		disk = DefaultDisk
	}

	report(reporter, fmt.Sprintf("Resizing %s on VM %d to %s", disk, vmid, size))
	task, err := service.Client.ResizeDisk(ctx, vm.Node, vmid, disk, size)
	if err != nil {
		return err
	}

	return service.Client.WaitForTask(ctx, vm.Node, task)
}

func (service *Service) SSHCommand(ctx context.Context, vmid int, userOverride string) (string, error) {
	vm, vmConfig, err := service.ShowVM(ctx, vmid)
	if err != nil {
		return "", err
	}
	if vm.IsContainer() {
		return "", fmt.Errorf("ssh command lookup is only supported for QEMU VMs; container %d does not use the QEMU guest agent", vmid)
	}

	user := userOverride
	if user == "" {
		user = stringValue(vmConfig["ciuser"])
	}
	if user == "" {
		user = "ubuntu"
	}

	ipAddress, err := service.Client.FirstRoutableIP(ctx, vm.Node, vmid)
	if err != nil {
		return "", err
	}
	if ipAddress == "" {
		return "", fmt.Errorf("no routable guest agent IP found for VM %d", vmid)
	}

	return fmt.Sprintf("ssh %s@%s", user, ipAddress), nil
}

func (service *Service) ListSnapshots(ctx context.Context, vmid int) ([]proxmox.Snapshot, error) {
	guest, err := service.FindGuest(ctx, vmid)
	if err != nil {
		return nil, err
	}

	return service.Client.GuestSnapshots(ctx, guest.Node, guest.GuestType(), vmid)
}

func (service *Service) CreateSnapshot(ctx context.Context, vmid int, name string, description string, reporter Reporter) error {
	guest, err := service.FindGuest(ctx, vmid)
	if err != nil {
		return err
	}

	report(reporter, fmt.Sprintf("Creating snapshot %s on %s %d", name, guestKindLabel(guest), vmid))
	task, err := service.Client.CreateGuestSnapshot(ctx, guest.Node, guest.GuestType(), vmid, name, description)
	if err != nil {
		return err
	}

	return service.Client.WaitForTask(ctx, guest.Node, task)
}

func (service *Service) DeleteSnapshot(ctx context.Context, vmid int, name string, reporter Reporter) error {
	guest, err := service.FindGuest(ctx, vmid)
	if err != nil {
		return err
	}

	report(reporter, fmt.Sprintf("Deleting snapshot %s on %s %d", name, guestKindLabel(guest), vmid))
	task, err := service.Client.DeleteGuestSnapshot(ctx, guest.Node, guest.GuestType(), vmid, name)
	if err != nil {
		return err
	}

	return service.Client.WaitForTask(ctx, guest.Node, task)
}

func (service *Service) ContainerShellArgs(ctx context.Context, vmid int, command []string) ([]string, error) {
	guest, err := service.FindGuest(ctx, vmid)
	if err != nil {
		return nil, err
	}

	plan, err := service.shellPlanForGuest(ctx, guest, command, "")
	if err != nil {
		return nil, err
	}
	if plan.Kind != ShellTargetContainer {
		return nil, fmt.Errorf("shell helper is only supported for LXC containers; %d is a VM", vmid)
	}

	return plan.Args, nil
}

func (service *Service) ContainerShellCommand(ctx context.Context, vmid int, command []string) (string, error) {
	args, err := service.ContainerShellArgs(ctx, vmid, command)
	if err != nil {
		return "", err
	}

	return sshCommandText(args), nil
}

func (service *Service) ShellPlan(ctx context.Context, targetRef string, command []string, userOverride string) (ShellPlan, error) {
	targetRef = strings.TrimSpace(targetRef)
	if targetRef == "" {
		return ShellPlan{}, errors.New("shell target is required")
	}

	targetKind, ref := splitShellTargetRef(targetRef)
	switch targetKind {
	case "node":
		return service.nodeShellPlan(ref, command)
	case "guest":
		guest, err := service.ResolveGuestRef(ctx, ref)
		if err != nil {
			return ShellPlan{}, err
		}
		return service.shellPlanForGuest(ctx, guest, command, userOverride)
	case "lxc", "container":
		guest, err := service.ResolveGuestRef(ctx, ref)
		if err != nil {
			return ShellPlan{}, err
		}
		if !guest.IsContainer() {
			return ShellPlan{}, fmt.Errorf("%q resolved to VM %d; use vm:%s or guest:%s", ref, guest.VMID, ref, ref)
		}
		return service.shellPlanForGuest(ctx, guest, command, userOverride)
	case "vm", "qemu":
		guest, err := service.ResolveGuestRef(ctx, ref)
		if err != nil {
			return ShellPlan{}, err
		}
		if guest.IsContainer() {
			return ShellPlan{}, fmt.Errorf("%q resolved to LXC container %d; use lxc:%s or guest:%s", ref, guest.VMID, ref, ref)
		}
		return service.shellPlanForGuest(ctx, guest, command, userOverride)
	}

	if _, exists := service.Config.Nodes[targetRef]; exists {
		return service.nodeShellPlan(targetRef, command)
	}

	guest, err := service.ResolveGuestRef(ctx, targetRef)
	if err != nil {
		return ShellPlan{}, err
	}

	return service.shellPlanForGuest(ctx, guest, command, userOverride)
}

func (service *Service) nodeShellPlan(nodeName string, command []string) (ShellPlan, error) {
	nodeName = strings.TrimSpace(nodeName)
	if nodeName == "" {
		return ShellPlan{}, errors.New("node name is required")
	}
	sshHost, err := service.proxmoxSSHHostForNode(nodeName)
	if err != nil {
		return ShellPlan{}, err
	}

	args := append([]string{}, service.Config.Defaults.SSHOptions...)
	args = append(args, sshHost)
	if len(command) > 0 {
		args = append(args, "--")
		args = append(args, command...)
	}

	return ShellPlan{
		Kind:    ShellTargetNode,
		Name:    nodeName,
		Node:    nodeName,
		SSHHost: sshHost,
		Args:    args,
		Command: sshCommandText(args),
	}, nil
}

func (service *Service) shellPlanForGuest(ctx context.Context, guest proxmox.VMResource, command []string, userOverride string) (ShellPlan, error) {
	if guest.IsContainer() {
		return service.containerShellPlan(guest, command)
	}

	return service.vmShellPlan(ctx, guest, command, userOverride)
}

func (service *Service) containerShellPlan(guest proxmox.VMResource, command []string) (ShellPlan, error) {
	sshHost, err := service.proxmoxSSHHostForNode(guest.Node)
	if err != nil {
		return ShellPlan{}, err
	}

	args := append([]string{}, service.Config.Defaults.SSHOptions...)
	args = append(args, sshHost, "--", "pct")
	if len(command) == 0 {
		args = append(args, "enter", strconv.Itoa(guest.VMID))
	} else {
		args = append(args, "exec", strconv.Itoa(guest.VMID), "--")
		args = append(args, command...)
	}

	return ShellPlan{
		Kind:      ShellTargetContainer,
		VMID:      guest.VMID,
		Name:      guest.Name,
		Node:      guest.Node,
		GuestType: guest.GuestType(),
		SSHHost:   sshHost,
		Args:      args,
		Command:   sshCommandText(args),
	}, nil
}

func (service *Service) vmShellPlan(ctx context.Context, guest proxmox.VMResource, command []string, userOverride string) (ShellPlan, error) {
	vmConfig, err := service.Client.GuestConfig(ctx, guest.Node, guest.GuestType(), guest.VMID)
	if err != nil {
		return ShellPlan{}, err
	}

	user := userOverride
	if user == "" {
		user = stringValue(vmConfig["ciuser"])
	}
	if user == "" {
		user = "ubuntu"
	}

	ipAddress, err := service.Client.FirstRoutableIP(ctx, guest.Node, guest.VMID)
	if err != nil {
		return ShellPlan{}, err
	}
	if ipAddress == "" {
		return ShellPlan{}, fmt.Errorf("no routable guest agent IP found for VM %d", guest.VMID)
	}

	sshHost := fmt.Sprintf("%s@%s", user, ipAddress)
	args := append([]string{}, service.Config.Defaults.SSHOptions...)
	args = append(args, sshHost)
	if len(command) > 0 {
		args = append(args, "--")
		args = append(args, command...)
	}

	return ShellPlan{
		Kind:      ShellTargetVM,
		VMID:      guest.VMID,
		Name:      guest.Name,
		Node:      guest.Node,
		GuestType: guest.GuestType(),
		SSHHost:   sshHost,
		IPAddress: ipAddress,
		User:      user,
		Args:      args,
		Command:   sshCommandText(args),
	}, nil
}

func (service *Service) proxmoxSSHHostForNode(nodeName string) (string, error) {
	node, exists := service.Config.Nodes[nodeName]
	if !exists {
		return "", fmt.Errorf("unknown Proxmox node %q", nodeName)
	}
	if strings.TrimSpace(node.SSHHost) != "" {
		return node.SSHHost, nil
	}
	if len(service.Config.Nodes) == 1 && strings.TrimSpace(service.Config.Caddy.ProxmoxSSHHost) != "" {
		return service.Config.Caddy.ProxmoxSSHHost, nil
	}

	return "", fmt.Errorf("nodes.%s.ssh_host is required for shell access", nodeName)
}

func (service *Service) SetGuestTags(ctx context.Context, vmid int, tags []string) ([]string, error) {
	guest, err := service.FindGuest(ctx, vmid)
	if err != nil {
		return nil, err
	}

	normalizedTags, err := normalizeGuestTags(tags)
	if err != nil {
		return nil, err
	}

	values := url.Values{"tags": {strings.Join(normalizedTags, ";")}}
	if err := service.Client.SetGuestConfig(ctx, guest.Node, guest.GuestType(), vmid, values); err != nil {
		return nil, err
	}

	return normalizedTags, nil
}

func (service *Service) AddGuestTags(ctx context.Context, vmid int, tags []string) ([]string, error) {
	detail, err := service.GuestDetail(ctx, vmid)
	if err != nil {
		return nil, err
	}

	return service.SetGuestTags(ctx, vmid, append(detail.Tags, tags...))
}

func (service *Service) RemoveGuestTags(ctx context.Context, vmid int, tags []string) ([]string, error) {
	detail, err := service.GuestDetail(ctx, vmid)
	if err != nil {
		return nil, err
	}

	removeTags, err := normalizeGuestTags(tags)
	if err != nil {
		return nil, err
	}
	removeTagSet := make(map[string]bool, len(removeTags))
	for _, tag := range removeTags {
		removeTagSet[tag] = true
	}

	remainingTags := make([]string, 0, len(detail.Tags))
	for _, tag := range detail.Tags {
		if !removeTagSet[tag] {
			remainingTags = append(remainingTags, tag)
		}
	}

	return service.SetGuestTags(ctx, vmid, remainingTags)
}

type resolvedCreateRequest struct {
	CreateResult
	SSHKeyContents string
	Bridge         string
	CPUType        string
	DNS            string
	IPAddress      string
	Gateway        string
}

type resolvedCreateContainerRequest struct {
	CreateContainerResult
	SSHKeyContents string
	Bridge         string
	OSType         string
	DNS            string
	IPAddress      string
	Gateway        string
	Unprivileged   bool
	Features       []string
}

func (service *Service) resolveCreateContainerRequest(request CreateContainerRequest) (resolvedCreateContainerRequest, error) {
	if request.Node == "" || request.Image == "" || request.Name == "" || request.Storage == "" {
		return resolvedCreateContainerRequest{}, errors.New("node, image, name, and storage are required")
	}

	if _, exists := service.Config.Nodes[request.Node]; !exists {
		return resolvedCreateContainerRequest{}, fmt.Errorf("unknown node %q", request.Node)
	}

	image, exists := service.Config.LXCImages[request.Image]
	if !exists {
		return resolvedCreateContainerRequest{}, fmt.Errorf("unknown LXC image %q", request.Image)
	}

	templateVolume := image.Templates[request.Node]
	if templateVolume == "" {
		return resolvedCreateContainerRequest{}, fmt.Errorf("LXC image %q has no template for node %q", request.Image, request.Node)
	}
	if err := validateLXCTemplateVolume(request.Image, request.Node, templateVolume); err != nil {
		return resolvedCreateContainerRequest{}, err
	}

	if _, exists := service.Config.Storages[request.Storage]; !exists {
		return resolvedCreateContainerRequest{}, fmt.Errorf("unknown storage %q", request.Storage)
	}

	sshKeyContents, err := service.resolveSSHKeyContents(CreateRequest{
		SSHKey:        request.SSHKey,
		SSHKeys:       request.SSHKeys,
		SSHPublicKeys: request.SSHPublicKeys,
	})
	if err != nil {
		return resolvedCreateContainerRequest{}, err
	}

	cores, memoryMB, diskGB, err := service.resolveResources(CreateRequest{
		Plan:     request.Plan,
		Cores:    request.Cores,
		MemoryMB: request.MemoryMB,
		DiskGB:   request.DiskGB,
	})
	if err != nil {
		return resolvedCreateContainerRequest{}, err
	}

	networkMode := request.NetworkMode
	if networkMode == "" {
		networkMode = service.Config.Defaults.Network
	}
	if networkMode != "dhcp" && networkMode != "static" {
		return resolvedCreateContainerRequest{}, fmt.Errorf("network must be dhcp or static, got %q", networkMode)
	}
	if networkMode == "static" && (request.IPAddress == "" || request.Gateway == "") {
		return resolvedCreateContainerRequest{}, errors.New("static network requires ip and gateway")
	}

	tags, err := normalizeGuestTags(request.Tags)
	if err != nil {
		return resolvedCreateContainerRequest{}, err
	}

	features, err := resolveContainerFeatures(request)
	if err != nil {
		return resolvedCreateContainerRequest{}, err
	}

	swapMB := request.SwapMB
	if swapMB < 0 {
		return resolvedCreateContainerRequest{}, errors.New("swap must be zero or greater")
	}
	if swapMB == 0 {
		swapMB = 512
	}

	return resolvedCreateContainerRequest{
		CreateContainerResult: CreateContainerResult{
			Node:        request.Node,
			Image:       request.Image,
			ImageLabel:  image.Label,
			Plan:        request.Plan,
			Template:    templateVolume,
			Name:        request.Name,
			Storage:     request.Storage,
			Cores:       cores,
			MemoryMB:    memoryMB,
			DiskGB:      diskGB,
			SwapMB:      swapMB,
			User:        image.DefaultUser,
			NetworkMode: networkMode,
			Tags:        tags,
			Started:     request.Start,
			Features:    features,
		},
		SSHKeyContents: sshKeyContents,
		Bridge:         service.Config.Defaults.Bridge,
		OSType:         image.OSType,
		DNS:            request.DNS,
		IPAddress:      request.IPAddress,
		Gateway:        request.Gateway,
		Unprivileged:   request.Unprivileged,
		Features:       features,
	}, nil
}

func validateLXCTemplateVolume(imageName string, nodeName string, templateVolume string) error {
	if strings.Contains(templateVolume, ":vztmpl/") {
		return nil
	}

	return fmt.Errorf("LXC image %q template for node %q must be a Proxmox template volume ID like local:vztmpl/ubuntu-24.04-standard_24.04-2_amd64.tar.zst, got %q; run `boringctl storage list --node %s --storage local --content vztmpl` to find valid template volume IDs", imageName, nodeName, templateVolume, nodeName)
}

func resolveContainerFeatures(request CreateContainerRequest) ([]string, error) {
	features := map[string]string{}
	addFeature := func(feature string) error {
		for _, rawPart := range strings.Split(feature, ",") {
			part := strings.TrimSpace(rawPart)
			if part == "" {
				continue
			}

			key, value, hasValue := strings.Cut(part, "=")
			key = strings.TrimSpace(key)
			value = strings.TrimSpace(value)
			if key == "" || strings.ContainsAny(key, " \t\n\r=") {
				return fmt.Errorf("invalid LXC feature %q", part)
			}
			if !hasValue {
				value = "1"
			}
			if value == "" {
				return fmt.Errorf("invalid LXC feature %q", part)
			}

			features[key] = value
		}

		return nil
	}

	if request.Docker {
		features["nesting"] = "1"
	}
	if request.Nesting {
		features["nesting"] = "1"
	}
	if request.Keyctl {
		features["keyctl"] = "1"
	}
	for _, feature := range request.Features {
		if err := addFeature(feature); err != nil {
			return nil, err
		}
	}

	featureNames := make([]string, 0, len(features))
	for featureName := range features {
		featureNames = append(featureNames, featureName)
	}
	sort.Strings(featureNames)

	resolvedFeatures := make([]string, 0, len(featureNames))
	for _, featureName := range featureNames {
		resolvedFeatures = append(resolvedFeatures, featureName+"="+features[featureName])
	}

	return resolvedFeatures, nil
}

func annotateContainerCreateError(err error, resolved resolvedCreateContainerRequest) error {
	if strings.Contains(err.Error(), "changing feature flags (except nesting) is only allowed for root@pam") {
		return fmt.Errorf("%w; remove --keyctl or --feature keyctl=1 when using an API token. --docker only sets nesting=1 because Proxmox allows nesting through normal API tokens", err)
	}
	if !strings.Contains(err.Error(), "Only root can pass arbitrary filesystem paths") {
		return err
	}

	return fmt.Errorf("%w; check lxc_images.%s.templates.%s is a Proxmox template volume ID like local:vztmpl/... (current value: %q)", err, resolved.Image, resolved.Node, resolved.Template)
}

func (service *Service) resolveCreateRequest(request CreateRequest) (resolvedCreateRequest, error) {
	if request.Node == "" || request.Image == "" || request.Name == "" || request.Storage == "" {
		return resolvedCreateRequest{}, errors.New("node, image, name, and storage are required")
	}

	if _, exists := service.Config.Nodes[request.Node]; !exists {
		return resolvedCreateRequest{}, fmt.Errorf("unknown node %q", request.Node)
	}

	image, exists := service.Config.Images[request.Image]
	if !exists {
		return resolvedCreateRequest{}, fmt.Errorf("unknown image %q", request.Image)
	}

	templateID := image.Templates[request.Node]
	if templateID == 0 {
		return resolvedCreateRequest{}, fmt.Errorf("image %q has no template for node %q", request.Image, request.Node)
	}

	if _, exists := service.Config.Storages[request.Storage]; !exists {
		return resolvedCreateRequest{}, fmt.Errorf("unknown storage %q", request.Storage)
	}

	sshKeyContents, err := service.resolveSSHKeyContents(request)
	if err != nil {
		return resolvedCreateRequest{}, err
	}

	cores, memoryMB, diskGB, err := service.resolveResources(request)
	if err != nil {
		return resolvedCreateRequest{}, err
	}

	networkMode := request.NetworkMode
	if networkMode == "" {
		networkMode = service.Config.Defaults.Network
	}
	if networkMode != "dhcp" && networkMode != "static" {
		return resolvedCreateRequest{}, fmt.Errorf("network must be dhcp or static, got %q", networkMode)
	}

	if networkMode == "static" && (request.IPAddress == "" || request.Gateway == "") {
		return resolvedCreateRequest{}, errors.New("static network requires ip and gateway")
	}

	return resolvedCreateRequest{
		CreateResult: CreateResult{
			Node:        request.Node,
			Image:       request.Image,
			ImageLabel:  image.Label,
			Plan:        request.Plan,
			TemplateID:  templateID,
			Name:        request.Name,
			Storage:     request.Storage,
			Cores:       cores,
			MemoryMB:    memoryMB,
			DiskGB:      diskGB,
			User:        image.DefaultUser,
			NetworkMode: networkMode,
		},
		SSHKeyContents: sshKeyContents,
		Bridge:         service.Config.Defaults.Bridge,
		CPUType:        service.Config.Defaults.CPUType,
		DNS:            request.DNS,
		IPAddress:      request.IPAddress,
		Gateway:        request.Gateway,
	}, nil
}

func (service *Service) resolveResources(request CreateRequest) (int, int, int, error) {
	cores := request.Cores
	memoryMB := request.MemoryMB
	diskGB := request.DiskGB

	if request.Plan != "" && request.Plan != "custom" {
		plan, exists := service.Config.Plans[request.Plan]
		if !exists {
			return 0, 0, 0, fmt.Errorf("unknown plan %q", request.Plan)
		}

		if cores == 0 {
			cores = plan.Cores
		}
		if memoryMB == 0 {
			memoryMB = plan.MemoryMB
		}
		if diskGB == 0 {
			diskGB = plan.DiskGB
		}
	}

	if cores <= 0 || memoryMB <= 0 || diskGB <= 0 {
		return 0, 0, 0, errors.New("cores, memory, and disk must be set")
	}

	return cores, memoryMB, diskGB, nil
}

func (service *Service) resolveSSHKeyContents(request CreateRequest) (string, error) {
	var sshKeyContents []string
	sshKeyNames := request.SSHKeys
	if request.SSHKey != "" {
		sshKeyNames = append(sshKeyNames, request.SSHKey)
	}
	if len(sshKeyNames) == 0 && len(request.SSHPublicKeys) == 0 {
		sshKeyNames = []string{service.Config.Defaults.SSHKey}
	}

	for _, sshKeyName := range sshKeyNames {
		sshKey, exists := service.Config.SSHKeys[sshKeyName]
		if !exists {
			return "", fmt.Errorf("unknown SSH key %q", sshKeyName)
		}

		sshKeyPath, err := service.Config.ExpandPath(sshKey.Path)
		if err != nil {
			return "", err
		}

		normalizedKey, err := readSSHAuthorizedKey(sshKeyPath)
		if err != nil {
			return "", err
		}
		sshKeyContents = append(sshKeyContents, normalizedKey)
	}

	for _, sshPublicKey := range request.SSHPublicKeys {
		normalizedKey, err := normalizeSSHAuthorizedKey([]byte(sshPublicKey))
		if err != nil {
			return "", err
		}
		sshKeyContents = append(sshKeyContents, normalizedKey)
	}

	if len(sshKeyContents) == 0 {
		return "", errors.New("at least one SSH key is required")
	}

	return strings.Join(dedupeStrings(sshKeyContents), "\n"), nil
}

func (resolved resolvedCreateRequest) proxmoxConfig() url.Values {
	values := url.Values{
		"cores":     {strconv.Itoa(resolved.Cores)},
		"memory":    {strconv.Itoa(resolved.MemoryMB)},
		"cpu":       {resolved.CPUType},
		"ciuser":    {resolved.User},
		"sshkeys":   {proxmoxSSHKeysValue(resolved.SSHKeyContents)},
		"ipconfig0": {resolved.ipConfig()},
		"net0":      {"virtio,bridge=" + resolved.Bridge},
	}

	if resolved.DNS != "" {
		values.Set("nameserver", resolved.DNS)
	}

	return values
}

func (resolved resolvedCreateContainerRequest) proxmoxConfig() url.Values {
	values := url.Values{
		"vmid":            {strconv.Itoa(resolved.VMID)},
		"hostname":        {resolved.Name},
		"ostemplate":      {resolved.Template},
		"rootfs":          {fmt.Sprintf("%s:%d", resolved.Storage, resolved.DiskGB)},
		"cores":           {strconv.Itoa(resolved.Cores)},
		"memory":          {strconv.Itoa(resolved.MemoryMB)},
		"swap":            {strconv.Itoa(resolved.SwapMB)},
		"net0":            {resolved.containerNet0()},
		"ssh-public-keys": {resolved.SSHKeyContents},
		"unprivileged":    {boolString(resolved.Unprivileged)},
		"start":           {boolString(resolved.Started)},
	}

	if resolved.OSType != "" {
		values.Set("ostype", resolved.OSType)
	}
	if resolved.DNS != "" {
		values.Set("nameserver", resolved.DNS)
	}
	if len(resolved.Tags) > 0 {
		values.Set("tags", strings.Join(resolved.Tags, ";"))
	}
	if len(resolved.Features) > 0 {
		values.Set("features", strings.Join(resolved.Features, ","))
	}

	return values
}

func (resolved resolvedCreateContainerRequest) containerNet0() string {
	parts := []string{
		"name=eth0",
		"bridge=" + resolved.Bridge,
		"type=veth",
	}
	if resolved.NetworkMode == "static" {
		parts = append(parts, "ip="+resolved.IPAddress, "gw="+resolved.Gateway)
	} else {
		parts = append(parts, "ip=dhcp")
	}
	return strings.Join(parts, ",")
}

func (resolved resolvedCreateRequest) ipConfig() string {
	if resolved.NetworkMode == "static" {
		return fmt.Sprintf("ip=%s,gw=%s", resolved.IPAddress, resolved.Gateway)
	}

	return "ip=dhcp"
}

func (service *Service) waitForIP(ctx context.Context, node string, vmid int, timeout time.Duration, reporter Reporter) (string, string, error) {
	pollContext, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()

	staleKnownHostWarningShown := false
	var duplicateWarning string
	var reportedDuplicateWarning string
	for {
		ipAddress, err := service.Client.FirstRoutableIP(pollContext, node, vmid)
		if err == nil && ipAddress != "" {
			staleKnownHostWarning := ""
			if hasKnownHostEntry(ipAddress) {
				staleKnownHostWarning = fmt.Sprintf("SSH known_hosts contains %s. If this IP is reused for a different VM, clear stale host keys with: ssh-keygen -R %s", ipAddress, ipAddress)
				if !staleKnownHostWarningShown {
					report(reporter, staleKnownHostWarning)
					staleKnownHostWarningShown = true
				}
			}

			conflict, exists := service.findIPConflict(pollContext, vmid, ipAddress)
			if !exists {
				if staleKnownHostWarning != "" {
					return ipAddress, staleKnownHostWarning, nil
				}
				return ipAddress, "", nil
			}

			duplicateWarning = fmt.Sprintf("Guest agent reported %s, but VM %d (%s) on %s already reports that IP. Check DHCP/template machine-id before using SSH. If this IP belonged to an old VM, clear the stale host key with: ssh-keygen -R %s", ipAddress, conflict.VMID, conflict.Name, conflict.Node, ipAddress)
			if duplicateWarning != reportedDuplicateWarning {
				report(reporter, duplicateWarning)
				reportedDuplicateWarning = duplicateWarning
			}
		}

		select {
		case <-pollContext.Done():
			return "", duplicateWarning, nil
		case <-ticker.C:
		}
	}
}

func (service *Service) waitForContainerIP(ctx context.Context, node string, vmid int, timeout time.Duration, reporter Reporter) (string, string, error) {
	pollContext, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()

	for {
		guest := proxmox.VMResource{VMID: vmid, Node: node, Type: proxmox.GuestTypeLXC}
		config, _ := service.Client.GuestConfig(pollContext, node, proxmox.GuestTypeLXC, vmid)
		ipAddresses, err := service.GuestIPAddresses(pollContext, guest, config)
		if err == nil && len(ipAddresses) > 0 {
			ipAddress := ipAddresses[0]
			if hasKnownHostEntry(ipAddress) {
				warning := fmt.Sprintf("SSH known_hosts contains %s. If this IP is reused for a different container, clear stale host keys with: ssh-keygen -R %s", ipAddress, ipAddress)
				report(reporter, warning)
				return ipAddress, warning, nil
			}
			return ipAddress, "", nil
		}

		select {
		case <-pollContext.Done():
			return "", "container was created, but no routable IP was reported before timeout", nil
		case <-ticker.C:
			report(reporter, "Still waiting for LXC IP...")
		}
	}
}

func hasKnownHostEntry(ip string) bool {
	if strings.TrimSpace(ip) == "" {
		return false
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return false
	}

	knownHostsPath := filepath.Join(homeDir, ".ssh", "known_hosts")
	fileContents, err := os.ReadFile(knownHostsPath)
	if err != nil {
		return false
	}

	for _, line := range strings.Split(string(fileContents), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		hostPart := strings.Fields(line)
		if len(hostPart) == 0 {
			continue
		}

		for _, host := range strings.Split(hostPart[0], ",") {
			host = strings.TrimSpace(host)
			if host == "" || strings.HasPrefix(host, "|") {
				continue
			}

			if trimmed, found := trimKnownHostToken(host); found && trimmed == ip {
				return true
			}
		}
	}

	return false
}

func trimKnownHostToken(host string) (string, bool) {
	host = strings.TrimSpace(host)
	if host == "" {
		return "", false
	}

	if strings.HasPrefix(host, "[") {
		closing := strings.Index(host, "]")
		if closing == -1 {
			return "", false
		}
		host = host[1:closing]
	}

	if strings.HasPrefix(host, "|") {
		return "", false
	}

	if trimmedAddress := strings.TrimSuffix(host, "."); trimmedAddress != "" {
		host = trimmedAddress
	}

	if candidate, _, exists := strings.Cut(host, ":"); exists {
		if parsed := net.ParseIP(candidate); parsed != nil {
			return parsed.String(), true
		}
	}

	if parsed := net.ParseIP(host); parsed == nil {
		return "", false
	}

	return net.ParseIP(host).String(), true
}

func (service *Service) findIPConflict(ctx context.Context, currentVMID int, ipAddress string) (proxmox.VMResource, bool) {
	vms, err := service.ListVMs(ctx)
	if err != nil {
		return proxmox.VMResource{}, false
	}

	for _, vm := range vms {
		if vm.VMID == currentVMID || vm.Status != "running" {
			continue
		}

		vmIPAddress, err := service.Client.FirstRoutableIP(ctx, vm.Node, vm.VMID)
		if err != nil || vmIPAddress == "" {
			continue
		}

		if vmIPAddress == ipAddress {
			return vm, true
		}
	}

	return proxmox.VMResource{}, false
}

func (service *Service) configuredNodeHealth(apiNodes []proxmox.Node) []NodeHealth {
	apiNodeByName := make(map[string]proxmox.Node, len(apiNodes))
	for _, node := range apiNodes {
		apiNodeByName[node.Name] = node
	}

	nodeHealth := make([]NodeHealth, 0, len(service.Config.Nodes))
	for _, nodeName := range service.Config.NodeNames() {
		apiNode, exists := apiNodeByName[nodeName]
		health := NodeHealth{
			Name:       nodeName,
			Status:     "unknown",
			Configured: true,
		}

		if exists {
			health.Status = apiNode.Status
			health.CPUPercent = apiNode.CPU * 100
			health.MaxCPU = apiNode.MaxCPU
			health.MemoryBytes = apiNode.Mem
			health.MaxMemBytes = apiNode.MaxMem
		}

		nodeHealth = append(nodeHealth, health)
	}
	for _, apiNode := range apiNodes {
		if _, configured := service.Config.Nodes[apiNode.Name]; configured {
			continue
		}
		nodeHealth = append(nodeHealth, NodeHealth{
			Name:        apiNode.Name,
			Status:      apiNode.Status,
			CPUPercent:  apiNode.CPU * 100,
			MaxCPU:      apiNode.MaxCPU,
			MemoryBytes: apiNode.Mem,
			MaxMemBytes: apiNode.MaxMem,
		})
	}

	return nodeHealth
}

func (service *Service) configuredStorageHealth(ctx context.Context) ([]StorageHealth, []string) {
	type nodeStorageResult struct {
		nodeName string
		storages []proxmox.StorageStatus
		err      error
	}

	nodeNames := service.Config.NodeNames()
	results := make(chan nodeStorageResult, len(nodeNames))
	semaphore := make(chan struct{}, max(min(len(nodeNames), 4), 1))
	for _, nodeName := range nodeNames {
		go func() {
			select {
			case semaphore <- struct{}{}:
				defer func() { <-semaphore }()
			case <-ctx.Done():
				results <- nodeStorageResult{nodeName: nodeName, err: ctx.Err()}
				return
			}

			apiStorages, err := service.Client.NodeStorages(ctx, nodeName)
			results <- nodeStorageResult{nodeName: nodeName, storages: apiStorages, err: err}
		}()
	}

	var storageHealth []StorageHealth
	var storageErrors []string
	for range nodeNames {
		result := <-results
		if result.err != nil {
			storageErrors = append(storageErrors, fmt.Sprintf("%s: %s", result.nodeName, result.err))
			continue
		}

		configuredStorageNames := map[string]bool{}
		if node, exists := service.Config.Nodes[result.nodeName]; exists {
			for _, storageName := range node.Storages {
				configuredStorageNames[storageName] = true
			}
		}

		seenConfiguredStorage := make(map[string]bool, len(configuredStorageNames))
		for _, apiStorage := range result.storages {
			if len(configuredStorageNames) > 0 && !configuredStorageNames[apiStorage.Name] {
				continue
			}
			seenConfiguredStorage[apiStorage.Name] = true
			if len(configuredStorageNames) > 0 && (apiStorage.Active != 1 || apiStorage.Enabled != 1) {
				storageErrors = append(storageErrors, fmt.Sprintf("%s/%s: storage is inactive or disabled", result.nodeName, apiStorage.Name))
			}

			storageHealth = append(storageHealth, StorageHealth{
				Node:           result.nodeName,
				Name:           apiStorage.Name,
				Type:           apiStorage.Type,
				Active:         apiStorage.Active == 1,
				Enabled:        apiStorage.Enabled == 1,
				Shared:         apiStorage.Shared == 1,
				UsedBytes:      apiStorage.Used,
				TotalBytes:     apiStorage.Total,
				AvailableBytes: apiStorage.Available,
			})
		}
		for storageName := range configuredStorageNames {
			if !seenConfiguredStorage[storageName] {
				storageErrors = append(storageErrors, fmt.Sprintf("%s/%s: configured storage was not returned by Proxmox", result.nodeName, storageName))
			}
		}
	}

	sort.Slice(storageHealth, func(leftIndex int, rightIndex int) bool {
		left := storageHealth[leftIndex]
		right := storageHealth[rightIndex]
		if left.Node == right.Node {
			return left.Name < right.Name
		}

		return left.Node < right.Node
	})
	sort.Strings(storageErrors)

	return storageHealth, storageErrors
}

func (service *Service) configuredSuggestedPlans() []SuggestedPlan {
	plans := make([]SuggestedPlan, 0, len(service.Config.Plans))
	for _, planName := range service.Config.PlanNames() {
		plan := service.Config.Plans[planName]
		plans = append(plans, SuggestedPlan{
			Name:     planName,
			Label:    plan.Label,
			Cores:    plan.Cores,
			MemoryMB: plan.MemoryMB,
			DiskGB:   plan.DiskGB,
			Reason:   "from config fallback",
		})
	}

	return plans
}

func findNodeHealth(nodes []NodeHealth, nodeName string) (NodeHealth, bool) {
	for _, node := range nodes {
		if node.Name == nodeName {
			return node, true
		}
	}

	return NodeHealth{}, false
}

func findStorageHealth(storages []StorageHealth, nodeName string, storageName string) (StorageHealth, bool) {
	for _, storage := range storages {
		if storage.Node == nodeName && storage.Name == storageName {
			return storage, true
		}
	}

	return StorageHealth{}, false
}

func roundDownMemoryMB(memoryMB int) int {
	if memoryMB >= 1024 {
		return memoryMB / 512 * 512
	}

	return memoryMB
}

func minInt(left int, right int) int {
	if left < right {
		return left
	}

	return right
}

func maxInt(left int, right int) int {
	if left > right {
		return left
	}

	return right
}

func joinWarnings(existingWarning string, newWarning string) string {
	if existingWarning == "" {
		return newWarning
	}

	if newWarning == "" {
		return existingWarning
	}

	return existingWarning + " " + newWarning
}

func report(reporter Reporter, message string) {
	if reporter != nil {
		reporter(message)
	}
}

func stringValue(value any) string {
	switch typedValue := value.(type) {
	case string:
		return typedValue
	default:
		return ""
	}
}

func titleAction(action string) string {
	if action == "" {
		return action
	}

	return strings.ToUpper(action[:1]) + action[1:]
}

func guestKindLabel(guest proxmox.VMResource) string {
	if guest.IsContainer() {
		return "container"
	}
	return "VM"
}

func guestTags(guest proxmox.VMResource, config map[string]any) []string {
	if guest.Tags != "" {
		tags, _ := normalizeGuestTags(strings.Split(guest.Tags, ";"))
		return tags
	}

	tags, _ := normalizeGuestTags(strings.Split(stringValue(config["tags"]), ";"))
	return tags
}

func normalizeGuestTags(tags []string) ([]string, error) {
	seen := map[string]bool{}
	normalizedTags := make([]string, 0, len(tags))
	for _, tag := range tags {
		for _, part := range strings.FieldsFunc(tag, func(character rune) bool {
			return character == ',' || character == ';' || character == ' ' || character == '\n' || character == '\t'
		}) {
			normalizedTag := strings.ToLower(strings.TrimSpace(part))
			if normalizedTag == "" {
				continue
			}
			if strings.ContainsAny(normalizedTag, "/\\") {
				return nil, fmt.Errorf("invalid tag %q", part)
			}
			if seen[normalizedTag] {
				continue
			}
			seen[normalizedTag] = true
			normalizedTags = append(normalizedTags, normalizedTag)
		}
	}

	sort.Strings(normalizedTags)
	return normalizedTags, nil
}

func lxcConfigIPAddresses(config map[string]any) []string {
	var ipAddresses []string
	for key, value := range config {
		if !strings.HasPrefix(key, "net") {
			continue
		}

		for _, part := range strings.Split(stringValue(value), ",") {
			name, address, exists := strings.Cut(strings.TrimSpace(part), "=")
			if !exists {
				continue
			}
			if name != "ip" && name != "ip6" {
				continue
			}
			if isUsableGuestAddress(address) {
				ipAddresses = appendUnique(ipAddresses, trimCIDR(address))
			}
		}
	}

	sort.Strings(ipAddresses)
	return ipAddresses
}

func isUsableGuestAddress(address string) bool {
	address = trimCIDR(address)
	if address == "" || address == "dhcp" || address == "auto" || address == "manual" {
		return false
	}

	parsedIP := net.ParseIP(address)
	if parsedIP == nil {
		return false
	}

	return !parsedIP.IsLoopback() &&
		!parsedIP.IsLinkLocalUnicast() &&
		!parsedIP.IsLinkLocalMulticast() &&
		!parsedIP.IsMulticast() &&
		!parsedIP.IsUnspecified()
}

func trimCIDR(address string) string {
	address = strings.TrimSpace(address)
	if host, _, err := net.ParseCIDR(address); err == nil {
		return host.String()
	}
	if host, _, exists := strings.Cut(address, "/"); exists {
		return strings.TrimSpace(host)
	}
	return address
}

func appendUnique(values []string, value string) []string {
	for _, existingValue := range values {
		if existingValue == value {
			return values
		}
	}
	return append(values, value)
}

func shellWord(value string) string {
	if value == "" {
		return "''"
	}
	if strings.ContainsAny(value, " \t\n'\"\\$`!#&()[]{};<>|*?~") {
		return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
	}
	return value
}

func splitShellTargetRef(targetRef string) (string, string) {
	targetKind, ref, hasPrefix := strings.Cut(targetRef, ":")
	if !hasPrefix {
		return "", targetRef
	}

	switch strings.ToLower(strings.TrimSpace(targetKind)) {
	case "node", "guest", "lxc", "container", "vm", "qemu":
		return strings.ToLower(strings.TrimSpace(targetKind)), strings.TrimSpace(ref)
	default:
		return "", targetRef
	}
}

func sshCommandText(args []string) string {
	parts := append([]string{"ssh"}, args...)
	for partIndex, part := range parts {
		parts[partIndex] = shellWord(part)
	}

	return strings.Join(parts, " ")
}

func guestRefs(guests []proxmox.VMResource) string {
	refs := make([]string, 0, len(guests))
	for _, guest := range guests {
		refs = append(refs, fmt.Sprintf("%s %d on %s", guestKindLabel(guest), guest.VMID, guest.Node))
	}
	sort.Strings(refs)
	return strings.Join(refs, ", ")
}

func readSSHAuthorizedKey(path string) (string, error) {
	sshKeyBytes, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}

	normalizedKey, err := normalizeSSHAuthorizedKey(sshKeyBytes)
	if err != nil {
		return "", fmt.Errorf("invalid SSH public key %s: %w", path, err)
	}

	return normalizedKey, nil
}

func normalizeSSHAuthorizedKey(sshKeyBytes []byte) (string, error) {
	publicKey, comment, _, rest, err := ssh.ParseAuthorizedKey(sshKeyBytes)
	if err != nil {
		return "", err
	}

	if strings.TrimSpace(string(rest)) != "" {
		return "", errors.New("SSH public key input must contain exactly one key")
	}

	normalizedKey := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(publicKey)))
	if comment != "" {
		normalizedKey += " " + comment
	}

	return normalizedKey, nil
}

func proxmoxSSHKeysValue(sshKeyContents string) string {
	return strings.ReplaceAll(url.QueryEscape(sshKeyContents), "+", "%20")
}

func urlValuesToMap(values url.Values) map[string]string {
	result := make(map[string]string, len(values))
	for key, value := range values {
		if len(value) == 0 {
			continue
		}

		result[key] = value[0]
	}

	return result
}

func boolString(value bool) string {
	if value {
		return "1"
	}

	return "0"
}

func requestSSHKeyName(request CreateRequest, loadedConfig *config.Config) string {
	var names []string
	if request.SSHKey != "" {
		names = append(names, request.SSHKey)
	}
	if len(request.SSHKeys) > 0 {
		names = append(names, request.SSHKeys...)
	}
	if len(names) == 0 && len(request.SSHPublicKeys) == 0 {
		names = append(names, loadedConfig.Defaults.SSHKey)
	}
	if len(request.SSHPublicKeys) > 0 {
		names = append(names, fmt.Sprintf("%d pasted", len(request.SSHPublicKeys)))
	}

	return strings.Join(names, ",")
}

func dedupeStrings(values []string) []string {
	seen := map[string]bool{}
	var deduped []string
	for _, value := range values {
		if seen[value] {
			continue
		}

		seen[value] = true
		deduped = append(deduped, value)
	}

	return deduped
}
