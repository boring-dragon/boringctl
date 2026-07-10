package app

import (
	"context"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/boring-dragon/boringctl/internal/config"
	"github.com/boring-dragon/boringctl/internal/proxmox"
)

func TestProxmoxSSHKeysValueEscapesSpacesAndPlusSigns(t *testing.T) {
	got := proxmoxSSHKeysValue("ssh-rsa AAA+BBB user@host\nssh-ed25519 CCC+DDD other@host")
	want := "ssh-rsa%20AAA%2BBBB%20user%40host%0Assh-ed25519%20CCC%2BDDD%20other%40host"

	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestResolveCreateRequestCombinesNamedAndPastedSSHKeys(t *testing.T) {
	tempDir := t.TempDir()
	keyPath := filepath.Join(tempDir, "id_ed25519.pub")
	if err := os.WriteFile(keyPath, []byte("ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIGwLz8VsdcZZWHEMJAZp6IeZLliVVVC+fJ1WkE83UN1G named@example\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	pastedKey := "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIAgEPlGLf4puPw7JN7C0gBDmbdWqNFB0ylmDDfjly5Qt pasted@example"
	service := NewService(testConfig(keyPath), &fakeProxmoxClient{})

	resolved, err := service.resolveCreateRequest(CreateRequest{
		Node:          "pve2",
		Image:         "debian-13",
		Plan:          "tiny",
		Name:          "multi-key-vm",
		Storage:       "fast-storage",
		SSHKeys:       []string{"test"},
		SSHPublicKeys: []string{pastedKey},
		NetworkMode:   "dhcp",
	})
	if err != nil {
		t.Fatal(err)
	}

	keys := strings.Split(resolved.SSHKeyContents, "\n")
	if len(keys) != 2 {
		t.Fatalf("expected 2 keys, got %d: %q", len(keys), resolved.SSHKeyContents)
	}
	if !strings.Contains(keys[0], "named@example") {
		t.Fatalf("expected first key from named config, got %q", keys[0])
	}
	if !strings.Contains(keys[1], "pasted@example") {
		t.Fatalf("expected second key from pasted input, got %q", keys[1])
	}
}

func TestResolveCreateRequestAllowsOnlyPastedSSHKeys(t *testing.T) {
	pastedKey := "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIAgEPlGLf4puPw7JN7C0gBDmbdWqNFB0ylmDDfjly5Qt pasted@example"
	service := NewService(testConfig(""), &fakeProxmoxClient{})

	resolved, err := service.resolveCreateRequest(CreateRequest{
		Node:          "pve2",
		Image:         "debian-13",
		Plan:          "tiny",
		Name:          "pasted-key-vm",
		Storage:       "fast-storage",
		SSHKeys:       []string{},
		SSHPublicKeys: []string{pastedKey},
		NetworkMode:   "dhcp",
	})
	if err != nil {
		t.Fatal(err)
	}

	if strings.Count(resolved.SSHKeyContents, "\n") != 0 {
		t.Fatalf("expected only one pasted key, got %q", resolved.SSHKeyContents)
	}
	if !strings.Contains(resolved.SSHKeyContents, "pasted@example") {
		t.Fatalf("expected pasted key, got %q", resolved.SSHKeyContents)
	}
}

func TestCreateVMReturnsPartialErrorAndCleansUpAfterPostCloneFailure(t *testing.T) {
	tempDir := t.TempDir()
	keyPath := filepath.Join(tempDir, "id_ed25519.pub")
	if err := os.WriteFile(keyPath, []byte("ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIGwLz8VsdcZZWHEMJAZp6IeZLliVVVC+fJ1WkE83UN1G test@example\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	fakeClient := &fakeProxmoxClient{
		setConfigError: errors.New("config failed"),
	}
	service := NewService(testConfig(keyPath), fakeClient)

	_, err := service.CreateVM(context.Background(), CreateRequest{
		Node:        "pve2",
		Image:       "debian-13",
		Plan:        "tiny",
		Name:        "partial-vm",
		Storage:     "fast-storage",
		NetworkMode: "dhcp",
	}, nil)
	if err == nil {
		t.Fatal("expected create to fail")
	}

	var partialError *PartialCreateError
	if !errors.As(err, &partialError) {
		t.Fatalf("expected PartialCreateError, got %T", err)
	}

	if partialError.VMID != 123 || partialError.Node != "pve2" {
		t.Fatalf("unexpected partial VM reference: %+v", partialError)
	}

	if err := service.CleanupPartialVM(context.Background(), partialError, nil); err != nil {
		t.Fatal(err)
	}

	if fakeClient.deletedVMID != 123 || fakeClient.deletedNode != "pve2" {
		t.Fatalf("expected VM 123 on pve2 to be deleted, got %d on %s", fakeClient.deletedVMID, fakeClient.deletedNode)
	}
}

func TestDiscoverConfigBuildsCatalogFromProxmoxTemplates(t *testing.T) {
	service := NewService(testConfig(""), &fakeProxmoxClient{
		nodes: []proxmox.Node{
			{Name: "pve1", Status: "online"},
			{Name: "pve2", Status: "online"},
		},
		nodeStorages: map[string][]proxmox.StorageStatus{
			"pve1": {
				{Name: "bulk-storage", Type: "zfspool", Active: 1, Enabled: 1, Total: 100, Used: 25, Available: 75},
			},
			"pve2": {
				{Name: "fast-storage", Type: "lvmthin", Active: 1, Enabled: 1, Total: 100, Used: 50, Available: 50},
			},
		},
		vms: []proxmox.VMResource{
			{VMID: 9000, Name: "tmpl-ubuntu-24-04-x64", Node: "pve1", Template: 1},
			{VMID: 9200, Name: "tmpl-ubuntu-24-04-x64", Node: "pve2", Template: 1},
			{VMID: 9010, Name: "tmpl-debian-13-x64", Node: "pve1", Template: 1},
			{VMID: 9210, Name: "tmpl-debian-13-x64", Node: "pve2", Template: 1},
		},
	})

	discoveredConfig, err := service.DiscoverConfig(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if discoveredConfig.Nodes["pve1"].Storages[0] != "bulk-storage" {
		t.Fatalf("expected pve1 storage bulk-storage, got %+v", discoveredConfig.Nodes["pve1"].Storages)
	}

	if discoveredConfig.Images["ubuntu-24.04"].Templates["pve2"] != 9200 {
		t.Fatalf("expected pve2 ubuntu template 9200, got %+v", discoveredConfig.Images["ubuntu-24.04"].Templates)
	}

	if discoveredConfig.Images["debian-13"].DefaultUser != "debian" {
		t.Fatalf("expected debian default user, got %s", discoveredConfig.Images["debian-13"].DefaultUser)
	}
}

type fakeProxmoxClient struct {
	nodes          []proxmox.Node
	nodeStorages   map[string][]proxmox.StorageStatus
	vms            []proxmox.VMResource
	setConfigError error
	deletedNode    string
	deletedVMID    int
}

func (fakeClient *fakeProxmoxClient) Nodes(ctx context.Context) ([]proxmox.Node, error) {
	return fakeClient.nodes, nil
}

func (fakeClient *fakeProxmoxClient) Storages(ctx context.Context) ([]proxmox.Storage, error) {
	return nil, nil
}

func (fakeClient *fakeProxmoxClient) NodeStorages(ctx context.Context, node string) ([]proxmox.StorageStatus, error) {
	return fakeClient.nodeStorages[node], nil
}

func (fakeClient *fakeProxmoxClient) NextID(ctx context.Context) (int, error) {
	return 123, nil
}

func (fakeClient *fakeProxmoxClient) VMs(ctx context.Context) ([]proxmox.VMResource, error) {
	return fakeClient.vms, nil
}

func (fakeClient *fakeProxmoxClient) GuestConfig(ctx context.Context, node string, guestType string, vmid int) (map[string]any, error) {
	return fakeClient.VMConfig(ctx, node, vmid)
}

func (fakeClient *fakeProxmoxClient) VMConfig(ctx context.Context, node string, vmid int) (map[string]any, error) {
	return map[string]any{DefaultDisk: "local-lvm:vm-9000-disk-0"}, nil
}

func (fakeClient *fakeProxmoxClient) CloneVM(ctx context.Context, node string, templateID int, newID int, name string, storage string, fullClone bool) (string, error) {
	return "clone-upid", nil
}

func (fakeClient *fakeProxmoxClient) CreateContainer(ctx context.Context, node string, values url.Values) (string, error) {
	return "create-lxc-upid", nil
}

func (fakeClient *fakeProxmoxClient) ResizeDisk(ctx context.Context, node string, vmid int, disk string, size string) (string, error) {
	return "resize-upid", nil
}

func (fakeClient *fakeProxmoxClient) SetGuestConfig(ctx context.Context, node string, guestType string, vmid int, values url.Values) error {
	return fakeClient.SetVMConfig(ctx, node, vmid, values)
}

func (fakeClient *fakeProxmoxClient) SetVMConfig(ctx context.Context, node string, vmid int, values url.Values) error {
	return fakeClient.setConfigError
}

func (fakeClient *fakeProxmoxClient) StartGuest(ctx context.Context, node string, guestType string, vmid int) (string, error) {
	return fakeClient.StartVM(ctx, node, vmid)
}

func (fakeClient *fakeProxmoxClient) StartVM(ctx context.Context, node string, vmid int) (string, error) {
	return "start-upid", nil
}

func (fakeClient *fakeProxmoxClient) ShutdownGuest(ctx context.Context, node string, guestType string, vmid int) (string, error) {
	return fakeClient.ShutdownVM(ctx, node, vmid)
}

func (fakeClient *fakeProxmoxClient) ShutdownVM(ctx context.Context, node string, vmid int) (string, error) {
	return "shutdown-upid", nil
}

func (fakeClient *fakeProxmoxClient) RebootGuest(ctx context.Context, node string, guestType string, vmid int) (string, error) {
	return fakeClient.RebootVM(ctx, node, vmid)
}

func (fakeClient *fakeProxmoxClient) RebootVM(ctx context.Context, node string, vmid int) (string, error) {
	return "reboot-upid", nil
}

func (fakeClient *fakeProxmoxClient) DeleteGuest(ctx context.Context, node string, guestType string, vmid int) (string, error) {
	return fakeClient.DeleteVM(ctx, node, vmid)
}

func (fakeClient *fakeProxmoxClient) DeleteVM(ctx context.Context, node string, vmid int) (string, error) {
	fakeClient.deletedNode = node
	fakeClient.deletedVMID = vmid
	return "delete-upid", nil
}

func (fakeClient *fakeProxmoxClient) RenameGuest(ctx context.Context, node string, guestType string, vmid int, name string) error {
	return fakeClient.RenameVM(ctx, node, vmid, name)
}

func (fakeClient *fakeProxmoxClient) RenameVM(ctx context.Context, node string, vmid int, name string) error {
	return nil
}

func (fakeClient *fakeProxmoxClient) GuestSnapshots(ctx context.Context, node string, guestType string, vmid int) ([]proxmox.Snapshot, error) {
	return fakeClient.Snapshots(ctx, node, vmid)
}

func (fakeClient *fakeProxmoxClient) Snapshots(ctx context.Context, node string, vmid int) ([]proxmox.Snapshot, error) {
	return nil, nil
}

func (fakeClient *fakeProxmoxClient) CreateGuestSnapshot(ctx context.Context, node string, guestType string, vmid int, name string, description string) (string, error) {
	return fakeClient.CreateSnapshot(ctx, node, vmid, name, description)
}

func (fakeClient *fakeProxmoxClient) CreateSnapshot(ctx context.Context, node string, vmid int, name string, description string) (string, error) {
	return "snapshot-upid", nil
}

func (fakeClient *fakeProxmoxClient) DeleteGuestSnapshot(ctx context.Context, node string, guestType string, vmid int, name string) (string, error) {
	return fakeClient.DeleteSnapshot(ctx, node, vmid, name)
}

func (fakeClient *fakeProxmoxClient) DeleteSnapshot(ctx context.Context, node string, vmid int, name string) (string, error) {
	return "delete-snapshot-upid", nil
}

func (fakeClient *fakeProxmoxClient) RollbackGuestSnapshot(ctx context.Context, node string, guestType string, vmid int, name string) (string, error) {
	return "rollback-snapshot-upid", nil
}

func (fakeClient *fakeProxmoxClient) AgentNetworkInterfaces(ctx context.Context, node string, vmid int) ([]proxmox.NetworkInterface, error) {
	return nil, nil
}

func (fakeClient *fakeProxmoxClient) ContainerInterfaces(ctx context.Context, node string, vmid int) ([]proxmox.ContainerInterface, error) {
	return nil, nil
}

func (fakeClient *fakeProxmoxClient) FirstRoutableIP(ctx context.Context, node string, vmid int) (string, error) {
	return "192.0.2.55", nil
}

func (fakeClient *fakeProxmoxClient) WaitForTask(ctx context.Context, node string, upid string) error {
	return nil
}

func (fakeClient *fakeProxmoxClient) WaitForTaskWithTimeout(ctx context.Context, node string, upid string, timeout time.Duration) (proxmox.TaskStatus, error) {
	return proxmox.TaskStatus{UPID: upid, Status: "stopped", ExitStatus: "OK"}, nil
}

func (fakeClient *fakeProxmoxClient) Tasks(ctx context.Context, filter proxmox.TaskListFilter) ([]proxmox.Task, error) {
	return nil, nil
}

func (fakeClient *fakeProxmoxClient) TaskStatus(ctx context.Context, node string, upid string) (proxmox.TaskStatus, error) {
	return proxmox.TaskStatus{UPID: upid, Status: "stopped", ExitStatus: "OK"}, nil
}

func (fakeClient *fakeProxmoxClient) TaskLog(ctx context.Context, upid string) ([]proxmox.TaskLogEntry, error) {
	return nil, nil
}

func (fakeClient *fakeProxmoxClient) StopTask(ctx context.Context, upid string) error {
	return nil
}

func (fakeClient *fakeProxmoxClient) StorageContent(ctx context.Context, node string, storage string, filter proxmox.StorageContentFilter) ([]proxmox.StorageContent, error) {
	return nil, nil
}

func (fakeClient *fakeProxmoxClient) UploadStorageContent(ctx context.Context, request proxmox.UploadRequest) (string, error) {
	return "upload-upid", nil
}

func (fakeClient *fakeProxmoxClient) DownloadStorageContentFromURL(ctx context.Context, request proxmox.DownloadURLRequest) (string, error) {
	return "download-url-upid", nil
}

func (fakeClient *fakeProxmoxClient) CreateBackup(ctx context.Context, request proxmox.BackupRequest) (string, error) {
	return "backup-upid", nil
}

func (fakeClient *fakeProxmoxClient) RestoreBackup(ctx context.Context, request proxmox.RestoreRequest) (string, error) {
	return "restore-upid", nil
}

func testConfig(keyPath string) *config.Config {
	return &config.Config{
		Cluster: config.ClusterConfig{Endpoint: "https://192.0.2.10:8006", InsecureTLS: true},
		Auth:    config.AuthConfig{TokenIDEnv: "PVE_TOKEN_ID", TokenSecretEnv: "PVE_TOKEN_SECRET"},
		Defaults: config.DefaultsConfig{
			Bridge:    "vmbr0",
			CPUType:   "host",
			FullClone: true,
			SSHKey:    "test",
			Network:   "dhcp",
		},
		Nodes: map[string]config.NodeConfig{
			"pve2": {Label: "Compute 2", Storages: []string{"fast-storage"}},
		},
		Storages: map[string]config.StorageConfig{
			"fast-storage": {Label: "Fast Storage"},
		},
		Images: map[string]config.ImageConfig{
			"debian-13": {
				Label:       "Debian 13",
				Family:      "Debian",
				DefaultUser: "debian",
				Templates:   map[string]int{"pve2": 9210},
			},
		},
		Plans: map[string]config.PlanConfig{
			"tiny": {Label: "Tiny", Cores: 1, MemoryMB: 1024, DiskGB: 20},
		},
		SSHKeys: map[string]config.SSHKeyConfig{
			"test": {Path: keyPath},
		},
	}
}
