package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"
)

type SSHConfigResult struct {
	Alias         string
	IPAddress     string
	User          string
	Command       string
	ConfigPath    string
	Added         bool
	AlreadyExists bool
}

func (service *Service) SSHConfigPath() (string, error) {
	if customPath := os.Getenv("BORINGCTL_SSH_CONFIG"); customPath != "" {
		return service.Config.ExpandPath(customPath)
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}

	return filepath.Join(homeDir, ".ssh", "config"), nil
}

func (service *Service) EnsureSSHConfig(ctx context.Context, vmid int, alias string, user string, dryRun bool) (SSHConfigResult, error) {
	vm, vmConfig, err := service.ShowVM(ctx, vmid)
	if err != nil {
		return SSHConfigResult{}, err
	}
	if vm.IsContainer() {
		return SSHConfigResult{}, fmt.Errorf("SSH config is only supported for QEMU VMs; container %d does not use the QEMU guest agent", vmid)
	}

	sshUser := strings.TrimSpace(user)
	if sshUser == "" {
		sshUser = stringValue(vmConfig["ciuser"])
	}
	if sshUser == "" {
		sshUser = "ubuntu"
	}

	ipAddress, err := service.Client.FirstRoutableIP(ctx, vm.Node, vmid)
	if err != nil {
		return SSHConfigResult{}, err
	}
	if ipAddress == "" {
		return SSHConfigResult{}, fmt.Errorf("no routable guest agent IP found for VM %d", vmid)
	}

	resolvedAlias := normalizeSSHAlias(alias, vm.Name, vmid)
	configPath, err := service.SSHConfigPath()
	if err != nil {
		return SSHConfigResult{}, err
	}

	entry, exists, err := readSSHAlias(configPath, resolvedAlias)
	if err != nil {
		return SSHConfigResult{}, err
	}

	result := SSHConfigResult{
		Alias:      resolvedAlias,
		IPAddress:  ipAddress,
		User:       sshUser,
		Command:    fmt.Sprintf("ssh %s", resolvedAlias),
		ConfigPath: configPath,
	}

	if exists {
		if strings.EqualFold(strings.TrimSpace(entry.HostName), ipAddress) &&
			(strings.EqualFold(strings.TrimSpace(entry.User), sshUser) || entry.User == "") {
			result.AlreadyExists = true
			return result, nil
		}

		if strings.TrimSpace(entry.User) == "" {
			return result, fmt.Errorf("ssh alias %q already exists in %s", resolvedAlias, configPath)
		}

		return result, fmt.Errorf("ssh alias %q already exists for %q in %s; use --alias to select a different alias", resolvedAlias, entry.HostName, configPath)
	}

	if dryRun {
		return result, nil
	}

	if err := writeSSHAlias(configPath, resolvedAlias, ipAddress, sshUser); err != nil {
		return SSHConfigResult{}, err
	}

	result.Added = true
	return result, nil
}

type sshConfigEntry struct {
	HostName string
	User     string
}

func readSSHAlias(configPath string, alias string) (sshConfigEntry, bool, error) {
	fileContents, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return sshConfigEntry{}, false, nil
		}
		return sshConfigEntry{}, false, err
	}

	targetAlias := strings.TrimSpace(strings.ToLower(alias))
	if targetAlias == "" {
		return sshConfigEntry{}, false, nil
	}

	lines := strings.Split(string(fileContents), "\n")
	inAlias := false
	found := false

	var foundEntry sshConfigEntry
	for _, rawLine := range lines {
		line := strings.TrimSpace(stripSSHComment(rawLine))
		if line == "" {
			continue
		}

		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}

		key := strings.ToLower(fields[0])
		if key == "host" {
			inAlias = aliasMatches(fields[1:], targetAlias)
			if inAlias {
				found = true
			}
			continue
		}

		if !inAlias {
			continue
		}
		if len(fields) < 2 {
			continue
		}

		switch key {
		case "hostname":
			foundEntry.HostName = strings.TrimSpace(strings.Join(fields[1:], " "))
		case "user":
			foundEntry.User = strings.TrimSpace(fields[1])
		}
	}

	return foundEntry, found, nil
}

func writeSSHAlias(configPath string, alias string, ipAddress string, user string) error {
	existing := ""
	if existingBytes, err := os.ReadFile(configPath); err == nil {
		existing = string(existingBytes)
	} else if !os.IsNotExist(err) {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(configPath), 0o700); err != nil {
		return err
	}

	var builder strings.Builder
	builder.WriteString(existing)
	if existing != "" && !strings.HasSuffix(existing, "\n") {
		builder.WriteString("\n")
	}
	if existing != "" {
		builder.WriteString("\n")
	}

	builder.WriteString("# Added by boringctl\n")
	builder.WriteString(fmt.Sprintf("Host %s\n", alias))
	builder.WriteString(fmt.Sprintf("  HostName %s\n", ipAddress))
	builder.WriteString(fmt.Sprintf("  User %s\n", user))
	builder.WriteString("  IdentityAgent none\n")

	if err := os.WriteFile(configPath, []byte(builder.String()), 0o600); err != nil {
		return err
	}

	return nil
}

func normalizeSSHAlias(alias string, fallbackName string, fallbackID int) string {
	alias = strings.TrimSpace(alias)
	if alias != "" {
		alias = strings.ToLower(alias)
	} else {
		alias = fallbackName
	}
	alias = strings.TrimSpace(alias)
	if alias == "" {
		alias = "vm-" + strconv.Itoa(fallbackID)
	}

	var normalized strings.Builder
	for _, char := range alias {
		switch {
		case unicode.IsLetter(char) || unicode.IsDigit(char):
			normalized.WriteRune(char)
		case char == '-' || char == '_' || char == '.':
			normalized.WriteRune(char)
		case unicode.IsSpace(char):
			normalized.WriteRune('-')
		default:
			normalized.WriteRune('-')
		}
	}

	cleaned := strings.Trim(strings.Trim(normalized.String(), "-_."), " ")
	for strings.Contains(cleaned, "--") {
		cleaned = strings.ReplaceAll(cleaned, "--", "-")
	}
	for strings.Contains(cleaned, "__") {
		cleaned = strings.ReplaceAll(cleaned, "__", "_")
	}
	if cleaned == "" {
		cleaned = "vm-" + strconv.Itoa(fallbackID)
	}

	return cleaned
}

func aliasMatches(patterns []string, alias string) bool {
	if alias == "" {
		return false
	}

	for _, pattern := range patterns {
		if strings.EqualFold(strings.TrimSpace(pattern), alias) {
			return true
		}
	}

	return false
}

func stripSSHComment(line string) string {
	commentIndex := strings.Index(line, "#")
	if commentIndex == -1 {
		return strings.TrimSpace(line)
	}

	return strings.TrimSpace(line[:commentIndex])
}

func isSSHAliasEmpty(entry sshConfigEntry) bool {
	return strings.TrimSpace(entry.HostName) != "" || strings.TrimSpace(entry.User) != ""
}
