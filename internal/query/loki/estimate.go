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
	DatasourceUID    string     `json:"datasourceUid"`
	Query            string     `json:"query"`
	Start            time.Time  `json:"start"`
	End              time.Time  `json:"end"`
	ScanStart        *time.Time `json:"scanStart,omitempty"`
	ScanEnd          *time.Time `json:"scanEnd,omitempty"`
	Bytes            *int64     `json:"estimatedBytes"`
	Threshold        int64      `json:"approvalThresholdBytes"`
	ApprovalRequired bool       `json:"approvalRequired"`
	Reason           string     `json:"reason,omitempty"`
	Caveat           string     `json:"caveat"`
	Hints            []string   `json:"hints"`
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
	if e.ScanStart != nil && e.ScanEnd != nil {
		if _, err := fmt.Fprintf(w, "Scan range: %s to %s\n", e.ScanStart.Format(time.RFC3339Nano), e.ScanEnd.Format(time.RFC3339Nano)); err != nil {
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
// Single-selector metric queries use the same indexed volume as log queries,
// with their lookback and offset included in the scan interval. The returned
// estimate retains query context even when the statistics request fails.
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
	if !req.IsRange() {
		e.Start, e.End = req.EvaluationTime, req.EvaluationTime
	}
	selector, lookback, offset, ok := scanSelector(req.Query)
	if !ok {
		e.Reason = "Volume estimation supports simple log queries and single-selector metric expressions with one literal lookback and an optional positive offset; this expression is unsupported."
		e.Hints = []string{"Simplify to a supported single-selector expression, or obtain approval for unknown volume. Narrowing the time range does not resolve unsupported syntax."}
		return e, nil
	}
	if !req.IsRange() && (lookback == 0 || req.EvaluationTime.IsZero()) {
		e.Reason = "Volume estimation requires an explicit log-query range or a metric lookback with a fixed evaluation time."
		e.Hints = []string{"Supply --since or --from/--to for a log query, or obtain approval for unknown volume."}
		return e, nil
	}
	scanStart, scanEnd := e.Start.Add(-offset).Add(-lookback), e.End.Add(-offset)
	if !time.Unix(0, scanStart.UnixNano()).Equal(scanStart) || !time.Unix(0, scanEnd.UnixNano()).Equal(scanEnd) {
		e.Reason = "The effective scan interval is outside the supported nanosecond timestamp range."
		e.Hints = []string{"Use representable timestamps, lookback, and offset, or obtain approval for unknown volume."}
		return e, nil
	}
	if !scanStart.Equal(e.Start) || !scanEnd.Equal(e.End) {
		e.ScanStart, e.ScanEnd = &scanStart, &scanEnd
	}
	values := url.Values{"query": {selector}, "start": {strconv.FormatInt(scanStart.UnixNano(), 10)}, "end": {strconv.FormatInt(scanEnd.UnixNano(), 10)}}
	path := "/api/datasources/uid/" + url.PathEscape(uid) + "/resources/index/stats"
	r, err := http.NewRequestWithContext(ctx, http.MethodGet, c.restConfig.Host+path+"?"+values.Encode(), nil)
	if err != nil {
		return e, fmt.Errorf("create scan estimate request: %w", err)
	}
	resp, err := c.httpClient.Do(r)
	if err != nil {
		return e, fmt.Errorf("estimate scan volume: %w", err)
	}
	defer resp.Body.Close()
	body, err := httputils.ReadResponseBody(resp.Body, httputils.DefaultResponseLimit)
	if err != nil {
		return e, fmt.Errorf("read scan estimate: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return e, queryerror.FromBody("loki", "scan estimate", resp.StatusCode, body)
	}
	var stats struct {
		Bytes *int64 `json:"bytes"`
	}
	if err := json.Unmarshal(body, &stats); err != nil || stats.Bytes == nil || *stats.Bytes < 0 {
		e.Reason = "The index statistics response did not contain a valid non-negative byte count."
		e.Hints = []string{ScanStatsRecoveryHint}
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
