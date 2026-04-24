package prometheus_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/query/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFormatTable_VectorCollapsesLabelsIntoSeriesColumn(t *testing.T) {
	resp := &prometheus.QueryResponse{
		Status: "success",
		Data: prometheus.ResultData{
			ResultType: "vector",
			Result: []prometheus.Sample{
				{
					Metric: map[string]string{
						"__name__": "up",
						"instance": "localhost:9090",
						"job":      "prometheus",
					},
					Value: []any{float64(1700000000), "1"},
				},
			},
		},
	}

	var buf bytes.Buffer
	require.NoError(t, prometheus.FormatTable(&buf, resp))

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	require.NotEmpty(t, lines)
	assert.Equal(t, []string{"VALUE", "TIMESTAMP", "SERIES"}, strings.Fields(lines[0]))
	require.Len(t, lines, 2)
	assert.Equal(t, "1", strings.Fields(lines[1])[0])
	assert.Contains(t, buf.String(), `{__name__="up",instance="localhost:9090",job="prometheus"}`)
	assert.Contains(t, buf.String(), "2023-11-14T")
}

func TestFormatWideTable_VectorExplodesLabelsIntoColumns(t *testing.T) {
	resp := &prometheus.QueryResponse{
		Status: "success",
		Data: prometheus.ResultData{
			ResultType: "vector",
			Result: []prometheus.Sample{
				{
					Metric: map[string]string{
						"__name__": "up",
						"instance": "localhost:9090",
						"job":      "prometheus",
					},
					Value: []any{float64(1700000000), "1"},
				},
			},
		},
	}

	var buf bytes.Buffer
	require.NoError(t, prometheus.FormatWideTable(&buf, resp))

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	require.NotEmpty(t, lines)
	assert.Equal(t, []string{"__NAME__", "INSTANCE", "JOB", "TIMESTAMP", "VALUE"}, strings.Fields(lines[0]))
	assert.Contains(t, buf.String(), "up")
	assert.Contains(t, buf.String(), "localhost:9090")
	assert.Contains(t, buf.String(), "prometheus")
	assert.Contains(t, buf.String(), "2023-11-14T")
}
