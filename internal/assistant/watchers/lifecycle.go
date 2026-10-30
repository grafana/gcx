package watchers

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// RunRequest acknowledges a request to enqueue work. TaskID is not a run-history ID.
type RunRequest struct {
	TaskID  string `json:"runId,omitempty"`
	Skipped bool   `json:"skipped,omitempty"`
}

// Run is a bounded history projection without reports, telemetry, or error bodies.
type Run struct {
	ID                string     `json:"id"`
	Status            string     `json:"status"`
	Trigger           string     `json:"trigger,omitempty"`
	Assessment        string     `json:"assessment,omitempty"`
	StartedAt         time.Time  `json:"startedAt"`
	FinishedAt        *time.Time `json:"finishedAt,omitempty"`
	DefinitionVersion int64      `json:"definitionVersion"`
}

// RunHistory is a bounded view of caller-visible retained history, not a snapshot.
type RunHistory struct {
	Items   []Run `json:"items"`
	HasMore bool  `json:"hasMore"`
}

func (c *Client) Start(ctx context.Context, id string) (*Watcher, error) {
	return c.lifecycle(ctx, id, "start", "start Watcher", "running", false)
}

func (c *Client) Pause(ctx context.Context, id string) (*Watcher, error) {
	return c.lifecycle(ctx, id, "pause", "pause Watcher", "paused", false)
}

func (c *Client) Archive(ctx context.Context, id string) (*Watcher, error) {
	return c.lifecycle(ctx, id, "archive", "archive Watcher", "paused", true)
}

func (c *Client) Unarchive(ctx context.Context, id string) (*Watcher, error) {
	return c.lifecycle(ctx, id, "restore", "unarchive Watcher", "paused", false)
}

func (c *Client) lifecycle(ctx context.Context, id, suffix, operation, status string, archived bool) (*Watcher, error) {
	path, err := watcherPath(id)
	if err != nil {
		return nil, err
	}
	var item Watcher
	if err := c.write(ctx, http.MethodPost, path+"/"+suffix, operation, nil, &item); err != nil {
		return nil, err
	}
	if item.ID != id || strings.TrimSpace(item.Name) == "" {
		return &item, fmt.Errorf("%s: response has an unexpected resource identity; outcome is uncertain", operation)
	}
	if item.Status != status || (item.ArchivedAt != nil) != archived {
		return &item, fmt.Errorf("%s: response does not confirm the requested lifecycle state; inspect Watcher status before retrying", operation)
	}
	return &item, nil
}

func (c *Client) RequestRun(ctx context.Context, id string) (*RunRequest, error) {
	path, err := watcherPath(id)
	if err != nil {
		return nil, err
	}
	var result RunRequest
	if err := c.write(ctx, http.MethodPost, path+"/runs", "request Watcher run", nil, &result); err != nil {
		return nil, err
	}
	if !result.Skipped && strings.TrimSpace(result.TaskID) == "" {
		return nil, errors.New("request Watcher run: response does not confirm acceptance; outcome is uncertain")
	}
	return &result, nil
}

func (c *Client) ListRuns(ctx context.Context, id string, limit int) (*RunHistory, error) {
	path, err := watcherPath(id)
	if err != nil {
		return nil, err
	}
	if limit < 1 || limit > 100 {
		return nil, errors.New("Watcher run history limit must be between 1 and 100")
	}
	page, err := c.runPage(ctx, path, min(limit+1, 100), 0)
	if err != nil {
		return nil, err
	}
	result := &RunHistory{Items: page, HasMore: len(page) > limit}
	if limit == 100 && len(page) == 100 {
		probe, err := c.runPage(ctx, path, 1, 100)
		if err != nil {
			return nil, err
		}
		result.HasMore = len(probe) > 0
	}
	if len(result.Items) > limit {
		result.Items = result.Items[:limit]
	}
	return result, nil
}

func (c *Client) runPage(ctx context.Context, path string, limit, offset int) ([]Run, error) {
	params := url.Values{"limit": {strconv.Itoa(limit)}, "offset": {strconv.Itoa(offset)}}
	var page struct {
		Runs *[]Run `json:"runs"`
	}
	if err := c.read(ctx, path+"/runs?"+params.Encode(), "list Watcher runs", false, &page); err != nil {
		return nil, err
	}
	if page.Runs == nil {
		return nil, errors.New("list Watcher runs: response omits collection items")
	}
	if len(*page.Runs) > limit {
		return nil, errors.New("list Watcher runs: response exceeds the requested page size")
	}
	for i := range *page.Runs {
		item := &(*page.Runs)[i]
		if strings.TrimSpace(item.ID) == "" || strings.TrimSpace(item.Status) == "" || item.StartedAt.IsZero() {
			return nil, errors.New("list Watcher runs: response contains an incomplete run")
		}
		if item.Status != "completed" || (item.Assessment != "ok" && item.Assessment != "warning" && item.Assessment != "critical") {
			item.Assessment = ""
		}
	}
	return *page.Runs, nil
}

// lifecycleAPIReason returns fixed public guidance, never server-supplied details.
func lifecycleAPIReason(operation string, kind error, message string) string {
	switch operation {
	case "start Watcher", "pause Watcher", "archive Watcher", "unarchive Watcher":
		if errors.Is(kind, ErrPermissionDenied) {
			return "Watcher write permission and access to the Watcher are required"
		}
	case "request Watcher run":
		if errors.Is(kind, ErrPermissionDenied) {
			return "Watcher create and write permissions, resource access, and an authenticated user are required to request a run"
		}
	case "list Watcher runs":
		if errors.Is(kind, ErrPermissionDenied) {
			return "Watcher read permission and access to the Watcher are required"
		}
	default:
		return ""
	}
	if errors.Is(kind, ErrConflict) || errors.Is(kind, ErrInvalidConfiguration) {
		if reason := lifecycleRefusalReason(operation, strings.ToLower(message)); reason != "" {
			return reason
		}
	}
	if errors.Is(kind, ErrConflict) {
		if operation == "pause Watcher" {
			return "Watcher state conflicts with pausing; inspect status and finish or cancel calibration in Grafana if it is active"
		}
		return "Watcher state or configuration conflicts with the request; inspect current status before retrying"
	}
	if errors.Is(kind, ErrInvalidConfiguration) && (operation == "start Watcher" || operation == "request Watcher run") {
		return "Watcher readiness or request validation failed; inspect calibration, enabled checks, and the automatic-stop deadline"
	}
	return ""
}

func lifecycleRefusalReason(operation, message string) string {
	if strings.Contains(message, "restore the archived watcher first") {
		return "unarchive the Watcher before requesting this operation"
	}
	if operation == "pause Watcher" && strings.Contains(message, "the watcher is calibrating and does not scan until calibration ends") {
		return "wait for calibration to finish or cancel calibration in Grafana before requesting this operation"
	}
	if operation != "start Watcher" && operation != "request Watcher run" {
		return ""
	}
	switch {
	case strings.Contains(message, "agent is calibrating"):
		return "wait for calibration to finish or cancel calibration in Grafana before requesting this operation"
	case strings.Contains(message, "agent is draft"):
		return "Watcher is a draft; calibrate it in Grafana before starting or requesting a run"
	case strings.Contains(message, "it must be calibrated at least once"):
		return "Watcher has not been calibrated; calibrate it in Grafana before starting or requesting a run"
	case strings.Contains(message, "at least one enabled query is required"):
		return "Watcher has no enabled checks; enable and calibrate a check in Grafana before starting or requesting a run"
	case strings.Contains(message, "stop time has passed; extend or remove it before starting"):
		return "Watcher automatic-stop deadline has expired; inspect its configuration in Grafana"
	default:
		return ""
	}
}
