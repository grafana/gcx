package services //nolint:testpackage // Tests cover unexported builders, merge logic, and codecs.

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/query/prometheus"
	"github.com/spf13/cobra"
)

func TestBuildServiceMapEdgeQuery_Callers(t *testing.T) {
	got, err := buildServiceMapEdgeQuery(serviceGraphRequestTotalMetric, callersDirection, "billing", "checkout", "5m", nil, nil)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	// Callers: X is the server; group by the client side + connection_type.
	want := `sum by (client, client_service_namespace, connection_type) (rate(traces_service_graph_request_total{server="checkout",server_service_namespace="billing"}[5m]))`
	if got != want {
		t.Errorf("got %q\nwant %q", got, want)
	}

	// With --group-by, the label joins the peer aggregation.
	got, err = buildServiceMapEdgeQuery(serviceGraphRequestTotalMetric, callersDirection, "billing", "checkout", "5m", nil, []string{"k8s_cluster_name"})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	want = `sum by (client, client_service_namespace, connection_type, k8s_cluster_name) (rate(traces_service_graph_request_total{server="checkout",server_service_namespace="billing"}[5m]))`
	if got != want {
		t.Errorf("grouped got %q\nwant %q", got, want)
	}
}

func TestBuildServiceMapEdgeQuery_Callees(t *testing.T) {
	got, err := buildServiceMapEdgeQuery(serviceGraphRequestTotalMetric, calleesDirection, "billing", "checkout", "5m", nil, nil)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	// Callees: X is the client; group by the server side + connection_type.
	want := `sum by (server, server_service_namespace, connection_type) (rate(traces_service_graph_request_total{client="checkout",client_service_namespace="billing"}[5m]))`
	if got != want {
		t.Errorf("got %q\nwant %q", got, want)
	}
}

func TestBuildServiceMapEdgeQuery_BareName(t *testing.T) {
	got, err := buildServiceMapEdgeQuery(serviceGraphRequestFailedTotalMetric, callersDirection, "", "auth", "1m", nil, nil)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	want := `sum by (client, client_service_namespace, connection_type) (rate(traces_service_graph_request_failed_total{server="auth"}[1m]))`
	if got != want {
		t.Errorf("got %q\nwant %q", got, want)
	}

	if _, err := buildServiceMapEdgeQuery(serviceGraphRequestTotalMetric, callersDirection, "", "", "5m", nil, nil); err == nil {
		t.Error("expected error for empty service name")
	}
	if _, err := buildServiceMapEdgeQuery("", callersDirection, "", "auth", "5m", nil, nil); err == nil {
		t.Error("expected error for empty metric")
	}
}

func TestBuildServiceMapLatencyQuery_DirectionPicksHistogram(t *testing.T) {
	// Callers should query the server_seconds bucket (how long X took to respond).
	got, err := buildServiceMapLatencyQuery(callersDirection, "billing", "checkout", "5m", 0.95, nil, nil)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	want := `histogram_quantile(0.95, sum by (le, client, client_service_namespace, connection_type) (rate(traces_service_graph_request_server_seconds_bucket{server="checkout",server_service_namespace="billing"}[5m])))`
	if got != want {
		t.Errorf("callers latency query wrong\ngot %q\nwant %q", got, want)
	}

	// Callees should query the client_seconds bucket (how long X waited on the peer).
	got, err = buildServiceMapLatencyQuery(calleesDirection, "billing", "checkout", "5m", 0.95, nil, nil)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	want = `histogram_quantile(0.95, sum by (le, server, server_service_namespace, connection_type) (rate(traces_service_graph_request_client_seconds_bucket{client="checkout",client_service_namespace="billing"}[5m])))`
	if got != want {
		t.Errorf("callees latency query wrong\ngot %q\nwant %q", got, want)
	}

	if _, err := buildServiceMapLatencyQuery(callersDirection, "", "x", "5m", 1.5, nil, nil); err == nil {
		t.Error("expected error for phi out of range")
	}
}

func TestExtractEdges(t *testing.T) {
	// Two peers, one with namespace + connection_type, one bare. Empty
	// peer name dropped; NaN value dropped; short value array dropped.
	resp := &prometheus.QueryResponse{Data: prometheus.ResultData{Result: []prometheus.Sample{
		{Metric: map[string]string{"client": "frontend", "client_service_namespace": "oteldemo01", "connection_type": ""}, Value: []any{1.0, "0.5"}},
		{Metric: map[string]string{"client": "postgres", "connection_type": "database"}, Value: []any{1.0, "0.044"}},
		{Metric: map[string]string{"client": "", "connection_type": ""}, Value: []any{1.0, "9.0"}},                 // dropped
		{Metric: map[string]string{"client": "user", "connection_type": "virtual_node"}, Value: []any{1.0, "NaN"}}, // dropped
		{Metric: map[string]string{"client": "noop"}, Value: []any{1.0}},                                           // dropped (short)
	}}}
	got := extractEdges(resp, "client", "client_service_namespace", nil)
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2: %v", len(got), got)
	}
	if got[edgeKey{name: "frontend", namespace: "oteldemo01", connType: ""}].value != 0.5 {
		t.Errorf("frontend value wrong: %v", got)
	}
	if got[edgeKey{name: "postgres", namespace: "", connType: "database"}].value != 0.044 {
		t.Errorf("postgres value wrong: %v", got)
	}

	if got := extractEdges(nil, "client", "client_service_namespace", nil); len(got) != 0 {
		t.Errorf("nil response should produce empty map, got %v", got)
	}
}

func TestExtractEdges_Grouped(t *testing.T) {
	// Same peer in two clusters → two distinct edgeKeys, each carrying
	// its group labels for display.
	resp := &prometheus.QueryResponse{Data: prometheus.ResultData{Result: []prometheus.Sample{
		{Metric: map[string]string{"client": "frontend", "k8s_cluster_name": "east"}, Value: []any{1.0, "0.5"}},
		{Metric: map[string]string{"client": "frontend", "k8s_cluster_name": "west"}, Value: []any{1.0, "0.2"}},
	}}}
	got := extractEdges(resp, "client", "client_service_namespace", []string{"k8s_cluster_name"})
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2: %v", len(got), got)
	}
	east := got[edgeKey{name: "frontend", groupKey: "east"}]
	if east.value != 0.5 || east.labels["k8s_cluster_name"] != "east" {
		t.Errorf("east edge wrong: %+v", east)
	}
}

func TestMergeEdges(t *testing.T) {
	rates := map[edgeKey]groupBucket{
		{name: "A", namespace: "ns"}:             {value: 1.0},
		{name: "B", namespace: "ns"}:             {value: 5.0},
		{name: "postgres", connType: "database"}: {value: 0.5},
	}
	errors := map[edgeKey]groupBucket{
		{name: "A", namespace: "ns"}: {value: 0.1}, // 10% error
	}
	p95s := map[edgeKey]groupBucket{
		{name: "B", namespace: "ns"}: {value: 0.020},
	}
	got := mergeEdges(rates, errors, p95s, nil)
	if len(got) != 3 {
		t.Fatalf("len = %d, want 3", len(got))
	}
	// Sorted by rate desc: B (5) > A (1) > postgres (0.5).
	if got[0].Peer.Name != "B" || got[1].Peer.Name != "A" || got[2].Peer.Name != "postgres" {
		t.Errorf("unexpected order: %v %v %v", got[0].Peer.Name, got[1].Peer.Name, got[2].Peer.Name)
	}

	byName := map[string]Edge{}
	for _, e := range got {
		byName[e.Peer.Name] = e
	}
	// A: has errors observed → 10%.
	if a := byName["A"]; a.ErrorPercent != 10 || !a.HasErrors {
		t.Errorf("A wrong: %+v", a)
	}
	// B: has traffic but no error series → HasErrors should still be true (healthy 0%).
	if b := byName["B"]; b.ErrorPercent != 0 || !b.HasErrors {
		t.Errorf("B wrong: %+v", b)
	}
	// postgres: connType preserved; no latency observed.
	if pg := byName["postgres"]; pg.ConnectionType != "database" || pg.HasLatency {
		t.Errorf("postgres wrong: %+v", pg)
	}
}

func TestMergeEdges_Empty(t *testing.T) {
	if got := mergeEdges(nil, nil, nil, nil); len(got) != 0 {
		t.Errorf("expected empty slice, got %v", got)
	}
}

func TestMapOptsValidate(t *testing.T) {
	mk := func(o mapOpts) mapOpts {
		o.IO.OutputFormat = "json"
		if o.Since == "" {
			o.Since = defaultRedWindow
		}
		if o.KG.Mode == "" {
			o.KG.Mode = string(kgModeAuto)
		}
		return o
	}
	tests := []struct {
		name    string
		opts    mapOpts
		wantErr bool
	}{
		{name: "defaults ok", opts: mk(mapOpts{})},
		{name: "PromQL day", opts: mk(mapOpts{Since: "1d"})},
		{name: "bad since", opts: mk(mapOpts{Since: "not-a-duration"}), wantErr: true},
		{name: "empty since", opts: mk(mapOpts{Since: "  "}), wantErr: true},
	}
	cmd := &cobra.Command{Use: "map"}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.opts.Validate(cmd); (err != nil) != tt.wantErr {
				t.Errorf("err = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestServiceMapTableCodec(t *testing.T) {
	codec := &serviceMapTableCodec{}
	resp := &ServiceMap{
		Service: Service{Name: "checkout", Namespace: "billing"},
		Window:  "5m",
		Callers: []Edge{
			{Peer: Service{Name: "frontend", Namespace: "billing"}, RatePerSecond: 0.5, ErrorPercent: 0, P95Seconds: 0.072, HasErrors: true, HasLatency: true},
		},
		Callees: []Edge{
			{Peer: Service{Name: "cartservice", Namespace: "billing"}, RatePerSecond: 0.044, ErrorPercent: 0, P95Seconds: 0.025, HasErrors: true, HasLatency: true},
			{Peer: Service{Name: "postgres"}, ConnectionType: "database", RatePerSecond: 0.044, HasErrors: true},
		},
	}
	var buf bytes.Buffer
	if err := codec.Encode(&buf, resp); err != nil {
		t.Fatalf("Encode err = %v", err)
	}
	out := buf.String()
	for _, want := range []string{"CALLERS", "CALLEES", "frontend (billing)", "cartservice (billing)", "postgres", "0.500 req/s", "0.044 req/s", "database", "72.00ms"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q\n----\n%s", want, out)
		}
	}

	// Empty section renders "(none)" instead of an empty table.
	buf.Reset()
	if err := codec.Encode(&buf, &ServiceMap{Service: Service{Name: "lonely"}}); err != nil {
		t.Fatalf("Encode err = %v", err)
	}
	if out := buf.String(); !strings.Contains(out, "CALLERS: (none)") || !strings.Contains(out, "CALLEES: (none)") {
		t.Errorf("expected `(none)` placeholders, got:\n%s", out)
	}

	if err := codec.Encode(&buf, "not a response"); err == nil {
		t.Error("expected error on wrong type")
	}
}

func TestServiceMapMermaidCodec(t *testing.T) {
	codec := &serviceMapMermaidCodec{}
	resp := &ServiceMap{
		Service: Service{Name: "checkout", Namespace: "billing"},
		Window:  "5m",
		Callers: []Edge{
			{Peer: Service{Name: "frontend", Namespace: "billing"}, RatePerSecond: 0.5},
		},
		Callees: []Edge{
			{Peer: Service{Name: "postgres"}, ConnectionType: connTypeDatabase, RatePerSecond: 0.044},
			{Peer: Service{Name: "kafka"}, ConnectionType: connTypeMessagingSystem, RatePerSecond: 0.022},
			{Peer: Service{Name: "user"}, ConnectionType: connTypeVirtualNode, RatePerSecond: 0.01},
		},
	}
	var buf bytes.Buffer
	if err := codec.Encode(&buf, resp); err != nil {
		t.Fatalf("Encode err = %v", err)
	}
	out := buf.String()
	for _, want := range []string{
		"graph LR",
		`{{"billing/checkout"}}`, // center hex-shape
		`["frontend (billing)"]`, // regular peer rectangle
		`[("postgres")]`,         // database cylinder
		`[/"kafka"\]`,            // messaging_system queue
		`("user")`,               // virtual_node rounded
		"-->|0.500 req/s|",       // edge label
	} {
		if !strings.Contains(out, want) {
			t.Errorf("mermaid output missing %q\n----\n%s", want, out)
		}
	}
}

func TestMermaidNodeID(t *testing.T) {
	tests := []struct{ name, ns, want string }{
		{name: "checkout", ns: "billing", want: "billing_checkout"},
		{name: "checkout-service", ns: "billing.prod", want: "billing_prod_checkout_service"}, // dots/hyphens folded
		{name: "auth", ns: "", want: "auth"},
		{name: "", ns: "", want: "node"}, // fallback so we never emit an empty ID
	}
	for _, tt := range tests {
		if got := mermaidNodeID(tt.name, tt.ns); got != tt.want {
			t.Errorf("mermaidNodeID(%q, %q) = %q, want %q", tt.name, tt.ns, got, tt.want)
		}
	}
}

func TestServiceMapDOTCodec(t *testing.T) {
	codec := &serviceMapDOTCodec{}
	resp := &ServiceMap{
		Service: Service{Name: "checkout", Namespace: "billing"},
		Callers: []Edge{
			{Peer: Service{Name: "frontend", Namespace: "billing"}, RatePerSecond: 0.5},
		},
		Callees: []Edge{
			{Peer: Service{Name: "postgres"}, ConnectionType: connTypeDatabase, RatePerSecond: 0.044},
			{Peer: Service{Name: "kafka"}, ConnectionType: connTypeMessagingSystem, RatePerSecond: 0.02},
		},
	}
	var buf bytes.Buffer
	if err := codec.Encode(&buf, resp); err != nil {
		t.Fatalf("Encode err = %v", err)
	}
	out := buf.String()
	for _, want := range []string{
		"digraph G {",
		"rankdir=LR",
		`"billing/checkout"`,  // center node
		"fillcolor=lightblue", // center styling
		`"frontend (billing)" -> "billing/checkout"`, // caller edge
		`"billing/checkout" -> "postgres"`,           // callee edge
		"shape=cylinder",                             // database shape
		"shape=parallelogram",                        // messaging_system shape
		`label="0.500 req/s"`,                        // rate label
	} {
		if !strings.Contains(out, want) {
			t.Errorf("dot output missing %q\n----\n%s", want, out)
		}
	}
}

func TestDirectionLatencyBucketMetric(t *testing.T) {
	if callersDirection.latencyBucketMetric() != serviceGraphRequestServerBucketMetric {
		t.Errorf("callers should use server_seconds_bucket")
	}
	if calleesDirection.latencyBucketMetric() != serviceGraphRequestClientBucketMetric {
		t.Errorf("callees should use client_seconds_bucket")
	}
}

func TestFetchServiceMapInstrumentation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Queries []struct {
				Expr string `json:"expr"`
			} `json:"queries"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		expr := body.Queries[0].Expr
		var labels []map[string]string
		switch {
		case strings.Contains(expr, "target_info"):
			if strings.Contains(expr, `k8s_cluster_name="test-cluster"`) {
				t.Errorf("edge filter restricts metadata baseline: %s", expr)
			}
			if !strings.Contains(expr, "(.+/)?inventory") {
				t.Errorf("namespace-less peer missing from metadata matcher: %s", expr)
			}
			if !strings.Contains(expr, "(.+/)?payment") {
				t.Errorf("namespaced peer missing from metadata matcher: %s", expr)
			}
			if !strings.Contains(expr, `job=~`) {
				t.Errorf("metadata is not scoped: %s", expr)
			}
			labels = []map[string]string{{"job": "billing/checkout"}, {"job": "billing/frontend"}, {"job": "payment"}, {"job": "other/inventory"}, {"job": "postgres"}, {"job": "user"}}
		case strings.Contains(expr, `server="checkout"`):
			labels = []map[string]string{{"client": "frontend", "client_service_namespace": "billing"}}
		default:
			labels = []map[string]string{{"server": "payment", "server_service_namespace": "billing"}, {"server": "inventory"}, {"server": "postgres", "connection_type": "database"}, {"server": "user", "connection_type": "virtual_node"}}
		}
		frames := make([]any, 0, len(labels))
		for _, l := range labels {
			frames = append(frames, map[string]any{"schema": map[string]any{"fields": []any{map[string]any{"name": "Time", "type": "time"}, map[string]any{"name": "Value", "type": "number", "labels": l}}}, "data": map[string]any{"values": []any{[]int{1000}, []int{1}}}})
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"results": map[string]any{"A": map[string]any{"frames": frames}}})
	}))
	defer server.Close()
	client, err := prometheus.NewClient(newKGTestConfig(server.URL))
	if err != nil {
		t.Fatal(err)
	}
	result, err := fetchServiceMap(context.Background(), client, "prom", "billing", "checkout", "5m", []Matcher{{Label: "k8s_cluster_name", Op: "=", Value: "test-cluster"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Service.Instrumented {
		t.Error("checkout metadata proves instrumentation")
	}
	for _, e := range append(result.Callers, result.Callees...) {
		want := e.Peer.Name == "frontend" || e.Peer.Name == "payment" || e.Peer.Name == "inventory"
		if e.Peer.Instrumented != want {
			t.Errorf("%s instrumented=%v want=%v", e.Peer.Name, e.Peer.Instrumented, want)
		}
	}
}

func TestFetchServiceMapMetadataError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Queries []struct {
				Expr string `json:"expr"`
			} `json:"queries"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if strings.Contains(body.Queries[0].Expr, "target_info") {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"results":{"A":{"frames":[]}}}`))
	}))
	defer server.Close()
	client, err := prometheus.NewClient(newKGTestConfig(server.URL))
	if err != nil {
		t.Fatal(err)
	}
	result, err := fetchServiceMap(context.Background(), client, "prom", "billing", "checkout", "5m", nil, nil)
	if err == nil || result != nil {
		t.Fatalf("metadata denial must not produce uninstrumented result: result=%+v err=%v", result, err)
	}
}
