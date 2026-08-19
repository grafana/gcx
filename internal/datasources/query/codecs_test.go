package query_test

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/arrowtable"
	dsquery "github.com/grafana/gcx/internal/datasources/query"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/query/athena"
	"github.com/grafana/gcx/internal/query/clickhouse"
	"github.com/grafana/gcx/internal/query/cloudwatch"
	"github.com/grafana/gcx/internal/query/infinity"
	"github.com/grafana/gcx/internal/query/influxdb"
	"github.com/grafana/gcx/internal/query/loki"
	"github.com/grafana/gcx/internal/query/prometheus"
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

// TestArrowCodecDispatch verifies the arrow codec routes each of the
// response types the table codec supports to the matching FormatArrow
// function, and rejects a type none of them produce.
func TestArrowCodecDispatch(t *testing.T) {
	newArrowIO := func() *cmdio.Options {
		t.Helper()
		ioOpts := &cmdio.Options{OutputFormat: "arrow"}
		dsquery.RegisterCodecs(ioOpts, false)
		return ioOpts
	}

	t.Run("prometheus", func(t *testing.T) {
		resp := &prometheus.QueryResponse{
			Status: "success",
			Data: prometheus.ResultData{
				ResultType: "vector",
				Result: []prometheus.Sample{
					{Metric: map[string]string{"job": "prom"}, Value: []any{float64(1700000000), "1"}},
				},
			},
		}
		var out bytes.Buffer
		require.NoError(t, newArrowIO().Encode(&out, resp))
		headers, rows, err := arrowtable.ReadStream(&out)
		require.NoError(t, err)
		assert.Equal(t, []string{"JOB", "TIMESTAMP", "VALUE"}, headers)
		assert.Len(t, rows, 1)
	})

	t.Run("clickhouse table info", func(t *testing.T) {
		resp := []clickhouse.TableInfo{{Database: "default", Name: "events", Engine: "MergeTree"}}
		var out bytes.Buffer
		require.NoError(t, newArrowIO().Encode(&out, resp))
		_, rows, err := arrowtable.ReadStream(&out)
		require.NoError(t, err)
		assert.Len(t, rows, 1)
	})

	t.Run("athena string list", func(t *testing.T) {
		resp := athena.StringList{Header: "CATALOG", Items: []string{"a", "b"}}
		var out bytes.Buffer
		require.NoError(t, newArrowIO().Encode(&out, resp))
		headers, rows, err := arrowtable.ReadStream(&out)
		require.NoError(t, err)
		assert.Equal(t, []string{"CATALOG"}, headers)
		assert.Len(t, rows, 2)
	})

	t.Run("cloudwatch", func(t *testing.T) {
		resp := &cloudwatch.QueryResponse{} // empty is a valid no-op case
		var out bytes.Buffer
		require.NoError(t, newArrowIO().Encode(&out, resp))
		assert.Empty(t, out.String())
	})

	t.Run("unsupported type rejected", func(t *testing.T) {
		var out bytes.Buffer
		err := newArrowIO().Encode(&out, "not a query response")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid data type for query arrow codec")
	})
}
