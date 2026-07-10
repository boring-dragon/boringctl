package proxmox

import (
	"fmt"
	"net/http"
	"strings"
)

const (
	ErrorKindAuth     = "auth"
	ErrorKindNotFound = "not_found"
	ErrorKindConflict = "conflict"
	ErrorKindAPI      = "api"
	ErrorKindTask     = "task_failed"
	ErrorKindTimeout  = "timeout"
	ErrorKindConfirm  = "confirmation_required"
	ErrorKindConfig   = "config"
	ErrorKindUsage    = "usage"
	ErrorKindOther    = "other"
)

type APIError struct {
	Method     string
	Path       string
	Status     string
	StatusCode int
	Body       string
	Details    map[string]string
}

func (err *APIError) Error() string {
	if len(err.Details) > 0 {
		details := make([]string, 0, len(err.Details))
		for field, message := range err.Details {
			details = append(details, fmt.Sprintf("%s: %s", field, message))
		}
		return fmt.Sprintf("proxmox %s %s failed: %s: %s", err.Method, err.Path, err.Status, strings.Join(details, "; "))
	}

	return fmt.Sprintf("proxmox %s %s failed: %s: %s", err.Method, err.Path, err.Status, err.Body)
}

func (err *APIError) Kind() string {
	switch err.StatusCode {
	case http.StatusUnauthorized, http.StatusForbidden:
		return ErrorKindAuth
	case http.StatusNotFound:
		return ErrorKindNotFound
	case http.StatusConflict:
		return ErrorKindConflict
	default:
		return ErrorKindAPI
	}
}

type TaskError struct {
	UPID       string
	ExitStatus string
}

func (err *TaskError) Error() string {
	if err.UPID == "" {
		return fmt.Sprintf("task failed with exit status %s", err.ExitStatus)
	}

	return fmt.Sprintf("task %s failed with exit status %s", err.UPID, err.ExitStatus)
}

func (err *TaskError) Kind() string {
	return ErrorKindTask
}

type TimeoutError struct {
	Message string
}

func (err *TimeoutError) Error() string {
	return err.Message
}

func (err *TimeoutError) Kind() string {
	return ErrorKindTimeout
}
