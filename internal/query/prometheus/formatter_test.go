package prometheus_test

import (
	"bytes"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/grafana/gcx/internal/arrowtable"
	"github.com/grafana/gcx/internal/query/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFormatVectorTableVariants(t *testing.T) {
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

	tests := []struct {
		name           string
		format         func(io.Writer, *prometheus.QueryResponse) error
		wantHeader     []string
		wantLineCount  int
		wantFirstValue string
		wantContains   []string
	}{
		{
			name:           "table collapses labels into series column",
			format:         prometheus.FormatTable,
			wantHeader:     []string{"VALUE", "TIMESTAMP", "SERIES"},
			wantLineCount:  2,
			wantFirstValue: "1",
			wantContains: []string{
				`{__name__="up",instance="localhost:9090",job="prometheus"}`,
				"2023-11-14T",
			},
		},
		{
			name:       "wide table explodes labels into columns",
			format:     prometheus.FormatWideTable,
			wantHeader: []string{"__NAME__", "INSTANCE", "JOB", "TIMESTAMP", "VALUE"},
			wantContains: []string{
				"up",
				"localhost:9090",
				"prometheus",
				"2023-11-14T",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			require.NoError(t, tt.format(&buf, resp))

			lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
			require.NotEmpty(t, lines)
			assert.Equal(t, tt.wantHeader, strings.Fields(lines[0]))
			if tt.wantLineCount > 0 {
				require.Len(t, lines, tt.wantLineCount)
			}
			if tt.wantFirstValue != "" {
				assert.Equal(t, tt.wantFirstValue, strings.Fields(lines[1])[0])
			}
			for _, want := range tt.wantContains {
				assert.Contains(t, buf.String(), want)
			}
		})
	}
}

func TestFormatArrow(t *testing.T) {
	resp := &prometheus.QueryResponse{
		Status: "success",
		Data: prometheus.ResultData{
			ResultType: "vector",
			Result: []prometheus.Sample{
				{
					Metric: map[string]string{
						"instance": "localhost:9090",
						"job":      "prometheus",
					},
					Value: []any{float64(1700000000), "1"},
				},
			},
		},
	}

	var buf bytes.Buffer
	require.NoError(t, prometheus.FormatArrow(&buf, resp))

	headers, rows, err := arrowtable.ReadStream(&buf)
	require.NoError(t, err)
	assert.Equal(t, []string{"INSTANCE", "JOB", "TIMESTAMP", "VALUE"}, headers)
	require.Len(t, rows, 1)
	assert.Equal(t, "localhost:9090", rows[0][0])
	assert.Equal(t, "prometheus", rows[0][1])
	assert.Equal(t, time.Unix(1700000000, 0).UTC(), rows[0][2])
	assert.InEpsilon(t, 1.0, rows[0][3], 0.0001)
}

func TestFormatArrow_NoData(t *testing.T) {
	resp := &prometheus.QueryResponse{
		Status: "success",
		Data:   prometheus.ResultData{ResultType: "vector", Result: nil},
	}

	var buf bytes.Buffer
	require.NoError(t, prometheus.FormatArrow(&buf, resp))
	assert.Empty(t, buf.String())
}
