package services //nolint:testpackage // Tests cover unexported fleet query builders.

import (
	"strings"
	"testing"
)

func TestBuildFleetRankQuery(t *testing.T) {
	v3, _ := metricNamesByMode(MetricsModeV3)
	got, err := buildFleetRankQuery(v3, "5m", []string{spanKindServer}, nil, 5)
	if err != nil {
		t.Fatalf("build err = %v", err)
	}
	want := `topk(5, sum by (job, span_name) (rate(traces_span_metrics_duration_seconds_sum{span_kind=~"SPAN_KIND_SERVER"}[5m])))`
	if got != want {
		t.Errorf("got %q\nwant %q", got, want)
	}
}

func TestBuildFleetTotalTimeQuery(t *testing.T) {
	v3, _ := metricNamesByMode(MetricsModeV3)
	got, err := buildFleetTotalTimeQuery(v3, "5m", []string{spanKindServer}, nil)
	if err != nil {
		t.Fatalf("build err = %v", err)
	}
	want := `sum(rate(traces_span_metrics_duration_seconds_sum{span_kind=~"SPAN_KIND_SERVER"}[5m]))`
	if got != want {
		t.Errorf("got %q\nwant %q", got, want)
	}
}

func TestBuildFleetRateQuery(t *testing.T) {
	v3, _ := metricNamesByMode(MetricsModeV3)
	got, err := buildFleetRateQuery(v3, "5m", []string{spanKindServer}, nil, nil, 5)
	if err != nil {
		t.Fatalf("build err = %v", err)
	}
	want := `(sum by (job, span_name) (rate(traces_span_metrics_calls_total{span_kind=~"SPAN_KIND_SERVER"}[5m]))) and on(job, span_name) (topk(5, sum by (job, span_name) (rate(traces_span_metrics_duration_seconds_sum{span_kind=~"SPAN_KIND_SERVER"}[5m]))))`
	if got != want {
		t.Errorf("got %q\nwant %q", got, want)
	}

	// --group-by appends to the By() set on the left side only — the rank
	// side stays (job, span_name) so the "and on" join keys still match.
	grouped, err := buildFleetRateQuery(v3, "5m", []string{spanKindServer}, nil, []string{"k8s_cluster_name"}, 5)
	if err != nil {
		t.Fatalf("build err = %v", err)
	}
	if !strings.Contains(grouped, "sum by (job, span_name, k8s_cluster_name)") {
		t.Errorf("group-by not applied to rate aggregation: %s", grouped)
	}
	if !strings.Contains(grouped, "and on(job, span_name)") {
		t.Errorf("rank restriction missing: %s", grouped)
	}
}

func TestBuildFleetErrorRateQuery(t *testing.T) {
	v3, _ := metricNamesByMode(MetricsModeV3)
	got, err := buildFleetErrorRateQuery(v3, "5m", []string{spanKindServer}, nil, nil, 5)
	if err != nil {
		t.Fatalf("build err = %v", err)
	}
	if !strings.Contains(got, `status_code="STATUS_CODE_ERROR"`) {
		t.Errorf("missing status_code filter: %s", got)
	}
	if !strings.Contains(got, "and on(job, span_name)") {
		t.Errorf("rank restriction missing: %s", got)
	}
}

func TestBuildFleetLatencyQuantileQuery(t *testing.T) {
	v3, _ := metricNamesByMode(MetricsModeV3)
	got, err := buildFleetLatencyQuantileQuery(v3, "5m", []string{spanKindServer}, 0.95, nil, nil, 5)
	if err != nil {
		t.Fatalf("build err = %v", err)
	}
	want := `(histogram_quantile(0.95, sum by (le, job, span_name) (rate(traces_span_metrics_duration_seconds_bucket{span_kind=~"SPAN_KIND_SERVER"}[5m])))) and on(job, span_name) (topk(5, sum by (job, span_name) (rate(traces_span_metrics_duration_seconds_sum{span_kind=~"SPAN_KIND_SERVER"}[5m]))))`
	if got != want {
		t.Errorf("got %q\nwant %q", got, want)
	}

	if _, err := buildFleetLatencyQuantileQuery(v3, "5m", nil, 1.5, nil, nil, 5); err == nil {
		t.Error("expected error for phi out of [0,1]")
	}
}

func TestBuildFleetAvgLatencyQuery(t *testing.T) {
	v3, _ := metricNamesByMode(MetricsModeV3)
	got, err := buildFleetAvgLatencyQuery(v3, "5m", []string{spanKindServer}, nil, nil, 5)
	if err != nil {
		t.Fatalf("build err = %v", err)
	}
	want := `((sum by (job, span_name) (rate(traces_span_metrics_duration_seconds_sum{span_kind=~"SPAN_KIND_SERVER"}[5m]))) / (sum by (job, span_name) (rate(traces_span_metrics_duration_seconds_count{span_kind=~"SPAN_KIND_SERVER"}[5m])))) and on(job, span_name) (topk(5, sum by (job, span_name) (rate(traces_span_metrics_duration_seconds_sum{span_kind=~"SPAN_KIND_SERVER"}[5m]))))`
	if got != want {
		t.Errorf("got %q\nwant %q", got, want)
	}
}

func TestBuildFleetModeProbeQuery(t *testing.T) {
	v3, _ := metricNamesByMode(MetricsModeV3)
	got, err := buildFleetModeProbeQuery(v3.calls, nil)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	want := `count(traces_span_metrics_calls_total)`
	if got != want {
		t.Errorf("got %q\nwant %q", got, want)
	}

	m := []Matcher{{Label: "k8s_cluster_name", Op: "=", Value: "prod-us"}}
	got, err = buildFleetModeProbeQuery(v3.calls, m)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if !strings.Contains(got, `k8s_cluster_name="prod-us"`) {
		t.Errorf("matcher not applied: %s", got)
	}

	if _, err := buildFleetModeProbeQuery("", nil); err == nil {
		t.Error("expected error for empty metric")
	}
}

// TestFleetQueryBuildersAllMetricsModes confirms none of the six fleet
// builders panic across every supported metrics-mode family.
func TestFleetQueryBuildersAllMetricsModes(t *testing.T) {
	for _, mode := range []MetricsMode{MetricsModeV3, MetricsModeTempo, MetricsModeOTel} {
		names, ok := metricNamesByMode(mode)
		if !ok {
			t.Fatalf("unknown mode %q", mode)
		}
		if _, err := buildFleetRankQuery(names, "5m", nil, nil, 10); err != nil {
			t.Errorf("%s: buildFleetRankQuery err = %v", mode, err)
		}
		if _, err := buildFleetTotalTimeQuery(names, "5m", nil, nil); err != nil {
			t.Errorf("%s: buildFleetTotalTimeQuery err = %v", mode, err)
		}
		if _, err := buildFleetRateQuery(names, "5m", nil, nil, nil, 10); err != nil {
			t.Errorf("%s: buildFleetRateQuery err = %v", mode, err)
		}
		if _, err := buildFleetErrorRateQuery(names, "5m", nil, nil, nil, 10); err != nil {
			t.Errorf("%s: buildFleetErrorRateQuery err = %v", mode, err)
		}
		if _, err := buildFleetLatencyQuantileQuery(names, "5m", nil, 0.99, nil, nil, 10); err != nil {
			t.Errorf("%s: buildFleetLatencyQuantileQuery err = %v", mode, err)
		}
		if _, err := buildFleetAvgLatencyQuery(names, "5m", nil, nil, nil, 10); err != nil {
			t.Errorf("%s: buildFleetAvgLatencyQuery err = %v", mode, err)
		}
	}
}

// TestFleetQueryFilterAndLimit locks in that --filter matchers and --limit
// thread through every fleet builder.
func TestFleetQueryFilterAndLimit(t *testing.T) {
	v3, _ := metricNamesByMode(MetricsModeV3)
	m := []Matcher{{Label: "k8s_cluster_name", Op: "=", Value: "prod-us"}}
	const clusterSel = `k8s_cluster_name="prod-us"`

	checks := []struct {
		name      string
		build     func() (string, error)
		wantLimit bool
	}{
		{"rank", func() (string, error) { return buildFleetRankQuery(v3, "5m", nil, m, 7) }, true},
		// buildFleetTotalTimeQuery has no by(...) or topk(...) — it's the
		// honest fleet-wide denominator, deliberately unrestricted by limit.
		{"total", func() (string, error) { return buildFleetTotalTimeQuery(v3, "5m", nil, m) }, false},
		{"rate", func() (string, error) { return buildFleetRateQuery(v3, "5m", nil, m, nil, 7) }, true},
		{"error", func() (string, error) { return buildFleetErrorRateQuery(v3, "5m", nil, m, nil, 7) }, true},
		{"quantile", func() (string, error) { return buildFleetLatencyQuantileQuery(v3, "5m", nil, 0.5, m, nil, 7) }, true},
		{"avg", func() (string, error) { return buildFleetAvgLatencyQuery(v3, "5m", nil, m, nil, 7) }, true},
	}
	for _, c := range checks {
		t.Run(c.name, func(t *testing.T) {
			got, err := c.build()
			if err != nil {
				t.Fatalf("build err = %v", err)
			}
			if !strings.Contains(got, clusterSel) {
				t.Errorf("query missing cluster selector %q: %s", clusterSel, got)
			}
			if c.wantLimit && !strings.Contains(got, "topk(7,") {
				t.Errorf("query missing limit 7: %s", got)
			}
		})
	}
}
