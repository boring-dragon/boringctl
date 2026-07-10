package proxmox

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type Task struct {
	UPID       string `json:"upid"`
	Node       string `json:"node"`
	ID         string `json:"id"`
	User       string `json:"user"`
	Type       string `json:"type"`
	Status     string `json:"status"`
	ExitStatus string `json:"exitstatus"`
	StartTime  int64  `json:"starttime"`
	EndTime    int64  `json:"endtime"`
}

type TaskLogEntry struct {
	LineNumber int    `json:"n"`
	Text       string `json:"t"`
}

type TaskListFilter struct {
	Node   string
	Source string
	Status string
	Limit  int
}

type StorageContent struct {
	VolumeID     string `json:"volid"`
	Content      string `json:"content"`
	Format       string `json:"format"`
	Size         int64  `json:"size"`
	Used         int64  `json:"used"`
	VMID         int    `json:"vmid"`
	Name         string `json:"name"`
	Notes        string `json:"notes"`
	CreationTime int64  `json:"ctime"`
	Protected    int    `json:"protected"`
}

type StorageContentFilter struct {
	Content string
	VMID    int
}

type UploadRequest struct {
	Node     string
	Storage  string
	Content  string
	FilePath string
	Filename string
}

type DownloadURLRequest struct {
	Node               string
	Storage            string
	Content            string
	URL                string
	Filename           string
	Checksum           string
	ChecksumAlgorithm  string
	VerifyCertificates bool
}

type BackupRequest struct {
	Node     string
	VMID     int
	Storage  string
	Mode     string
	Compress string
	Notes    string
}

type RestoreRequest struct {
	Node    string
	VMID    int
	Kind    string
	Archive string
	Storage string
	Force   bool
}

func (client *Client) RawRequest(ctx context.Context, method string, path string, values url.Values, rawResponse bool) (json.RawMessage, error) {
	responseBody, err := client.doRequest(ctx, strings.ToUpper(method), path, values, "")
	if err != nil {
		return nil, err
	}

	if rawResponse {
		return responseBody, nil
	}

	var wrappedResponse apiResponse
	if err := json.Unmarshal(responseBody, &wrappedResponse); err != nil {
		return nil, err
	}

	if len(wrappedResponse.Data) == 0 {
		return []byte("null"), nil
	}

	return wrappedResponse.Data, nil
}

func (client *Client) Tasks(ctx context.Context, filter TaskListFilter) ([]Task, error) {
	values := url.Values{}
	if filter.Source != "" {
		values.Set("source", filter.Source)
	}
	if filter.Status == "running" {
		values.Set("statusfilter", "active")
	}
	path := "/cluster/tasks"
	if filter.Node != "" {
		path = fmt.Sprintf("/nodes/%s/tasks", url.PathEscape(filter.Node))
	}

	var tasks []Task
	if err := client.requestJSON(ctx, http.MethodGet, path, values, &tasks); err != nil {
		return nil, err
	}

	if filter.Status != "" && filter.Status != "running" {
		filteredTasks := make([]Task, 0, len(tasks))
		for _, task := range tasks {
			switch filter.Status {
			case "ok":
				if task.ExitStatus == "OK" {
					filteredTasks = append(filteredTasks, task)
				}
			case "error":
				if task.ExitStatus != "" && task.ExitStatus != "OK" {
					filteredTasks = append(filteredTasks, task)
				}
			default:
				filteredTasks = append(filteredTasks, task)
			}
		}
		tasks = filteredTasks
	}

	if filter.Limit > 0 && len(tasks) > filter.Limit {
		return tasks[:filter.Limit], nil
	}

	return tasks, nil
}

func (client *Client) TaskLog(ctx context.Context, upid string) ([]TaskLogEntry, error) {
	node, err := ParseUPIDNode(upid)
	if err != nil {
		return nil, err
	}

	var logEntries []TaskLogEntry
	err = client.requestJSON(ctx, http.MethodGet, fmt.Sprintf("/nodes/%s/tasks/%s/log", url.PathEscape(node), url.PathEscape(upid)), nil, &logEntries)
	return logEntries, err
}

func (client *Client) StopTask(ctx context.Context, upid string) error {
	node, err := ParseUPIDNode(upid)
	if err != nil {
		return err
	}

	return client.requestJSON(ctx, http.MethodDelete, fmt.Sprintf("/nodes/%s/tasks/%s", url.PathEscape(node), url.PathEscape(upid)), nil, nil)
}

func (client *Client) WaitForTaskWithTimeout(ctx context.Context, node string, upid string, timeout time.Duration) (TaskStatus, error) {
	if upid == "" {
		return TaskStatus{}, nil
	}

	waitContext := ctx
	cancel := func() {}
	if timeout > 0 {
		waitContext, cancel = context.WithTimeout(ctx, timeout)
	}
	defer cancel()

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		status, err := client.TaskStatus(waitContext, node, upid)
		if err != nil {
			return TaskStatus{}, err
		}

		if status.Status != "running" {
			if taskCompleted(status.ExitStatus) {
				return status, nil
			}

			return status, &TaskError{UPID: upid, ExitStatus: status.ExitStatus}
		}

		select {
		case <-waitContext.Done():
			if timeout > 0 && waitContext.Err() == context.DeadlineExceeded {
				return TaskStatus{}, &TimeoutError{Message: fmt.Sprintf("task %s did not complete within %s", upid, timeout)}
			}
			return TaskStatus{}, waitContext.Err()
		case <-ticker.C:
		}
	}
}

func (client *Client) RollbackGuestSnapshot(ctx context.Context, node string, guestType string, vmid int, name string) (string, error) {
	return client.taskRequest(ctx, http.MethodPost, guestPath(node, guestType, vmid, "snapshot/"+url.PathEscape(name)+"/rollback"), nil)
}

func (client *Client) StorageContent(ctx context.Context, node string, storage string, filter StorageContentFilter) ([]StorageContent, error) {
	values := url.Values{}
	if filter.Content != "" {
		values.Set("content", filter.Content)
	}
	if filter.VMID > 0 {
		values.Set("vmid", strconv.Itoa(filter.VMID))
	}

	var content []StorageContent
	err := client.requestJSON(ctx, http.MethodGet, fmt.Sprintf("/nodes/%s/storage/%s/content", url.PathEscape(node), url.PathEscape(storage)), values, &content)
	return content, err
}

func (client *Client) UploadStorageContent(ctx context.Context, request UploadRequest) (string, error) {
	filename := request.Filename
	if filename == "" {
		filename = filepath.Base(request.FilePath)
	}

	file, err := os.Open(request.FilePath)
	if err != nil {
		return "", err
	}
	defer file.Close()

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("content", request.Content); err != nil {
		return "", err
	}

	part, err := writer.CreateFormFile("filename", filename)
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(part, file); err != nil {
		return "", err
	}
	if err := writer.Close(); err != nil {
		return "", err
	}

	path := fmt.Sprintf("/nodes/%s/storage/%s/upload", url.PathEscape(request.Node), url.PathEscape(request.Storage))
	responseBody, err := client.doMultipartRequest(ctx, path, writer.FormDataContentType(), &body)
	if err != nil {
		return "", err
	}

	var wrappedResponse apiResponse
	if err := json.Unmarshal(responseBody, &wrappedResponse); err != nil {
		return "", err
	}
	if len(wrappedResponse.Data) == 0 || string(wrappedResponse.Data) == "null" {
		return "", nil
	}

	var upid string
	if err := json.Unmarshal(wrappedResponse.Data, &upid); err != nil {
		return "", err
	}
	return upid, nil
}

func (client *Client) DownloadStorageContentFromURL(ctx context.Context, request DownloadURLRequest) (string, error) {
	values := url.Values{
		"content":             {request.Content},
		"url":                 {request.URL},
		"verify-certificates": {boolInt(request.VerifyCertificates)},
	}
	if request.Filename != "" {
		values.Set("filename", request.Filename)
	}
	if request.Checksum != "" {
		values.Set("checksum", request.Checksum)
	}
	if request.ChecksumAlgorithm != "" {
		values.Set("checksum-algorithm", request.ChecksumAlgorithm)
	}

	return client.taskRequest(ctx, http.MethodPost, fmt.Sprintf("/nodes/%s/storage/%s/download-url", url.PathEscape(request.Node), url.PathEscape(request.Storage)), values)
}

func (client *Client) CreateBackup(ctx context.Context, request BackupRequest) (string, error) {
	values := url.Values{
		"vmid": {strconv.Itoa(request.VMID)},
	}
	if request.Storage != "" {
		values.Set("storage", request.Storage)
	}
	if request.Mode != "" {
		values.Set("mode", request.Mode)
	}
	if request.Compress != "" {
		values.Set("compress", request.Compress)
	}
	if request.Notes != "" {
		values.Set("notes-template", request.Notes)
	}

	return client.taskRequest(ctx, http.MethodPost, fmt.Sprintf("/nodes/%s/vzdump", url.PathEscape(request.Node)), values)
}

func (client *Client) RestoreBackup(ctx context.Context, request RestoreRequest) (string, error) {
	values := url.Values{
		"vmid": {strconv.Itoa(request.VMID)},
	}
	if request.Storage != "" {
		values.Set("storage", request.Storage)
	}
	if request.Force {
		values.Set("force", "1")
	}

	guestKind := request.Kind
	if guestKind == "" {
		guestKind = GuestTypeQEMU
	}
	if guestKind == GuestTypeLXC {
		values.Set("ostemplate", request.Archive)
		return client.taskRequest(ctx, http.MethodPost, fmt.Sprintf("/nodes/%s/lxc", url.PathEscape(request.Node)), values)
	}

	values.Set("archive", request.Archive)
	return client.taskRequest(ctx, http.MethodPost, fmt.Sprintf("/nodes/%s/qemu", url.PathEscape(request.Node)), values)
}

func ParseUPIDNode(upid string) (string, error) {
	parts := strings.Split(upid, ":")
	if len(parts) < 2 || strings.TrimSpace(parts[1]) == "" {
		return "", fmt.Errorf("invalid UPID %q", upid)
	}

	return parts[1], nil
}

func (client *Client) doMultipartRequest(ctx context.Context, path string, contentType string, body io.Reader) ([]byte, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, client.baseURL+path, body)
	if err != nil {
		return nil, err
	}

	request.Header.Set("Authorization", "PVEAPIToken="+client.tokenID+"="+client.tokenSecret)
	request.Header.Set("Content-Type", contentType)

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
		return nil, formatAPIError(http.MethodPost, path, response.Status, response.StatusCode, responseBody)
	}

	return responseBody, nil
}
