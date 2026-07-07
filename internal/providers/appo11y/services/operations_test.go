package services //nolint:testpackage // Tests cover unexported builders, merge logic, and codec.

import (
	"bytes"
	"maps"
	"math"
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/query/prometheus"
	"github.com/spf13/cobra"
)

func TestBuildOperationsRateQuery(t *testing.T) {
	v3, _ := metricNamesByMode(MetricsModeV3)
	got, err := buildOperationsRateQuery(v3, "billing", "checkout", "5m", []string{spanKindServer, spanKindConsumer}, nil, nil)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	want := `sum by (span_name) (rate(traces_span_metrics_calls_total{job="billing/checkout",span_kind=~"SPAN_KIND_SERVER|SPAN_KIND_CONSUMER"}[5m]))`
	if got != want {
		t.Errorf("got %q\nwant %q", got, want)
	}

	// With --group-by, the label joins the span_name aggregation.
	got, err = buildOperationsRateQuery(v3, "billing", "checkout", "5m", []string{spanKindServer}, nil, []string{"k8s_cluster_name"})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	want = `sum by (span_name, k8s_cluster_name) (rate(traces_span_metrics_calls_total{job="billing/checkout",span_kind=~"SPAN_KIND_SERVER"}[5m]))`
	if got != want {
		t.Errorf("grouped got %q\nwant %q", got, want)
	}

	if _, err := buildOperationsRateQuery(v3, "", "", "5m", nil, nil, nil); err == nil {
		t.Error("expected error for empty service name")
	}
}

func TestBuildOperationsErrorRateQuery(t *testing.T) {
	tempo, _ := metricNamesByMode(MetricsModeTempo)
	got, err := buildOperationsErrorRateQuery(tempo, "", "auth", "1m", []string{spanKindServer}, nil, nil)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	want := `sum by (span_name) (rate(traces_spanmetrics_calls_total{job="auth",span_kind=~"SPAN_KIND_SERVER",status_code="STATUS_CODE_ERROR"}[1m]))`
	if got != want {
		t.Errorf("got %q\nwant %q", got, want)
	}
}

func TestBuildOperationsLatencyQuantileQuery(t *testing.T) {
	v3, _ := metricNamesByMode(MetricsModeV3)
	got, err := buildOperationsLatencyQuantileQuery(v3, "billing", "checkout", "5m", []string{spanKindServer}, 0.95, nil, nil)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	want := `histogram_quantile(0.95, sum by (le, span_name) (rate(traces_span_metrics_duration_seconds_bucket{job="billing/checkout",span_kind=~"SPAN_KIND_SERVER"}[5m])))`
	if got != want {
		t.Errorf("got %q\nwant %q", got, want)
	}
	if _, err := buildOperationsLatencyQuantileQuery(v3, "", "x", "5m", nil, 1.5, nil, nil); err == nil {
		t.Error("expected error for phi out of range")
	}
}

func TestBuildOperationsAvgLatencyQuery(t *testing.T) {
	v3, _ := metricNamesByMode(MetricsModeV3)
	got, err := buildOperationsAvgLatencyQuery(v3, "billing", "checkout", "5m", []string{spanKindServer}, nil, nil)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	// promql.Div wraps each operand in parens — semantically identical
	// but the string form needs to reflect what the builder emits.
	want := `(sum by (span_name) (rate(traces_span_metrics_duration_seconds_sum{job="billing/checkout",span_kind=~"SPAN_KIND_SERVER"}[5m]))) / (sum by (span_name) (rate(traces_span_metrics_duration_seconds_count{job="billing/checkout",span_kind=~"SPAN_KIND_SERVER"}[5m])))`
	if got != want {
		t.Errorf("got %q\nwant %q", got, want)
	}
}

// opBuckets builds an extractOperations-style map from span_name→value,
// with no group labels (groupKey empty) — the non-grouped shape.
func opBuckets(m map[string]float64) map[opAggKey]groupBucket {
	out := make(map[opAggKey]groupBucket, len(m))
	for name, v := range m {
		out[opAggKey{name: name}] = groupBucket{value: v}
	}
	return out
}

func TestExtractOperations(t *testing.T) {
	resp := &prometheus.QueryResponse{Data: prometheus.ResultData{Result: []prometheus.Sample{
		{Metric: map[string]string{"span_name": "GET /api/foo"}, Value: []any{1.0, "12.5"}},
		{Metric: map[string]string{"span_name": "POST /api/bar"}, Value: []any{1.0, "0.5"}},
		{Metric: map[string]string{"span_name": ""}, Value: []any{1.0, "9.0"}},     // dropped
		{Metric: map[string]string{"span_name": "noop"}, Value: []any{1.0, "NaN"}}, // dropped (NaN)
		{Metric: map[string]string{"span_name": "noop2"}, Value: []any{1.0}},       // dropped (short)
	}}}
	got := extractOperations(resp, nil)
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2: %v", len(got), got)
	}
	if got[opAggKey{name: "GET /api/foo"}].value != 12.5 || got[opAggKey{name: "POST /api/bar"}].value != 0.5 {
		t.Errorf("values wrong: %v", got)
	}

	if got := extractOperations(nil, nil); len(got) != 0 {
		t.Errorf("nil response should produce empty map, got %v", got)
	}
}

func TestExtractOperations_Grouped(t *testing.T) {
	// Same span_name in two clusters must produce two distinct keys, each
	// carrying its group labels.
	resp := &prometheus.QueryResponse{Data: prometheus.ResultData{Result: []prometheus.Sample{
		{Metric: map[string]string{"span_name": "GET /", "k8s_cluster_name": "east"}, Value: []any{1.0, "10"}},
		{Metric: map[string]string{"span_name": "GET /", "k8s_cluster_name": "west"}, Value: []any{1.0, "3"}},
	}}}
	got := extractOperations(resp, []string{"k8s_cluster_name"})
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2: %v", len(got), got)
	}
	east := got[opAggKey{name: "GET /", groupKey: "east"}]
	if east.value != 10 || east.labels["k8s_cluster_name"] != "east" {
		t.Errorf("east bucket wrong: %+v", east)
	}
	west := got[opAggKey{name: "GET /", groupKey: "west"}]
	if west.value != 3 || west.labels["k8s_cluster_name"] != "west" {
		t.Errorf("west bucket wrong: %+v", west)
	}
}

func TestMergeOperations_TimeShareNormalisation(t *testing.T) {
	// Two operations: A is high-rate-fast (10 req/s × 5ms = 50ms/s);
	// B is low-rate-slow (1 req/s × 200ms = 200ms/s). Time-share
	// must rank B above A even though A has higher rate — that's the
	// whole point of the metric.
	rates := opBuckets(map[string]float64{"A": 10, "B": 1})
	errors := opBuckets(map[string]float64{})
	avgs := opBuckets(map[string]float64{"A": 0.005, "B": 0.200})
	p50s := opBuckets(map[string]float64{"A": 0.004, "B": 0.150})
	p95s := opBuckets(map[string]float64{"A": 0.008, "B": 0.300})
	p99s := opBuckets(map[string]float64{"A": 0.010, "B": 0.400})

	got := mergeOperations(rates, errors, avgs, p50s, p95s, p99s, nil)
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	if got[0].Name != "B" || got[1].Name != "A" {
		t.Errorf("expected B,A order (B has 4x more wall-time), got %s,%s", got[0].Name, got[1].Name)
	}

	totalWall := 50e-3 + 200e-3 // 0.25 s/s
	wantB := (200e-3 / totalWall) * 100
	wantA := (50e-3 / totalWall) * 100
	if math.Abs(got[0].TimeSharePercent-wantB) > 0.001 {
		t.Errorf("B time-share = %.4f, want %.4f", got[0].TimeSharePercent, wantB)
	}
	if math.Abs(got[1].TimeSharePercent-wantA) > 0.001 {
		t.Errorf("A time-share = %.4f, want %.4f", got[1].TimeSharePercent, wantA)
	}

	// HasTraffic must be true for both (traffic + avg present).
	for _, op := range got {
		if !op.HasTraffic || !op.HasAvgLatency {
			t.Errorf("op %s missing flags: %+v", op.Name, op)
		}
	}
}

func TestMergeOperations_TimeShareNormalisedWithinGroup(t *testing.T) {
	// Same op "A" in two clusters. Time-share is normalized WITHIN each
	// group, so each cluster's lone op is 100% of its own group — not
	// split across clusters.
	mk := func(cluster string, v float64) map[opAggKey]groupBucket {
		return map[opAggKey]groupBucket{
			{name: "A", groupKey: cluster}: {value: v, labels: map[string]string{"k8s_cluster_name": cluster}},
		}
	}
	merge := func(m1, m2 map[opAggKey]groupBucket) map[opAggKey]groupBucket {
		out := map[opAggKey]groupBucket{}
		maps.Copy(out, m1)
		maps.Copy(out, m2)
		return out
	}
	rates := merge(mk("east", 10), mk("west", 1))
	avgs := merge(mk("east", 0.01), mk("west", 0.5))

	got := mergeOperations(rates, nil, avgs, nil, nil, nil, []string{"k8s_cluster_name"})
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2: %+v", len(got), got)
	}
	for _, op := range got {
		if math.Abs(op.TimeSharePercent-100) > 0.001 {
			t.Errorf("op in cluster %q time-share = %.4f, want 100 (within-group)", op.Labels["k8s_cluster_name"], op.TimeSharePercent)
		}
	}
	// Sorted by group label asc: east before west.
	if got[0].Labels["k8s_cluster_name"] != "east" || got[1].Labels["k8s_cluster_name"] != "west" {
		t.Errorf("expected east,west order, got %q,%q", got[0].Labels["k8s_cluster_name"], got[1].Labels["k8s_cluster_name"])
	}
}

func TestMergeOperations_OperationWithoutAvgLatencyHasZeroTimeShare(t *testing.T) {
	// Op "C" appears only in the rate map — no latency at all. Should
	// render in the output with HasTraffic=true but TimeSharePercent=0,
	// not crash and not skew the others' shares.
	rates := opBuckets(map[string]float64{"A": 10, "C": 5})
	avgs := opBuckets(map[string]float64{"A": 0.010})
	got := mergeOperations(rates, nil, avgs, nil, nil, nil, nil)

	byName := map[string]Operation{}
	for _, op := range got {
		byName[op.Name] = op
	}
	if c := byName["C"]; c.HasAvgLatency || c.TimeSharePercent != 0 {
		t.Errorf("C should have no avg latency and zero share: %+v", c)
	}
	// A is the only contributor to totalWall, so its share is 100%.
	if a := byName["A"]; math.Abs(a.TimeSharePercent-100) > 0.001 {
		t.Errorf("A time-share = %.4f, want ~100", a.TimeSharePercent)
	}
}

func TestMergeOperations_NoTrafficAtAll(t *testing.T) {
	// Edge case: no rate data anywhere. We should return an empty
	// slice (not nil-deref) and not panic on a zero denominator.
	got := mergeOperations(nil, nil, nil, nil, nil, nil, nil)
	if len(got) != 0 {
		t.Errorf("expected empty, got %v", got)
	}
}

func TestMergeOperations_ErrorPercentBoundedAndHasErrorsInferred(t *testing.T) {
	rates := opBuckets(map[string]float64{"A": 10, "B": 5})
	errors := opBuckets(map[string]float64{"A": 2}) // only A has errors observed
	got := mergeOperations(rates, errors, nil, nil, nil, nil, nil)

	byName := map[string]Operation{}
	for _, op := range got {
		byName[op.Name] = op
	}
	if a := byName["A"]; math.Abs(a.ErrorPercent-20) > 0.001 {
		t.Errorf("A error %% = %.4f, want 20", a.ErrorPercent)
	}
	// B has traffic but no error series → hasErrors must still be true
	// so the codec doesn't render `-` for a healthy 0%.
	if b := byName["B"]; !b.HasErrors || b.ErrorPercent != 0 {
		t.Errorf("B should have hasErrors=true and ErrorPercent=0: %+v", b)
	}
}

func TestOperationsOptsValidate(t *testing.T) {
	mk := func(o operationsOpts) operationsOpts {
		o.IO.OutputFormat = "json"
		if o.Since == "" {
			o.Since = defaultRedWindow
		}
		if o.Kind == "" {
			o.Kind = "inbound"
		}
		return o
	}
	tests := []struct {
		name    string
		opts    operationsOpts
		wantErr bool
	}{
		{name: "defaults ok", opts: mk(operationsOpts{})},
		{name: "since PromQL duration", opts: mk(operationsOpts{Since: "1d"})},
		{name: "bad since", opts: mk(operationsOpts{Since: "not-a-duration"}), wantErr: true},
		{name: "negative limit rejected", opts: mk(operationsOpts{Limit: -1}), wantErr: true},
		{name: "zero limit ok (unlimited)", opts: mk(operationsOpts{Limit: 0})},
		{name: "bogus kind", opts: mk(operationsOpts{Kind: "OUTGOING"}), wantErr: true},
		{name: "bogus metrics-mode", opts: mk(operationsOpts{MetricsMode: "fictional"}), wantErr: true},
	}
	cmd := &cobra.Command{Use: "list-operations"}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.opts.Validate(cmd); (err != nil) != tt.wantErr {
				t.Errorf("err = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestOperationsTableCodec(t *testing.T) {
	codec := &operationsTableCodec{}
	resp := &OperationsResponse{
		Service:     Service{Name: "checkout", Namespace: "billing"},
		Window:      "5m",
		MetricsMode: MetricsModeV3,
		SpanKinds:   "SPAN_KIND_SERVER|SPAN_KIND_CONSUMER",
		Items: []Operation{
			{
				Name: "POST /checkout", RatePerSecond: 10, ErrorRatePerSec: 0.2, ErrorPercent: 2.0,
				AvgSeconds: 0.080, P50Seconds: 0.060, P95Seconds: 0.150, P99Seconds: 0.300,
				TimeSharePercent: 80,
				HasTraffic:       true, HasErrors: true, HasAvgLatency: true,
				HasLatencyP50: true, HasLatencyP95: true, HasLatencyP99: true,
			},
			{
				Name: "GET /health", RatePerSecond: 100, ErrorRatePerSec: 0, ErrorPercent: 0,
				AvgSeconds: 0.001, P95Seconds: 0.002,
				TimeSharePercent: 20,
				HasTraffic:       true, HasErrors: true, HasAvgLatency: true, HasLatencyP95: true,
			},
		},
	}
	var buf bytes.Buffer
	if err := codec.Encode(&buf, resp); err != nil {
		t.Fatalf("Encode err = %v", err)
	}
	out := buf.String()
	// Default columns: OPERATION, RATE, ERROR %, P95, TIME %
	for _, want := range []string{"OPERATION", "RATE", "ERROR %", "P95", "TIME %", "POST /checkout", "GET /health", "10.000 req/s", "2.00%", "150.00ms", "80.00%"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q\n----\n%s", want, out)
		}
	}
	// Default view does NOT include p50/p99.
	for _, unwanted := range []string{"P50", "P99"} {
		if strings.Contains(out, unwanted) {
			t.Errorf("default view unexpectedly contains %q\n----\n%s", unwanted, out)
		}
	}

	// Wide view: P50, P99, ERRORS appear.
	buf.Reset()
	wide := &operationsTableCodec{Wide: true}
	if err := wide.Encode(&buf, resp); err != nil {
		t.Fatalf("Encode err = %v", err)
	}
	out = buf.String()
	for _, want := range []string{"P50", "P99", "ERRORS", "60.00ms", "300.00ms", "0.200 req/s"} {
		if !strings.Contains(out, want) {
			t.Errorf("wide output missing %q\n----\n%s", want, out)
		}
	}

	// Empty items path: friendly message, no crash.
	buf.Reset()
	if err := codec.Encode(&buf, &OperationsResponse{Service: Service{Name: "auth"}}); err != nil {
		t.Fatalf("Encode err = %v", err)
	}
	if !strings.Contains(buf.String(), "No operations found") {
		t.Errorf("expected friendly empty message, got %q", buf.String())
	}

	// Wrong type rejected.
	if err := codec.Encode(&buf, "not a response"); err == nil {
		t.Error("expected error on wrong type")
	}
}
