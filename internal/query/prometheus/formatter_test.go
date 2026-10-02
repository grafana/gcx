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
	tests := []struct {
		name           string
		point          prometheus.Sample
		format         func(io.Writer, *prometheus.QueryResponse) error
		wantHeader     []string
		wantLineCount  int
		wantFirstValue string
		wantContains   []string
	}{
		{
			name:           "table collapses labels into series column",
			point:          prometheus.Sample{Value: []any{float64(1700000000), "1"}},
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
			point:      prometheus.Sample{Value: []any{float64(1700000000), "1"}},
			format:     prometheus.FormatWideTable,
			wantHeader: []string{"__NAME__", "INSTANCE", "JOB", "TIMESTAMP", "VALUE"},
			wantContains: []string{
				"up",
				"localhost:9090",
				"prometheus",
				"2023-11-14T",
			},
		},
		{
			name:           "table accepts plural values from a vector API",
			point:          prometheus.Sample{Values: [][]any{{float64(1700000000), "1"}}},
			format:         prometheus.FormatTable,
			wantHeader:     []string{"VALUE", "TIMESTAMP", "SERIES"},
			wantLineCount:  2,
			wantFirstValue: "1",
		},
		{
			name:       "wide table accepts plural values from a vector API",
			point:      prometheus.Sample{Values: [][]any{{float64(1700000000), "1"}}},
			format:     prometheus.FormatWideTable,
			wantHeader: []string{"__NAME__", "INSTANCE", "JOB", "TIMESTAMP", "VALUE"},
			wantContains: []string{
				"localhost:9090",
				"2023-11-14T",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.point.Metric = map[string]string{
				"__name__": "up",
				"instance": "localhost:9090",
				"job":      "prometheus",
			}
			resp := &prometheus.QueryResponse{
				Status: "success",
				Data: prometheus.ResultData{
					ResultType: "vector",
					Result:     []prometheus.Sample{tt.point},
				},
			}
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
