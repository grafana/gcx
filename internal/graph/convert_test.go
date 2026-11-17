package graph_test

import (
	"bytes"
	"testing"
	"time"

	"github.com/grafana/gcx/internal/graph"
	"github.com/grafana/gcx/internal/query/dataframe"
	"github.com/grafana/gcx/internal/query/loki"
	"github.com/grafana/gcx/internal/query/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMetricGraphsSkipNonFiniteSamples(t *testing.T) {
	points := [][]any{
		{1.0, dataframe.MetricValue(nil)},
		{2.0, "NaN"},
		{3.0, "+Inf"},
		{4.0, "-Inf"},
		{5.0, "0"},
		{6.0, "3"},
	}
	for _, resultType := range []string{"vector", "matrix"} {
		var promSamples []prometheus.Sample
		var lokiSamples []loki.MetricQuerySample
		if resultType == "matrix" {
			promSamples = []prometheus.Sample{{Values: points}}
			lokiSamples = []loki.MetricQuerySample{{Values: points}}
		} else {
			for _, point := range points {
				promSamples = append(promSamples, prometheus.Sample{Value: point})
				lokiSamples = append(lokiSamples, loki.MetricQuerySample{Value: point})
			}
		}
		for _, tc := range []struct {
			name    string
			convert func() (*graph.ChartData, error)
		}{
			{"prometheus", func() (*graph.ChartData, error) {
				return graph.FromPrometheusResponse(&prometheus.QueryResponse{Data: prometheus.ResultData{ResultType: resultType, Result: promSamples}})
			}},
			{"loki", func() (*graph.ChartData, error) {
				return graph.FromLokiMetricResponse(&loki.MetricQueryResponse{Data: loki.MetricQueryData{ResultType: resultType, Result: lokiSamples}})
			}},
		} {
			t.Run(tc.name+"/"+resultType, func(t *testing.T) {
				data, err := tc.convert()
				require.NoError(t, err)
				var got []graph.Point
				for _, series := range data.Series {
					got = append(got, series.Points...)
				}
				require.Equal(t, []graph.Point{{Time: time.Unix(5, 0), Value: 0}, {Time: time.Unix(6, 0), Value: 3}}, got)
				var output bytes.Buffer
				require.NoError(t, graph.RenderLineChart(&output, data, graph.ChartOptions{Width: 60, Height: 12}))
				assert.NotContains(t, output.String(), "NaN")
				assert.NotContains(t, output.String(), "Inf")
			})
		}
	}
}
