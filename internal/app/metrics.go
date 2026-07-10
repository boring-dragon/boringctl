package app

import (
	"context"
	"errors"

	"github.com/boring-labs/boringctl/internal/proxmox"
)

type metricsAPI interface {
	NodeRRDData(ctx context.Context, node string) ([]proxmox.RRDDataPoint, error)
	GuestRRDData(ctx context.Context, node string, guestType string, vmid int) ([]proxmox.RRDDataPoint, error)
}

func (service *Service) NodeMetrics(ctx context.Context, node string) ([]proxmox.RRDDataPoint, error) {
	client, ok := service.Client.(metricsAPI)
	if !ok {
		return nil, errors.New("Proxmox metrics are not supported by this client")
	}

	return client.NodeRRDData(ctx, node)
}

func (service *Service) GuestMetrics(ctx context.Context, guest proxmox.VMResource) ([]proxmox.RRDDataPoint, error) {
	client, ok := service.Client.(metricsAPI)
	if !ok {
		return nil, errors.New("Proxmox metrics are not supported by this client")
	}

	return client.GuestRRDData(ctx, guest.Node, guest.GuestType(), guest.VMID)
}
