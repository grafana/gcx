package query_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/agent"
	dsquery "github.com/grafana/gcx/internal/datasources/query"
	"github.com/grafana/gcx/internal/query/loki"
	querysql "github.com/grafana/gcx/internal/query/sql"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func rows(n int) [][]any {
	out := make([][]any, n)
	for i := range out {
		out[i] = []any{i}
	}
	return out
}

func TestSurfaceRowLimits(t *testing.T) {
	t.Run("capped: drops sentinel row and warns", func(t *testing.T) {
		resp := &querysql.QueryResponse{Rows: rows(101)}
		var buf bytes.Buffer
		dsquery.SurfaceRowLimits(&buf, resp, true, 100, 1000)

		assert.Len(t, resp.Rows, 100, "sentinel row dropped before output")
		assert.Contains(t, buf.String(), "showing the first 100 rows")
	})

	t.Run("capped but within limit: no warning", func(t *testing.T) {
		resp := &querysql.QueryResponse{Rows: rows(42)}
		var buf bytes.Buffer
		dsquery.SurfaceRowLimits(&buf, resp, true, 100, 1000)

		assert.Len(t, resp.Rows, 42)
		assert.Empty(t, buf.String())
	})

	t.Run("not capped: no truncation, no gcx warning", func(t *testing.T) {
		resp := &querysql.QueryResponse{Rows: rows(500)}
		var buf bytes.Buffer
		dsquery.SurfaceRowLimits(&buf, resp, false, 100, 1000)

		assert.Len(t, resp.Rows, 500, "user-controlled result left intact")
		assert.NotContains(t, buf.String(), "showing the first")
	})

	t.Run("server-side notices surfaced verbatim", func(t *testing.T) {
		resp := &querysql.QueryResponse{
			Rows:    rows(100),
			Notices: []string{"Results have been limited to 100 because the SQL row limit was reached"},
		}
		var buf bytes.Buffer
		dsquery.SurfaceRowLimits(&buf, resp, false, 100, 1000)

		assert.Contains(t, buf.String(), "Results have been limited to 100")
	})

	t.Run("both gcx truncation and server notice fire", func(t *testing.T) {
		resp := &querysql.QueryResponse{
			Rows:    rows(101),
			Notices: []string{"server notice"},
		}
		var buf bytes.Buffer
		dsquery.SurfaceRowLimits(&buf, resp, true, 100, 1000)

		assert.Len(t, resp.Rows, 100)
		// One emitted line per source (gcx truncation + server notice), in both
		// TTY and agent-JSON modes since each EmitHint prints exactly one line.
		lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
		assert.Len(t, lines, 2, "one hint line per source")
	})
}

func TestSurfaceLogLimit(t *testing.T) {
	for _, tc := range []struct {
		name         string
		count, limit int
		warn         bool
	}{
		{"empty", 0, 50, false}, {"below", 49, 50, false}, {"at cap", 50, 50, true},
		{"above cap", 51, 50, true}, {"uncapped", 50, 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resp := &loki.QueryResponse{Data: loki.QueryResultData{Result: []loki.StreamEntry{
				{Values: make([]loki.LogEntry, tc.count/2)}, {Values: make([]loki.LogEntry, tc.count-tc.count/2)},
			}}}
			var stderr bytes.Buffer
			dsquery.SurfaceLogLimit(&stderr, resp, tc.limit)
			if tc.warn {
				assert.Contains(t, stderr.String(), "results may be incomplete")
			} else {
				assert.Empty(t, stderr.String())
			}
		})
	}
}

func TestSurfaceDroppedLogs(t *testing.T) {
	sampled := func(value string) loki.LogEntry {
		return loki.LogEntry{StructuredMetadata: map[string]string{loki.SampledMetadataKey: value}}
	}
	notice := loki.FrameNotice{Severity: "warning", Text: "Some logs may have been dropped by Adaptive Logs sampling"}
	const command = "gcx logs query '{job=\"app\"}' --dropped-logs"

	tests := []struct {
		name        string
		resp        *loki.QueryResponse
		wantSummary string
	}{
		{
			name: "no evidence",
			resp: &loki.QueryResponse{Data: loki.QueryResultData{Result: []loki.StreamEntry{{Values: []loki.LogEntry{{Line: "x"}}}}}},
		},
		{
			name:        "numeric marker",
			resp:        &loki.QueryResponse{Data: loki.QueryResultData{Result: []loki.StreamEntry{{Values: []loki.LogEntry{sampled("90.00"), sampled("99.99")}}}}},
			wantSummary: "2 returned line(s) were sampled by Adaptive Logs (up to 99.99% of similar lines dropped); the dropped lines may be recoverable from the time machine",
		},
		{
			name:        "legacy marker",
			resp:        &loki.QueryResponse{Data: loki.QueryResultData{Result: []loki.StreamEntry{{Values: []loki.LogEntry{sampled("true")}}}}},
			wantSummary: "1 returned line(s) were sampled by Adaptive Logs; the dropped lines may be recoverable from the time machine",
		},
		{
			name:        "notice only",
			resp:        &loki.QueryResponse{Data: loki.QueryResultData{Notices: []loki.FrameNotice{notice}}},
			wantSummary: "Adaptive Logs sampling dropped some matching lines; they may be recoverable from the time machine",
		},
	}
	for _, tt := range tests {
		for _, agentMode := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/agent=%t", tt.name, agentMode), func(t *testing.T) {
				agent.SetFlag(agentMode)
				t.Cleanup(func() { agent.SetFlag(false) })

				var stderr bytes.Buffer
				dsquery.SurfaceDroppedLogs(&stderr, tt.resp, command)

				switch {
				case tt.wantSummary == "":
					assert.Empty(t, stderr.String())
				case agentMode:
					var event map[string]string
					require.NoError(t, json.Unmarshal(stderr.Bytes(), &event))
					assert.Equal(t, map[string]string{"class": "hint", "summary": tt.wantSummary, "command": command}, event)
				default:
					assert.Equal(t, "hint: "+tt.wantSummary+": "+command+"\n", stderr.String())
				}
			})
		}
	}
}

func TestSurfaceNotices(t *testing.T) {
	resp := &loki.QueryResponse{Data: loki.QueryResultData{Notices: []loki.FrameNotice{
		{Severity: "warning", Text: "first"}, {Severity: "warning", Text: "second"},
	}}}
	var stderr bytes.Buffer
	dsquery.SurfaceNotices(&stderr, resp)
	assert.Equal(t, "hint: first\nhint: second\n", stderr.String())
}
