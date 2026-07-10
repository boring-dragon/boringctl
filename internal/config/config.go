package config

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

const DefaultRelativeConfigPath = ".config/boringctl/config.yaml"

const profilesRelativeDir = ".config/boringctl"

type Config struct {
	Cluster   ClusterConfig             `yaml:"cluster"`
	Auth      AuthConfig                `yaml:"auth"`
	Defaults  DefaultsConfig            `yaml:"defaults"`
	Caddy     CaddyConfig               `yaml:"caddy"`
	Nodes     map[string]NodeConfig     `yaml:"nodes"`
	Storages  map[string]StorageConfig  `yaml:"storages"`
	Images    map[string]ImageConfig    `yaml:"images"`
	LXCImages map[string]LXCImageConfig `yaml:"lxc_images"`
	Plans     map[string]PlanConfig     `yaml:"plans"`
	SSHKeys   map[string]SSHKeyConfig   `yaml:"ssh_keys"`
}

type ClusterConfig struct {
	Endpoint    string `yaml:"endpoint"`
	InsecureTLS bool   `yaml:"insecure_tls"`
	CAFile      string `yaml:"ca_file,omitempty"`
}

type AuthConfig struct {
	TokenIDEnv     string `yaml:"token_id_env"`
	TokenSecretEnv string `yaml:"token_secret_env"`
}

type DefaultsConfig struct {
	Bridge             string   `yaml:"bridge"`
	CPUType            string   `yaml:"cpu_type"`
	FullClone          bool     `yaml:"full_clone"`
	SSHKey             string   `yaml:"ssh_key"`
	SSHOptions         []string `yaml:"ssh_options,omitempty"`
	Network            string   `yaml:"network"`
	DHCPToStatic       bool     `yaml:"dhcp_to_static"`
	StaticGateway      string   `yaml:"static_gateway"`
	StaticDNS          string   `yaml:"static_dns"`
	StaticPrefixLength int      `yaml:"static_prefix_length"`
}

type CaddyConfig struct {
	RepoPath           string   `yaml:"repo_path"`
	ProxmoxSSHHost     string   `yaml:"proxmox_ssh_host"`
	ContainerID        int      `yaml:"container_id"`
	SSHOptions         []string `yaml:"ssh_options"`
	RemoteArchivePath  string   `yaml:"remote_archive_path"`
	DefaultDomain      string   `yaml:"default_domain"`
	CommonProxySnippet string   `yaml:"common_proxy_snippet,omitempty"`
	InternalACLSnippet string   `yaml:"internal_acl_snippet,omitempty"`
	PublicWAFByDefault bool     `yaml:"public_waf_by_default,omitempty"`
}

type NodeConfig struct {
	Label    string   `yaml:"label"`
	Storages []string `yaml:"storages"`
	SSHHost  string   `yaml:"ssh_host"`
}

type StorageConfig struct {
	Label string `yaml:"label"`
}

type ImageConfig struct {
	Label       string         `yaml:"label"`
	Family      string         `yaml:"family"`
	DefaultUser string         `yaml:"default_user"`
	Recommended bool           `yaml:"recommended"`
	Templates   map[string]int `yaml:"templates"`
}

type LXCImageConfig struct {
	Label       string            `yaml:"label"`
	Family      string            `yaml:"family"`
	OSType      string            `yaml:"ostype"`
	DefaultUser string            `yaml:"default_user"`
	Recommended bool              `yaml:"recommended"`
	Templates   map[string]string `yaml:"templates"`
}

type PlanConfig struct {
	Label    string `yaml:"label"`
	Cores    int    `yaml:"cores"`
	MemoryMB int    `yaml:"memory_mb"`
	DiskGB   int    `yaml:"disk_gb"`
}

type SSHKeyConfig struct {
	Path string `yaml:"path"`
}

func ResolvePath(flagPath string) (string, error) {
	return ResolvePathForProfile(flagPath, "")
}

func ResolvePathForProfile(flagPath string, profile string) (string, error) {
	if flagPath != "" {
		return expandHome(flagPath)
	}

	if envPath := os.Getenv("BORINGCTL_CONFIG"); envPath != "" {
		return expandHome(envPath)
	}

	if profile == "" {
		profile = os.Getenv("BORINGCTL_PROFILE")
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}

	if profile != "" {
		profilePath, err := profileConfigPath(homeDir, profile)
		if err != nil {
			return "", err
		}
		return profilePath, nil
	}

	return filepath.Join(homeDir, DefaultRelativeConfigPath), nil
}

func Load(flagPath string) (*Config, string, error) {
	return LoadProfile(flagPath, "")
}

func LoadProfile(flagPath string, profile string) (*Config, string, error) {
	configPath, err := ResolvePathForProfile(flagPath, profile)
	if err != nil {
		return nil, "", err
	}

	fileContents, err := os.ReadFile(configPath)
	if err != nil {
		return nil, configPath, err
	}

	var loadedConfig Config
	if err := yaml.Unmarshal(fileContents, &loadedConfig); err != nil {
		return nil, configPath, err
	}

	loadedConfig.applyDefaults()

	if err := loadedConfig.Validate(); err != nil {
		return nil, configPath, err
	}

	return &loadedConfig, configPath, nil
}

func profileConfigPath(homeDir string, profile string) (string, error) {
	profile = strings.TrimSpace(profile)
	if profile == "" {
		return "", errors.New("profile is required")
	}

	if strings.Contains(profile, "/") || strings.Contains(profile, "\\") || profile == "." || profile == ".." {
		return "", fmt.Errorf("invalid profile %q", profile)
	}

	if filepath.Ext(profile) == "" {
		profile += ".yaml"
	}

	return filepath.Join(homeDir, profilesRelativeDir, profile), nil
}

func (loadedConfig *Config) Validate() error {
	var validationErrors []error

	if loadedConfig.Cluster.Endpoint == "" {
		validationErrors = append(validationErrors, errors.New("cluster.endpoint is required"))
	} else if endpoint, err := url.Parse(loadedConfig.Cluster.Endpoint); err != nil || endpoint.Scheme != "https" || endpoint.Host == "" || endpoint.User != nil || (endpoint.Path != "" && endpoint.Path != "/") || endpoint.RawQuery != "" || endpoint.Fragment != "" {
		validationErrors = append(validationErrors, errors.New("cluster.endpoint must be an https origin without credentials, path, query, or fragment"))
	}

	if loadedConfig.Cluster.InsecureTLS && loadedConfig.Cluster.CAFile != "" {
		validationErrors = append(validationErrors, errors.New("cluster.insecure_tls and cluster.ca_file cannot be used together"))
	}

	if loadedConfig.Auth.TokenIDEnv == "" {
		validationErrors = append(validationErrors, errors.New("auth.token_id_env is required"))
	}

	if loadedConfig.Auth.TokenSecretEnv == "" {
		validationErrors = append(validationErrors, errors.New("auth.token_secret_env is required"))
	}

	if len(loadedConfig.Nodes) == 0 {
		validationErrors = append(validationErrors, errors.New("at least one node is required"))
	}

	if len(loadedConfig.Images) == 0 {
		validationErrors = append(validationErrors, errors.New("at least one image is required"))
	}

	if len(loadedConfig.Plans) == 0 {
		validationErrors = append(validationErrors, errors.New("at least one plan is required"))
	}

	for imageName, image := range loadedConfig.Images {
		if image.DefaultUser == "" {
			validationErrors = append(validationErrors, fmt.Errorf("images.%s.default_user is required", imageName))
		}

		if len(image.Templates) == 0 {
			validationErrors = append(validationErrors, fmt.Errorf("images.%s.templates must define at least one node template", imageName))
		}

		for nodeName, templateID := range image.Templates {
			if _, exists := loadedConfig.Nodes[nodeName]; !exists {
				validationErrors = append(validationErrors, fmt.Errorf("images.%s.templates.%s references unknown node", imageName, nodeName))
			}
			if templateID <= 0 {
				validationErrors = append(validationErrors, fmt.Errorf("images.%s.templates.%s must be a positive VMID", imageName, nodeName))
			}
		}
	}

	for imageName, image := range loadedConfig.LXCImages {
		if image.DefaultUser == "" {
			validationErrors = append(validationErrors, fmt.Errorf("lxc_images.%s.default_user is required", imageName))
		}
		if len(image.Templates) == 0 {
			validationErrors = append(validationErrors, fmt.Errorf("lxc_images.%s.templates must define at least one node template", imageName))
		}
		for nodeName, templateVolume := range image.Templates {
			if _, exists := loadedConfig.Nodes[nodeName]; !exists {
				validationErrors = append(validationErrors, fmt.Errorf("lxc_images.%s.templates.%s references unknown node", imageName, nodeName))
			}
			if strings.TrimSpace(templateVolume) == "" {
				validationErrors = append(validationErrors, fmt.Errorf("lxc_images.%s.templates.%s is required", imageName, nodeName))
			}
		}
	}

	if loadedConfig.caddyConfigured() {
		if strings.TrimSpace(loadedConfig.Caddy.RepoPath) == "" {
			validationErrors = append(validationErrors, errors.New("caddy.repo_path is required when Caddy integration is configured"))
		}
		if strings.TrimSpace(loadedConfig.Caddy.ProxmoxSSHHost) == "" {
			validationErrors = append(validationErrors, errors.New("caddy.proxmox_ssh_host is required when Caddy integration is configured"))
		}
		if loadedConfig.Caddy.ContainerID <= 0 {
			validationErrors = append(validationErrors, errors.New("caddy.container_id must be positive when Caddy integration is configured"))
		}
	}

	return errors.Join(validationErrors...)
}

func (loadedConfig *Config) NodeNames() []string {
	return sortedKeys(loadedConfig.Nodes)
}

func (loadedConfig *Config) StorageNames() []string {
	return sortedKeys(loadedConfig.Storages)
}

func (loadedConfig *Config) ImageNames() []string {
	return sortedKeys(loadedConfig.Images)
}

func (loadedConfig *Config) LXCImageNames() []string {
	return sortedKeys(loadedConfig.LXCImages)
}

func (loadedConfig *Config) PlanNames() []string {
	return sortedKeys(loadedConfig.Plans)
}

func (loadedConfig *Config) SSHKeyNames() []string {
	return sortedKeys(loadedConfig.SSHKeys)
}

func (loadedConfig *Config) ExpandPath(path string) (string, error) {
	return expandHome(path)
}

func (loadedConfig *Config) CaddyRepoPath() (string, error) {
	if strings.TrimSpace(loadedConfig.Caddy.RepoPath) == "" {
		return "", errors.New("caddy.repo_path is required; configure Caddy integration before using caddy commands")
	}
	return expandHome(loadedConfig.Caddy.RepoPath)
}

func (loadedConfig *Config) applyDefaults() {
	if loadedConfig.Auth.TokenIDEnv == "" {
		loadedConfig.Auth.TokenIDEnv = "PVE_TOKEN_ID"
	}

	if loadedConfig.Auth.TokenSecretEnv == "" {
		loadedConfig.Auth.TokenSecretEnv = "PVE_TOKEN_SECRET"
	}

	if loadedConfig.Defaults.Bridge == "" {
		loadedConfig.Defaults.Bridge = "vmbr0"
	}

	if loadedConfig.Defaults.CPUType == "" {
		loadedConfig.Defaults.CPUType = "host"
	}

	if loadedConfig.Defaults.Network == "" {
		loadedConfig.Defaults.Network = "dhcp"
	}
	if len(loadedConfig.Defaults.SSHOptions) == 0 {
		loadedConfig.Defaults.SSHOptions = []string{"-o", "BatchMode=yes", "-o", "StrictHostKeyChecking=yes"}
	}
	if loadedConfig.Defaults.StaticPrefixLength == 0 {
		loadedConfig.Defaults.StaticPrefixLength = 24
	}

	if len(loadedConfig.Caddy.SSHOptions) == 0 {
		loadedConfig.Caddy.SSHOptions = []string{"-o", "BatchMode=yes", "-o", "IdentityAgent=none", "-o", "StrictHostKeyChecking=yes"}
	}
	if loadedConfig.Caddy.RemoteArchivePath == "" {
		loadedConfig.Caddy.RemoteArchivePath = "/root/boringctl-caddy.tgz"
	}
}

func (loadedConfig *Config) caddyConfigured() bool {
	return strings.TrimSpace(loadedConfig.Caddy.RepoPath) != "" ||
		strings.TrimSpace(loadedConfig.Caddy.ProxmoxSSHHost) != "" ||
		loadedConfig.Caddy.ContainerID != 0 ||
		strings.TrimSpace(loadedConfig.Caddy.DefaultDomain) != "" ||
		strings.TrimSpace(loadedConfig.Caddy.CommonProxySnippet) != "" ||
		strings.TrimSpace(loadedConfig.Caddy.InternalACLSnippet) != "" ||
		loadedConfig.Caddy.PublicWAFByDefault
}

func sortedKeys[T any](items map[string]T) []string {
	keys := make([]string, 0, len(items))
	for key := range items {
		keys = append(keys, key)
	}

	sort.Strings(keys)

	return keys
}

func expandHome(path string) (string, error) {
	if path == "" || path[0] != '~' {
		return path, nil
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}

	if path == "~" {
		return homeDir, nil
	}

	if len(path) > 1 && os.IsPathSeparator(path[1]) {
		return filepath.Join(homeDir, path[2:]), nil
	}

	return "", fmt.Errorf("unsupported home path %q", path)
}
