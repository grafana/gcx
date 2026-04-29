package prometheus_test

import (
	"bytes"
	"io"
	"strings"
	"testing"

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
