package loki_test

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/grafana/gcx/internal/arrowtable"
	"github.com/grafana/gcx/internal/query/loki"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFormatQueryTable_HumanFriendlyMixedFormats(t *testing.T) {
	resp := &loki.QueryResponse{
		Data: loki.QueryResultData{
			Result: []loki.StreamEntry{
				{
					Stream: map[string]string{"namespace": "tempo-prod"},
					Values: []loki.LogEntry{{
						Timestamp: "1775637286777686890",
						Line:      `level=info ts=2026-04-08T08:34:46.77768689Z caller=retention.go:113 msg="deleting block" blockID=47f92c6c tenantID=120351`,
					}},
				},
				{
					Stream: map[string]string{"app": "adaptive-traces", "namespace": "tempo-prod"},
					Values: []loki.LogEntry{{
						Timestamp: "1775637286554667000",
						Line:      `{"level":"info","ts":1775637286.554667,"caller":"zap@v1.1.7/zap.go:125","msg":"/adaptive-traces/api/v1/config","component":"api","status":200,"method":"GET","path":"/adaptive-traces/api/v1/config","query":"","tenant":1336544}`,
					}},
				},
			},
		},
	}

	var buf bytes.Buffer
	err := loki.FormatQueryTable(&buf, resp)
	require.NoError(t, err)

	out := buf.String()
	assert.Contains(t, out, "TIME")
	assert.Contains(t, out, "LEVEL")
	assert.Contains(t, out, "SOURCE")
	assert.Contains(t, out, "STREAM")
	assert.Contains(t, out, "MESSAGE")
	assert.Contains(t, out, "DETAILS")

	assert.Contains(t, out, "2026-04-08T08:34:46.77768689Z")
	assert.Contains(t, out, "retention.go:113")
	assert.Contains(t, out, "deleting block")
	assert.Contains(t, out, "blockID=47f92c6c")
	assert.Contains(t, out, "tenantID=120351")
	assert.Contains(t, out, "namespace=tempo-prod")

	assert.Contains(t, out, "api")
	assert.Contains(t, out, "GET /adaptive-traces/api/v1/config")
	assert.Contains(t, out, "status=200")
	assert.Contains(t, out, "tenant=1336544")
	assert.Contains(t, out, "caller=zap@v1.1.7/zap.go:125")
}

func TestFormatQueryTableWide_IncludesTimestampAndLabels(t *testing.T) {
	resp := &loki.QueryResponse{
		Data: loki.QueryResultData{
			Result: []loki.StreamEntry{{
				Stream: map[string]string{
					"app":       "tempo",
					"namespace": "prod",
					"__meta":    "hidden",
				},
				Values: []loki.LogEntry{{
					Timestamp: "1775637286777686890",
					Line:      `level=warn caller=retention.go:113 msg="compaction delayed" tenantID=120351`,
				}},
			}},
		},
	}

	var buf bytes.Buffer
	err := loki.FormatQueryTableWide(&buf, resp)
	require.NoError(t, err)

	out := buf.String()
	assert.Contains(t, out, "TIME")
	assert.Contains(t, out, "LEVEL")
	assert.Contains(t, out, "SOURCE")
	assert.Contains(t, out, "APP")
	assert.Contains(t, out, "NAMESPACE")
	assert.Contains(t, out, "MESSAGE")
	assert.Contains(t, out, "DETAILS")
	assert.NotContains(t, out, "__META")

	assert.Contains(t, out, "2026-04-08T08:34:46.77768689Z")
	assert.Contains(t, out, "warn")
	assert.Contains(t, out, "retention.go:113")
	assert.Contains(t, out, "tempo")
	assert.Contains(t, out, "prod")
	assert.Contains(t, out, "compaction delayed")
	assert.Contains(t, out, "tenantID=120351")
}

func TestFormatQueryArrow_IncludesTimestampAndLabels(t *testing.T) {
	resp := &loki.QueryResponse{
		Data: loki.QueryResultData{
			Result: []loki.StreamEntry{{
				Stream: map[string]string{
					"app":       "tempo",
					"namespace": "prod",
					"__meta":    "hidden",
				},
				Values: []loki.LogEntry{{
					Timestamp: "1775637286777686890",
					Line:      `level=warn caller=retention.go:113 msg="compaction delayed" tenantID=120351`,
				}},
			}},
		},
	}

	var buf bytes.Buffer
	require.NoError(t, loki.FormatQueryArrow(&buf, resp))

	headers, rows, err := arrowtable.ReadStream(&buf)
	require.NoError(t, err)
	assert.Equal(t, []string{"TIME", "LEVEL", "SOURCE", "APP", "NAMESPACE", "MESSAGE", "DETAILS"}, headers)
	require.Len(t, rows, 1)

	row := rows[0]
	assert.Equal(t, time.Unix(0, 1775637286777686890).UTC(), row[0])
	assert.Equal(t, "warn", row[1])
	assert.Equal(t, "retention.go:113", row[2])
	assert.Equal(t, "tempo", row[3])
	assert.Equal(t, "prod", row[4])
	assert.Contains(t, row[5], "compaction delayed")
	assert.Contains(t, row[6], "tenantID=120351")
}

func TestFormatQueryArrow_NoData(t *testing.T) {
	resp := &loki.QueryResponse{Data: loki.QueryResultData{Result: nil}}

	var buf bytes.Buffer
	require.NoError(t, loki.FormatQueryArrow(&buf, resp))
	assert.Empty(t, buf.String())
}

func TestFormatMetricQueryArrow(t *testing.T) {
	resp := &loki.MetricQueryResponse{
		Data: loki.MetricQueryData{
			ResultType: "matrix",
			Result: []loki.MetricQuerySample{
				{
					Metric: map[string]string{"level": "error"},
					Values: [][]any{{float64(1700000000), "3.5"}},
				},
			},
		},
	}

	var buf bytes.Buffer
	require.NoError(t, loki.FormatMetricQueryArrow(&buf, resp))

	headers, rows, err := arrowtable.ReadStream(&buf)
	require.NoError(t, err)
	assert.Equal(t, []string{"TIMESTAMP", "VALUE", "LEVEL"}, headers)
	require.Len(t, rows, 1)
	assert.Equal(t, time.Unix(1700000000, 0).UTC(), rows[0][0])
	assert.InEpsilon(t, 3.5, rows[0][1], 0.0001)
	assert.Equal(t, "error", rows[0][2])
}

func TestFormatMetricQueryArrow_NoData(t *testing.T) {
	resp := &loki.MetricQueryResponse{Data: loki.MetricQueryData{Result: nil}}

	var buf bytes.Buffer
	require.NoError(t, loki.FormatMetricQueryArrow(&buf, resp))
	assert.Empty(t, buf.String())
}

func TestFormatQueryTable_FallsBackToPlainMessage(t *testing.T) {
	resp := &loki.QueryResponse{
		Data: loki.QueryResultData{
			Result: []loki.StreamEntry{{
				Values: []loki.LogEntry{{Timestamp: "1775637286777686890", Line: "plain unstructured log line"}},
			}},
		},
	}

	var buf bytes.Buffer
	err := loki.FormatQueryTable(&buf, resp)
	require.NoError(t, err)

	out := strings.TrimSpace(buf.String())
	assert.Contains(t, out, "TIME")
	assert.Contains(t, out, "MESSAGE")
	assert.Contains(t, out, "plain unstructured log line")
	assert.NotContains(t, out, "LEVEL")
	assert.NotContains(t, out, "SOURCE")
	assert.NotContains(t, out, "DETAILS")
}

func TestFormatQueryTable_RejectsAmbiguousLogfmtBareTokens(t *testing.T) {
	resp := &loki.QueryResponse{
		Data: loki.QueryResultData{
			Result: []loki.StreamEntry{{
				Values: []loki.LogEntry{{Timestamp: "1775637286777686890", Line: `msg=login failed for user=bob`}},
			}},
		},
	}

	var buf bytes.Buffer
	err := loki.FormatQueryTable(&buf, resp)
	require.NoError(t, err)

	out := buf.String()
	assert.Contains(t, out, "MESSAGE")
	assert.Contains(t, out, `msg=login failed for user=bob`)
	assert.NotContains(t, out, "DETAILS")
	assert.NotContains(t, out, `failed=""`)
	assert.NotContains(t, out, `for=""`)
}

func TestFormatQueryTable_RejectsAmbiguousLogfmtWithoutQuotedMessage(t *testing.T) {
	resp := &loki.QueryResponse{
		Data: loki.QueryResultData{
			Result: []loki.StreamEntry{{
				Values: []loki.LogEntry{{Timestamp: "1775637286777686890", Line: `level=info request completed status=200`}},
			}},
		},
	}

	var buf bytes.Buffer
	err := loki.FormatQueryTable(&buf, resp)
	require.NoError(t, err)

	out := buf.String()
	assert.Contains(t, out, "MESSAGE")
	assert.Contains(t, out, `level=info request completed status=200`)
	assert.NotContains(t, out, "LEVEL")
	assert.NotContains(t, out, "DETAILS")
}

func TestFormatQueryTable_SurfacesStructuredMetadataInDetails(t *testing.T) {
	// Structured metadata (and parsed labels) are per-line, non-indexed context.
	// They should appear in DETAILS, and detected_level should populate LEVEL
	// when the body carried none, but they must NOT appear in the STREAM column.
	resp := &loki.QueryResponse{
		Data: loki.QueryResultData{
			Result: []loki.StreamEntry{{
				Stream: map[string]string{"service_name": "checkout"},
				Values: []loki.LogEntry{{
					Timestamp:          "1775637286777686890",
					Line:               "a plain unstructured line",
					StructuredMetadata: map[string]string{"detected_level": "error", "trace_id": "abc123"},
				}},
			}},
		},
	}

	var buf bytes.Buffer
	require.NoError(t, loki.FormatQueryTable(&buf, resp))

	out := buf.String()
	assert.Contains(t, out, "LEVEL")
	assert.Contains(t, out, "error")           // detected_level -> LEVEL
	assert.Contains(t, out, "trace_id=abc123") // structured metadata -> DETAILS
	assert.Contains(t, out, "DETAILS")
	// detected_level was consumed into LEVEL, not left dangling in DETAILS.
	assert.NotContains(t, out, "detected_level=")
}

func TestFormatQueryRaw_PrintsOriginalLineBodies(t *testing.T) {
	resp := &loki.QueryResponse{
		Data: loki.QueryResultData{
			Result: []loki.StreamEntry{{
				Values: []loki.LogEntry{{Timestamp: "1", Line: "first line"}, {Timestamp: "2", Line: "second line"}},
			}},
		},
	}

	var buf bytes.Buffer
	err := loki.FormatQueryRaw(&buf, resp)
	require.NoError(t, err)
	assert.Equal(t, "first line\nsecond line\n", buf.String())
}

func TestFormatQueryRaw_EmptyIsSilent(t *testing.T) {
	var buf bytes.Buffer
	err := loki.FormatQueryRaw(&buf, &loki.QueryResponse{})
	require.NoError(t, err)
	assert.Empty(t, buf.String())
}
