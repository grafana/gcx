package loki

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/grafana/gcx/internal/httputils"
	"github.com/grafana/gcx/internal/queryerror"
)

// ScanEstimate describes indexed data matching a selector, not a billing or
// runtime byte ceiling. Bytes is nil when the volume is unknown, never a fake zero.
type ScanEstimate struct {
	DatasourceUID    string    `json:"datasourceUid"`
	Query            string    `json:"query"`
	Start            time.Time `json:"start"`
	End              time.Time `json:"end"`
	Bytes            *int64    `json:"estimatedBytes"`
	Threshold        int64     `json:"approvalThresholdBytes"`
	ApprovalRequired bool      `json:"approvalRequired"`
	Reason           string    `json:"reason,omitempty"`
	Caveat           string    `json:"caveat"`
	Hints            []string  `json:"hints"`
}

// FormatScanEstimate renders estimate-only output without mixing it with logs.
func FormatScanEstimate(w io.Writer, e *ScanEstimate) error {
	volume := "unknown"
	if e.Bytes != nil {
		volume = fmt.Sprintf("%.3f GB (%d bytes)", float64(*e.Bytes)/1e9, *e.Bytes)
	}
	_, err := fmt.Fprintf(w, "Datasource: %s\nQuery: %s\nRange: %s to %s\nEstimated scan: %s\nApproval threshold: %.3f GB\nApproval required: %t\n%s\n", e.DatasourceUID, e.Query, e.Start.Format(time.RFC3339Nano), e.End.Format(time.RFC3339Nano), volume, float64(e.Threshold)/1e9, e.ApprovalRequired, e.Caveat)
	if err != nil {
		return err
	}
	if e.Reason != "" {
		if _, err := fmt.Fprintln(w, e.Reason); err != nil {
			return err
		}
	}
	for _, hint := range e.Hints {
		if _, err := fmt.Fprintln(w, hint); err != nil {
			return err
		}
	}
	return nil
}

// EstimateScan uses the same datasource resource endpoint as Grafana Explore.
// Only simple log-stream queries with an explicit range are estimated in v1.
func (c *Client) EstimateScan(ctx context.Context, uid string, req QueryRequest) (*ScanEstimate, error) {
	e := &ScanEstimate{
		DatasourceUID: uid, Query: req.Query, Start: req.Start, End: req.End,
		Caveat: "Approximate indexed volume; excludes ingester data and may count chunks more than once. Not a scan ceiling or billable GB.",
		Hints: []string{
			"Narrow the time range with --since and select specific indexed labels.",
			"Put inexpensive line filters before parsers where semantics permit.",
			"--limit caps returned lines, not scanned bytes.",
		},
	}
	selector, ok := logSelector(req.Query)
	if !ok || !req.IsRange() {
		e.Reason = "Volume estimation requires a simple log-stream expression and an explicit time range; metric LogQL and complex expressions are not estimated."
		return e, nil
	}
	values := url.Values{"query": {selector}, "start": {strconv.FormatInt(req.Start.UnixNano(), 10)}, "end": {strconv.FormatInt(req.End.UnixNano(), 10)}}
	path := "/api/datasources/uid/" + url.PathEscape(uid) + "/resources/index/stats"
	r, err := http.NewRequestWithContext(ctx, http.MethodGet, c.restConfig.Host+path+"?"+values.Encode(), nil)
	if err != nil {
		return nil, fmt.Errorf("create scan estimate request: %w", err)
	}
	resp, err := c.httpClient.Do(r)
	if err != nil {
		return nil, fmt.Errorf("estimate scan volume: %w", err)
	}
	defer resp.Body.Close()
	body, err := httputils.ReadResponseBody(resp.Body, httputils.DefaultResponseLimit)
	if err != nil {
		return nil, fmt.Errorf("read scan estimate: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, queryerror.FromBody("loki", "scan estimate", resp.StatusCode, body)
	}
	var stats struct {
		Bytes *int64 `json:"bytes"`
	}
	if err := json.Unmarshal(body, &stats); err != nil || stats.Bytes == nil || *stats.Bytes < 0 {
		e.Reason = "The index statistics response did not contain a valid non-negative byte count."
		return e, nil
	}
	e.Bytes = stats.Bytes
	return e, nil
}

// logSelector deliberately recognizes a bounded subset, not the LogQL grammar.
// Quoted braces/operators cannot introduce another selector. Any unquoted
// grouping, range, comment or additional selector makes the estimate unknown.
func logSelector(expr string) (string, bool) {
	expr = strings.TrimSpace(expr)
	if !strings.HasPrefix(expr, "{") {
		return "", false
	}
	var quote byte
	escaped := false
	end := -1
	for i := 1; i < len(expr); i++ {
		ch := expr[i]
		if quote != 0 {
			if escaped {
				escaped = false
				continue
			}
			if ch == '\\' && quote != '`' {
				escaped = true
				continue
			}
			if ch == quote {
				quote = 0
			}
			continue
		}
		switch ch {
		case '"', '`':
			quote = ch
		case '}':
			if end != -1 {
				return "", false
			}
			end = i
		case '{', '[', ']', '(', ')', '#':
			return "", false
		}
	}
	if quote != 0 || end == -1 {
		return "", false
	}
	tail := strings.TrimSpace(expr[end+1:])
	if tail != "" && !strings.HasPrefix(tail, "|") && !strings.HasPrefix(tail, "!=") && !strings.HasPrefix(tail, "!~") {
		return "", false
	}
	return expr[:end+1], true
}
