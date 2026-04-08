package loki_test

import (
	"bytes"
	"strings"
	"testing"

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
					Values: [][]string{{
						"1775637286777686890",
						`level=info ts=2026-04-08T08:34:46.77768689Z caller=retention.go:113 msg="deleting block" blockID=47f92c6c tenantID=120351`,
					}},
				},
				{
					Stream: map[string]string{"app": "adaptive-traces", "namespace": "tempo-prod"},
					Values: [][]string{{
						"1775637286554667000",
						`{"level":"info","ts":1775637286.554667,"caller":"zap@v1.1.7/zap.go:125","msg":"/adaptive-traces/api/v1/config","component":"api","status":200,"method":"GET","path":"/adaptive-traces/api/v1/config","query":"","tenant":1336544}`,
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
				Values: [][]string{{
					"1775637286777686890",
					`level=warn caller=retention.go:113 msg="compaction delayed" tenantID=120351`,
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

func TestFormatQueryTable_FallsBackToPlainMessage(t *testing.T) {
	resp := &loki.QueryResponse{
		Data: loki.QueryResultData{
			Result: []loki.StreamEntry{{
				Values: [][]string{{"1775637286777686890", "plain unstructured log line"}},
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
				Values: [][]string{{"1775637286777686890", `msg=login failed for user=bob`}},
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
				Values: [][]string{{"1775637286777686890", `level=info request completed status=200`}},
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

func TestFormatQueryRaw_PrintsOriginalLineBodies(t *testing.T) {
	resp := &loki.QueryResponse{
		Data: loki.QueryResultData{
			Result: []loki.StreamEntry{{
				Values: [][]string{{"1", "first line"}, {"2", "second line"}},
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
