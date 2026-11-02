package k6

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
)

const (
	runTracesSearchPath = "/api/v1/tempo/api/search"
	runTracesGetPath    = "/api/v1/tempo/api/traces"
	runArtifactsPath    = "/cloud-resources/v1/files"
)

// RunTracesRequest defines a bounded Tempo search for one test run.
type RunTracesRequest struct {
	Query string
	Start int64
	End   int64
	Limit int
}

// RunTraceSummary is one trace returned by Tempo search.
type RunTraceSummary struct {
	DurationMS        float64         `json:"durationMs"`
	RootServiceName   string          `json:"rootServiceName"`
	RootTraceName     string          `json:"rootTraceName"`
	StartTimeUnixNano string          `json:"startTimeUnixNano"`
	TraceID           string          `json:"traceID"`
	SpanSet           json.RawMessage `json:"spanSet,omitempty"`
	ServiceStats      json.RawMessage `json:"serviceStats,omitempty"`
}

// RunTracesResponse is the Tempo trace-search response for one run.
type RunTracesResponse struct {
	Metrics json.RawMessage   `json:"metrics,omitempty"`
	Traces  []RunTraceSummary `json:"traces"`
}

// RunTrace is a full OTLP trace response.
type RunTrace struct {
	Batches []json.RawMessage `json:"batches"`
}

// RunArtifactDownload contains one short-lived artifact download URL.
type RunArtifactDownload struct {
	Name         string `json:"name"`
	PreSignedURL string `json:"pre_signed_url"`
}

func runIDHeaders(runID int) http.Header {
	headers := http.Header{}
	headers.Set("X-K6testrun-Id", strconv.Itoa(runID))
	return headers
}

// ListRunTraces searches browser traces for one test run.
func (c *cloudOperations) ListRunTraces(
	ctx context.Context, runID int, request RunTracesRequest,
) (*RunTracesResponse, error) {
	if request.Query == "" {
		return nil, errors.New("k6: trace query must not be empty")
	}
	if request.Start <= 0 || request.End <= request.Start {
		return nil, fmt.Errorf("k6: trace time range %d..%d is invalid", request.Start, request.End)
	}
	if request.Limit <= 0 {
		return nil, fmt.Errorf("k6: trace limit %d must be greater than 0", request.Limit)
	}
	query := url.Values{}
	query.Set("q", request.Query)
	query.Set("start", strconv.FormatInt(request.Start, 10))
	query.Set("end", strconv.FormatInt(request.End, 10))
	query.Set("limit", strconv.Itoa(request.Limit))
	resp, err := c.executor.doCloud(ctx, cloudRequest{
		Target: cloudTargetLogs, Auth: cloudAuthConfigured, Method: http.MethodGet,
		Path: runTracesSearchPath + "?" + query.Encode(), Accept: "application/json", Headers: runIDHeaders(runID),
	})
	if err != nil {
		return nil, fmt.Errorf("k6: list run traces: %w", err)
	}
	if err := checkCloudStatus(resp, "list run traces", http.StatusOK); err != nil {
		return nil, err
	}
	var result RunTracesResponse
	if err := json.Unmarshal(resp.Body, &result); err != nil {
		return nil, fmt.Errorf("k6: decode run traces response: %w", err)
	}
	if result.Traces == nil {
		result.Traces = []RunTraceSummary{}
	}
	return &result, nil
}

// GetRunTrace gets one full browser trace by its Tempo trace ID.
func (c *cloudOperations) GetRunTrace(ctx context.Context, runID int, traceID string) (*RunTrace, error) {
	resp, err := c.executor.doCloud(ctx, cloudRequest{
		Target: cloudTargetLogs, Auth: cloudAuthConfigured, Method: http.MethodGet,
		Path: runTracesGetPath + "/" + url.PathEscape(traceID), Accept: "application/json", Headers: runIDHeaders(runID),
	})
	if err != nil {
		return nil, fmt.Errorf("k6: get run trace: %w", err)
	}
	if err := checkCloudStatus(resp, "get run trace", http.StatusOK); err != nil {
		return nil, err
	}
	var result RunTrace
	if err := json.Unmarshal(resp.Body, &result); err != nil {
		return nil, fmt.Errorf("k6: decode run trace response: %w", err)
	}
	if result.Batches == nil {
		result.Batches = []json.RawMessage{}
	}
	return &result, nil
}

// ListRunArtifacts lists browser screenshot paths for one test run.
func (c *cloudOperations) ListRunArtifacts(ctx context.Context, runID int) ([]string, error) {
	resp, err := c.executor.doCloud(ctx, cloudRequest{
		Target: cloudTargetCloud, Auth: cloudAuthConfigured, Method: http.MethodGet,
		Path: runArtifactsPath + "/index", Accept: "application/json", Headers: runIDHeaders(runID),
	})
	if err != nil {
		return nil, fmt.Errorf("k6: list run artifacts: %w", err)
	}
	if err := checkCloudStatus(resp, "list run artifacts", http.StatusOK); err != nil {
		return nil, err
	}
	var result []string
	if err := json.Unmarshal(resp.Body, &result); err != nil {
		return nil, fmt.Errorf("k6: decode run artifacts response: %w", err)
	}
	if result == nil {
		result = []string{}
	}
	return result, nil
}

// SignRunArtifactDownloads creates short-lived URLs for run artifact downloads.
func (c *cloudOperations) SignRunArtifactDownloads(
	ctx context.Context, runID int, names []string,
) ([]RunArtifactDownload, error) {
	files := make([]map[string]string, len(names))
	for i, name := range names {
		files[i] = map[string]string{"name": name}
	}
	body, err := json.Marshal(map[string]any{
		"service": "aws_s3", "operation": "download", "files": files,
	})
	if err != nil {
		return nil, fmt.Errorf("k6: encode artifact download request: %w", err)
	}
	resp, err := c.executor.doCloud(ctx, cloudRequest{
		Target: cloudTargetCloud, Auth: cloudAuthConfigured, Method: http.MethodPost,
		Path: runArtifactsPath + "/generate-pre-signed-url", Body: body,
		ContentType: "application/json", Accept: "application/json", Headers: runIDHeaders(runID),
	})
	if err != nil {
		return nil, fmt.Errorf("k6: sign run artifact downloads: %w", err)
	}
	if err := checkCloudStatus(resp, "sign run artifact downloads", http.StatusOK); err != nil {
		return nil, err
	}
	var result struct {
		URLs []RunArtifactDownload `json:"urls"`
	}
	if err := json.Unmarshal(resp.Body, &result); err != nil {
		return nil, fmt.Errorf("k6: decode artifact download response: %w", err)
	}
	if result.URLs == nil {
		result.URLs = []RunArtifactDownload{}
	}
	return result.URLs, nil
}
