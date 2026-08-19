package query_test

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	dsquery "github.com/grafana/gcx/internal/datasources/query"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/query/clickhouse"
	"github.com/grafana/gcx/internal/query/infinity"
	"github.com/grafana/gcx/internal/query/influxdb"
	"github.com/grafana/gcx/internal/query/loki"
	"github.com/grafana/gcx/internal/query/prometheus"
	"github.com/grafana/gcx/internal/query/pyroscope"
	"github.com/grafana/gcx/internal/query/tempo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGraphCodecRejectsUnsupportedResponseTypes(t *testing.T) {
	newGraphIO := func() *cmdio.Options {
		t.Helper()
		ioOpts := &cmdio.Options{OutputFormat: "graph"}
		dsquery.RegisterCodecs(ioOpts, true)
		return ioOpts
	}

	t.Run("rejects loki log stream responses", func(t *testing.T) {
		var out bytes.Buffer
		err := newGraphIO().Encode(&out, &loki.QueryResponse{})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "graph output is not supported for log stream queries")
		assert.Contains(t, err.Error(), "gcx logs metrics")
	})

	t.Run("rejects tempo trace search responses", func(t *testing.T) {
		var out bytes.Buffer
		err := newGraphIO().Encode(&out, &tempo.SearchResponse{})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "graph output is not supported for trace search results")
	})

	t.Run("rejects infinity query responses", func(t *testing.T) {
		var out bytes.Buffer
		err := newGraphIO().Encode(&out, &infinity.QueryResponse{})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "Infinity")
	})
}

func TestQueryJSONCodecInfluxDBTimestamps(t *testing.T) {
	newJSONIO := func() *cmdio.Options {
		t.Helper()
		ioOpts := &cmdio.Options{OutputFormat: "json"}
		dsquery.RegisterCodecs(ioOpts, false)
		return ioOpts
	}

	t.Run("influxdb timestamps rendered as RFC3339 in JSON", func(t *testing.T) {
		resp := &influxdb.QueryResponse{
			Columns:     []string{"time", "value"},
			Rows:        [][]any{{float64(1719849600000), float64(42.5)}},
			TimeColumns: map[int]bool{0: true},
		}

		var out bytes.Buffer
		err := newJSONIO().Encode(&out, resp)
		require.NoError(t, err)

		output := out.String()
		assert.Contains(t, output, "2024-07-01T16:00:00Z", "expected RFC3339 timestamp in JSON output")
		assert.NotContains(t, output, "1719849600000", "raw ms integer should not appear in JSON output")
	})

	t.Run("non-influxdb type passes through unchanged", func(t *testing.T) {
		resp := &prometheus.QueryResponse{}

		var out bytes.Buffer
		err := newJSONIO().Encode(&out, resp)
		require.NoError(t, err)

		// Just verify it encoded without error -- the exact content depends
		// on the Prometheus response type's JSON serialization.
		assert.NotEmpty(t, out.String())
	})

	t.Run("raw ms integers absent from influxdb JSON output", func(t *testing.T) {
		resp := &influxdb.QueryResponse{
			Columns: []string{"time", "cpu", "host"},
			Rows: [][]any{
				{float64(1719849600000), float64(55.2), "server-a"},
				{float64(1719936000000), float64(63.8), "server-b"},
			},
			TimeColumns: map[int]bool{0: true},
		}

		var out bytes.Buffer
		err := newJSONIO().Encode(&out, resp)
		require.NoError(t, err)

		output := out.String()
		assert.NotContains(t, output, "1719849600000")
		assert.NotContains(t, output, "1719936000000")
		assert.Contains(t, output, "2024-07-01T16:00:00Z")
		assert.Contains(t, output, "2024-07-02T16:00:00Z")
	})

	t.Run("influxdb JSON output is valid JSON", func(t *testing.T) {
		resp := &influxdb.QueryResponse{
			Columns:     []string{"time", "value"},
			Rows:        [][]any{{float64(1719849600000), float64(42.5)}},
			TimeColumns: map[int]bool{0: true},
		}

		var out bytes.Buffer
		err := newJSONIO().Encode(&out, resp)
		require.NoError(t, err)

		assert.True(t, json.Valid(out.Bytes()), "output should be valid JSON")
	})
}

func TestQueryYAMLCodecInfluxDBTimestamps(t *testing.T) {
	newYAMLIO := func() *cmdio.Options {
		t.Helper()
		ioOpts := &cmdio.Options{OutputFormat: "yaml"}
		dsquery.RegisterCodecs(ioOpts, false)
		return ioOpts
	}

	t.Run("influxdb timestamps rendered as RFC3339 in YAML", func(t *testing.T) {
		resp := &influxdb.QueryResponse{
			Columns:     []string{"time", "value"},
			Rows:        [][]any{{float64(1719849600000), float64(42.5)}},
			TimeColumns: map[int]bool{0: true},
		}

		var out bytes.Buffer
		err := newYAMLIO().Encode(&out, resp)
		require.NoError(t, err)

		output := out.String()
		assert.Contains(t, output, "2024-07-01T16:00:00Z", "expected RFC3339 timestamp in YAML output")
		assert.NotContains(t, output, "1719849600000", "raw ms integer should not appear in YAML output")
	})

	t.Run("multiple rows with timestamps in YAML", func(t *testing.T) {
		resp := &influxdb.QueryResponse{
			Columns: []string{"time", "host"},
			Rows: [][]any{
				{float64(1719849600000), "server-a"},
				{float64(1719936000000), "server-b"},
			},
			TimeColumns: map[int]bool{0: true},
		}

		var out bytes.Buffer
		err := newYAMLIO().Encode(&out, resp)
		require.NoError(t, err)

		output := out.String()
		assert.Contains(t, output, "2024-07-01T16:00:00Z")
		assert.Contains(t, output, "2024-07-02T16:00:00Z")
		assert.Contains(t, output, "server-a")
		assert.Contains(t, output, "server-b")

		// Non-time string values must survive unchanged.
		lines := strings.Split(output, "\n")
		found := false
		for _, line := range lines {
			if strings.Contains(line, "server-a") {
				found = true
				break
			}
		}
		assert.True(t, found, "expected server-a in YAML output")
	})
}

// TestTraceGetCodecDispatch verifies that table and wide codecs route a
// *tempo.GetTraceResponse to the corresponding tempo formatter.
func TestTraceGetCodecDispatch(t *testing.T) {
	newIO := func(format string) *cmdio.Options {
		t.Helper()
		ioOpts := &cmdio.Options{OutputFormat: format}
		dsquery.RegisterCodecs(ioOpts, true)
		return ioOpts
	}

	// An empty *GetTraceResponse renders only the header line.
	// We verify dispatch by asserting the formatter's signature output.
	resp := &tempo.GetTraceResponse{}

	t.Run("table dispatches to FormatTraceTable", func(t *testing.T) {
		var out bytes.Buffer
		err := newIO("table").Encode(&out, resp)
		require.NoError(t, err)
		assert.Contains(t, out.String(), "spans: 0")
		assert.Contains(t, out.String(), "services: 0")
	})

	t.Run("wide dispatches to FormatTraceWide", func(t *testing.T) {
		var out bytes.Buffer
		err := newIO("wide").Encode(&out, resp)
		require.NoError(t, err)
		assert.Contains(t, out.String(), "spans: 0")
		assert.Contains(t, out.String(), "services: 0")
	})
}

// TestCSVCodecDispatch verifies the csv codec routes the six types that
// table supports but wide doesn't (loki metric query, pyroscope, tempo
// metrics, influxdb, clickhouse table/column info) to the matching
// FormatCSV function, still rejects trace-get, and rejects an unsupported
// type.
func TestCSVCodecDispatch(t *testing.T) {
	newCSVIO := func() *cmdio.Options {
		t.Helper()
		ioOpts := &cmdio.Options{OutputFormat: "csv"}
		dsquery.RegisterCodecs(ioOpts, false)
		return ioOpts
	}

	t.Run("loki metric query", func(t *testing.T) {
		resp := &loki.MetricQueryResponse{
			Data: loki.MetricQueryData{
				Result: []loki.MetricQuerySample{
					{Metric: map[string]string{"level": "error"}, Values: [][]any{{float64(1700000000), "3.5"}}},
				},
			},
		}
		var out bytes.Buffer
		require.NoError(t, newCSVIO().Encode(&out, resp))
		assert.Contains(t, out.String(), "TIMESTAMP,VALUE,LEVEL")
	})

	t.Run("pyroscope", func(t *testing.T) {
		resp := &pyroscope.QueryResponse{
			Flamegraph: &pyroscope.Flamegraph{
				Names:  []string{"total", "main"},
				Levels: []pyroscope.Level{{Values: []string{"0", "10", "10", "1"}}},
			},
		}
		var out bytes.Buffer
		require.NoError(t, newCSVIO().Encode(&out, resp))
		assert.Contains(t, out.String(), "FUNCTION,SELF,TOTAL,PERCENTAGE")
	})

	t.Run("tempo metrics", func(t *testing.T) {
		val := float64(1)
		resp := &tempo.MetricsResponse{
			Instant: true,
			Series:  []tempo.MetricsSeries{{Value: &val}},
		}
		var out bytes.Buffer
		require.NoError(t, newCSVIO().Encode(&out, resp))
		assert.Contains(t, out.String(), "VALUE")
	})

	t.Run("influxdb", func(t *testing.T) {
		resp := &influxdb.QueryResponse{
			Columns: []string{"host"},
			Rows:    [][]any{{"server-a"}},
		}
		var out bytes.Buffer
		require.NoError(t, newCSVIO().Encode(&out, resp))
		assert.Equal(t, "host\nserver-a\n", out.String())
	})

	t.Run("clickhouse table info", func(t *testing.T) {
		resp := []clickhouse.TableInfo{{Database: "default", Name: "events", Engine: "MergeTree"}}
		var out bytes.Buffer
		require.NoError(t, newCSVIO().Encode(&out, resp))
		assert.Contains(t, out.String(), "DATABASE,NAME,ENGINE")
	})

	t.Run("clickhouse column info", func(t *testing.T) {
		resp := []clickhouse.ColumnInfo{{Name: "id", Type: "UInt64"}}
		var out bytes.Buffer
		require.NoError(t, newCSVIO().Encode(&out, resp))
		assert.Contains(t, out.String(), "NAME,TYPE")
	})

	t.Run("trace get still rejected", func(t *testing.T) {
		var out bytes.Buffer
		err := newCSVIO().Encode(&out, &tempo.GetTraceResponse{})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "csv output is not supported for trace get")
	})

	t.Run("unsupported type rejected", func(t *testing.T) {
		var out bytes.Buffer
		err := newCSVIO().Encode(&out, "not a query response")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid data type for query csv codec")
	})
}
