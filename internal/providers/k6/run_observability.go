package k6

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/grafana/gcx/internal/query/loki"
)

const (
	runLogsPath      = "/api/v1/query_range"
	runInsightsPath  = "/insights/api/v1/testrun"
	defaultLogQuery  = `{test_run_id="%d"}`
	defaultLogLimit  = 50
	logDirectionBack = "backward"
)

// RunLogsRequest defines a bounded Loki log query for one test run.
type RunLogsRequest struct {
	Query     string
	Start     time.Time
	End       time.Time
	Direction string
	Limit     int
}

func (r RunLogsRequest) validate() error {
	if strings.TrimSpace(r.Query) == "" {
		return errors.New("log query must not be empty")
	}
	if r.Start.IsZero() || r.End.IsZero() {
		return errors.New("log query start and end are required")
	}
	if !r.Start.Before(r.End) {
		return errors.New("log query start must be before end")
	}
	if r.Direction != "forward" && r.Direction != logDirectionBack {
		return fmt.Errorf("invalid log direction %q: must be forward or backward", r.Direction)
	}
	if r.Limit <= 0 {
		return fmt.Errorf("invalid log limit %d: must be greater than 0", r.Limit)
	}
	return nil
}

type runLogsWireResponse struct {
	Status    string `json:"status"`
	ErrorType string `json:"errorType,omitempty"`
	Error     string `json:"error,omitempty"`
	Data      struct {
		ResultType string `json:"resultType"`
		Result     []struct {
			Stream map[string]string   `json:"stream"`
			Values [][]json.RawMessage `json:"values"`
		} `json:"result"`
		Stats   *loki.QueryStats   `json:"stats,omitempty"`
		Notices []loki.FrameNotice `json:"notices,omitempty"`
	} `json:"data"`
}

func decodeRunLogs(body []byte) (*loki.QueryResponse, error) {
	var wire runLogsWireResponse
	if err := json.Unmarshal(body, &wire); err != nil {
		return nil, fmt.Errorf("k6: decode run logs response: %w", err)
	}
	resultType := wire.Data.ResultType
	if resultType == "" {
		resultType = "streams"
	}
	result := &loki.QueryResponse{
		Status:    wire.Status,
		ErrorType: wire.ErrorType,
		Error:     wire.Error,
		Data: loki.QueryResultData{
			ResultType: resultType,
			Result:     make([]loki.StreamEntry, 0, len(wire.Data.Result)),
			Stats:      wire.Data.Stats,
			Notices:    wire.Data.Notices,
		},
	}
	for streamIndex, stream := range wire.Data.Result {
		entry := loki.StreamEntry{
			Stream: stream.Stream,
			Values: make([]loki.LogEntry, 0, len(stream.Values)),
		}
		for valueIndex, value := range stream.Values {
			if len(value) < 2 {
				return nil, fmt.Errorf("k6: decode run logs response: stream %d value %d has fewer than two fields", streamIndex, valueIndex)
			}
			var timestamp, line string
			if err := json.Unmarshal(value[0], &timestamp); err != nil {
				return nil, fmt.Errorf("k6: decode run logs timestamp at stream %d value %d: %w", streamIndex, valueIndex, err)
			}
			if err := json.Unmarshal(value[1], &line); err != nil {
				return nil, fmt.Errorf("k6: decode run log line at stream %d value %d: %w", streamIndex, valueIndex, err)
			}
			logEntry := loki.LogEntry{Timestamp: timestamp, Line: line}
			if len(value) > 2 && string(value[2]) != "null" {
				if err := json.Unmarshal(value[2], &logEntry.StructuredMetadata); err != nil {
					return nil, fmt.Errorf("k6: decode run log metadata at stream %d value %d: %w", streamIndex, valueIndex, err)
				}
			}
			entry.Values = append(entry.Values, logEntry)
		}
		result.Data.Result = append(result.Data.Result, entry)
	}
	return result, nil
}

// InsightExecution identifies one Cloud Insights analysis execution.
type InsightExecution struct {
	ID      string `json:"id"`
	Version int    `json:"version"`
}

// InsightExecutionsResponse is the execution list for one test run.
type InsightExecutionsResponse struct {
	Schema     string             `json:"$schema,omitempty"`
	Executions []InsightExecution `json:"executions"`
}

// InsightAudit defines one Cloud Insights audit.
type InsightAudit struct {
	CategoryID  string  `json:"category_id"`
	Description string  `json:"description"`
	GroupID     string  `json:"group_id"`
	ID          string  `json:"id"`
	Title       string  `json:"title"`
	Weight      float64 `json:"weight"`
}

// InsightAuditsResponse is the audit definition list for one execution.
type InsightAuditsResponse struct {
	Schema string         `json:"$schema,omitempty"`
	Audits []InsightAudit `json:"audits"`
}

// InsightScore is a numeric or binary Cloud Insights score.
type InsightScore struct {
	Type  string          `json:"type"`
	Value json.RawMessage `json:"value"`
}

// InsightAuditResult is the outcome of one Cloud Insights audit.
type InsightAuditResult struct {
	AuditID      string          `json:"audit_id"`
	Explanation  string          `json:"explanation,omitempty"`
	Score        *InsightScore   `json:"score,omitempty"`
	Status       string          `json:"status"`
	StatusReason string          `json:"status_reason,omitempty"`
	Actions      json.RawMessage `json:"actions,omitempty"`
}

// InsightAuditResultsResponse is the audit result list for one execution.
type InsightAuditResultsResponse struct {
	Schema string               `json:"$schema,omitempty"`
	Audits []InsightAuditResult `json:"audits"`
}

// JoinedInsightAudit keeps an audit definition and its optional result together.
type JoinedInsightAudit struct {
	Definition InsightAudit        `json:"definition"`
	Result     *InsightAuditResult `json:"result,omitempty"`
}

// RunInsights is the joined Cloud Insights view for one test run.
type RunInsights struct {
	Execution        *InsightExecution    `json:"execution"`
	ExecutionSchema  string               `json:"execution_schema,omitempty"`
	DefinitionSchema string               `json:"definition_schema,omitempty"`
	ResultSchema     string               `json:"result_schema,omitempty"`
	Audits           []JoinedInsightAudit `json:"audits"`
	UnmatchedResults []InsightAuditResult `json:"unmatched_results,omitempty"`
}

func joinRunInsights(
	execution *InsightExecution,
	executionSchema string,
	definitions *InsightAuditsResponse,
	results *InsightAuditResultsResponse,
) *RunInsights {
	joined := &RunInsights{
		Execution:        execution,
		ExecutionSchema:  executionSchema,
		DefinitionSchema: definitions.Schema,
		ResultSchema:     results.Schema,
		Audits:           make([]JoinedInsightAudit, 0, len(definitions.Audits)),
		UnmatchedResults: []InsightAuditResult{},
	}
	resultsByID := make(map[string]InsightAuditResult, len(results.Audits))
	for _, result := range results.Audits {
		resultsByID[result.AuditID] = result
	}
	for _, definition := range definitions.Audits {
		entry := JoinedInsightAudit{Definition: definition}
		if result, ok := resultsByID[definition.ID]; ok {
			resultCopy := result
			entry.Result = &resultCopy
			delete(resultsByID, definition.ID)
		}
		joined.Audits = append(joined.Audits, entry)
	}
	for _, result := range results.Audits {
		if _, ok := resultsByID[result.AuditID]; ok {
			joined.UnmatchedResults = append(joined.UnmatchedResults, result)
			delete(resultsByID, result.AuditID)
		}
	}
	return joined
}

// ListRunLogs lists logs for one test run.
func (c *cloudOperations) ListRunLogs(
	ctx context.Context, runID int, request RunLogsRequest,
) (*loki.QueryResponse, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	q := url.Values{}
	q.Set("query", request.Query)
	q.Set("direction", request.Direction)
	q.Set("start", strconv.FormatInt(request.Start.Unix(), 10))
	q.Set("end", strconv.FormatInt(request.End.Unix(), 10))
	q.Set("limit", strconv.Itoa(request.Limit))
	headers := http.Header{}
	headers.Set("X-K6testrun-Id", strconv.Itoa(runID))
	resp, err := c.executor.doCloud(ctx, cloudRequest{
		Target: cloudTargetLogs, Auth: cloudAuthConfigured, Method: http.MethodGet,
		Path: runLogsPath + "?" + q.Encode(), Accept: "application/json", Headers: headers,
	})
	if err != nil {
		return nil, fmt.Errorf("k6: list run logs: %w", err)
	}
	if err := checkCloudStatus(resp, "list run logs", http.StatusOK); err != nil {
		return nil, err
	}
	return decodeRunLogs(resp.Body)
}

// ListInsightExecutions lists Cloud Insights executions for one test run.
func (c *cloudOperations) ListInsightExecutions(
	ctx context.Context, runID int,
) (*InsightExecutionsResponse, error) {
	path := fmt.Sprintf("%s/%d/executions", runInsightsPath, runID)
	resp, err := c.executor.doCloud(ctx, cloudRequest{
		Target: cloudTargetInsights, Auth: cloudAuthConfigured, Method: http.MethodGet, Path: path, Accept: "application/json",
	})
	if err != nil {
		return nil, fmt.Errorf("k6: list insight executions: %w", err)
	}
	if err := checkCloudStatus(resp, "list insight executions", http.StatusOK); err != nil {
		return nil, err
	}
	result, err := decodeCloudJSON[InsightExecutionsResponse](resp)
	if err != nil {
		return nil, err
	}
	if result.Executions == nil {
		result.Executions = []InsightExecution{}
	}
	return &result, nil
}

// ListInsightAudits lists audit definitions for one Cloud Insights execution.
func (c *cloudOperations) ListInsightAudits(
	ctx context.Context, runID int, executionID string,
) (*InsightAuditsResponse, error) {
	path := fmt.Sprintf("%s/%d/executions/%s/audits", runInsightsPath, runID, url.PathEscape(executionID))
	resp, err := c.executor.doCloud(ctx, cloudRequest{
		Target: cloudTargetInsights, Auth: cloudAuthConfigured, Method: http.MethodGet, Path: path, Accept: "application/json",
	})
	if err != nil {
		return nil, fmt.Errorf("k6: list insight audits: %w", err)
	}
	if err := checkCloudStatus(resp, "list insight audits", http.StatusOK); err != nil {
		return nil, err
	}
	result, err := decodeCloudJSON[InsightAuditsResponse](resp)
	if err != nil {
		return nil, err
	}
	if result.Audits == nil {
		result.Audits = []InsightAudit{}
	}
	return &result, nil
}

// ListInsightAuditResults lists audit results for one Cloud Insights execution.
func (c *cloudOperations) ListInsightAuditResults(
	ctx context.Context, runID int, executionID string,
) (*InsightAuditResultsResponse, error) {
	path := fmt.Sprintf("%s/%d/executions/%s/audits/results", runInsightsPath, runID, url.PathEscape(executionID))
	resp, err := c.executor.doCloud(ctx, cloudRequest{
		Target: cloudTargetInsights, Auth: cloudAuthConfigured, Method: http.MethodGet, Path: path, Accept: "application/json",
	})
	if err != nil {
		return nil, fmt.Errorf("k6: list insight audit results: %w", err)
	}
	if err := checkCloudStatus(resp, "list insight audit results", http.StatusOK); err != nil {
		return nil, err
	}
	result, err := decodeCloudJSON[InsightAuditResultsResponse](resp)
	if err != nil {
		return nil, err
	}
	if result.Audits == nil {
		result.Audits = []InsightAuditResult{}
	}
	return &result, nil
}
