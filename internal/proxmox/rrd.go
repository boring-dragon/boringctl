package proxmox

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"sort"
)

const maxRecentRRDPoints = 120

// RRDDataPoint is a normalized Proxmox metric sample. Usage values are ratios
// between 0 and 1; network values are bytes per second.
type RRDDataPoint struct {
	Timestamp                int64   `json:"timestamp"`
	CPUUsage                 float64 `json:"cpu_usage"`
	MemoryUsage              float64 `json:"memory_usage"`
	DiskUsage                float64 `json:"disk_usage"`
	DiskReadBytesPerSecond   float64 `json:"disk_read_bytes_per_second"`
	DiskWriteBytesPerSecond  float64 `json:"disk_write_bytes_per_second"`
	NetworkInBytesPerSecond  float64 `json:"network_in_bytes_per_second"`
	NetworkOutBytesPerSecond float64 `json:"network_out_bytes_per_second"`
}

type rawRRDDataPoint struct {
	Timestamp   int64   `json:"time"`
	CPU         float64 `json:"cpu"`
	Memory      float64 `json:"mem"`
	MaxMemory   float64 `json:"maxmem"`
	MemoryUsed  float64 `json:"memused"`
	MemoryTotal float64 `json:"memtotal"`
	Disk        float64 `json:"disk"`
	MaxDisk     float64 `json:"maxdisk"`
	DiskRead    float64 `json:"diskread"`
	DiskWrite   float64 `json:"diskwrite"`
	RootUsed    float64 `json:"rootused"`
	RootTotal   float64 `json:"roottotal"`
	NetworkIn   float64 `json:"netin"`
	NetworkOut  float64 `json:"netout"`
}

func (client *Client) NodeRRDData(ctx context.Context, node string) ([]RRDDataPoint, error) {
	path := fmt.Sprintf("/nodes/%s/rrddata", url.PathEscape(node))
	return client.recentRRDData(ctx, path)
}

func (client *Client) GuestRRDData(ctx context.Context, node string, guestType string, vmid int) ([]RRDDataPoint, error) {
	return client.recentRRDData(ctx, guestPath(node, guestType, vmid, "rrddata"))
}

func (client *Client) recentRRDData(ctx context.Context, path string) ([]RRDDataPoint, error) {
	var rawPoints []rawRRDDataPoint
	values := url.Values{
		"timeframe": {"hour"},
		"cf":        {"AVERAGE"},
	}
	if err := client.requestJSON(ctx, http.MethodGet, path, values, &rawPoints); err != nil {
		return nil, err
	}

	sort.Slice(rawPoints, func(leftIndex int, rightIndex int) bool {
		return rawPoints[leftIndex].Timestamp < rawPoints[rightIndex].Timestamp
	})
	if len(rawPoints) > maxRecentRRDPoints {
		rawPoints = rawPoints[len(rawPoints)-maxRecentRRDPoints:]
	}

	points := make([]RRDDataPoint, 0, len(rawPoints))
	for _, rawPoint := range rawPoints {
		if rawPoint.Timestamp == 0 {
			continue
		}

		diskUsed := rawPoint.Disk
		diskTotal := rawPoint.MaxDisk
		if rawPoint.RootTotal > 0 {
			diskUsed = rawPoint.RootUsed
			diskTotal = rawPoint.RootTotal
		}
		memoryUsed := rawPoint.Memory
		memoryTotal := rawPoint.MaxMemory
		if rawPoint.MemoryTotal > 0 {
			memoryUsed = rawPoint.MemoryUsed
			memoryTotal = rawPoint.MemoryTotal
		}

		points = append(points, RRDDataPoint{
			Timestamp:                rawPoint.Timestamp,
			CPUUsage:                 boundedRatio(rawPoint.CPU),
			MemoryUsage:              usageRatio(memoryUsed, memoryTotal),
			DiskUsage:                usageRatio(diskUsed, diskTotal),
			DiskReadBytesPerSecond:   nonNegative(rawPoint.DiskRead),
			DiskWriteBytesPerSecond:  nonNegative(rawPoint.DiskWrite),
			NetworkInBytesPerSecond:  nonNegative(rawPoint.NetworkIn),
			NetworkOutBytesPerSecond: nonNegative(rawPoint.NetworkOut),
		})
	}

	return points, nil
}

func usageRatio(used float64, total float64) float64 {
	if total <= 0 {
		return 0
	}

	return boundedRatio(used / total)
}

func boundedRatio(value float64) float64 {
	if value < 0 {
		return 0
	}
	if value > 1 {
		return 1
	}

	return value
}

func nonNegative(value float64) float64 {
	if value < 0 {
		return 0
	}

	return value
}
