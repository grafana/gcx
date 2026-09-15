package services //nolint:testpackage // Tests cover the unexported buildServiceLinks helper.

import (
	"net/url"
	"strings"
	"testing"
)

func TestBuildServiceLinks_MissingGrafanaURLOrDatasource(t *testing.T) {
	tests := []struct {
		name          string
		grafanaURL    string
		datasourceUID string
	}{
		{name: "empty grafanaURL", grafanaURL: "", datasourceUID: "prom-uid"},
		{name: "empty datasourceUID", grafanaURL: "https://example.grafana.net", datasourceUID: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildServiceLinks(tt.grafanaURL, tt.datasourceUID, 1, "5m", "rate(x)", "rate(y)", "histogram_quantile(0.95,z)")
			if got != nil {
				t.Errorf("buildServiceLinks() = %+v, want nil", got)
			}
		})
	}
}

func TestBuildServiceLinks_Populated(t *testing.T) {
	const (
		rateExpr = `sum(rate(traces_span_metrics_calls_total{job="billing/checkout"}[5m]))`
		errExpr  = `sum(rate(traces_span_metrics_calls_total{job="billing/checkout",status_code="STATUS_CODE_ERROR"}[5m]))`
		p95Expr  = `histogram_quantile(0.95, sum by (le) (rate(traces_span_metrics_duration_seconds_bucket{job="billing/checkout"}[5m])))`
	)
	got := buildServiceLinks("https://example.grafana.net", "prom-uid", 1, "5m", rateExpr, errExpr, p95Expr)
	if got == nil {
		t.Fatal("buildServiceLinks() = nil, want populated *ServiceLinks")
	}
	assertURLContainsExpr(t, "Rate", got.Rate, rateExpr)
	assertURLContainsExpr(t, "Errors", got.Errors, errExpr)
	assertURLContainsExpr(t, "LatencyP95", got.LatencyP95, p95Expr)
}

// TestBuildServiceLinks_CompoundWindow guards against a link built with a
// compound --since value (e.g. "1h30m") embedding that string verbatim as
// datemath's `from` — Grafana's datemath parser only accepts a single
// amount+unit pair, so "now-1h30m" silently breaks the link's time range.
func TestBuildServiceLinks_CompoundWindow(t *testing.T) {
	got := buildServiceLinks("https://example.grafana.net", "prom-uid", 1, "1h30m", "rate(x)", "rate(y)", "histogram_quantile(0.95,z)")
	if got == nil {
		t.Fatal("buildServiceLinks() = nil, want populated *ServiceLinks")
	}
	u, err := url.Parse(got.Rate)
	if err != nil {
		t.Fatalf("Rate URL %q failed to parse: %v", got.Rate, err)
	}
	panes := u.Query().Get("panes")
	if strings.Contains(panes, `"now-1h30m"`) {
		t.Errorf("panes = %q, contains invalid compound-unit datemath %q", panes, "now-1h30m")
	}
	if !strings.Contains(panes, `"now-5400s"`) {
		t.Errorf("panes = %q, want it to contain the normalized \"now-5400s\"", panes)
	}
}

func TestDatemathFrom(t *testing.T) {
	tests := []struct {
		window string
		want   string
	}{
		{window: "5m", want: "now-300s"},
		{window: "1h", want: "now-3600s"},
		{window: "1h30m", want: "now-5400s"},
		{window: "1d", want: "now-86400s"},
	}
	for _, tt := range tests {
		t.Run(tt.window, func(t *testing.T) {
			if got := datemathFrom(tt.window); got != tt.want {
				t.Errorf("datemathFrom(%q) = %q, want %q", tt.window, got, tt.want)
			}
		})
	}
}

// assertURLContainsExpr confirms the query-string payload of an Explore
// link carries expr. It doesn't overspecify the full URL shape — that's
// prometheus.QueryExploreURL's own tested contract.
func assertURLContainsExpr(t *testing.T, field, rawURL, expr string) {
	t.Helper()
	if rawURL == "" {
		t.Errorf("%s URL is empty", field)
		return
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("%s URL %q failed to parse: %v", field, rawURL, err)
	}
	panes := u.Query().Get("panes")
	// panes is a JSON-encoded blob, so quotes in expr are backslash-escaped
	// there.
	jsonEscaped := strings.ReplaceAll(expr, `"`, `\"`)
	if !strings.Contains(panes, jsonEscaped) {
		t.Errorf("%s panes = %q, want it to contain %q", field, panes, jsonEscaped)
	}
}
