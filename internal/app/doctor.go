package app

import (
	"context"
	"crypto/x509"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/boring-labs/boringctl/internal/proxmox"
)

const (
	DoctorStatusPass = "pass"
	DoctorStatusWarn = "warn"
	DoctorStatusFail = "fail"
)

type DoctorCheck struct {
	Name     string   `json:"name"`
	Status   string   `json:"status"`
	Required bool     `json:"required"`
	Message  string   `json:"message"`
	Details  []string `json:"details,omitempty"`
}

type DoctorReport struct {
	CheckedAt  time.Time     `json:"checked_at"`
	ConfigPath string        `json:"config_path"`
	Endpoint   string        `json:"endpoint"`
	Status     string        `json:"status"`
	Checks     []DoctorCheck `json:"checks"`
}

func (report DoctorReport) FailureCount() int {
	failures := 0
	for _, check := range report.Checks {
		if check.Required && check.Status == DoctorStatusFail {
			failures++
		}
	}
	return failures
}

func (service *Service) Doctor(ctx context.Context, configPath string, clientSetupErrors ...error) DoctorReport {
	report := DoctorReport{
		CheckedAt:  time.Now().UTC(),
		ConfigPath: configPath,
		Endpoint:   service.Config.Cluster.Endpoint,
	}

	report.Checks = append(report.Checks,
		service.doctorConfigCheck(configPath),
		service.doctorCredentialsCheck(),
		service.doctorTLSCheck(),
	)

	var clientSetupError error
	if len(clientSetupErrors) > 0 {
		clientSetupError = clientSetupErrors[0]
	}

	var health ClusterHealth
	if clientSetupError != nil {
		report.Checks = append(report.Checks,
			DoctorCheck{Name: "proxmox_api", Status: DoctorStatusFail, Required: true, Message: "could not initialize the Proxmox client", Details: []string{clientSetupError.Error()}},
			DoctorCheck{Name: "nodes", Status: DoctorStatusWarn, Message: "node health check skipped because the Proxmox client is unavailable"},
		)
	} else {
		healthContext, cancelHealth := context.WithTimeout(ctx, 20*time.Second)
		health = service.Health(healthContext)
		cancelHealth()
		report.Checks = append(report.Checks, doctorAPIAndNodeChecks(health)...)
	}

	if clientSetupError == nil && health.Connected {
		report.Checks = append(report.Checks, service.doctorStorageCheck(health))
		guestsContext, cancelGuests := context.WithTimeout(ctx, 15*time.Second)
		guests, guestsError := service.Client.VMs(guestsContext)
		cancelGuests()
		if guestsError != nil {
			report.Checks = append(report.Checks,
				DoctorCheck{Name: "template_drift", Status: DoctorStatusFail, Required: true, Message: "could not inspect configured templates", Details: []string{guestsError.Error()}},
				DoctorCheck{Name: "guest_agent", Status: DoctorStatusWarn, Message: "guest-agent check skipped because guests could not be listed"},
			)
		} else {
			report.Checks = append(report.Checks,
				service.doctorTemplateCheck(ctx, guests),
				service.doctorGuestAgentCheck(ctx, guests),
			)
		}
	} else {
		report.Checks = append(report.Checks,
			DoctorCheck{Name: "storage_drift", Status: DoctorStatusWarn, Message: "storage check skipped because the Proxmox API is unavailable"},
			DoctorCheck{Name: "template_drift", Status: DoctorStatusWarn, Message: "template check skipped because the Proxmox API is unavailable"},
			DoctorCheck{Name: "guest_agent", Status: DoctorStatusWarn, Message: "guest-agent check skipped because the Proxmox API is unavailable"},
		)
	}

	report.Checks = append(report.Checks, service.doctorSSHCheck(ctx), service.doctorCaddyCheck(ctx))
	report.Status = doctorOverallStatus(report.Checks)

	return report
}

func (service *Service) doctorConfigCheck(configPath string) DoctorCheck {
	fileInfo, err := os.Stat(configPath)
	if err != nil {
		return DoctorCheck{Name: "config", Status: DoctorStatusFail, Required: true, Message: "config file is not readable", Details: []string{err.Error()}}
	}
	if !fileInfo.Mode().IsRegular() {
		return DoctorCheck{Name: "config", Status: DoctorStatusFail, Required: true, Message: "config path is not a regular file"}
	}

	return DoctorCheck{Name: "config", Status: DoctorStatusPass, Required: true, Message: "config loaded and validated"}
}

func (service *Service) doctorCredentialsCheck() DoctorCheck {
	check := DoctorCheck{Name: "credentials", Required: true}
	credentialsPath, err := defaultCredentialsPath()
	if err != nil {
		check.Status = DoctorStatusFail
		check.Message = "could not resolve the credentials file"
		check.Details = []string{err.Error()}
		return check
	}

	fileInfo, statError := os.Stat(credentialsPath)
	if statError == nil && fileInfo.Mode().Perm()&0o077 != 0 {
		check.Details = append(check.Details, fmt.Sprintf("%s has mode %04o; expected 0600 or stricter", credentialsPath, fileInfo.Mode().Perm()))
	}
	if statError != nil && !errors.Is(statError, os.ErrNotExist) {
		check.Details = append(check.Details, statError.Error())
	}
	if credentialsError := service.credentialsError(); credentialsError != nil {
		check.Details = append(check.Details, credentialsError.Error())
	}

	if len(check.Details) > 0 {
		check.Status = DoctorStatusFail
		check.Message = "Proxmox credentials are missing or insecure"
		return check
	}

	check.Status = DoctorStatusPass
	if statError == nil {
		check.Message = fmt.Sprintf("credentials are configured and %s is private", credentialsPath)
	} else {
		check.Message = "credentials are configured through environment variables"
	}
	return check
}

func (service *Service) doctorTLSCheck() DoctorCheck {
	endpoint, err := url.Parse(service.Config.Cluster.Endpoint)
	if err != nil || endpoint.Scheme == "" {
		return DoctorCheck{Name: "tls", Status: DoctorStatusFail, Required: true, Message: "Proxmox endpoint is not a valid URL"}
	}
	if endpoint.Scheme != "https" {
		return DoctorCheck{Name: "tls", Status: DoctorStatusFail, Required: true, Message: "Proxmox endpoint does not use HTTPS"}
	}
	if service.Config.Cluster.InsecureTLS {
		return DoctorCheck{Name: "tls", Status: DoctorStatusWarn, Message: "HTTPS is enabled, but certificate verification is disabled"}
	}
	if strings.TrimSpace(service.Config.Cluster.CAFile) != "" {
		caFile, expandError := service.Config.ExpandPath(service.Config.Cluster.CAFile)
		if expandError != nil {
			return DoctorCheck{Name: "tls", Status: DoctorStatusFail, Required: true, Message: "custom Proxmox CA file path is invalid", Details: []string{expandError.Error()}}
		}
		fileInfo, statError := os.Stat(caFile)
		if statError != nil || !fileInfo.Mode().IsRegular() {
			check := DoctorCheck{Name: "tls", Status: DoctorStatusFail, Required: true, Message: "custom Proxmox CA file is not readable"}
			if statError != nil {
				check.Details = []string{statError.Error()}
			}
			return check
		}
		certificatePEM, readError := os.ReadFile(caFile)
		if readError != nil {
			return DoctorCheck{Name: "tls", Status: DoctorStatusFail, Required: true, Message: "custom Proxmox CA file is not readable", Details: []string{readError.Error()}}
		}
		certificatePool := x509.NewCertPool()
		if !certificatePool.AppendCertsFromPEM(certificatePEM) {
			return DoctorCheck{Name: "tls", Status: DoctorStatusFail, Required: true, Message: "custom Proxmox CA file does not contain a valid PEM certificate"}
		}
		return DoctorCheck{Name: "tls", Status: DoctorStatusPass, Message: fmt.Sprintf("certificate verification uses custom CA %s", caFile)}
	}

	return DoctorCheck{Name: "tls", Status: DoctorStatusPass, Message: "HTTPS certificate verification is enabled"}
}

func doctorAPIAndNodeChecks(health ClusterHealth) []DoctorCheck {
	if !health.Connected {
		return []DoctorCheck{
			{Name: "proxmox_api", Status: DoctorStatusFail, Required: true, Message: "could not connect to the Proxmox API", Details: nonEmptyDetails(health.Error)},
			{Name: "nodes", Status: DoctorStatusWarn, Message: "node health check skipped because the Proxmox API is unavailable"},
		}
	}

	apiCheck := DoctorCheck{Name: "proxmox_api", Status: DoctorStatusPass, Required: true, Message: "connected to the Proxmox API"}
	configuredNodes := 0
	for _, node := range health.Nodes {
		if node.Configured {
			configuredNodes++
		}
	}
	nodeCheck := DoctorCheck{Name: "nodes", Status: DoctorStatusPass, Required: true, Message: fmt.Sprintf("%d configured nodes are online", configuredNodes)}
	for _, node := range health.Nodes {
		if !node.Configured {
			continue
		}
		if node.Status != "online" {
			nodeCheck.Status = DoctorStatusFail
			nodeCheck.Details = append(nodeCheck.Details, fmt.Sprintf("%s is %s", node.Name, node.Status))
		}
	}
	if len(nodeCheck.Details) > 0 {
		nodeCheck.Message = "one or more configured nodes are unavailable"
	}

	return []DoctorCheck{apiCheck, nodeCheck}
}

func (service *Service) doctorStorageCheck(health ClusterHealth) DoctorCheck {
	configuredCount := 0
	liveStorage := make(map[string]StorageHealth, len(health.Storages))
	for _, storage := range health.Storages {
		liveStorage[storage.Node+"/"+storage.Name] = storage
	}

	check := DoctorCheck{Name: "storage_drift", Status: DoctorStatusPass, Required: true}
	for _, storageError := range health.StorageErrors {
		check.Details = append(check.Details, "storage status unavailable: "+storageError)
	}
	for _, nodeName := range service.Config.NodeNames() {
		for _, storageName := range service.Config.Nodes[nodeName].Storages {
			configuredCount++
			storage, exists := liveStorage[nodeName+"/"+storageName]
			if !exists {
				check.Details = append(check.Details, fmt.Sprintf("%s/%s is configured but was not returned by Proxmox", nodeName, storageName))
				continue
			}
			if !storage.Enabled || !storage.Active {
				check.Details = append(check.Details, fmt.Sprintf("%s/%s is inactive or disabled", nodeName, storageName))
			}
		}
	}

	if configuredCount == 0 {
		check.Status = DoctorStatusWarn
		check.Required = false
		check.Message = "no per-node storages are configured"
	} else if len(check.Details) > 0 {
		check.Status = DoctorStatusFail
		check.Message = "configured storage does not match live Proxmox storage"
	} else {
		check.Message = fmt.Sprintf("%d configured node storage mappings are available", configuredCount)
	}
	return check
}

func (service *Service) doctorTemplateCheck(ctx context.Context, guests []proxmox.VMResource) DoctorCheck {
	check := DoctorCheck{Name: "template_drift", Status: DoctorStatusPass, Required: true}
	liveVMTemplates := make(map[string]bool)
	for _, guest := range guests {
		if guest.GuestType() == proxmox.GuestTypeQEMU && guest.Template == 1 {
			liveVMTemplates[fmt.Sprintf("%s/%d", guest.Node, guest.VMID)] = true
		}
	}

	templateCount := 0
	for _, imageName := range service.Config.ImageNames() {
		image := service.Config.Images[imageName]
		for nodeName, templateID := range image.Templates {
			templateCount++
			if !liveVMTemplates[fmt.Sprintf("%s/%d", nodeName, templateID)] {
				check.Details = append(check.Details, fmt.Sprintf("VM image %s expects template %d on %s", imageName, templateID, nodeName))
			}
		}
	}

	contentByStorage := map[string][]proxmox.StorageContent{}
	contentErrors := map[string]error{}
	for _, imageName := range service.Config.LXCImageNames() {
		image := service.Config.LXCImages[imageName]
		for nodeName, volumeID := range image.Templates {
			templateCount++
			storageName, _, hasStorage := strings.Cut(volumeID, ":")
			if !hasStorage {
				check.Details = append(check.Details, fmt.Sprintf("LXC image %s has invalid volume %q on %s", imageName, volumeID, nodeName))
				continue
			}

			storageKey := nodeName + "/" + storageName
			content, fetched := contentByStorage[storageKey]
			contentError, failed := contentErrors[storageKey]
			if !fetched && !failed {
				contentContext, cancelContent := context.WithTimeout(ctx, 10*time.Second)
				content, contentError = service.Client.StorageContent(contentContext, nodeName, storageName, proxmox.StorageContentFilter{Content: "vztmpl"})
				cancelContent()
				if contentError != nil {
					contentErrors[storageKey] = contentError
				} else {
					contentByStorage[storageKey] = content
				}
			}
			if contentError != nil {
				check.Details = append(check.Details, fmt.Sprintf("could not inspect %s for LXC image %s: %s", storageKey, imageName, contentError))
				continue
			}

			found := false
			for _, item := range content {
				if item.VolumeID == volumeID {
					found = true
					break
				}
			}
			if !found {
				check.Details = append(check.Details, fmt.Sprintf("LXC image %s expects %s on %s", imageName, volumeID, nodeName))
			}
		}
	}

	if len(check.Details) > 0 {
		check.Status = DoctorStatusFail
		check.Message = "configured templates do not match live Proxmox templates"
	} else {
		check.Message = fmt.Sprintf("%d configured template mappings are available", templateCount)
	}
	return check
}

func (service *Service) doctorGuestAgentCheck(ctx context.Context, guests []proxmox.VMResource) DoctorCheck {
	check := DoctorCheck{Name: "guest_agent", Status: DoctorStatusPass}
	var runningVMs []proxmox.VMResource
	for _, guest := range guests {
		if guest.GuestType() == proxmox.GuestTypeQEMU && guest.Template != 1 && guest.Status == "running" {
			runningVMs = append(runningVMs, guest)
		}
	}

	for _, guest := range runningVMs {
		agentContext, cancelAgent := context.WithTimeout(ctx, 5*time.Second)
		_, err := service.Client.AgentNetworkInterfaces(agentContext, guest.Node, guest.VMID)
		cancelAgent()
		if err != nil {
			check.Details = append(check.Details, fmt.Sprintf("%s (%d) on %s: %s", guest.Name, guest.VMID, guest.Node, err))
		}
	}

	if len(check.Details) > 0 {
		check.Status = DoctorStatusWarn
		check.Message = fmt.Sprintf("guest agent is unavailable on %d of %d running QEMU VMs", len(check.Details), len(runningVMs))
	} else {
		check.Message = fmt.Sprintf("guest agent responded on %d running QEMU VMs", len(runningVMs))
	}
	return check
}

func (service *Service) doctorSSHCheck(ctx context.Context) DoctorCheck {
	check := DoctorCheck{Name: "node_ssh", Status: DoctorStatusPass}
	configuredHosts := 0
	for _, nodeName := range service.Config.NodeNames() {
		sshHost := strings.TrimSpace(service.Config.Nodes[nodeName].SSHHost)
		if sshHost == "" {
			check.Details = append(check.Details, fmt.Sprintf("%s has no SSH host configured", nodeName))
			continue
		}
		configuredHosts++

		sshContext, cancelSSH := context.WithTimeout(ctx, 5*time.Second)
		sshArgs := append([]string{}, service.Config.Defaults.SSHOptions...)
		sshArgs = append(sshArgs,
			"-o", "BatchMode=yes",
			"-o", "ConnectionAttempts=1",
			"-o", "ConnectTimeout=4",
			sshHost,
			"true",
		)
		output, err := exec.CommandContext(sshContext, "ssh", sshArgs...).CombinedOutput()
		cancelSSH()
		if err != nil {
			detail := strings.TrimSpace(string(output))
			if detail == "" {
				detail = err.Error()
			}
			check.Details = append(check.Details, fmt.Sprintf("%s (%s): %s", nodeName, sshHost, firstLine(detail)))
		}
	}

	if len(check.Details) > 0 {
		check.Status = DoctorStatusWarn
		check.Message = "one or more nodes have missing or unreachable non-interactive SSH targets"
	} else {
		check.Message = fmt.Sprintf("%d configured node SSH hosts are reachable", configuredHosts)
	}
	return check
}

func (service *Service) doctorCaddyCheck(ctx context.Context) DoctorCheck {
	check := DoctorCheck{Name: "caddy_local", Status: DoctorStatusPass}
	if strings.TrimSpace(service.Config.Caddy.RepoPath) == "" {
		check.Status = DoctorStatusWarn
		check.Message = "Caddy integration is not configured"
		return check
	}

	repoPath, err := service.Config.CaddyRepoPath()
	if err != nil {
		check.Status = DoctorStatusFail
		check.Required = true
		check.Message = "Caddy repository path is invalid"
		check.Details = []string{err.Error()}
		return check
	}
	caddyFile := filepath.Join(repoPath, "caddy", "Caddyfile")
	if fileInfo, statError := os.Stat(caddyFile); statError != nil || !fileInfo.Mode().IsRegular() {
		check.Status = DoctorStatusFail
		check.Required = true
		check.Message = "configured Caddy tree does not contain caddy/Caddyfile"
		if statError != nil {
			check.Details = []string{statError.Error()}
		}
		return check
	}

	sites, err := service.ListCaddySites()
	if err != nil {
		check.Status = DoctorStatusFail
		check.Required = true
		check.Message = "local Caddy routes or manifest are invalid"
		check.Details = []string{err.Error()}
		return check
	}

	gitContext, cancelGit := context.WithTimeout(ctx, 5*time.Second)
	changes, err := service.CaddyGitChanges(gitContext)
	cancelGit()
	if err != nil {
		check.Status = DoctorStatusWarn
		check.Message = fmt.Sprintf("local Caddy tree is structurally readable, but Git state could not be inspected (%d routes found)", len(sites))
		check.Details = []string{firstLine(err.Error())}
		return check
	}
	if len(changes) > 0 {
		check.Status = DoctorStatusWarn
		check.Message = fmt.Sprintf("local Caddy tree is structurally readable with %d uncommitted changes", len(changes))
		check.Details = changes
		return check
	}

	check.Message = fmt.Sprintf("local Caddy tree is structurally readable, clean, and %d routes were parsed", len(sites))
	return check
}

func doctorOverallStatus(checks []DoctorCheck) string {
	status := DoctorStatusPass
	for _, check := range checks {
		if check.Required && check.Status == DoctorStatusFail {
			return DoctorStatusFail
		}
		if check.Status == DoctorStatusWarn || check.Status == DoctorStatusFail {
			status = DoctorStatusWarn
		}
	}
	return status
}

func nonEmptyDetails(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return []string{value}
}

func firstLine(value string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(value), "\n")
	return line
}
