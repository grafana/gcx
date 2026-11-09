package loki

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/grafana/gcx/internal/httputils"
	"github.com/grafana/gcx/internal/queryerror"
)

const droppedLogsOperation = "dropped logs query"

// QueryDroppedLogs fetches lines Adaptive Logs dropped from the time machine.
// The response holds only dropped lines, each tagged with
// TimeMachineMetadataKey; time machine warnings (such as a window older than
// its retention floor) are returned as notices.
func (c *Client) QueryDroppedLogs(ctx context.Context, datasourceUID string, req QueryRequest) (*QueryResponse, error) {
	start, end := req.EffectiveRange(time.Now())

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, c.restConfig.Host+c.buildDroppedLogsPath(datasourceUID), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	q := url.Values{}
	q.Set("query", req.Query)
	q.Set("start", strconv.FormatInt(start.UnixNano(), 10))
	q.Set("end", strconv.FormatInt(end.UnixNano(), 10))
	q.Set("direction", "backward")
	if req.Limit > 0 {
		q.Set("limit", strconv.Itoa(req.Limit))
	}
	httpReq.URL.RawQuery = q.Encode()

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to query dropped logs: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := httputils.ReadResponseBody(resp.Body, httputils.DefaultResponseLimit)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, droppedLogsError(resp.StatusCode, respBody)
	}

	return decodeDroppedLogs(respBody)
}

// Grafana's Loki datasource resource calls only reach /loki/api/v1, so the
// time machine route needs the datasource proxy.
func (c *Client) buildDroppedLogsPath(datasourceUID string) string {
	return fmt.Sprintf("/api/datasources/proxy/uid/%s/time-machine/loki/api/v1/query_range", url.PathEscape(datasourceUID))
}

type droppedLogsCostBody struct {
	EstimatedRecordBytes *uint64 `json:"estimated_record_bytes"`
	LimitBytes           uint64  `json:"limit_bytes"`
	HardLimit            bool    `json:"hard_limit"`
}

func droppedLogsError(status int, body []byte) error {
	var cost droppedLogsCostBody
	if status == http.StatusBadRequest && json.Unmarshal(body, &cost) == nil && cost.EstimatedRecordBytes != nil {
		kind := "budget"
		if cost.HardLimit {
			kind = "hard limit"
		}
		msg := fmt.Sprintf("the query would read an estimated %d bytes, over the time machine %s of %d bytes; narrow the stream selector or the time range",
			*cost.EstimatedRecordBytes, kind, cost.LimitBytes)
		return queryerror.New("loki", droppedLogsOperation, status, msg, "")
	}

	apiErr := queryerror.FromBody("loki", droppedLogsOperation, status, body).WithAvailability(true, true)
	switch status {
	case http.StatusNotFound:
		return fmt.Errorf("%w (HTTP 404 can mean the datasource's Loki endpoint does not serve the Adaptive Logs time machine: it needs a Grafana Cloud Logs datasource on a cell with the time machine enabled)", apiErr)
	case http.StatusGatewayTimeout:
		return fmt.Errorf("%w (the time machine query timed out; narrow the stream selector or the time range)", apiErr)
	}
	return apiErr
}

type droppedLogsWire struct {
	Status   string   `json:"status"`
	Warnings []string `json:"warnings"`
	Data     struct {
		ResultType string `json:"resultType"`
		Result     []struct {
			Stream map[string]string `json:"stream"`
			Values []json.RawMessage `json:"values"`
		} `json:"result"`
		Stats *QueryStats `json:"stats"`
	} `json:"data"`
}

type droppedLogsEntryMetadata struct {
	StructuredMetadata map[string]string `json:"structuredMetadata"`
	Parsed             map[string]string `json:"parsed"`
}

func decodeDroppedLogs(body []byte) (*QueryResponse, error) {
	var wire droppedLogsWire
	if err := json.Unmarshal(body, &wire); err != nil {
		return nil, fmt.Errorf("failed to parse dropped logs response: %w", err)
	}
	if wire.Data.ResultType != "" && wire.Data.ResultType != "streams" {
		return nil, fmt.Errorf("unexpected dropped logs result type %q (want streams)", wire.Data.ResultType)
	}

	out := &QueryResponse{
		Status: wire.Status,
		Data: QueryResultData{
			ResultType: "streams",
			Result:     make([]StreamEntry, 0, len(wire.Data.Result)),
			Stats:      wire.Data.Stats,
		},
	}
	for _, w := range wire.Warnings {
		out.Data.Notices = append(out.Data.Notices, FrameNotice{Severity: "warning", Text: w})
	}

	for _, stream := range wire.Data.Result {
		entry := StreamEntry{Stream: stream.Stream, Values: make([]LogEntry, 0, len(stream.Values))}
		for _, raw := range stream.Values {
			logEntry, err := decodeDroppedLogsEntry(raw)
			if err != nil {
				return nil, err
			}
			entry.Values = append(entry.Values, logEntry)
		}
		out.Data.Result = append(out.Data.Result, entry)
	}

	return out, nil
}

// decodeDroppedLogsEntry parses Loki's [ts, line] or categorize-labels
// [ts, line, {"structuredMetadata": ..., "parsed": ...}] value tuple.
func decodeDroppedLogsEntry(raw json.RawMessage) (LogEntry, error) {
	var parts []json.RawMessage
	if err := json.Unmarshal(raw, &parts); err != nil {
		return LogEntry{}, fmt.Errorf("failed to parse dropped logs entry: %w", err)
	}
	if len(parts) < 2 {
		return LogEntry{}, errors.New("failed to parse dropped logs entry: want [timestamp, line, ...]")
	}

	var entry LogEntry
	if err := json.Unmarshal(parts[0], &entry.Timestamp); err != nil {
		return LogEntry{}, fmt.Errorf("failed to parse dropped logs timestamp: %w", err)
	}
	if err := json.Unmarshal(parts[1], &entry.Line); err != nil {
		return LogEntry{}, fmt.Errorf("failed to parse dropped logs line: %w", err)
	}
	if len(parts) > 2 {
		var md droppedLogsEntryMetadata
		if err := json.Unmarshal(parts[2], &md); err != nil {
			return LogEntry{}, fmt.Errorf("failed to parse dropped logs entry metadata: %w", err)
		}
		if len(md.StructuredMetadata) > 0 {
			entry.StructuredMetadata = md.StructuredMetadata
		}
		if len(md.Parsed) > 0 {
			entry.Parsed = md.Parsed
		}
	}

	return entry, nil
}
