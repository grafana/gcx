package prometheus_test

import (
	"reflect"
	"testing"

	"github.com/grafana/gcx/internal/query/prometheus"
)

func TestParseSimpleVectorQuery_Valid(t *testing.T) {
	tests := []struct {
		name       string
		expr       string
		wantMetric string
		wantFilter []prometheus.PromQLFilter
	}{
		{
			name:       "bare metric no labels",
			expr:       `up`,
			wantMetric: "up",
		},
		{
			name:       "bare vector selector",
			expr:       `up{job="grafana"}`,
			wantMetric: "up",
			wantFilter: []prometheus.PromQLFilter{{Label: "job", Operator: "=", Value: "grafana"}},
		},
		{
			name:       "multiple matchers, all operators",
			expr:       `up{job="grafana", instance!="localhost", env=~"prod.*", region!~"us-west.*"}`,
			wantMetric: "up",
			wantFilter: []prometheus.PromQLFilter{
				{Label: "job", Operator: "=", Value: "grafana"},
				{Label: "instance", Operator: "!=", Value: "localhost"},
				{Label: "env", Operator: "=~", Value: "prod.*"},
				{Label: "region", Operator: "!~", Value: "us-west.*"},
			},
		},
		{
			name:       "range vector wrapped in a function",
			expr:       `rate(http_requests_total{job="grafana"}[5m])`,
			wantMetric: "http_requests_total",
			wantFilter: []prometheus.PromQLFilter{{Label: "job", Operator: "=", Value: "grafana"}},
		},
		{
			name:       "aggregation over a function over a selector",
			expr:       `sum(rate(http_requests_total{job="grafana"}[5m]))`,
			wantMetric: "http_requests_total",
			wantFilter: []prometheus.PromQLFilter{{Label: "job", Operator: "=", Value: "grafana"}},
		},
		{
			name:       "parenthesized selector",
			expr:       `(up{job="grafana"})`,
			wantMetric: "up",
			wantFilter: []prometheus.PromQLFilter{{Label: "job", Operator: "=", Value: "grafana"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			metric, filters, ok := prometheus.ParseSimpleVectorQuery(tt.expr)
			if !ok {
				t.Fatalf("expected ok=true, got false")
			}
			if metric != tt.wantMetric {
				t.Errorf("metric = %q, want %q", metric, tt.wantMetric)
			}
			if !reflect.DeepEqual(filters, tt.wantFilter) {
				t.Errorf("filters = %+v, want %+v", filters, tt.wantFilter)
			}
		})
	}
}

func TestParseSimpleVectorQuery_Unsupported(t *testing.T) {
	tests := map[string]string{
		"binary expr across two metrics": `sum(rate(a[5m]) + rate(b[5m]))`,
		"two metrics no wrapper":         `a + b`,
		"invalid syntax":                 `up{job=`,
		"subquery":                       `rate(up[5m:1m])`,
	}

	for name, expr := range tests {
		t.Run(name, func(t *testing.T) {
			if _, _, ok := prometheus.ParseSimpleVectorQuery(expr); ok {
				t.Fatalf("expected ok=false for %q", expr)
			}
		})
	}
}
