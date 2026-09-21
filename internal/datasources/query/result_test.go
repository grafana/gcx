package query_test

import (
	"errors"
	"testing"

	query "github.com/grafana/gcx/internal/datasources/query"
	"github.com/grafana/gcx/internal/query/loki"
	"github.com/grafana/gcx/internal/query/prometheus"
	"github.com/grafana/gcx/internal/query/pyroscope"
	"github.com/grafana/gcx/internal/query/tempo"
)

func TestRequireResult(t *testing.T) {
	tests := []struct {
		name  string
		value any
		empty bool
	}{
		{name: "prometheus empty", value: &prometheus.QueryResponse{}, empty: true},
		{name: "prometheus result", value: &prometheus.QueryResponse{Data: prometheus.ResultData{Result: []prometheus.Sample{{}}}}},
		{name: "loki empty", value: &loki.QueryResponse{}, empty: true},
		{name: "loki result", value: &loki.QueryResponse{Data: loki.QueryResultData{Result: []loki.StreamEntry{{}}}}},
		{name: "loki metric empty", value: &loki.MetricQueryResponse{}, empty: true},
		{name: "tempo empty", value: &tempo.SearchResponse{}, empty: true},
		{name: "tempo result", value: &tempo.SearchResponse{Traces: []tempo.SearchTrace{{}}}},
		{name: "tempo metrics empty", value: &tempo.MetricsResponse{}, empty: true},
		{name: "tempo metrics result", value: &tempo.MetricsResponse{Series: []tempo.MetricsSeries{{}}}},
		{name: "pyroscope empty", value: &pyroscope.QueryResponse{}, empty: true},
		{name: "pyroscope flamegraph", value: &pyroscope.QueryResponse{Flamegraph: &pyroscope.Flamegraph{}}},
		{name: "unsupported response", value: struct{}{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := query.RequireResult(tt.value)
			if tt.empty && !errors.Is(err, query.ErrNoResult) {
				t.Fatalf("RequireResult() error = %v, want ErrNoResult", err)
			}
			if !tt.empty && err != nil {
				t.Fatalf("RequireResult() error = %v, want nil", err)
			}
		})
	}
}
