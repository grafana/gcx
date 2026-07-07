package services //nolint:testpackage // Tests cover unexported builders and helpers.

import (
	"bytes"
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/query/prometheus"
	"github.com/spf13/cobra"
)

func TestResolveSpanKinds(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    []string
		wantErr bool
	}{
		{name: "default empty", in: "", want: []string{spanKindServer, spanKindConsumer}},
		{name: "inbound alias", in: "inbound", want: []string{spanKindServer, spanKindConsumer}},
		{name: "server alias", in: "server", want: []string{spanKindServer}},
		{name: "consumer alias", in: "consumer", want: []string{spanKindConsumer}},
		{name: "all alias is sorted as written", in: "all"}, // length-checked below
		{name: "explicit single literal", in: "SPAN_KIND_CLIENT", want: []string{"SPAN_KIND_CLIENT"}},
		{name: "explicit comma list", in: "SPAN_KIND_SERVER,SPAN_KIND_CLIENT", want: []string{"SPAN_KIND_SERVER", "SPAN_KIND_CLIENT"}},
		{name: "case-insensitive literal", in: "span_kind_server", want: []string{spanKindServer}},
		{name: "comma list with spaces", in: " SPAN_KIND_SERVER , SPAN_KIND_CONSUMER ", want: []string{spanKindServer, spanKindConsumer}},
		{name: "bogus literal rejected", in: "MYTHICAL_KIND", wantErr: true},
		{name: "comma list with empty entries collapses to non-empty", in: "SPAN_KIND_SERVER,,SPAN_KIND_CONSUMER", want: []string{spanKindServer, spanKindConsumer}},
		{name: "comma list of only empty rejected", in: ",,,", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolveSpanKinds(tt.in)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if tt.in == "all" {
				// The "all" alias intentionally includes every defined kind;
				// length check is enough to detect accidental drift.
				if len(got) != 5 {
					t.Errorf("len(all) = %d, want 5: %v", len(got), got)
				}
				return
			}
			if len(got) != len(tt.want) {
				t.Fatalf("len = %d, want %d (%v)", len(got), len(tt.want), got)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("[%d] = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestParseServiceArg(t *testing.T) {
	tests := []struct {
		name, arg, flagNs string
		wantNs, wantName  string
		wantErr           bool
	}{
		{name: "bare name", arg: "checkout", wantName: "checkout"},
		{name: "namespace/name", arg: "billing/checkout", wantNs: "billing", wantName: "checkout"},
		{name: "flag namespace fills in", arg: "checkout", flagNs: "billing", wantNs: "billing", wantName: "checkout"},
		{name: "arg namespace overrides empty flag", arg: "billing/checkout", flagNs: "", wantNs: "billing", wantName: "checkout"},
		{name: "flag matches arg namespace", arg: "billing/checkout", flagNs: "billing", wantNs: "billing", wantName: "checkout"},
		{name: "conflict between arg and flag", arg: "billing/checkout", flagNs: "shipping", wantErr: true},
		{name: "empty arg", arg: "", wantErr: true},
		{name: "whitespace arg", arg: "   ", wantErr: true},
		{name: "leading slash treated as empty namespace", arg: "/checkout", wantName: "checkout"},
		{name: "trailing slash is empty name", arg: "billing/", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ns, name, err := parseServiceArg(tt.arg, tt.flagNs)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if ns != tt.wantNs || name != tt.wantName {
				t.Errorf("got (%q, %q), want (%q, %q)", ns, name, tt.wantNs, tt.wantName)
			}
		})
	}
}

func TestBuildServiceMetadataQuery(t *testing.T) {
	tests := []struct {
		name, metric, ns, svc string
		wantContains          []string
		wantErr               bool
	}{
		{
			name:   "namespaced",
			metric: "target_info", ns: "billing", svc: "checkout",
			wantContains: []string{`target_info{job="billing/checkout"}`, "group by (telemetry_sdk_language, job"},
		},
		{
			name:   "bare name",
			metric: "traces_target_info", ns: "", svc: "auth",
			wantContains: []string{`traces_target_info{job="auth"}`},
		},
		{name: "empty name rejected", metric: "target_info", ns: "x", svc: "", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := buildServiceMetadataQuery(tt.metric, tt.ns, tt.svc, nil)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			for _, s := range tt.wantContains {
				if !strings.Contains(got, s) {
					t.Errorf("query missing %q: %s", s, got)
				}
			}
		})
	}
}

func TestBuildRateQuery(t *testing.T) {
	v3, _ := metricNamesByMode(MetricsModeV3)
	got, err := buildRateQuery(v3, "billing", "checkout", "5m", []string{spanKindServer, spanKindConsumer}, nil, nil)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	want := `sum(rate(traces_span_metrics_calls_total{job="billing/checkout",span_kind=~"SPAN_KIND_SERVER|SPAN_KIND_CONSUMER"}[5m]))`
	if got != want {
		t.Errorf("got %q\nwant %q", got, want)
	}

	// Tempo (legacy) mode swaps to traces_spanmetrics_* without changing the job/kind filters.
	tempo, _ := metricNamesByMode(MetricsModeTempo)
	got, err = buildRateQuery(tempo, "", "auth", "1m", []string{spanKindServer}, nil, nil)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	want = `sum(rate(traces_spanmetrics_calls_total{job="auth",span_kind=~"SPAN_KIND_SERVER"}[1m]))`
	if got != want {
		t.Errorf("got %q\nwant %q", got, want)
	}

	if _, err := buildRateQuery(v3, "", "", "5m", nil, nil, nil); err == nil {
		t.Error("expected error for empty service name")
	}
}

func TestBuildErrorRateQuery(t *testing.T) {
	v3, _ := metricNamesByMode(MetricsModeV3)
	got, err := buildErrorRateQuery(v3, "billing", "checkout", "5m", []string{spanKindServer}, nil, nil)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	want := `sum(rate(traces_span_metrics_calls_total{job="billing/checkout",span_kind=~"SPAN_KIND_SERVER",status_code="STATUS_CODE_ERROR"}[5m]))`
	if got != want {
		t.Errorf("got %q\nwant %q", got, want)
	}
}

func TestBuildLatencyQuantileQuery(t *testing.T) {
	v3, _ := metricNamesByMode(MetricsModeV3)
	got, err := buildLatencyQuantileQuery(v3, "billing", "checkout", "5m", []string{spanKindServer}, 0.95, nil, nil)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	want := `histogram_quantile(0.95, sum by (le) (rate(traces_span_metrics_duration_seconds_bucket{job="billing/checkout",span_kind=~"SPAN_KIND_SERVER"}[5m])))`
	if got != want {
		t.Errorf("got %q\nwant %q", got, want)
	}

	// OTel mode uses the bare names (no traces_ prefix).
	otel, _ := metricNamesByMode(MetricsModeOTel)
	got, err = buildLatencyQuantileQuery(otel, "billing", "checkout", "5m", []string{spanKindServer}, 0.95, nil, nil)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	want = `histogram_quantile(0.95, sum by (le) (rate(duration_seconds_bucket{job="billing/checkout",span_kind=~"SPAN_KIND_SERVER"}[5m])))`
	if got != want {
		t.Errorf("got %q\nwant %q", got, want)
	}

	if _, err := buildLatencyQuantileQuery(v3, "", "x", "5m", nil, 1.5, nil, nil); err == nil {
		t.Error("expected error for phi out of range")
	}
}

func TestResolveMetricsMode(t *testing.T) {
	tests := []struct {
		name     string
		in       string
		wantMode MetricsMode
		wantAuto bool
		wantErr  bool
	}{
		{name: "empty defaults to auto", in: "", wantAuto: true},
		{name: "explicit auto", in: "auto", wantAuto: true},
		{name: "v3", in: "v3", wantMode: MetricsModeV3},
		{name: "otel-109 alias", in: "otel-109", wantMode: MetricsModeV3},
		{name: "tempo", in: "tempo", wantMode: MetricsModeTempo},
		{name: "legacy alias for tempo", in: "legacy", wantMode: MetricsModeTempo},
		{name: "beyla alias for tempo", in: "beyla", wantMode: MetricsModeTempo},
		{name: "otel", in: "otel", wantMode: MetricsModeOTel},
		{name: "case-insensitive", in: "TEMPO", wantMode: MetricsModeTempo},
		{name: "unknown rejected", in: "fictional-mode", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mode, auto, err := resolveMetricsMode(tt.in)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if auto != tt.wantAuto {
				t.Errorf("auto = %v, want %v", auto, tt.wantAuto)
			}
			if !auto && mode != tt.wantMode {
				t.Errorf("mode = %q, want %q", mode, tt.wantMode)
			}
		})
	}
}

func TestBuildBareNameLookupQuery(t *testing.T) {
	got, err := buildBareNameLookupQuery("target_info", "checkoutservice", nil)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	want := `group by (job) (target_info{job=~"(.+/)?checkoutservice"})`
	if got != want {
		t.Errorf("got %q\nwant %q", got, want)
	}

	// Regex metachars in the name are escaped so a service named "v1.api"
	// doesn't accidentally match "v1Xapi" via the literal `.`.
	got, err = buildBareNameLookupQuery("traces_target_info", "v1.api", nil)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	want = `group by (job) (traces_target_info{job=~"(.+/)?v1\\.api"})`
	if got != want {
		t.Errorf("got %q\nwant %q", got, want)
	}

	if _, err := buildBareNameLookupQuery("target_info", "", nil); err == nil {
		t.Error("expected error for empty name")
	}
}

func TestExtractJobsFromResponses(t *testing.T) {
	r1 := &prometheus.QueryResponse{Data: prometheus.ResultData{Result: []prometheus.Sample{
		{Metric: map[string]string{"job": "payments/checkout"}},
		{Metric: map[string]string{"job": "shipping/checkout"}},
		{Metric: map[string]string{"job": ""}}, // dropped
	}}}
	r2 := &prometheus.QueryResponse{Data: prometheus.ResultData{Result: []prometheus.Sample{
		{Metric: map[string]string{"job": "payments/checkout"}}, // duplicate across responses → dedup
		{Metric: map[string]string{"job": "checkout"}},          // bare-name shape
	}}}
	got := extractJobsFromResponses([]*prometheus.QueryResponse{r1, nil, r2})
	want := []string{"checkout", "payments/checkout", "shipping/checkout"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("[%d] got %q, want %q", i, got[i], want[i])
		}
	}
}

func TestNamespacesForName(t *testing.T) {
	tests := []struct {
		name string
		jobs []string
		svc  string
		want []string
	}{
		{name: "empty", jobs: nil, svc: "checkout", want: []string{}},
		{name: "bare only", jobs: []string{"checkout"}, svc: "checkout", want: []string{""}},
		{name: "single namespace", jobs: []string{"payments/checkout"}, svc: "checkout", want: []string{"payments"}},
		{
			name: "mixed bare + namespaced (both real possibilities)",
			jobs: []string{"checkout", "payments/checkout"},
			svc:  "checkout",
			want: []string{"", "payments"},
		},
		{
			name: "multiple namespaces sorted",
			jobs: []string{"shipping/checkout", "payments/checkout", "billing/checkout"},
			svc:  "checkout",
			want: []string{"billing", "payments", "shipping"},
		},
		{
			name: "regex over-match dropped if suffix doesn't end in /<name>",
			jobs: []string{"checkout-internal"}, // would match `(.+/)?checkout` regex without the suffix guard
			svc:  "checkout",
			want: []string{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := namespacesForName(tt.jobs, tt.svc)
			if len(got) != len(tt.want) {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("[%d] got %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestBuildModeProbeQuery(t *testing.T) {
	got, err := buildModeProbeQuery("traces_span_metrics_calls_total", "billing/checkout", nil)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	want := `count(traces_span_metrics_calls_total{job="billing/checkout"})`
	if got != want {
		t.Errorf("got %q\nwant %q", got, want)
	}

	if _, err := buildModeProbeQuery("", "x", nil); err == nil {
		t.Error("expected error for empty metric")
	}
	if _, err := buildModeProbeQuery("metric", "", nil); err == nil {
		t.Error("expected error for empty job")
	}
}

func TestJobLabel(t *testing.T) {
	cases := map[string]struct{ ns, name, want string }{
		"namespaced": {ns: "billing", name: "checkout", want: "billing/checkout"},
		"bare":       {ns: "", name: "auth", want: "auth"},
	}
	for label, tc := range cases {
		t.Run(label, func(t *testing.T) {
			if got := jobLabel(tc.ns, tc.name); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestInstantScalar(t *testing.T) {
	tests := []struct {
		name    string
		resp    *prometheus.QueryResponse
		wantVal float64
		wantHas bool
	}{
		{name: "nil response", resp: nil, wantHas: false},
		{name: "empty result", resp: &prometheus.QueryResponse{}, wantHas: false},
		{
			name: "single sample",
			resp: &prometheus.QueryResponse{Data: prometheus.ResultData{Result: []prometheus.Sample{
				{Value: []any{1.0, "12.345"}},
			}}},
			wantVal: 12.345, wantHas: true,
		},
		{
			name: "NaN treated as no data",
			resp: &prometheus.QueryResponse{Data: prometheus.ResultData{Result: []prometheus.Sample{
				{Value: []any{1.0, "NaN"}},
			}}},
			wantHas: false,
		},
		{
			name: "+Inf treated as no data",
			resp: &prometheus.QueryResponse{Data: prometheus.ResultData{Result: []prometheus.Sample{
				{Value: []any{1.0, "+Inf"}},
			}}},
			wantHas: false,
		},
		{
			name: "non-string value",
			resp: &prometheus.QueryResponse{Data: prometheus.ResultData{Result: []prometheus.Sample{
				{Value: []any{1.0, 12.34}},
			}}},
			wantHas: false,
		},
		{
			name: "short value array",
			resp: &prometheus.QueryResponse{Data: prometheus.ResultData{Result: []prometheus.Sample{
				{Value: []any{1.0}},
			}}},
			wantHas: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotVal, gotHas := instantScalar(tt.resp)
			if gotHas != tt.wantHas {
				t.Errorf("has = %v, want %v", gotHas, tt.wantHas)
			}
			if tt.wantHas && gotVal != tt.wantVal {
				t.Errorf("val = %v, want %v", gotVal, tt.wantVal)
			}
		})
	}
}

func TestComputeErrorPercent(t *testing.T) {
	tests := []struct {
		name        string
		errs, total float64
		want        float64
	}{
		{name: "zero total stays zero (no division)", total: 0, errs: 5, want: 0},
		{name: "normal", total: 100, errs: 5, want: 5},
		{name: "100%", total: 7, errs: 7, want: 100},
		{name: "clamps above 100", total: 1, errs: 3, want: 100},
		{name: "clamps negative to zero", total: 1, errs: -2, want: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := computeErrorPercent(tt.errs, tt.total); got != tt.want {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSelectMetadataService(t *testing.T) {
	rows := []Service{
		{Name: "checkout", Namespace: "billing", Language: "go", Instrumented: true},
		{Name: "checkout", Namespace: "shipping", Language: "java", Instrumented: true},
	}
	got := selectMetadataService(rows, "shipping", "checkout")
	if got.Namespace != "shipping" || got.Language != "java" {
		t.Errorf("exact match lost: %+v", got)
	}

	// No exact match → first row wins.
	got = selectMetadataService(rows, "unknown", "checkout")
	if got.Namespace != "billing" {
		t.Errorf("fallback should pick first row, got %+v", got)
	}

	// Empty list → synthesized uninstrumented placeholder.
	got = selectMetadataService(nil, "billing", "checkout")
	if got.Instrumented || got.Name != "checkout" || got.Namespace != "billing" {
		t.Errorf("placeholder wrong: %+v", got)
	}
}

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		name string
		s    float64
		has  bool
		want string
	}{
		{name: "no data", s: 0.5, has: false, want: "-"},
		{name: "microseconds", s: 0.0001, has: true, want: "100µs"},
		{name: "milliseconds", s: 0.025, has: true, want: "25.00ms"},
		{name: "seconds", s: 1.5, has: true, want: "1.500s"},
		{name: "zero with has=true is microseconds", s: 0, has: true, want: "0µs"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := formatDuration(tt.s, tt.has); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestGetOptsValidate(t *testing.T) {
	mk := func(o getOpts) getOpts {
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
		opts    getOpts
		wantErr bool
	}{
		{name: "defaults ok", opts: mk(getOpts{})},
		{name: "custom since (hours)", opts: mk(getOpts{Since: "1h"})},
		{name: "PromQL day duration accepted (Go ParseDuration rejects this)", opts: mk(getOpts{Since: "1d"})},
		{name: "PromQL week duration accepted", opts: mk(getOpts{Since: "1w"})},
		{name: "bad since", opts: mk(getOpts{Since: "not-a-duration"}), wantErr: true},
		{name: "empty since", opts: mk(getOpts{Since: "  "}), wantErr: true},
		{name: "bogus kind", opts: mk(getOpts{Kind: "OUTGOING"}), wantErr: true},
		{name: "comma kinds ok", opts: mk(getOpts{Kind: "SPAN_KIND_SERVER,SPAN_KIND_CLIENT"})},
		{name: "default metrics-mode (empty) ok", opts: mk(getOpts{MetricsMode: ""})},
		{name: "explicit tempo mode", opts: mk(getOpts{MetricsMode: "tempo"})},
		{name: "bogus metrics-mode rejected", opts: mk(getOpts{MetricsMode: "fictional"}), wantErr: true},
	}
	// Validate's UsageError wraps require a *cobra.Command for the help
	// suggestion; a bare instance is enough — we only check error-vs-nil.
	cmd := &cobra.Command{Use: "get"}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.opts.Validate(cmd); (err != nil) != tt.wantErr {
				t.Errorf("err = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestServiceDetailCodec(t *testing.T) {
	codec := &serviceDetailCodec{}
	d := &ServiceDetail{
		Service: Service{
			Name: "checkout", Namespace: "billing", Language: "go",
			Instrumented: true,
			Labels: map[string]string{
				"deployment_environment": "production",
				"k8s_namespace_name":     "prod",
			},
		},
		RED: REDStats{
			Window: "5m", SpanKinds: "SPAN_KIND_SERVER|SPAN_KIND_CONSUMER",
			RatePerSecond: 12.5, ErrorRatePerSec: 0.25, ErrorPercent: 2.0,
			P50Seconds: 0.010, P95Seconds: 0.150, P99Seconds: 0.450,
			HasTraffic: true, HasErrors: true,
			HasLatencyP50: true, HasLatencyP95: true, HasLatencyP99: true,
		},
	}
	var buf bytes.Buffer
	if err := codec.Encode(&buf, d); err != nil {
		t.Fatalf("Encode err = %v", err)
	}
	out := buf.String()
	// Describe-style block: kubectl-like "Field: value" lines, single
	// section break between identity and RED, latency under a sub-heading.
	for _, want := range []string{
		"Name:", "checkout",
		"Namespace:", "billing",
		"Language:", "go",
		"Status:", "instrumented",
		"Environment:", "production",
		"Window:", "5m",
		"Rate:", "12.500 req/s",
		"Errors:", "0.250 req/s (2.00%)",
		"Latency:",
		"p50:", "10.00ms",
		"p95:", "150.00ms",
		"p99:", "450.00ms",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q\n----\n%s", want, out)
		}
	}

	// no-data path: every measured row prints "-", but the structural
	// labels still appear so the user can tell what's missing.
	d2 := &ServiceDetail{
		Service: Service{Name: "auth"},
		RED:     REDStats{Window: "5m", SpanKinds: "SPAN_KIND_SERVER"},
	}
	buf.Reset()
	if err := codec.Encode(&buf, d2); err != nil {
		t.Fatalf("Encode err = %v", err)
	}
	out = buf.String()
	for _, want := range []string{"Name:", "auth", "Rate:", "Errors:", "Latency:", "p50:", "p95:", "p99:"} {
		if !strings.Contains(out, want) {
			t.Errorf("no-data output missing %q\n----\n%s", want, out)
		}
	}
	if strings.Count(out, "-") < 6 {
		t.Errorf("expected several dash rows for no-data, got:\n%s", out)
	}

	if err := codec.Encode(&buf, "not a *ServiceDetail"); err == nil {
		t.Error("expected error on wrong type")
	}
}
