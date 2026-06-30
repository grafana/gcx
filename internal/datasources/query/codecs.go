package query

import (
	"errors"
	"io"

	"github.com/grafana/gcx/internal/format"
	"github.com/grafana/gcx/internal/graph"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/query/athena"
	"github.com/grafana/gcx/internal/query/clickhouse"
	"github.com/grafana/gcx/internal/query/cloudwatch"
	"github.com/grafana/gcx/internal/query/infinity"
	"github.com/grafana/gcx/internal/query/influxdb"
	"github.com/grafana/gcx/internal/query/loki"
	"github.com/grafana/gcx/internal/query/prometheus"
	"github.com/grafana/gcx/internal/query/pyroscope"
	querysql "github.com/grafana/gcx/internal/query/sql"
	"github.com/grafana/gcx/internal/query/tempo"
)

type queryTableCodec struct{}

func (c *queryTableCodec) Format() format.Format {
	return "table"
}

func (c *queryTableCodec) Encode(w io.Writer, data any) error {
	switch resp := data.(type) {
	case *prometheus.QueryResponse:
		return prometheus.FormatTable(w, resp)
	case *loki.QueryResponse:
		return loki.FormatQueryTable(w, resp)
	case *loki.MetricQueryResponse:
		return loki.FormatMetricQueryTable(w, resp)
	case *pyroscope.QueryResponse:
		return pyroscope.FormatQueryTable(w, resp)
	case *tempo.SearchResponse:
		return tempo.FormatSearchTable(w, resp)
	case *tempo.MetricsResponse:
		return tempo.FormatMetricsTable(w, resp)
	case *infinity.QueryResponse:
		return infinity.FormatTable(w, resp)
	case *influxdb.QueryResponse:
		return influxdb.FormatQueryTable(w, resp)
	case *tempo.GetTraceResponse:
		return tempo.FormatTraceTable(w, resp)
	case *querysql.QueryResponse:
		return querysql.FormatTable(w, resp)
	case []clickhouse.TableInfo:
		return clickhouse.FormatListTablesTable(w, resp)
	case []clickhouse.ColumnInfo:
		return clickhouse.FormatDescribeTableTable(w, resp)
	case athena.StringList:
		return athena.FormatStringList(w, resp.Items, resp.Header)
	case *cloudwatch.QueryResponse:
		return cloudwatch.FormatTable(w, resp)
	default:
		return errors.New("invalid data type for query table codec")
	}
}

func (c *queryTableCodec) Decode(io.Reader, any) error {
	return errors.New("query table codec does not support decoding")
}

type queryWideCodec struct{}

func (c *queryWideCodec) Format() format.Format {
	return "wide"
}

func (c *queryWideCodec) Encode(w io.Writer, data any) error {
	switch resp := data.(type) {
	case *prometheus.QueryResponse:
		return prometheus.FormatWideTable(w, resp)
	case *loki.QueryResponse:
		return loki.FormatQueryTableWide(w, resp)
	case *tempo.SearchResponse:
		return tempo.FormatSearchTable(w, resp)
	case *infinity.QueryResponse:
		return infinity.FormatTable(w, resp)
	case *tempo.GetTraceResponse:
		return tempo.FormatTraceWide(w, resp)
	case *querysql.QueryResponse:
		return querysql.FormatWideTable(w, resp)
	case athena.StringList:
		return athena.FormatStringList(w, resp.Items, resp.Header)
	case *cloudwatch.QueryResponse:
		return cloudwatch.FormatWide(w, resp)
	default:
		return errors.New("invalid data type for query wide codec")
	}
}

func (c *queryWideCodec) Decode(io.Reader, any) error {
	return errors.New("query wide codec does not support decoding")
}

type queryGraphCodec struct{}

func (c *queryGraphCodec) Format() format.Format {
	return "graph"
}

func (c *queryGraphCodec) Encode(w io.Writer, data any) error {
	var chartData *graph.ChartData
	var err error

	switch resp := data.(type) {
	case *prometheus.QueryResponse:
		chartData, err = graph.FromPrometheusResponse(resp)
		if err != nil {
			return err
		}
	case *loki.QueryResponse:
		return errors.New("graph output is not supported for log stream queries; use -o table/json/yaml or use 'gcx logs metrics' for time-series data")
	case *loki.MetricQueryResponse:
		chartData, err = graph.FromLokiMetricResponse(resp)
		if err != nil {
			return err
		}
	case *pyroscope.QueryResponse:
		chartData, err = graph.FromPyroscopeResponse(resp)
		if err != nil {
			return err
		}
	case *cloudwatch.QueryResponse:
		chartData, err = graph.FromCloudWatchResponse(resp)
		if err != nil {
			return err
		}
	case *tempo.SearchResponse:
		return errors.New("graph output is not supported for trace search results; use -o table/json/yaml")
	case *infinity.QueryResponse:
		return errors.New("graph output is not supported for Infinity queries; use -o table/json/yaml")
	case *tempo.MetricsResponse:
		chartData, err = graph.FromTempoMetricsResponse(resp)
		if err != nil {
			return err
		}
	case *influxdb.QueryResponse:
		chartData, err = graph.FromInfluxDBResponse(resp)
		if err != nil {
			return err
		}
	case *querysql.QueryResponse:
		return errors.New("graph output is not supported for SQL datasource queries; use -o table/json/yaml")
	case []clickhouse.TableInfo:
		return errors.New("graph output is not supported for ClickHouse list-tables; use -o table/json/yaml")
	case []clickhouse.ColumnInfo:
		return errors.New("graph output is not supported for ClickHouse describe-table; use -o table/json/yaml")
	case athena.StringList:
		return errors.New("graph output is not supported for Athena discovery; use -o table/json/yaml")
	default:
		return errors.New("invalid data type for graph codec")
	}

	opts := graph.DefaultChartOptions()
	return graph.RenderChart(w, chartData, opts)
}

func (c *queryGraphCodec) Decode(io.Reader, any) error {
	return errors.New("graph codec does not support decoding")
}

type queryJSONCodec struct {
	inner *format.JSONCodec
}

func (c *queryJSONCodec) Format() format.Format {
	return format.JSON
}

func (c *queryJSONCodec) Encode(w io.Writer, data any) error {
	// InfluxDB responses carry millisecond-epoch timestamps in time columns.
	// FormatQueryJSON converts those to RFC3339 strings so the JSON output
	// matches what users expect rather than raw numeric epoch values.
	if resp, ok := data.(*influxdb.QueryResponse); ok {
		return c.inner.Encode(w, influxdb.FormatQueryJSON(resp))
	}
	return c.inner.Encode(w, data)
}

func (c *queryJSONCodec) Decode(r io.Reader, v any) error {
	return c.inner.Decode(r, v)
}

type queryYAMLCodec struct {
	inner *format.YAMLCodec
}

func (c *queryYAMLCodec) Format() format.Format {
	return format.YAML
}

func (c *queryYAMLCodec) Encode(w io.Writer, data any) error {
	// Same as the JSON codec: convert millisecond-epoch time columns to
	// RFC3339 strings before serializing so the output is human-readable.
	if resp, ok := data.(*influxdb.QueryResponse); ok {
		return c.inner.Encode(w, influxdb.FormatQueryJSON(resp))
	}
	return c.inner.Encode(w, data)
}

func (c *queryYAMLCodec) Decode(r io.Reader, v any) error {
	return c.inner.Decode(r, v)
}

// RegisterCodecs registers the table and wide codecs, plus graph when enabled,
// on the given IO options.
func RegisterCodecs(ioOpts *cmdio.Options, enableGraph bool) {
	ioOpts.RegisterCustomCodec("table", &queryTableCodec{})
	ioOpts.RegisterCustomCodec("wide", &queryWideCodec{})
	ioOpts.RegisterCustomCodec("json", &queryJSONCodec{inner: format.NewJSONCodec()})
	ioOpts.RegisterCustomCodec("yaml", &queryYAMLCodec{inner: format.NewYAMLCodec()})
	if enableGraph {
		ioOpts.RegisterCustomCodec("graph", &queryGraphCodec{})
	}
	ioOpts.DefaultFormat("table")
}
