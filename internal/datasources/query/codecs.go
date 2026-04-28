package query

import (
	"errors"
	"io"

	"github.com/grafana/gcx/internal/format"
	"github.com/grafana/gcx/internal/graph"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/query/loki"
	"github.com/grafana/gcx/internal/query/prometheus"
	"github.com/grafana/gcx/internal/query/pyroscope"
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
	case *tempo.SearchResponse:
		return errors.New("graph output is not supported for trace search results; use -o table/json/yaml")
	case *tempo.MetricsResponse:
		chartData, err = graph.FromTempoMetricsResponse(resp)
		if err != nil {
			return err
		}
	default:
		return errors.New("invalid data type for graph codec")
	}

	opts := graph.DefaultChartOptions()
	return graph.RenderChart(w, chartData, opts)
}

func (c *queryGraphCodec) Decode(io.Reader, any) error {
	return errors.New("graph codec does not support decoding")
}

// RegisterCodecs registers the table and wide codecs, plus graph when enabled,
// on the given IO options.
func RegisterCodecs(ioOpts *cmdio.Options, enableGraph bool) {
	ioOpts.RegisterCustomCodec("table", &queryTableCodec{})
	ioOpts.RegisterCustomCodec("wide", &queryWideCodec{})
	if enableGraph {
		ioOpts.RegisterCustomCodec("graph", &queryGraphCodec{})
	}
	ioOpts.DefaultFormat("table")
}
