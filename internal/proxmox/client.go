package proxmox

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

type Client struct {
	baseURL     string
	tokenID     string
	tokenSecret string
	httpClient  *http.Client
}

type Config struct {
	Endpoint    string
	InsecureTLS bool
	CAFile      string
	TokenID     string
	TokenSecret string
}

type Node struct {
	Name   string  `json:"node"`
	Status string  `json:"status"`
	CPU    float64 `json:"cpu"`
	MaxCPU int     `json:"maxcpu"`
	MaxMem int64   `json:"maxmem"`
	Mem    int64   `json:"mem"`
}

type Storage struct {
	Name string `json:"storage"`
	Type string `json:"type"`
	Node string `json:"node"`
}

type StorageStatus struct {
	Name      string `json:"storage"`
	Type      string `json:"type"`
	Active    int    `json:"active"`
	Enabled   int    `json:"enabled"`
	Shared    int    `json:"shared"`
	Total     int64  `json:"total"`
	Used      int64  `json:"used"`
	Available int64  `json:"avail"`
}

const (
	GuestTypeQEMU = "qemu"
	GuestTypeLXC  = "lxc"
)

type VMResource struct {
	VMID     int     `json:"vmid"`
	Name     string  `json:"name"`
	Node     string  `json:"node"`
	Type     string  `json:"type"`
	Status   string  `json:"status"`
	MaxMem   int64   `json:"maxmem"`
	Mem      int64   `json:"mem"`
	MaxDisk  int64   `json:"maxdisk"`
	Disk     int64   `json:"disk"`
	CPU      float64 `json:"cpu"`
	Uptime   int64   `json:"uptime"`
	Template int     `json:"template"`
	Tags     string  `json:"tags"`
}

func (resource VMResource) GuestType() string {
	if resource.Type == "" {
		return GuestTypeQEMU
	}
	return resource.Type
}

func (resource VMResource) IsContainer() bool {
	return resource.GuestType() == GuestTypeLXC
}

type TaskStatus struct {
	UPID       string `json:"upid"`
	Status     string `json:"status"`
	ExitStatus string `json:"exitstatus"`
	Type       string `json:"type"`
}

type Snapshot struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	SnapTime    int64  `json:"snaptime"`
}

type NetworkInterface struct {
	Name        string      `json:"name"`
	IPAddresses []IPAddress `json:"ip-addresses"`
}

type ContainerInterface struct {
	Name      string `json:"name"`
	HWAddress string `json:"hwaddr"`
	IPv4      string `json:"inet"`
	IPv6      string `json:"inet6"`
}

type IPAddress struct {
	Address string `json:"ip-address"`
	Type    string `json:"ip-address-type"`
	Prefix  int    `json:"prefix"`
}

type apiResponse struct {
	Data json.RawMessage `json:"data"`
}

type apiErrorResponse struct {
	Data   json.RawMessage   `json:"data"`
	Errors map[string]string `json:"errors"`
}

func NewClient(config Config) (*Client, error) {
	if config.Endpoint == "" {
		return nil, errors.New("proxmox endpoint is required")
	}

	transport := http.DefaultTransport.(*http.Transport).Clone()
	tlsConfig := &tls.Config{
		MinVersion:         tls.VersionTLS12,
		InsecureSkipVerify: config.InsecureTLS,
	}
	if config.CAFile != "" {
		caCertificate, err := os.ReadFile(config.CAFile)
		if err != nil {
			return nil, fmt.Errorf("read Proxmox CA file: %w", err)
		}
		rootCAs, err := x509.SystemCertPool()
		if err != nil {
			return nil, fmt.Errorf("load system certificate pool: %w", err)
		}
		if !rootCAs.AppendCertsFromPEM(caCertificate) {
			return nil, errors.New("Proxmox CA file does not contain a valid PEM certificate")
		}
		tlsConfig.RootCAs = rootCAs
	}
	transport.TLSClientConfig = tlsConfig

	return &Client{
		baseURL:     strings.TrimRight(config.Endpoint, "/") + "/api2/json",
		tokenID:     config.TokenID,
		tokenSecret: config.TokenSecret,
		httpClient: &http.Client{
			Timeout:   60 * time.Second,
			Transport: transport,
		},
	}, nil
}

func (client *Client) Nodes(ctx context.Context) ([]Node, error) {
	var nodes []Node
	err := client.requestJSON(ctx, http.MethodGet, "/nodes", nil, &nodes)
	return nodes, err
}

func (client *Client) Storages(ctx context.Context) ([]Storage, error) {
	var storages []Storage
	err := client.requestJSON(ctx, http.MethodGet, "/storage", nil, &storages)
	return storages, err
}

func (client *Client) NodeStorages(ctx context.Context, node string) ([]StorageStatus, error) {
	var storages []StorageStatus
	err := client.requestJSON(ctx, http.MethodGet, fmt.Sprintf("/nodes/%s/storage", url.PathEscape(node)), nil, &storages)
	return storages, err
}

func (client *Client) NextID(ctx context.Context) (int, error) {
	var nextID any
	err := client.requestJSON(ctx, http.MethodGet, "/cluster/nextid", nil, &nextID)
	if err != nil {
		return 0, err
	}

	return parseIntResponse(nextID)
}

func (client *Client) VMs(ctx context.Context) ([]VMResource, error) {
	var vms []VMResource
	err := client.requestJSON(ctx, http.MethodGet, "/cluster/resources", url.Values{"type": {"vm"}}, &vms)
	return vms, err
}

func (client *Client) GuestConfig(ctx context.Context, node string, guestType string, vmid int) (map[string]any, error) {
	var config map[string]any
	err := client.requestJSON(ctx, http.MethodGet, guestPath(node, guestType, vmid, "config"), nil, &config)
	return config, err
}

func (client *Client) VMConfig(ctx context.Context, node string, vmid int) (map[string]any, error) {
	return client.GuestConfig(ctx, node, GuestTypeQEMU, vmid)
}

func (client *Client) CloneVM(ctx context.Context, node string, templateID int, newID int, name string, storage string, fullClone bool) (string, error) {
	values := url.Values{
		"newid":   {strconv.Itoa(newID)},
		"name":    {name},
		"target":  {node},
		"storage": {storage},
		"full":    {boolInt(fullClone)},
	}

	return client.taskRequest(ctx, http.MethodPost, fmt.Sprintf("/nodes/%s/qemu/%d/clone", url.PathEscape(node), templateID), values)
}

func (client *Client) CreateContainer(ctx context.Context, node string, values url.Values) (string, error) {
	return client.taskRequest(ctx, http.MethodPost, fmt.Sprintf("/nodes/%s/lxc", url.PathEscape(node)), values)
}

func (client *Client) ResizeDisk(ctx context.Context, node string, vmid int, disk string, size string) (string, error) {
	values := url.Values{
		"disk": {disk},
		"size": {size},
	}

	return client.taskRequest(ctx, http.MethodPut, fmt.Sprintf("/nodes/%s/qemu/%d/resize", url.PathEscape(node), vmid), values)
}

func (client *Client) SetVMConfig(ctx context.Context, node string, vmid int, values url.Values) error {
	return client.requestJSON(ctx, http.MethodPut, fmt.Sprintf("/nodes/%s/qemu/%d/config", url.PathEscape(node), vmid), values, nil)
}

func (client *Client) SetGuestConfig(ctx context.Context, node string, guestType string, vmid int, values url.Values) error {
	return client.requestJSON(ctx, http.MethodPut, guestPath(node, guestType, vmid, "config"), values, nil)
}

func (client *Client) StartGuest(ctx context.Context, node string, guestType string, vmid int) (string, error) {
	return client.taskRequest(ctx, http.MethodPost, guestPath(node, guestType, vmid, "status/start"), nil)
}

func (client *Client) StartVM(ctx context.Context, node string, vmid int) (string, error) {
	return client.StartGuest(ctx, node, GuestTypeQEMU, vmid)
}

func (client *Client) ShutdownGuest(ctx context.Context, node string, guestType string, vmid int) (string, error) {
	return client.taskRequest(ctx, http.MethodPost, guestPath(node, guestType, vmid, "status/shutdown"), nil)
}

func (client *Client) ShutdownVM(ctx context.Context, node string, vmid int) (string, error) {
	return client.ShutdownGuest(ctx, node, GuestTypeQEMU, vmid)
}

func (client *Client) RebootGuest(ctx context.Context, node string, guestType string, vmid int) (string, error) {
	return client.taskRequest(ctx, http.MethodPost, guestPath(node, guestType, vmid, "status/reboot"), nil)
}

func (client *Client) RebootVM(ctx context.Context, node string, vmid int) (string, error) {
	return client.RebootGuest(ctx, node, GuestTypeQEMU, vmid)
}

func (client *Client) DeleteGuest(ctx context.Context, node string, guestType string, vmid int) (string, error) {
	values := url.Values{
		"purge":                      {"1"},
		"destroy-unreferenced-disks": {"1"},
	}

	return client.taskRequest(ctx, http.MethodDelete, guestPath(node, guestType, vmid, ""), values)
}

func (client *Client) DeleteVM(ctx context.Context, node string, vmid int) (string, error) {
	return client.DeleteGuest(ctx, node, GuestTypeQEMU, vmid)
}

func (client *Client) RenameGuest(ctx context.Context, node string, guestType string, vmid int, name string) error {
	key := "name"
	if guestType == GuestTypeLXC {
		key = "hostname"
	}
	return client.SetGuestConfig(ctx, node, guestType, vmid, url.Values{key: {name}})
}

func (client *Client) RenameVM(ctx context.Context, node string, vmid int, name string) error {
	return client.RenameGuest(ctx, node, GuestTypeQEMU, vmid, name)
}

func (client *Client) GuestSnapshots(ctx context.Context, node string, guestType string, vmid int) ([]Snapshot, error) {
	var snapshots []Snapshot
	err := client.requestJSON(ctx, http.MethodGet, guestPath(node, guestType, vmid, "snapshot"), nil, &snapshots)
	return snapshots, err
}

func (client *Client) Snapshots(ctx context.Context, node string, vmid int) ([]Snapshot, error) {
	return client.GuestSnapshots(ctx, node, GuestTypeQEMU, vmid)
}

func (client *Client) CreateGuestSnapshot(ctx context.Context, node string, guestType string, vmid int, name string, description string) (string, error) {
	values := url.Values{
		"snapname": {name},
	}

	if description != "" {
		values.Set("description", description)
	}

	return client.taskRequest(ctx, http.MethodPost, guestPath(node, guestType, vmid, "snapshot"), values)
}

func (client *Client) CreateSnapshot(ctx context.Context, node string, vmid int, name string, description string) (string, error) {
	return client.CreateGuestSnapshot(ctx, node, GuestTypeQEMU, vmid, name, description)
}

func (client *Client) DeleteGuestSnapshot(ctx context.Context, node string, guestType string, vmid int, name string) (string, error) {
	return client.taskRequest(ctx, http.MethodDelete, guestPath(node, guestType, vmid, "snapshot/"+url.PathEscape(name)), nil)
}

func (client *Client) DeleteSnapshot(ctx context.Context, node string, vmid int, name string) (string, error) {
	return client.DeleteGuestSnapshot(ctx, node, GuestTypeQEMU, vmid, name)
}

func (client *Client) AgentNetworkInterfaces(ctx context.Context, node string, vmid int) ([]NetworkInterface, error) {
	var interfaces struct {
		Result []NetworkInterface `json:"result"`
	}

	err := client.requestJSON(ctx, http.MethodGet, fmt.Sprintf("/nodes/%s/qemu/%d/agent/network-get-interfaces", url.PathEscape(node), vmid), nil, &interfaces)
	return interfaces.Result, err
}

func (client *Client) ContainerInterfaces(ctx context.Context, node string, vmid int) ([]ContainerInterface, error) {
	var interfaces []ContainerInterface
	err := client.requestJSON(ctx, http.MethodGet, guestPath(node, GuestTypeLXC, vmid, "interfaces"), nil, &interfaces)
	return interfaces, err
}

func guestPath(node string, guestType string, vmid int, suffix string) string {
	if guestType != GuestTypeLXC {
		guestType = GuestTypeQEMU
	}

	basePath := fmt.Sprintf("/nodes/%s/%s/%d", url.PathEscape(node), guestType, vmid)
	if suffix == "" {
		return basePath
	}
	return basePath + "/" + strings.TrimLeft(suffix, "/")
}

func (client *Client) WaitForTask(ctx context.Context, node string, upid string) error {
	_, err := client.WaitForTaskWithTimeout(ctx, node, upid, 0)
	return err
}

func taskCompleted(exitStatus string) bool {
	return exitStatus == "" || exitStatus == "OK" || strings.HasPrefix(exitStatus, "WARNINGS")
}

func (client *Client) TaskStatus(ctx context.Context, node string, upid string) (TaskStatus, error) {
	var status TaskStatus
	err := client.requestJSON(ctx, http.MethodGet, fmt.Sprintf("/nodes/%s/tasks/%s/status", url.PathEscape(node), url.PathEscape(upid)), nil, &status)
	return status, err
}

func (client *Client) FirstRoutableIP(ctx context.Context, node string, vmid int) (string, error) {
	interfaces, err := client.AgentNetworkInterfaces(ctx, node, vmid)
	if err != nil {
		return "", err
	}

	for _, networkInterface := range interfaces {
		for _, ipAddress := range networkInterface.IPAddresses {
			parsedIP := net.ParseIP(ipAddress.Address)
			if parsedIP == nil || !isRoutableGuestIP(parsedIP) {
				continue
			}

			return ipAddress.Address, nil
		}
	}

	return "", nil
}

func (client *Client) taskRequest(ctx context.Context, method string, path string, values url.Values) (string, error) {
	var upid string
	err := client.requestJSON(ctx, method, path, values, &upid)
	return upid, err
}

func (client *Client) requestJSON(ctx context.Context, method string, path string, values url.Values, target any) error {
	responseBody, err := client.doRequest(ctx, method, path, values, "application/x-www-form-urlencoded")
	if err != nil {
		return err
	}

	if target == nil {
		return nil
	}

	var wrappedResponse apiResponse
	if err := json.Unmarshal(responseBody, &wrappedResponse); err != nil {
		return err
	}

	if len(wrappedResponse.Data) == 0 || string(wrappedResponse.Data) == "null" {
		return nil
	}

	return json.Unmarshal(wrappedResponse.Data, target)
}

func (client *Client) doRequest(ctx context.Context, method string, path string, values url.Values, contentType string) ([]byte, error) {
	requestURL := client.baseURL + path
	var body io.Reader

	if len(values) > 0 && (method == http.MethodGet || method == http.MethodDelete) {
		requestURL += "?" + values.Encode()
	} else if len(values) > 0 {
		body = strings.NewReader(values.Encode())
	}

	request, err := http.NewRequestWithContext(ctx, method, requestURL, body)
	if err != nil {
		return nil, err
	}

	request.Header.Set("Authorization", "PVEAPIToken="+client.tokenID+"="+client.tokenSecret)
	if body != nil {
		if contentType == "" {
			contentType = "application/x-www-form-urlencoded"
		}
		request.Header.Set("Content-Type", contentType)
	}

	response, err := client.httpClient.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()

	responseBody, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, err
	}

	if response.StatusCode < 200 || response.StatusCode > 299 {
		return nil, formatAPIError(method, path, response.Status, response.StatusCode, responseBody)
	}

	return responseBody, nil
}

func boolInt(value bool) string {
	if value {
		return "1"
	}

	return "0"
}

func isRoutableGuestIP(ip net.IP) bool {
	if ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsMulticast() || ip.IsUnspecified() {
		return false
	}

	return true
}

func parseIntResponse(value any) (int, error) {
	switch typedValue := value.(type) {
	case float64:
		return int(typedValue), nil
	case string:
		parsedValue, err := strconv.Atoi(typedValue)
		if err != nil {
			return 0, err
		}

		return parsedValue, nil
	default:
		return 0, fmt.Errorf("expected integer response, got %T", value)
	}
}

func formatAPIError(method string, path string, status string, statusCode int, responseBody []byte) error {
	trimmedBody := strings.TrimSpace(string(responseBody))
	var errorResponse apiErrorResponse
	if err := json.Unmarshal(responseBody, &errorResponse); err == nil && len(errorResponse.Errors) > 0 {
		return &APIError{
			Method:     method,
			Path:       path,
			Status:     status,
			StatusCode: statusCode,
			Body:       trimmedBody,
			Details:    errorResponse.Errors,
		}
	}

	return &APIError{
		Method:     method,
		Path:       path,
		Status:     status,
		StatusCode: statusCode,
		Body:       trimmedBody,
	}
}
