package services //nolint:testpackage // Tests cover unexported helpers (buildServicesQuery, parseFilter, parseServicesResponse).

import (
	"testing"

	"github.com/grafana/gcx/internal/query/prometheus"
)

func TestBuildServicesQuery(t *testing.T) {
	wantGroup := "group by (telemetry_sdk_language, job, service_namespace, k8s_namespace_name, k8s_cluster_name, cloud_region)"

	tests := []struct {
		name    string
		metric  string
		filters []string
		want    string
	}{
		{
			name: "default metric, no filters",
			want: wantGroup + ` (target_info)`,
		},
		{
			name:   "explicit metric, no filters",
			metric: "otel_target_info",
			want:   wantGroup + ` (otel_target_info)`,
		},
		{
			name:    "default metric with single filter",
			filters: []string{`k8s_namespace_name="prod"`},
			want:    wantGroup + ` (target_info{k8s_namespace_name="prod"})`,
		},
		{
			name:    "default metric with multiple filters",
			filters: []string{`k8s_namespace_name="prod"`, `telemetry_sdk_language="go"`},
			want:    wantGroup + ` (target_info{k8s_namespace_name="prod", telemetry_sdk_language="go"})`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildServicesQuery(tt.metric, tt.filters)
			if got != tt.want {
				t.Errorf("buildServicesQuery() =\n  %q\nwant\n  %q", got, tt.want)
			}
		})
	}
}

func TestParseFilter(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    string
		wantErr bool
	}{
		{name: "bare equals", in: "k8s_namespace_name=prod", want: `k8s_namespace_name="prod"`},
		{name: "quoted equals", in: `k8s_namespace_name="prod"`, want: `k8s_namespace_name="prod"`},
		{name: "regex match", in: "service_namespace=~payments.*", want: `service_namespace=~"payments.*"`},
		{name: "negative match", in: "telemetry_sdk_language!=java", want: `telemetry_sdk_language!="java"`},
		{name: "negative regex", in: "job!~test_.*", want: `job!~"test_.*"`},
		{name: "value with quote escaped", in: `cloud_region=eu"west`, want: `cloud_region="eu\"west"`},
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
				t.Errorf("parseFilter() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestParseServicesResponse(t *testing.T) {
	resp := &prometheus.QueryResponse{
		Data: prometheus.ResultData{
			Result: []prometheus.Sample{
				{Metric: map[string]string{"job": "checkout", "telemetry_sdk_language": "go", "k8s_namespace_name": "prod"}},
				{Metric: map[string]string{"job": "payments", "telemetry_sdk_language": "java"}},
				{Metric: map[string]string{"job": "", "telemetry_sdk_language": "go"}}, // dropped
				{Metric: map[string]string{"job": "auth", "__name__": "target_info"}},
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
	if got[0].Name != "auth" || got[1].Name != "checkout" || got[2].Name != "payments" {
		t.Errorf("services not sorted by name: %+v", got)
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
				{Metric: map[string]string{"job": "checkout", "telemetry_sdk_language": "go", "k8s_namespace_name": "prod", "cloud_region": ""}},
				{Metric: map[string]string{"job": "checkout", "telemetry_sdk_language": "go", "k8s_namespace_name": "prod", "cloud_region": "us-east"}},
				{Metric: map[string]string{"job": "checkout", "telemetry_sdk_language": "go", "k8s_namespace_name": "staging"}},
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
	if got[0].Labels["cloud_region"] != "us-east" {
		t.Errorf("cloud_region not picked up across samples: %v", got[0].Labels)
	}
	// First non-empty namespace wins (prod, seen first).
	if got[0].Labels["k8s_namespace_name"] != "prod" {
		t.Errorf("k8s_namespace_name = %q, want prod", got[0].Labels["k8s_namespace_name"])
	}
}
