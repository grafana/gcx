package sql

import (
	"encoding/json"
	"fmt"
	"strconv"
	"time"
)

// RawQueryRequest is a raw-SQL query request, shared by the dialects (e.g.
// postgres, mysql) whose Grafana datasource plugin expects a plain string
// "format" value. ClickHouse's sqlds-based plugin sends a numeric format
// instead and builds its own body rather than using this helper.
type RawQueryRequest struct {
	RawSQL     string
	Start      time.Time
	End        time.Time
	IntervalMs int64
}

// BuildRawQueryBody assembles the unified query API request body shared by
// the raw-SQL dialects: one query keyed "A" with format:"table", and a time
// range that defaults to the last hour when Start/End are zero, matching how
// these dialects behave when called without an explicit range.
func BuildRawQueryBody(pluginID, datasourceUID string, req RawQueryRequest) ([]byte, error) {
	intervalMs := req.IntervalMs
	if intervalMs == 0 {
		intervalMs = 60000
	}

	from := strconv.FormatInt(req.Start.UnixMilli(), 10)
	to := strconv.FormatInt(req.End.UnixMilli(), 10)
	if req.Start.IsZero() || req.End.IsZero() {
		now := time.Now()
		from = strconv.FormatInt(now.Add(-1*time.Hour).UnixMilli(), 10)
		to = strconv.FormatInt(now.UnixMilli(), 10)
	}

	bodyMap := map[string]any{
		"queries": []any{
			map[string]any{
				"refId":      "A",
				"datasource": map[string]any{"type": pluginID, "uid": datasourceUID},
				"rawSql":     req.RawSQL,
				"format":     "table",
				"intervalMs": intervalMs,
			},
		},
		"from": from,
		"to":   to,
	}

	body, err := json.Marshal(bodyMap)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}
	return body, nil
}
