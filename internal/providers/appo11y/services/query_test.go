package services //nolint:testpackage // Tests cover unexported helpers (buildServicesQuery, parseFilter, parseServicesResponse).

import (
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
