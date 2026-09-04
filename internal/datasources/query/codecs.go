package query

import (
	"errors"
	"io"

	"github.com/grafana/gcx/internal/format"
	"github.com/grafana/gcx/internal/graph"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/query/athena"
	"github.com/grafana/gcx/internal/query/azuremonitor"
	"github.com/grafana/gcx/internal/query/bigquery"
	"github.com/grafana/gcx/internal/query/clickhouse"
	"github.com/grafana/gcx/internal/query/cloudmonitoring"
	"github.com/grafana/gcx/internal/query/cloudwatch"
	"github.com/grafana/gcx/internal/query/elasticsearch"
	"github.com/grafana/gcx/internal/query/infinity"
	"github.com/grafana/gcx/internal/query/influxdb"
	"github.com/grafana/gcx/internal/query/loki"
	"github.com/grafana/gcx/internal/query/opensearch"
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
	case *tempo.BaselineResult:
		return tempo.FormatBaselineTable(w, resp)
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
	case bigquery.StringList:
		return bigquery.FormatStringList(w, resp.Items, resp.Header)
	case []bigquery.TableInfo:
		return bigquery.FormatListTablesTable(w, resp)
	case []bigquery.ColumnInfo:
		return bigquery.FormatDescribeTableTable(w, resp)
	case *cloudwatch.QueryResponse:
		return cloudwatch.FormatTable(w, resp)
	case *cloudmonitoring.QueryResponse:
		return cloudmonitoring.FormatTable(w, resp)
	case *elasticsearch.MetricsResponse:
		return elasticsearch.FormatMetricsTable(w, resp)
	case *opensearch.MetricsResponse:
		return opensearch.FormatMetricsTable(w, resp)
	case *azuremonitor.QueryResponse:
		return azuremonitor.FormatTable(w, resp)
	case *azuremonitor.TableResponse:
		return azuremonitor.FormatTableResponse(w, resp)
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
	case *tempo.BaselineResult:
		return tempo.FormatBaselineTable(w, resp)
	case *infinity.QueryResponse:
		return infinity.FormatTable(w, resp)
	case *tempo.GetTraceResponse:
		return tempo.FormatTraceWide(w, resp)
	case *querysql.QueryResponse:
		return querysql.FormatWideTable(w, resp)
	case athena.StringList:
		return athena.FormatStringList(w, resp.Items, resp.Header)
	case bigquery.StringList:
		return bigquery.FormatStringList(w, resp.Items, resp.Header)
	case []bigquery.TableInfo:
		return bigquery.FormatListTablesTable(w, resp)
	case []bigquery.ColumnInfo:
		return bigquery.FormatDescribeTableTable(w, resp)
	case *cloudwatch.QueryResponse:
		return cloudwatch.FormatWide(w, resp)
	case *cloudmonitoring.QueryResponse:
		return cloudmonitoring.FormatWide(w, resp)
	case *elasticsearch.MetricsResponse:
		return elasticsearch.FormatMetricsTable(w, resp)
	case *opensearch.MetricsResponse:
		return opensearch.FormatMetricsTable(w, resp)
	case *azuremonitor.QueryResponse:
		return azuremonitor.FormatWide(w, resp)
	case *azuremonitor.TableResponse:
		return azuremonitor.FormatTableResponse(w, resp)
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

	// Each success case only assigns chartData/err; the single check below
	// the switch (rather than one per case) is what keeps this dispatch
	// table's complexity flat as datasource kinds are added — a repeated
	// "if err != nil { return err }" per case was pushing gocyclo over its
	// threshold on every new kind, when the cases never disagree on how to
	// react to an error.
	switch resp := data.(type) {
	case *prometheus.QueryResponse:
		chartData, err = graph.FromPrometheusResponse(resp)
	case *loki.QueryResponse:
		return errors.New("graph output is not supported for log stream queries; use -o table/json/yaml or use 'gcx logs metrics' for time-series data")
	case *loki.MetricQueryResponse:
		chartData, err = graph.FromLokiMetricResponse(resp)
	case *pyroscope.QueryResponse:
		chartData, err = graph.FromPyroscopeResponse(resp)
	case *cloudwatch.QueryResponse:
		chartData, err = graph.FromCloudWatchResponse(resp)
	case *cloudmonitoring.QueryResponse:
		chartData, err = graph.FromCloudMonitoringResponse(resp)
	case *azuremonitor.QueryResponse:
		chartData, err = graph.FromAzureMonitorResponse(resp)
	case *azuremonitor.TableResponse:
		return errors.New("graph output is not supported for KQL table results; use -o table/json/yaml")
	case *tempo.SearchResponse:
		return errors.New("graph output is not supported for trace search results; use -o table/json/yaml")
	case *infinity.QueryResponse:
		return errors.New("graph output is not supported for Infinity queries; use -o table/json/yaml")
	case *tempo.MetricsResponse:
		chartData, err = graph.FromTempoMetricsResponse(resp)
	case *influxdb.QueryResponse:
		chartData, err = graph.FromInfluxDBResponse(resp)
	case *elasticsearch.MetricsResponse:
		chartData, err = graph.FromElasticsearchResponse(resp)
	case *opensearch.MetricsResponse:
		chartData, err = graph.FromOpenSearchResponse(resp)
	case *querysql.QueryResponse:
		return errors.New("graph output is not supported for SQL datasource queries; use -o table/json/yaml")
	case []clickhouse.TableInfo:
		return errors.New("graph output is not supported for ClickHouse list-tables; use -o table/json/yaml")
	case []clickhouse.ColumnInfo:
		return errors.New("graph output is not supported for ClickHouse describe-table; use -o table/json/yaml")
	case athena.StringList:
		return errors.New("graph output is not supported for Athena discovery; use -o table/json/yaml")
	case bigquery.StringList:
		return errors.New("graph output is not supported for BigQuery list-datasets; use -o table/json/yaml")
	case []bigquery.TableInfo:
		return errors.New("graph output is not supported for BigQuery list-tables; use -o table/json/yaml")
	case []bigquery.ColumnInfo:
		return errors.New("graph output is not supported for BigQuery describe-table; use -o table/json/yaml")
	default:
		return errors.New("invalid data type for graph codec")
	}
	if err != nil {
		return err
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

// RegisterStructuredCodecs registers only the JSON and YAML codecs (default
// JSON) — no table, wide, or graph. Use it for datasource commands whose
// payload is an opaque or free-form structure with no meaningful tabular
// projection, e.g. the experimental Tempo trace-diff patch. The table/wide
// codecs switch on concrete response types and reject anything else with
// "invalid data type", so advertising them for such commands would surface a
// runtime error on a documented output path.
func RegisterStructuredCodecs(ioOpts *cmdio.Options) {
	ioOpts.RegisterCustomCodec("json", &queryJSONCodec{inner: format.NewJSONCodec()})
	ioOpts.RegisterCustomCodec("yaml", &queryYAMLCodec{inner: format.NewYAMLCodec()})
	ioOpts.DefaultFormat("json")
}
