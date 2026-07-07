package services //nolint:testpackage // Tests cover unexported helpers (buildServicesQuery, parseFilter, parseServicesResponse).

import (
	"maps"
	"math"
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/query/prometheus"
)

func TestBuildServicesQuery(t *testing.T) {
	wantGroup := "group by (telemetry_sdk_language, job, deployment_environment, deployment_environment_name, k8s_namespace_name, k8s_cluster_name, cloud_region)"

	tests := []struct {
		name     string
		metric   string
		matchers []Matcher
		extra    []string
		want     string
	}{
		{
			name:   "target_info, no matchers",
			metric: "target_info",
			want:   wantGroup + ` (target_info)`,
		},
		{
			name:   "traces_target_info, no matchers",
			metric: "traces_target_info",
			want:   wantGroup + ` (traces_target_info)`,
		},
		{
			name:     "single matcher",
			metric:   "target_info",
			matchers: []Matcher{{Label: "k8s_namespace_name", Op: "=", Value: "prod"}},
			want:     wantGroup + ` (target_info{k8s_namespace_name="prod"})`,
		},
		{
			name:   "multiple matchers",
			metric: "target_info",
			matchers: []Matcher{
				{Label: "k8s_namespace_name", Op: "=", Value: "prod"},
				{Label: "telemetry_sdk_language", Op: "=", Value: "go"},
			},
			want: wantGroup + ` (target_info{k8s_namespace_name="prod",telemetry_sdk_language="go"})`,
		},
		{
			name:   "all matcher operators",
			metric: "target_info",
			matchers: []Matcher{
				{Label: "a", Op: "=", Value: "x"},
				{Label: "b", Op: "!=", Value: "y"},
				{Label: "c", Op: "=~", Value: "z.*"},
				{Label: "d", Op: "!~", Value: "w.*"},
			},
			want: wantGroup + ` (target_info{a="x",b!="y",c=~"z.*",d!~"w.*"})`,
		},
		{
			name:   "value containing quote and backslash is escaped (no injection)",
			metric: "target_info",
			matchers: []Matcher{
				{Label: "foo", Op: "=", Value: `bar"; drop_table--`},
				{Label: "baz", Op: "=", Value: `c:\path`},
			},
			want: wantGroup + ` (target_info{foo="bar\"; drop_table--",baz="c:\\path"})`,
		},
		{
			name:   "extra columns appended once (incl. label that's not in defaults)",
			metric: "target_info",
			extra:  []string{"service_version", "k8s_pod_name", "service_namespace"},
			want:   "group by (telemetry_sdk_language, job, deployment_environment, deployment_environment_name, k8s_namespace_name, k8s_cluster_name, cloud_region, service_version, k8s_pod_name, service_namespace) (target_info)",
		},
		{
			name:   "extra columns with empty string ignored",
			metric: "target_info",
			extra:  []string{"", "service_version"},
			want:   "group by (telemetry_sdk_language, job, deployment_environment, deployment_environment_name, k8s_namespace_name, k8s_cluster_name, cloud_region, service_version) (target_info)",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := buildServicesQuery(tt.metric, tt.matchers, tt.extra)
			if err != nil {
				t.Fatalf("buildServicesQuery() err = %v", err)
			}
			if got != tt.want {
				t.Errorf("buildServicesQuery() =\n  %q\nwant\n  %q", got, tt.want)
			}
		})
	}
}

func TestSummarizeByLanguage(t *testing.T) {
	items := []Service{
		{Name: "a", Language: "go"},
		{Name: "b", Language: "go"},
		{Name: "c", Language: "java"},
		{Name: "d", Language: "java"},
		{Name: "e", Language: "java"},
		{Name: "f", Language: ""},
	}
	got := summarizeByLanguage(items)
	if got.Total != 6 {
		t.Fatalf("Total = %d, want 6", got.Total)
	}
	if len(got.ByLanguage) != 3 {
		t.Fatalf("ByLanguage len = %d, want 3", len(got.ByLanguage))
	}
	// Sorted by count desc, then language asc.
	want := []LanguageCount{
		{Language: "java", Count: 3},
		{Language: "go", Count: 2},
		{Language: "(unknown)", Count: 1},
	}
	for i, w := range want {
		if got.ByLanguage[i] != w {
			t.Errorf("ByLanguage[%d] = %+v, want %+v", i, got.ByLanguage[i], w)
		}
	}
}

func TestSummarizeByLanguage_Empty(t *testing.T) {
	got := summarizeByLanguage(nil)
	if got.Total != 0 || len(got.ByLanguage) != 0 {
		t.Errorf("expected empty summary, got %+v", got)
	}
}

func TestBuildServiceGraphQuery(t *testing.T) {
	got, err := buildServiceGraphQuery(defaultServiceGraphMetric)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	want := `group by (server, server_service_namespace) (traces_service_graph_request_total{connection_type!=""})`
	if got != want {
		t.Errorf("buildServiceGraphQuery() =\n  %q\nwant\n  %q", got, want)
	}

	got, err = buildServiceGraphQuery("traces_spanmetrics_calls_total")
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	want = `group by (server, server_service_namespace) (traces_spanmetrics_calls_total{connection_type!=""})`
	if got != want {
		t.Errorf("override =\n  %q\nwant\n  %q", got, want)
	}
}

func TestParseServiceGraphResponse(t *testing.T) {
	resp := &prometheus.QueryResponse{
		Data: prometheus.ResultData{
			Result: []prometheus.Sample{
				{Metric: map[string]string{"server": "payments"}},
				{Metric: map[string]string{"server": "checkout", "server_service_namespace": "billing"}},
				{Metric: map[string]string{"server": "payments"}}, // exact duplicate
				{Metric: map[string]string{"server": "payments", "server_service_namespace": "legacy"}},
				{Metric: map[string]string{"server": ""}}, // dropped
				{Metric: map[string]string{}},             // dropped
			},
		},
	}
	got, err := parseServiceGraphResponse(resp)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	// 3 distinct (namespace, name) pairs survive; sort puts no-namespace ahead.
	if len(got) != 3 {
		t.Fatalf("len = %d, want 3: %+v", len(got), got)
	}
	if got[0].Namespace != "" || got[0].Name != "payments" {
		t.Errorf("got[0] = %+v, want {Namespace:'' Name:payments}", got[0])
	}
	if got[1].Namespace != "billing" || got[1].Name != "checkout" {
		t.Errorf("got[1] = %+v, want {Namespace:billing Name:checkout}", got[1])
	}
	if got[2].Namespace != "legacy" || got[2].Name != "payments" {
		t.Errorf("got[2] = %+v, want {Namespace:legacy Name:payments}", got[2])
	}
	for _, s := range got {
		if s.Instrumented {
			t.Errorf("%q should be marked uninstrumented", s.Name)
		}
	}
}

func TestMergeServiceSets(t *testing.T) {
	instrumented := []Service{
		{Name: "checkout", Namespace: "oteldemo01", Language: "go", Instrumented: true},
		{Name: "payments", Namespace: "oteldemo01", Language: "java", Instrumented: true},
	}
	graph := []Service{
		// Service-graph "payments" with no namespace — folded into the
		// instrumented oteldemo01/payments via the bare-name fallback.
		{Name: "payments"},
		// Brand-new service not in target_info.
		{Name: "legacy-billing"},
	}
	got := mergeServiceSets(instrumented, instrumented, graph)
	if len(got) != 3 {
		t.Fatalf("len = %d, want 3: %+v", len(got), got)
	}
	// Sort = (namespace, name): "" < "oteldemo01".
	if got[0].Name != "legacy-billing" || got[1].Name != "checkout" || got[2].Name != "payments" {
		t.Errorf("merge not sorted by (namespace,name): %+v", got)
	}
	byName := map[string]Service{}
	for _, s := range got {
		byName[s.Name] = s
	}
	if pay := byName["payments"]; !pay.Instrumented || pay.Language != "java" {
		t.Errorf("payments instrumented metadata lost: %+v", pay)
	}
	if leg := byName["legacy-billing"]; leg.Instrumented {
		t.Errorf("legacy-billing should be uninstrumented: %+v", leg)
	}
}

func TestParseFilter(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    Matcher
		wantErr bool
	}{
		{name: "bare equals", in: "k8s_namespace_name=prod", want: Matcher{Label: "k8s_namespace_name", Op: "=", Value: "prod"}},
		{name: "quoted equals", in: `k8s_namespace_name="prod"`, want: Matcher{Label: "k8s_namespace_name", Op: "=", Value: "prod"}},
		{name: "regex match", in: "service_namespace=~payments.*", want: Matcher{Label: "service_namespace", Op: "=~", Value: "payments.*"}},
		{name: "negative match", in: "telemetry_sdk_language!=java", want: Matcher{Label: "telemetry_sdk_language", Op: "!=", Value: "java"}},
		{name: "negative regex", in: "job!~test_.*", want: Matcher{Label: "job", Op: "!~", Value: "test_.*"}},
		{name: "value with embedded quote", in: `cloud_region=eu"west`, want: Matcher{Label: "cloud_region", Op: "=", Value: `eu"west`}},
		{name: "invalid: missing operator", in: "k8s_namespace_name", wantErr: true},
		{name: "invalid: bad label", in: "1foo=bar", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseFilter(tt.in)
			if (err != nil) != tt.wantErr {
				t.Fatalf("parseFilter() err = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr && got != tt.want {
				t.Errorf("parseFilter() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestParseFilters(t *testing.T) {
	// Empty in → nil out (callers treat nil as "no scoping").
	got, err := parseFilters(nil)
	if err != nil {
		t.Fatalf("parseFilters(nil) err = %v", err)
	}
	if got != nil {
		t.Errorf("parseFilters(nil) = %v, want nil", got)
	}

	got, err = parseFilters([]string{"k8s_cluster_name=prod-us", "telemetry_sdk_language!=java"})
	if err != nil {
		t.Fatalf("parseFilters() err = %v", err)
	}
	want := []Matcher{
		{Label: "k8s_cluster_name", Op: "=", Value: "prod-us"},
		{Label: "telemetry_sdk_language", Op: "!=", Value: "java"},
	}
	if len(got) != len(want) {
		t.Fatalf("parseFilters() = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("parseFilters()[%d] = %+v, want %+v", i, got[i], want[i])
		}
	}

	if _, err := parseFilters([]string{"bogus"}); err == nil {
		t.Error("expected error for malformed filter")
	}
}

func TestParseGroupBy(t *testing.T) {
	// nil / empty in → nil out.
	if got, err := parseGroupBy(nil); err != nil || got != nil {
		t.Fatalf("parseGroupBy(nil) = %v, %v; want nil, nil", got, err)
	}

	// Comma-separated and repeated entries flatten, trim, and de-dupe
	// (order preserved by first occurrence).
	got, err := parseGroupBy([]string{"k8s_cluster_name, cloud_region", "k8s_cluster_name", " deployment_environment "})
	if err != nil {
		t.Fatalf("parseGroupBy err = %v", err)
	}
	want := []string{"k8s_cluster_name", "cloud_region", "deployment_environment"}
	if len(got) != len(want) {
		t.Fatalf("parseGroupBy = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("parseGroupBy[%d] = %q, want %q", i, got[i], want[i])
		}
	}

	// An invalid label name is rejected (protects the `by (...)` clause).
	if _, err := parseGroupBy([]string{"1bad"}); err == nil {
		t.Error("expected error for label name starting with a digit")
	}
	if _, err := parseGroupBy([]string{"has-dash"}); err == nil {
		t.Error("expected error for label name with a dash")
	}
}

func TestBuildSeriesSelector(t *testing.T) {
	got := buildSeriesSelector("traces_span_metrics_calls_total", "billing/checkout", nil)
	want := `traces_span_metrics_calls_total{job="billing/checkout"}`
	if got != want {
		t.Errorf("got %q\nwant %q", got, want)
	}

	got = buildSeriesSelector("traces_span_metrics_calls_total", "auth", []Matcher{
		{Label: "k8s_cluster_name", Op: "=", Value: "prod"},
		{Label: "cloud_region", Op: "!=", Value: `eu"west`}, // embedded quote escaped
	})
	want = `traces_span_metrics_calls_total{job="auth",k8s_cluster_name="prod",cloud_region!="eu\"west"}`
	if got != want {
		t.Errorf("got %q\nwant %q", got, want)
	}
}

func TestCollectAndSummarizeLabels(t *testing.T) {
	series := []map[string]string{
		{"__name__": "traces_span_metrics_calls_total", "job": "billing/checkout", "k8s_cluster_name": "east", "span_name": "GET /"},
		{"__name__": "traces_span_metrics_calls_total", "job": "billing/checkout", "k8s_cluster_name": "west", "span_name": "GET /"},
		{"__name__": "traces_span_metrics_calls_total", "job": "billing/checkout", "k8s_cluster_name": "east", "span_name": "POST /x"},
	}
	index := collectSeriesLabels(series)
	// __name__ dropped; job/k8s_cluster_name/span_name kept.
	if _, ok := index["__name__"]; ok {
		t.Error("__name__ should be dropped")
	}
	if len(index["k8s_cluster_name"]) != 2 {
		t.Errorf("k8s_cluster_name distinct = %d, want 2", len(index["k8s_cluster_name"]))
	}
	if len(index["job"]) != 1 {
		t.Errorf("job distinct = %d, want 1", len(index["job"]))
	}

	// Summary is sorted by name; sampleN caps values but Cardinality is full.
	rows := summarizeLabels(index, 1)
	if len(rows) != 3 {
		t.Fatalf("rows = %d, want 3: %+v", len(rows), rows)
	}
	if rows[0].Name != "job" || rows[1].Name != "k8s_cluster_name" || rows[2].Name != "span_name" {
		t.Errorf("rows not sorted by name: %+v", rows)
	}
	cluster := rows[1]
	if cluster.Cardinality != 2 || len(cluster.Values) != 1 {
		t.Errorf("cluster row wrong (want cardinality 2, 1 sampled value): %+v", cluster)
	}

	// sampleN=0 returns all values.
	rows = summarizeLabels(index, 0)
	for _, r := range rows {
		if r.Name == "k8s_cluster_name" {
			if len(r.Values) != 2 || r.Values[0] != "east" || r.Values[1] != "west" {
				t.Errorf("full values wrong/unsorted: %+v", r)
			}
		}
	}
}

func TestMergeGroupedRED(t *testing.T) {
	// Two clusters; "west" is the latency/error outlier. mergeGroupedRED
	// sorts by rate desc, so east (busier) comes first, but the point is
	// each row carries its own RED + group label.
	bucket := func(cluster string, v float64) map[string]groupBucket {
		return map[string]groupBucket{cluster: {value: v, labels: map[string]string{"k8s_cluster_name": cluster}}}
	}
	merge2 := func(a, b map[string]groupBucket) map[string]groupBucket {
		out := map[string]groupBucket{}
		maps.Copy(out, a)
		maps.Copy(out, b)
		return out
	}
	rates := merge2(bucket("east", 12), bucket("west", 3))
	errs := merge2(bucket("east", 0), bucket("west", 1.5)) // west 50% errors
	p95s := merge2(bucket("east", 0.05), bucket("west", 1.2))

	items := mergeGroupedRED("5m", MetricsModeV3, []string{spanKindServer}, []string{"k8s_cluster_name"},
		rates, errs, nil, p95s, nil)
	if len(items) != 2 {
		t.Fatalf("len = %d, want 2: %+v", len(items), items)
	}
	// Sorted by rate desc → east first.
	if items[0].Labels["k8s_cluster_name"] != "east" || items[1].Labels["k8s_cluster_name"] != "west" {
		t.Fatalf("order wrong: %q, %q", items[0].Labels["k8s_cluster_name"], items[1].Labels["k8s_cluster_name"])
	}
	west := items[1].RED
	if !west.HasTraffic || west.RatePerSecond != 3 {
		t.Errorf("west RED wrong: %+v", west)
	}
	if math.Abs(west.ErrorPercent-50) > 0.001 {
		t.Errorf("west error%% = %.4f, want 50", west.ErrorPercent)
	}
	if !west.HasLatencyP95 || west.P95Seconds != 1.2 {
		t.Errorf("west p95 wrong: %+v", west)
	}
	// east: traffic present, no error series → HasErrors inferred true, 0%.
	if east := items[0].RED; !east.HasErrors || east.ErrorPercent != 0 {
		t.Errorf("east errors wrong: %+v", east)
	}
}

// TestFilterMatchersThreadIntoQueries locks in that a --filter matcher is
// appended to every per-service query family that carries it — the RED
// span-metric queries, the operations breakdown, the service-graph edges,
// and the target_info metadata/lookup queries — so a multi-cluster service
// can be scoped one cluster at a time. It also confirms the matcher value
// is quote-escaped identically to the job selector (no injection).
func TestFilterMatchersThreadIntoQueries(t *testing.T) {
	v3, _ := metricNamesByMode(MetricsModeV3)
	m := []Matcher{{Label: "k8s_cluster_name", Op: "=", Value: "prod-us"}}
	const clusterSel = `k8s_cluster_name="prod-us"`

	checks := []struct {
		name  string
		build func() (string, error)
	}{
		{"rate", func() (string, error) {
			return buildRateQuery(v3, "billing", "checkout", "5m", []string{spanKindServer}, m, nil)
		}},
		{"error", func() (string, error) {
			return buildErrorRateQuery(v3, "billing", "checkout", "5m", []string{spanKindServer}, m, nil)
		}},
		{"latency", func() (string, error) {
			return buildLatencyQuantileQuery(v3, "billing", "checkout", "5m", []string{spanKindServer}, 0.95, m, nil)
		}},
		{"ops-rate", func() (string, error) {
			return buildOperationsRateQuery(v3, "billing", "checkout", "5m", []string{spanKindServer}, m, nil)
		}},
		{"ops-avg", func() (string, error) {
			return buildOperationsAvgLatencyQuery(v3, "billing", "checkout", "5m", []string{spanKindServer}, m, nil)
		}},
		{"map-edge", func() (string, error) {
			return buildServiceMapEdgeQuery(serviceGraphRequestTotalMetric, callersDirection, "billing", "checkout", "5m", m, nil)
		}},
		{"map-latency", func() (string, error) {
			return buildServiceMapLatencyQuery(callersDirection, "billing", "checkout", "5m", 0.95, m, nil)
		}},
		{"metadata", func() (string, error) { return buildServiceMetadataQuery("target_info", "billing", "checkout", m) }},
		{"probe", func() (string, error) { return buildModeProbeQuery(v3.calls, "billing/checkout", m) }},
		{"bare-name", func() (string, error) { return buildBareNameLookupQuery("target_info", "checkout", m) }},
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
		})
	}

	// Injection guard: a matcher value with an embedded quote is escaped so
	// it can't break out of the selector.
	evil := []Matcher{{Label: "cloud_region", Op: "=", Value: `x"} or up{`}}
	got, err := buildRateQuery(v3, "", "checkout", "5m", []string{spanKindServer}, evil, nil)
	if err != nil {
		t.Fatalf("build err = %v", err)
	}
	if !strings.Contains(got, `cloud_region="x\"} or up{"`) {
		t.Errorf("matcher value not escaped: %s", got)
	}
}

func TestParseServicesResponse(t *testing.T) {
	resp := &prometheus.QueryResponse{
		Data: prometheus.ResultData{
			Result: []prometheus.Sample{
				{Metric: map[string]string{"job": "billing/checkout", "telemetry_sdk_language": "go", "k8s_namespace_name": "prod"}},
				{Metric: map[string]string{"job": "billing/payments", "telemetry_sdk_language": "java"}},
				{Metric: map[string]string{"job": "", "telemetry_sdk_language": "go"}}, // dropped
				{Metric: map[string]string{"job": "auth", "__name__": "target_info"}},  // no namespace
			},
		},
	}
	got, err := parseServicesResponse(resp)
	if err != nil {
		t.Fatalf("parseServicesResponse() err = %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d services, want 3", len(got))
	}
	// Sort = (namespace, name): "" < "billing".
	if got[0].Namespace != "" || got[0].Name != "auth" {
		t.Errorf("got[0] = %+v, want {Namespace:'' Name:auth}", got[0])
	}
	if got[1].Namespace != "billing" || got[1].Name != "checkout" {
		t.Errorf("got[1] = %+v, want {Namespace:billing Name:checkout}", got[1])
	}
	if got[2].Namespace != "billing" || got[2].Name != "payments" {
		t.Errorf("got[2] = %+v, want {Namespace:billing Name:payments}", got[2])
	}
	if got[0].Language != "" {
		t.Errorf("auth language = %q, want empty", got[0].Language)
	}
	if got[1].Labels["k8s_namespace_name"] != "prod" {
		t.Errorf("checkout labels = %v, want k8s_namespace_name=prod", got[1].Labels)
	}
	if _, has := got[0].Labels["__name__"]; has {
		t.Errorf("__name__ leaked into labels: %v", got[0].Labels)
	}
}

func TestParseServicesResponse_Nil(t *testing.T) {
	if _, err := parseServicesResponse(nil); err == nil {
		t.Fatal("expected error for nil response")
	}
}

func TestParseServicesResponse_MergesByNameLanguage(t *testing.T) {
	resp := &prometheus.QueryResponse{
		Data: prometheus.ResultData{
			Result: []prometheus.Sample{
				{Metric: map[string]string{"job": "billing/checkout", "telemetry_sdk_language": "go", "k8s_namespace_name": "prod", "cloud_region": ""}},
				{Metric: map[string]string{"job": "billing/checkout", "telemetry_sdk_language": "go", "k8s_namespace_name": "prod", "cloud_region": "us-east"}},
				{Metric: map[string]string{"job": "billing/checkout", "telemetry_sdk_language": "go", "k8s_namespace_name": "staging"}},
			},
		},
	}
	got, err := parseServicesResponse(resp)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 merged service, got %d: %+v", len(got), got)
	}
	if got[0].Namespace != "billing" || got[0].Name != "checkout" {
		t.Errorf("got = %+v, want {Namespace:billing Name:checkout}", got[0])
	}
	if got[0].Labels["cloud_region"] != "us-east" {
		t.Errorf("cloud_region not picked up across samples: %v", got[0].Labels)
	}
	// First non-empty k8s namespace wins (prod, seen first).
	if got[0].Labels["k8s_namespace_name"] != "prod" {
		t.Errorf("k8s_namespace_name = %q, want prod", got[0].Labels["k8s_namespace_name"])
	}
}

func TestParseJob(t *testing.T) {
	tests := []struct {
		in            string
		wantNamespace string
		wantName      string
	}{
		{"oteldemo01/checkoutservice", "oteldemo01", "checkoutservice"},
		{"flagd", "", "flagd"},
		{"oteldemo01/foo/bar", "oteldemo01", "foo/bar"}, // first slash only
		{"/leading-slash", "", "leading-slash"},
		{"", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			ns, n := parseJob(tt.in)
			if ns != tt.wantNamespace || n != tt.wantName {
				t.Errorf("parseJob(%q) = (%q, %q), want (%q, %q)", tt.in, ns, n, tt.wantNamespace, tt.wantName)
			}
		})
	}
}
