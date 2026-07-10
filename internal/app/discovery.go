package app

import (
	"context"
	"sort"
	"strconv"
	"strings"

	"github.com/boring-dragon/boringctl/internal/config"
)

func (service *Service) DiscoverConfig(ctx context.Context) (*config.Config, error) {
	nodes, err := service.Client.Nodes(ctx)
	if err != nil {
		return nil, err
	}

	vms, err := service.Client.VMs(ctx)
	if err != nil {
		return nil, err
	}

	discoveredConfig := &config.Config{
		Cluster:  service.Config.Cluster,
		Auth:     service.Config.Auth,
		Defaults: service.Config.Defaults,
		Nodes:    map[string]config.NodeConfig{},
		Storages: map[string]config.StorageConfig{},
		Images:   map[string]config.ImageConfig{},
		Plans:    defaultPlans(),
		SSHKeys:  service.Config.SSHKeys,
	}

	if discoveredConfig.Defaults.Bridge == "" {
		discoveredConfig.Defaults.Bridge = "vmbr0"
	}
	if discoveredConfig.Defaults.CPUType == "" {
		discoveredConfig.Defaults.CPUType = "host"
	}
	if discoveredConfig.Defaults.Network == "" {
		discoveredConfig.Defaults.Network = "dhcp"
	}

	sort.Slice(nodes, func(leftIndex int, rightIndex int) bool {
		return nodes[leftIndex].Name < nodes[rightIndex].Name
	})

	for _, node := range nodes {
		storageNames, err := service.discoverNodeStorages(ctx, node.Name, discoveredConfig.Storages)
		if err != nil {
			return nil, err
		}

		discoveredConfig.Nodes[node.Name] = config.NodeConfig{
			Label:    titleFromKey(node.Name),
			Storages: storageNames,
		}
	}

	for _, vm := range vms {
		if vm.Template != 1 || !strings.HasPrefix(vm.Name, "tmpl-") {
			continue
		}

		imageKey := imageKeyFromTemplateName(vm.Name)
		if imageKey == "" {
			continue
		}

		image := discoveredConfig.Images[imageKey]
		if image.Templates == nil {
			image = config.ImageConfig{
				Label:       labelFromImageKey(imageKey),
				Family:      familyFromImageKey(imageKey),
				DefaultUser: defaultUserFromImageKey(imageKey),
				Recommended: imageKey == "ubuntu-24.04",
				Templates:   map[string]int{},
			}
		}

		image.Templates[vm.Node] = vm.VMID
		discoveredConfig.Images[imageKey] = image
	}

	return discoveredConfig, nil
}

func (service *Service) discoverNodeStorages(ctx context.Context, nodeName string, storageCatalog map[string]config.StorageConfig) ([]string, error) {
	storages, err := service.Client.NodeStorages(ctx, nodeName)
	if err != nil {
		return nil, err
	}

	var storageNames []string
	for _, storage := range storages {
		if storage.Enabled == 0 || storage.Active == 0 {
			continue
		}

		if storage.Type == "dir" && (strings.Contains(storage.Name, "iso") || strings.Contains(storage.Name, "template")) {
			continue
		}

		storageCatalog[storage.Name] = config.StorageConfig{Label: titleFromKey(storage.Name)}
		storageNames = append(storageNames, storage.Name)
	}

	sort.Strings(storageNames)

	return storageNames, nil
}

func defaultPlans() map[string]config.PlanConfig {
	return map[string]config.PlanConfig{
		"tiny":   {Label: "Tiny", Cores: 1, MemoryMB: 1024, DiskGB: 20},
		"small":  {Label: "Small", Cores: 2, MemoryMB: 4096, DiskGB: 40},
		"medium": {Label: "Medium", Cores: 4, MemoryMB: 8192, DiskGB: 80},
		"large":  {Label: "Large", Cores: 8, MemoryMB: 16384, DiskGB: 160},
	}
}

func imageKeyFromTemplateName(templateName string) string {
	imageKey := strings.TrimPrefix(templateName, "tmpl-")
	imageKey = strings.TrimSuffix(imageKey, "-x64")

	parts := strings.Split(imageKey, "-")
	if len(parts) >= 3 && isNumeric(parts[len(parts)-1]) && isNumeric(parts[len(parts)-2]) {
		lastIndex := len(parts) - 1
		parts[lastIndex-1] = parts[lastIndex-1] + "." + parts[lastIndex]
		parts = parts[:lastIndex]
	}

	return strings.Join(parts, "-")
}

func labelFromImageKey(imageKey string) string {
	parts := strings.Split(imageKey, "-")
	if len(parts) == 0 {
		return imageKey
	}

	switch parts[0] {
	case "ubuntu":
		return "Ubuntu " + strings.TrimPrefix(imageKey, "ubuntu-") + " LTS"
	case "debian":
		return "Debian " + strings.TrimPrefix(imageKey, "debian-")
	case "fedora":
		return "Fedora " + strings.TrimPrefix(imageKey, "fedora-")
	case "almalinux":
		return "AlmaLinux " + strings.TrimPrefix(imageKey, "almalinux-")
	case "rocky":
		return "Rocky Linux " + strings.TrimPrefix(imageKey, "rocky-")
	case "opensuse":
		return "openSUSE " + strings.TrimPrefix(imageKey, "opensuse-")
	default:
		return titleFromKey(imageKey)
	}
}

func familyFromImageKey(imageKey string) string {
	switch {
	case strings.HasPrefix(imageKey, "ubuntu-"):
		return "Ubuntu"
	case strings.HasPrefix(imageKey, "debian-"):
		return "Debian"
	case strings.HasPrefix(imageKey, "fedora-"):
		return "Fedora"
	case strings.HasPrefix(imageKey, "almalinux-"):
		return "AlmaLinux"
	case strings.HasPrefix(imageKey, "rocky-"):
		return "Rocky Linux"
	case strings.HasPrefix(imageKey, "opensuse-"):
		return "openSUSE"
	default:
		return "Linux"
	}
}

func defaultUserFromImageKey(imageKey string) string {
	switch {
	case strings.HasPrefix(imageKey, "ubuntu-"):
		return "ubuntu"
	case strings.HasPrefix(imageKey, "debian-"):
		return "debian"
	case strings.HasPrefix(imageKey, "fedora-"):
		return "fedora"
	case strings.HasPrefix(imageKey, "almalinux-"):
		return "almalinux"
	case strings.HasPrefix(imageKey, "rocky-"):
		return "rocky"
	case strings.HasPrefix(imageKey, "opensuse-"):
		return "opensuse"
	default:
		return "root"
	}
}

func titleFromKey(key string) string {
	parts := strings.FieldsFunc(key, func(r rune) bool {
		return r == '-' || r == '_' || r == '.'
	})

	for index, part := range parts {
		if part == "" {
			continue
		}
		parts[index] = strings.ToUpper(part[:1]) + part[1:]
	}

	return strings.Join(parts, " ")
}

func isNumeric(value string) bool {
	_, err := strconv.Atoi(value)
	return err == nil
}
