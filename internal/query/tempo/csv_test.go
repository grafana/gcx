package tempo_test

import (
	"bytes"
	"encoding/csv"
	"testing"

	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/query/tempo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCSVProjections(t *testing.T) {
	zero := 0
	value := 3.25
	for _, tt := range []struct {
		name  string
		value cmdio.CSVMarshaler
		want  []string
	}{
		{"search", &tempo.SearchResponse{Traces: []tempo.SearchTrace{{TraceID: "id", RootServiceName: "api", RootTraceName: "GET, /", StartTimeUnixNano: "1700000000123456789", DurationMs: 123}}}, []string{"id", "api", "GET, /", "1700000000123456789", "123", ""}},
		{"baseline", &tempo.BaselineResult{SeedTraceID: "seed", Candidates: []tempo.BaselineCandidate{{TraceID: "id", ErrorCount: &zero}}}, []string{"seed", "id", "", "", "", "0", "", "", "0"}},
		{"instant", &tempo.MetricsResponse{Instant: true, Series: []tempo.MetricsSeries{{Value: &value, Labels: []tempo.MetricsLabel{{Key: "value", Value: map[string]any{"stringValue": "label"}}}}}}, []string{"3.25", "label"}},
		{"range samples take precedence", &tempo.MetricsResponse{Series: []tempo.MetricsSeries{{Value: &value, Samples: []tempo.MetricsSample{{TimestampMs: "1700000000123", Value: 9}}}}}, []string{"1700000000123", "9"}},
		{"range", &tempo.MetricsResponse{Series: []tempo.MetricsSeries{{Samples: []tempo.MetricsSample{{TimestampMs: "1700000000123", Value: 3.25}}}}}, []string{"1700000000123", "3.25"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var out bytes.Buffer
			require.NoError(t, (cmdio.CSV[cmdio.CSVMarshaler]{Records: cmdio.CSVMarshaler.CSVRecords}).Encode(&out, tt.value))
			rows, err := csv.NewReader(&out).ReadAll()
			require.NoError(t, err)
			require.Len(t, rows, 2)
			assert.Equal(t, tt.want, rows[1])
		})
	}
}
