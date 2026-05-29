package kg_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/providers/kg"
	"github.com/grafana/gcx/internal/query/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"
)

func newTestPromClient(t *testing.T, server *httptest.Server) *prometheus.Client {
	t.Helper()
	cfg := config.NamespacedRESTConfig{
		Config:    rest.Config{Host: server.URL},
		Namespace: "stack-123",
	}
	c, err := prometheus.NewClient(cfg)
	require.NoError(t, err)
	return c
}

func TestRunDiagnose_AllHealthy(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/plugins/grafana-asserts-app/resources/asserts/api-server/v1/stack/status":
			writeJSON(w, kg.Status{
				Status:  "complete",
				Enabled: true,
				SanityCheckResults: []kg.SanityCheckResult{
					{CheckName: "traces_service_graph", DataPresent: true},
				},
			})
		case "/api/plugins/grafana-asserts-app/resources/asserts/api-server/v1/entity_type/count":
			writeJSON(w, map[string]int64{"Service": 10, "Pod": 20})
		case "/api/plugins/grafana-asserts-app/resources/asserts/api-server/v1/entity_scope":
			writeJSON(w, map[string]any{"scopeValues": map[string][]string{
				"env":       {"production"},
				"site":      {"us-east-1"},
				"namespace": {"default"},
			}})
		case "/api/plugins/grafana-asserts-app/resources/asserts/api-server/v2/config/log":
			writeJSON(w, kg.LogConfigsResponse{LogDrilldownConfigs: []kg.LogDrilldownConfig{{Name: "default-loki"}}})
		case "/api/plugins/grafana-asserts-app/resources/asserts/api-server/v2/config/trace":
			writeJSON(w, kg.TraceConfigsResponse{TraceDrilldownConfigs: []kg.TraceDrilldownConfig{{Name: "default-tempo"}}})
		case "/api/plugins/grafana-asserts-app/resources/asserts/api-server/v2/config/profile":
			writeJSON(w, kg.ProfileConfigsResponse{ProfileDrilldownConfigs: []kg.ProfileDrilldownConfig{{Name: "default-pyroscope"}}})
		default:
			http.NotFound(w, r)
		}
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	client := newTestClient(t, server)
	scope := kg.NewTestScopeFlags("", "", "")
	result := kg.RunDiagnose(t.Context(), client, &scope, nil, "")

	assert.Equal(t, 7, result.Summary.Total)
	assert.Equal(t, 7, result.Summary.Passed)
	assert.Equal(t, 0, result.Summary.Failed)
	assert.Equal(t, 0, result.Summary.Warned)
}

func TestRunDiagnose_StackDisabled(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/plugins/grafana-asserts-app/resources/asserts/api-server/v1/stack/status":
			writeJSON(w, kg.Status{Status: "not_initialized", Enabled: false})
		case "/api/plugins/grafana-asserts-app/resources/asserts/api-server/v1/entity_type/count":
			writeJSON(w, map[string]int64{})
		case "/api/plugins/grafana-asserts-app/resources/asserts/api-server/v1/entity_scope":
			writeJSON(w, map[string]any{"scopeValues": map[string][]string{}})
		case "/api/plugins/grafana-asserts-app/resources/asserts/api-server/v2/config/log":
			writeJSON(w, kg.LogConfigsResponse{})
		case "/api/plugins/grafana-asserts-app/resources/asserts/api-server/v2/config/trace":
			writeJSON(w, kg.TraceConfigsResponse{})
		case "/api/plugins/grafana-asserts-app/resources/asserts/api-server/v2/config/profile":
			writeJSON(w, kg.ProfileConfigsResponse{})
		default:
			http.NotFound(w, r)
		}
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	client := newTestClient(t, server)
	scope := kg.NewTestScopeFlags("", "", "")
	result := kg.RunDiagnose(t.Context(), client, &scope, nil, "")

	// Stack status should fail.
	var stackCheck *kg.CheckResult
	for i := range result.Checks {
		if result.Checks[i].Name == "Stack status" {
			stackCheck = &result.Checks[i]
			break
		}
	}
	require.NotNil(t, stackCheck)
	assert.Equal(t, kg.CheckFail, stackCheck.Status)
	assert.Contains(t, stackCheck.Detail, "not_initialized")
	assert.NotEmpty(t, stackCheck.Recommendation)
}

func TestRunDiagnose_SanityCheckBlocker(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/plugins/grafana-asserts-app/resources/asserts/api-server/v1/stack/status":
			writeJSON(w, kg.Status{
				Status:  "complete",
				Enabled: true,
				SanityCheckResults: []kg.SanityCheckResult{
					{
						CheckName:   "traces_service_graph",
						DataPresent: false,
						StepResults: []kg.SanityStepResult{
							{
								Name:         "traces_service_graph_request_total present",
								Blockers:     []string{"metric not found"},
								Troubleshoot: "Verify Tempo metrics generation is enabled.",
							},
						},
					},
				},
			})
		case "/api/plugins/grafana-asserts-app/resources/asserts/api-server/v1/entity_type/count":
			writeJSON(w, map[string]int64{"Service": 5})
		case "/api/plugins/grafana-asserts-app/resources/asserts/api-server/v1/entity_scope":
			writeJSON(w, map[string]any{"scopeValues": map[string][]string{"env": {"prod"}}})
		case "/api/plugins/grafana-asserts-app/resources/asserts/api-server/v2/config/log":
			writeJSON(w, kg.LogConfigsResponse{})
		case "/api/plugins/grafana-asserts-app/resources/asserts/api-server/v2/config/trace":
			writeJSON(w, kg.TraceConfigsResponse{})
		case "/api/plugins/grafana-asserts-app/resources/asserts/api-server/v2/config/profile":
			writeJSON(w, kg.ProfileConfigsResponse{})
		default:
			http.NotFound(w, r)
		}
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	client := newTestClient(t, server)
	scope := kg.NewTestScopeFlags("", "", "")
	result := kg.RunDiagnose(t.Context(), client, &scope, nil, "")

	var sanityCheck *kg.CheckResult
	for i := range result.Checks {
		if result.Checks[i].Name == "Sanity: traces_service_graph" {
			sanityCheck = &result.Checks[i]
			break
		}
	}
	require.NotNil(t, sanityCheck)
	assert.Equal(t, kg.CheckFail, sanityCheck.Status)
	assert.Contains(t, sanityCheck.Detail, "blocker")
	assert.Equal(t, "Verify Tempo metrics generation is enabled.", sanityCheck.Recommendation)
}

func TestRunDiagnose_NoEntities(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/plugins/grafana-asserts-app/resources/asserts/api-server/v1/stack/status":
			writeJSON(w, kg.Status{Status: "complete", Enabled: true})
		case "/api/plugins/grafana-asserts-app/resources/asserts/api-server/v1/entity_type/count":
			writeJSON(w, map[string]int64{})
		case "/api/plugins/grafana-asserts-app/resources/asserts/api-server/v1/entity_scope":
			writeJSON(w, map[string]any{"scopeValues": map[string][]string{"env": {"prod"}}})
		case "/api/plugins/grafana-asserts-app/resources/asserts/api-server/v2/config/log":
			writeJSON(w, kg.LogConfigsResponse{})
		case "/api/plugins/grafana-asserts-app/resources/asserts/api-server/v2/config/trace":
			writeJSON(w, kg.TraceConfigsResponse{})
		case "/api/plugins/grafana-asserts-app/resources/asserts/api-server/v2/config/profile":
			writeJSON(w, kg.ProfileConfigsResponse{})
		default:
			http.NotFound(w, r)
		}
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	client := newTestClient(t, server)
	scope := kg.NewTestScopeFlags("", "", "")
	result := kg.RunDiagnose(t.Context(), client, &scope, nil, "")

	var entityCheck *kg.CheckResult
	for i := range result.Checks {
		if result.Checks[i].Name == "Entity counts" {
			entityCheck = &result.Checks[i]
			break
		}
	}
	require.NotNil(t, entityCheck)
	assert.Equal(t, kg.CheckFail, entityCheck.Status)
}

func TestDiagnoseTableCodec_Encode(t *testing.T) {
	result := kg.DiagnoseResult{
		Env: "production",
		Checks: []kg.CheckResult{
			{Name: "Stack status", Status: kg.CheckPass, Detail: "status=complete"},
			{Name: "Entity counts", Status: kg.CheckFail, Detail: "no entities", Recommendation: "Check recording rules."},
		},
	}
	result.Summary.Total = 2
	result.Summary.Passed = 1
	result.Summary.Failed = 1

	codec := &kg.DiagnoseTableCodec{}
	var buf bytes.Buffer
	err := codec.Encode(&buf, result)
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "CHECK")
	assert.Contains(t, output, "PASS")
	assert.Contains(t, output, "FAIL")
	assert.Contains(t, output, "Check recording rules.")
	assert.Contains(t, output, "1/2 checks passed")
}

func TestDiagnoseResult_JSONRoundTrip(t *testing.T) {
	result := kg.DiagnoseResult{
		Checks: []kg.CheckResult{
			{Name: "Stack status", Status: kg.CheckPass, Detail: "ok"},
		},
	}
	result.Summary.Total = 1
	result.Summary.Passed = 1

	b, err := json.Marshal(result)
	require.NoError(t, err)

	var decoded kg.DiagnoseResult
	require.NoError(t, json.Unmarshal(b, &decoded))
	assert.Equal(t, result.Checks[0].Name, decoded.Checks[0].Name)
	assert.Equal(t, result.Checks[0].Status, decoded.Checks[0].Status)
}

// minimalKGServer returns an httptest.Server with a minimal healthy KG mock.
func minimalKGServer() *httptest.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/plugins/grafana-asserts-app/resources/asserts/api-server/v1/stack/status":
			writeJSON(w, kg.Status{Status: "complete", Enabled: true})
		case "/api/plugins/grafana-asserts-app/resources/asserts/api-server/v1/entity_type/count":
			writeJSON(w, map[string]int64{"Service": 5})
		case "/api/plugins/grafana-asserts-app/resources/asserts/api-server/v1/entity_scope":
			writeJSON(w, map[string]any{"scopeValues": map[string][]string{"env": {"prod"}}})
		case "/api/plugins/grafana-asserts-app/resources/asserts/api-server/v2/config/log":
			writeJSON(w, kg.LogConfigsResponse{LogDrilldownConfigs: []kg.LogDrilldownConfig{{Name: "loki"}}})
		case "/api/plugins/grafana-asserts-app/resources/asserts/api-server/v2/config/trace":
			writeJSON(w, kg.TraceConfigsResponse{TraceDrilldownConfigs: []kg.TraceDrilldownConfig{{Name: "tempo"}}})
		case "/api/plugins/grafana-asserts-app/resources/asserts/api-server/v2/config/profile":
			writeJSON(w, kg.ProfileConfigsResponse{})
		default:
			http.NotFound(w, r)
		}
	})
	return httptest.NewServer(mux)
}

func TestRunDiagnose_MetricChecksPass(t *testing.T) {
	kgServer := minimalKGServer()
	defer kgServer.Close()

	// Prometheus API mock — returns a Grafana datasource query response with data.
	promMux := http.NewServeMux()
	promMux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		// All metric queries return a single-value instant result.
		writeJSON(w, map[string]any{
			"results": map[string]any{
				"A": map[string]any{
					"frames": []map[string]any{
						{
							"schema": map[string]any{
								"fields": []map[string]any{
									{"name": "Time", "type": "time"},
									{"name": "Value", "type": "number"},
								},
							},
							"data": map[string]any{
								"values": []any{
									[]int64{1715100000000},
									[]float64{42},
								},
							},
						},
					},
				},
			},
		})
	})
	promServer := httptest.NewServer(promMux)
	defer promServer.Close()

	kgClient := newTestClient(t, kgServer)
	promClient := newTestPromClient(t, promServer)
	scope := kg.NewTestScopeFlags("prod", "", "")
	result := kg.RunDiagnose(t.Context(), kgClient, &scope, promClient, "test-prom-uid")

	// Should have KG checks + 5 metric checks.
	var metricChecks []kg.CheckResult
	for _, c := range result.Checks {
		if len(c.Name) > 7 && c.Name[:7] == "Metric:" {
			metricChecks = append(metricChecks, c)
		}
	}
	assert.Len(t, metricChecks, 5, "expected 5 metric checks")

	// All metric checks should pass (mock returns data).
	for _, c := range metricChecks {
		assert.Equal(t, kg.CheckPass, c.Status, "metric check %q should pass", c.Name)
		assert.Contains(t, c.Detail, "series", "metric check %q detail should mention series count", c.Name)
	}

	// Total checks = 6 KG + 5 metric = 11 (profile warns, so 10 pass + 1 warn).
	assert.Equal(t, 11, result.Summary.Total)
	assert.Equal(t, 10, result.Summary.Passed)
	assert.Equal(t, 1, result.Summary.Warned) // profile config missing
}

func TestRunDiagnose_MetricChecksFail(t *testing.T) {
	// KG API mock.
	kgMux := http.NewServeMux()
	kgMux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/plugins/grafana-asserts-app/resources/asserts/api-server/v1/stack/status":
			writeJSON(w, kg.Status{Status: "complete", Enabled: true})
		case "/api/plugins/grafana-asserts-app/resources/asserts/api-server/v1/entity_type/count":
			writeJSON(w, map[string]int64{"Service": 5})
		case "/api/plugins/grafana-asserts-app/resources/asserts/api-server/v1/entity_scope":
			writeJSON(w, map[string]any{"scopeValues": map[string][]string{"env": {"prod"}}})
		case "/api/plugins/grafana-asserts-app/resources/asserts/api-server/v2/config/log":
			writeJSON(w, kg.LogConfigsResponse{LogDrilldownConfigs: []kg.LogDrilldownConfig{{Name: "loki"}}})
		case "/api/plugins/grafana-asserts-app/resources/asserts/api-server/v2/config/trace":
			writeJSON(w, kg.TraceConfigsResponse{TraceDrilldownConfigs: []kg.TraceDrilldownConfig{{Name: "tempo"}}})
		case "/api/plugins/grafana-asserts-app/resources/asserts/api-server/v2/config/profile":
			writeJSON(w, kg.ProfileConfigsResponse{})
		default:
			http.NotFound(w, r)
		}
	})
	kgServer := httptest.NewServer(kgMux)
	defer kgServer.Close()

	// Prometheus API mock — returns empty results (no data).
	promMux := http.NewServeMux()
	promMux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{
			"results": map[string]any{
				"A": map[string]any{
					"frames": []map[string]any{},
				},
			},
		})
	})
	promServer := httptest.NewServer(promMux)
	defer promServer.Close()

	kgClient := newTestClient(t, kgServer)
	promClient := newTestPromClient(t, promServer)
	scope := kg.NewTestScopeFlags("", "", "")
	result := kg.RunDiagnose(t.Context(), kgClient, &scope, promClient, "test-prom-uid")

	// All 5 metric checks should fail.
	var failedMetrics int
	for _, c := range result.Checks {
		if len(c.Name) > 7 && c.Name[:7] == "Metric:" {
			if c.Status == kg.CheckFail {
				failedMetrics++
				assert.NotEmpty(t, c.Recommendation, "failed metric check %q should have a recommendation", c.Name)
			}
		}
	}
	assert.Equal(t, 5, failedMetrics, "all 5 metric checks should fail when Prometheus returns no data")
}

func TestRunDiagnose_NilPromClientSkipsMetrics(t *testing.T) {
	// KG API mock.
	kgMux := http.NewServeMux()
	kgMux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/plugins/grafana-asserts-app/resources/asserts/api-server/v1/stack/status":
			writeJSON(w, kg.Status{Status: "complete", Enabled: true})
		case "/api/plugins/grafana-asserts-app/resources/asserts/api-server/v1/entity_type/count":
			writeJSON(w, map[string]int64{"Service": 5})
		case "/api/plugins/grafana-asserts-app/resources/asserts/api-server/v1/entity_scope":
			writeJSON(w, map[string]any{"scopeValues": map[string][]string{"env": {"prod"}}})
		case "/api/plugins/grafana-asserts-app/resources/asserts/api-server/v2/config/log":
			writeJSON(w, kg.LogConfigsResponse{LogDrilldownConfigs: []kg.LogDrilldownConfig{{Name: "loki"}}})
		case "/api/plugins/grafana-asserts-app/resources/asserts/api-server/v2/config/trace":
			writeJSON(w, kg.TraceConfigsResponse{TraceDrilldownConfigs: []kg.TraceDrilldownConfig{{Name: "tempo"}}})
		case "/api/plugins/grafana-asserts-app/resources/asserts/api-server/v2/config/profile":
			writeJSON(w, kg.ProfileConfigsResponse{})
		default:
			http.NotFound(w, r)
		}
	})
	kgServer := httptest.NewServer(kgMux)
	defer kgServer.Close()

	kgClient := newTestClient(t, kgServer)
	scope := kg.NewTestScopeFlags("", "", "")
	result := kg.RunDiagnose(t.Context(), kgClient, &scope, nil, "")

	// No metric checks should be present.
	for _, c := range result.Checks {
		assert.False(t, len(c.Name) > 7 && c.Name[:7] == "Metric:", "should have no metric checks when promClient is nil, got %q", c.Name)
	}
	assert.Equal(t, 6, result.Summary.Total, "should only have 6 KG checks")
}

// ---------------------------------------------------------------------------
// Service diagnosis tests
// ---------------------------------------------------------------------------

// cypherHandler returns an HTTP handler that responds to Cypher search requests
// with the given entities and edges.
func cypherHandler(entities []kg.CypherEntity, edges []kg.CypherEdge) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		if entities == nil {
			entities = []kg.CypherEntity{}
		}
		if edges == nil {
			edges = []kg.CypherEdge{}
		}
		writeJSON(w, kg.CypherSearchResponse{
			Entities: entities,
			Edges:    edges,
			LastPage: true,
		})
	}
}

func TestServiceDiagnose_HealthyService(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/plugins/grafana-asserts-app/resources/asserts/api-server/v1/search/cypher" {
			cypherHandler(
				[]kg.CypherEntity{
					{Type: "Service", Name: "api-service", Scope: map[string]any{"env": "prod", "namespace": "default"}, Properties: map[string]any{"_entity_source_10": "target_info_k8s", "otel_service": "api-service", "service": "api-service", "job": "default/api-service"}},
					{Type: "Service", Name: "checkout", Scope: map[string]any{"env": "prod"}},
				},
				[]kg.CypherEdge{
					{Type: "CALLS", SourceName: "api-service", SourceType: "Service", DestinationName: "checkout", DestinationType: "Service"},
				},
			)(w, r)
			return
		}
		http.NotFound(w, r)
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	client := newTestClient(t, server)
	scope := kg.NewTestScopeFlags("prod", "", "")
	result := kg.RunServiceDiagnose(t.Context(), client, "api-service", &scope, nil, "")

	assert.NotNil(t, result.Entity)
	assert.Equal(t, "api-service", result.Entity.Name)
	assert.Equal(t, "target_info_k8s", result.Entity.Source)
	assert.Len(t, result.Edges, 1)
	assert.Equal(t, "checkout", result.Edges[0].PeerName)

	// Entity lookup + Relationships + Insights should all pass.
	entityCheck := findCheck(result.Checks, "Entity lookup")
	require.NotNil(t, entityCheck)
	assert.Equal(t, kg.CheckPass, entityCheck.Status)

	relCheck := findCheck(result.Checks, "Relationships")
	require.NotNil(t, relCheck)
	assert.Equal(t, kg.CheckPass, relCheck.Status)

	assert.Contains(t, result.Diagnosis[0], "looks healthy")
}

func TestServiceDiagnose_NotFound(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/plugins/grafana-asserts-app/resources/asserts/api-server/v1/search/cypher" {
			cypherHandler(nil, nil)(w, r)
			return
		}
		http.NotFound(w, r)
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	client := newTestClient(t, server)
	scope := kg.NewTestScopeFlags("", "", "")
	result := kg.RunServiceDiagnose(t.Context(), client, "nonexistent", &scope, nil, "")

	assert.Nil(t, result.Entity)
	entityCheck := findCheck(result.Checks, "Entity lookup")
	require.NotNil(t, entityCheck)
	assert.Equal(t, kg.CheckFail, entityCheck.Status)
	assert.Contains(t, entityCheck.Detail, "not found")
	assert.NotEmpty(t, result.Diagnosis)
	assert.NotEmpty(t, result.NextSteps)
}

func TestServiceDiagnose_NoEdges(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/plugins/grafana-asserts-app/resources/asserts/api-server/v1/search/cypher" {
			// First call (with relationships) returns nothing; second (simple) finds the entity.
			cypherHandler(
				[]kg.CypherEntity{
					{Type: "Service", Name: "lonely-service", Scope: map[string]any{"env": "prod"}, Properties: map[string]any{"_entity_source_10": "target_info_k8s"}},
				},
				nil,
			)(w, r)
			return
		}
		http.NotFound(w, r)
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	client := newTestClient(t, server)
	scope := kg.NewTestScopeFlags("", "", "")
	result := kg.RunServiceDiagnose(t.Context(), client, "lonely-service", &scope, nil, "")

	assert.NotNil(t, result.Entity)
	relCheck := findCheck(result.Checks, "Relationships")
	require.NotNil(t, relCheck)
	assert.Equal(t, kg.CheckFail, relCheck.Status)
	assert.Contains(t, relCheck.Detail, "no edges")
}

// TestServiceDiagnose_ReclassifiesLabelMismatch verifies that the per-service
// metric checks (in diagnose_service.go) also reclassify FAIL → WARN when the
// scoped query returns nothing but the unscoped probe finds data for the same
// service. Without this, `gcx kg diagnose service NAME --env <env>` would
// report FAIL for every metric when the env value doesn't match what's in the
// label pipeline — masking the actual label-mismatch as a data-missing problem.
//
// This is a regression test for the bug PR #746 was supposed to fix but missed
// in the per-service path; see PR #746 review feedback from radiohead.
func TestServiceDiagnose_ReclassifiesLabelMismatch(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/plugins/grafana-asserts-app/resources/asserts/api-server/v1/search/cypher" {
			cypherHandler(
				[]kg.CypherEntity{
					{Type: "Service", Name: "api-service", Scope: map[string]any{"env": "actual-env"}, Properties: map[string]any{"_entity_source_10": "target_info_k8s", "service": "api-service"}},
				},
				nil,
			)(w, r)
			return
		}
		http.NotFound(w, r)
	})
	kgServer := httptest.NewServer(mux)
	defer kgServer.Close()

	// Prom mock: scoped queries (containing asserts_env=) return empty;
	// unscoped queries return data. Simulates a label-mismatch where the
	// env scope mask doesn't match what flows in the metric.
	promMux := http.NewServeMux()
	promMux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if strings.Contains(string(body), `asserts_env=`) {
			writeJSON(w, map[string]any{
				"results": map[string]any{
					"A": map[string]any{"frames": []map[string]any{}},
				},
			})
			return
		}
		writeJSON(w, map[string]any{
			"results": map[string]any{
				"A": map[string]any{
					"frames": []map[string]any{
						{
							"schema": map[string]any{"fields": []map[string]any{{"name": "Time", "type": "time"}, {"name": "Value", "type": "number"}}},
							"data":   map[string]any{"values": []any{[]int64{1715100000000}, []float64{7}}},
						},
					},
				},
			},
		})
	})
	promServer := httptest.NewServer(promMux)
	defer promServer.Close()

	kgClient := newTestClient(t, kgServer)
	promClient := newTestPromClient(t, promServer)
	scope := kg.NewTestScopeFlags("wrong-env", "", "")
	result := kg.RunServiceDiagnose(t.Context(), kgClient, "api-service", &scope, promClient, "test-prom-uid")

	var warned, failed int
	for _, c := range result.Checks {
		if !strings.HasPrefix(c.Name, "Metric:") {
			continue
		}
		switch c.Status {
		case kg.CheckWarn:
			warned++
			assert.Contains(t, c.Detail, "metric exists",
				"check %q WARN detail should mention the metric exists outside the scope", c.Name)
			assert.Contains(t, c.Recommendation, "asserts_env",
				"check %q WARN recommendation should mention asserts_env label mapping", c.Name)
		case kg.CheckFail:
			failed++
		}
	}
	assert.Equal(t, 5, warned, "all 5 per-service metric checks should reclassify to WARN when env scope masks real data")
	assert.Equal(t, 0, failed, "no per-service metric check should FAIL when unscoped probe finds data")
}

// TestServiceDiagnose_NoFallbackWhenEnvUnset verifies the per-service path
// does not attach a no-op fallback when --env is empty (scoped == unscoped).
func TestServiceDiagnose_NoFallbackWhenEnvUnset(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/plugins/grafana-asserts-app/resources/asserts/api-server/v1/search/cypher" {
			cypherHandler(
				[]kg.CypherEntity{
					{Type: "Service", Name: "api-service", Scope: map[string]any{"env": "any"}, Properties: map[string]any{"_entity_source_10": "target_info_k8s"}},
				},
				nil,
			)(w, r)
			return
		}
		http.NotFound(w, r)
	})
	kgServer := httptest.NewServer(mux)
	defer kgServer.Close()

	var (
		mu    sync.Mutex
		count int
	)
	promMux := http.NewServeMux()
	promMux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		// Service-scoped per-metric checks all reference a label key (client/server/service).
		if strings.Contains(string(body), `service=`) || strings.Contains(string(body), `client=`) || strings.Contains(string(body), `server=`) {
			count++
		}
		mu.Unlock()
		writeJSON(w, map[string]any{
			"results": map[string]any{"A": map[string]any{"frames": []map[string]any{}}},
		})
	})
	promServer := httptest.NewServer(promMux)
	defer promServer.Close()

	kgClient := newTestClient(t, kgServer)
	promClient := newTestPromClient(t, promServer)
	scope := kg.NewTestScopeFlags("", "", "") // no env
	_ = kg.RunServiceDiagnose(t.Context(), kgClient, "api-service", &scope, promClient, "test-prom-uid")

	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, 5, count, "expected exactly 5 service-metric queries (no fallback when env unset)")
}

func TestServiceDiagnoseTableCodec(t *testing.T) {
	result := kg.ServiceDiagnoseResult{
		ServiceName: "api-service",
		Env:         "production",
		Entity: &kg.EntityInfo{
			Type:   "Service",
			Name:   "api-service",
			Env:    "production",
			Source: "target_info_k8s",
		},
		Edges: []kg.EdgeInfo{
			{Direction: "outgoing", Type: "CALLS", PeerName: "checkout", PeerType: "Service"},
		},
		Checks: []kg.CheckResult{
			{Name: "Entity lookup", Status: kg.CheckPass, Detail: "type=Service"},
			{Name: "Relationships", Status: kg.CheckPass, Detail: "1 edges"},
		},
		Diagnosis: []string{"Service looks healthy."},
	}
	result.Summary.Total = 2
	result.Summary.Passed = 2

	codec := &kg.ServiceDiagnoseTableCodec{}
	var buf bytes.Buffer
	err := codec.Encode(&buf, result)
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "api-service")
	assert.Contains(t, output, "production")
	assert.Contains(t, output, "CALLS → checkout")
	assert.Contains(t, output, "PASS")
	assert.Contains(t, output, "Diagnosis")
	assert.Contains(t, output, "2/2 checks passed")
}

// findCheck returns the first check with the given name, or nil.
func findCheck(checks []kg.CheckResult, name string) *kg.CheckResult {
	for i := range checks {
		if checks[i].Name == name {
			return &checks[i]
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// Labels diagnosis tests
// ---------------------------------------------------------------------------

// grafanaFramesForLabels builds a Grafana query response with one frame per
// label value, matching the format that convertGrafanaResponse expects.
func grafanaFramesForLabels(labelName string, values []string) map[string]any {
	frames := make([]map[string]any, 0, len(values))
	for _, v := range values {
		frames = append(frames, map[string]any{
			"schema": map[string]any{
				"fields": []map[string]any{
					{"name": "Time", "type": "time"},
					{"name": "Value", "type": "number", "labels": map[string]string{labelName: v}},
				},
			},
			"data": map[string]any{
				"values": []any{
					[]int64{1715100000000},
					[]float64{1},
				},
			},
		})
	}
	return map[string]any{
		"results": map[string]any{
			"A": map[string]any{"frames": frames},
		},
	}
}

func TestLabelsDiagnose_AllMapped(t *testing.T) {
	kgMux := http.NewServeMux()
	kgMux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/plugins/grafana-asserts-app/resources/asserts/api-server/v1/entity_scope" {
			writeJSON(w, map[string]any{"scopeValues": map[string][]string{
				"env": {"production", "staging"},
			}})
			return
		}
		if r.URL.Path == "/api/plugins/grafana-asserts-app/resources/asserts/api-server/v1/entity_type/count" {
			writeJSON(w, map[string]int64{"Service": 10})
			return
		}
		http.NotFound(w, r)
	})
	kgServer := httptest.NewServer(kgMux)
	defer kgServer.Close()

	// Prometheus mock: asserts_env and deployment_environment both return "production", "staging".
	promMux := http.NewServeMux()
	promMux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		// Read the request body to determine which query was sent.
		body := make([]byte, 4096)
		n, _ := r.Body.Read(body)
		bodyStr := string(body[:n])

		switch {
		case strings.Contains(bodyStr, "asserts_env"):
			writeJSON(w, grafanaFramesForLabels("asserts_env", []string{"production", "staging"}))
		case strings.Contains(bodyStr, "deployment_environment"):
			writeJSON(w, grafanaFramesForLabels("deployment_environment", []string{"production", "staging"}))
		default:
			writeJSON(w, grafanaFramesForLabels("", nil))
		}
	})
	promServer := httptest.NewServer(promMux)
	defer promServer.Close()

	kgClient := newTestClient(t, kgServer)
	promClient := newTestPromClient(t, promServer)
	result := kg.RunLabelsDiagnose(t.Context(), kgClient, promClient, "test-uid")

	// All checks should pass.
	assert.GreaterOrEqual(t, result.Summary.Passed, 3, "expected at least 3 passing checks")
	assert.Equal(t, 0, result.Summary.Failed)

	// Mappings should all be "mapped".
	for _, m := range result.Mappings {
		assert.Equal(t, "mapped", m.Status, "mapping for %q should be 'mapped'", m.DeploymentEnv)
	}
}

func TestLabelsDiagnose_NilPromClient(t *testing.T) {
	kgMux := http.NewServeMux()
	kgMux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	})
	kgServer := httptest.NewServer(kgMux)
	defer kgServer.Close()

	kgClient := newTestClient(t, kgServer)
	result := kg.RunLabelsDiagnose(t.Context(), kgClient, nil, "")

	assert.Equal(t, 1, result.Summary.Total)
	assert.Equal(t, 1, result.Summary.Failed)
	promCheck := findCheck(result.Checks, "Prometheus connectivity")
	require.NotNil(t, promCheck)
	assert.Equal(t, kg.CheckFail, promCheck.Status)
}

func TestLabelsDiagnoseTableCodec(t *testing.T) {
	result := kg.LabelsDiagnoseResult{
		Mappings: []kg.LabelMapping{
			{DeploymentEnv: "production", AssertsEnv: "production", Status: "mapped"},
			{DeploymentEnv: "unknown-env", Status: "unmapped"},
		},
		Checks: []kg.CheckResult{
			{Name: "asserts_env in recording rules", Status: kg.CheckPass, Detail: "1 value"},
			{Name: "Label mapping consistency", Status: kg.CheckFail, Detail: "1 unmapped"},
		},
		Diagnosis: []string{"1 unmapped environment."},
	}
	result.Summary.Total = 2
	result.Summary.Passed = 1
	result.Summary.Failed = 1

	codec := &kg.LabelsDiagnoseTableCodec{}
	var buf bytes.Buffer
	err := codec.Encode(&buf, result)
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "Checks:")
	assert.Contains(t, output, "production")
	assert.Contains(t, output, "not mapped")
	assert.Contains(t, output, "Diagnosis")
	assert.Contains(t, output, "1/2 checks passed")
}

// ---------------------------------------------------------------------------
// "Looks healthy" verdict logic
// ---------------------------------------------------------------------------
//
// Regression coverage for the case where entity+edges exist but other
// checks fail. Previously the diagnosis branch unconditionally said
// "Service X looks healthy" whenever edges were present, producing
// internally contradictory output (e.g. "4/8 checks passed, 4 failed"
// + "Service looks healthy"). The fix derives the verdict from
// Summary.Failed instead.

func TestInterpretServiceResults_AllChecksPassSaysHealthy(t *testing.T) {
	r := &kg.ServiceDiagnoseResult{
		ServiceName: "api-service",
		Edges:       []kg.EdgeInfo{{Direction: "outgoing", Type: "CALLS", PeerName: "checkout"}},
		Checks: []kg.CheckResult{
			{Name: "Entity lookup", Status: kg.CheckPass},
			{Name: "Relationships", Status: kg.CheckPass},
			{Name: "Insights", Status: kg.CheckPass},
		},
	}
	kg.ComputeServiceSummary(r)
	diagnosis, _ := kg.InterpretServiceResults(r)

	require.NotEmpty(t, diagnosis)
	assert.Contains(t, diagnosis[0], "looks healthy")
}

func TestInterpretServiceResults_FailedChecksSuppressesHealthyVerdict(t *testing.T) {
	// Entity exists with edges but multiple metric checks fail.
	// Previously this produced the contradictory "looks healthy" line;
	// now it must report the failure count and name the failed checks.
	r := &kg.ServiceDiagnoseResult{
		ServiceName: "db",
		Edges: []kg.EdgeInfo{
			{Direction: "outgoing", Type: "CONTROLS", PeerName: "db-0"},
			{Direction: "outgoing", Type: "CONTROLS", PeerName: "db:schema"},
			{Direction: "incoming", Type: "CONTROLS", PeerName: "default"},
		},
		Checks: []kg.CheckResult{
			{Name: "Entity lookup", Status: kg.CheckPass},
			{Name: "Relationships", Status: kg.CheckPass},
			{Name: "Insights", Status: kg.CheckPass},
			{Name: "Metric: asserts:mixin_workload_job", Status: kg.CheckFail, Detail: "no data"},
			{Name: "Metric: traces_service_graph (server)", Status: kg.CheckFail, Detail: "no data"},
			{Name: "Metric: asserts:relation:calls", Status: kg.CheckFail, Detail: "no data"},
			{Name: "Metric: traces_service_graph (client)", Status: kg.CheckFail, Detail: "no data"},
			{Name: "Metric: asserts:request:rate5m", Status: kg.CheckPass},
		},
	}
	kg.ComputeServiceSummary(r)
	diagnosis, _ := kg.InterpretServiceResults(r)

	require.NotEmpty(t, diagnosis)
	joined := strings.Join(diagnosis, " ")
	assert.NotContains(t, joined, "looks healthy",
		"diagnosis must not declare service healthy when checks fail")
	assert.Contains(t, joined, "4 check(s) failed",
		"diagnosis should report the failure count")
	// Names of the failed checks should appear so the user can act.
	assert.Contains(t, joined, "Metric: asserts:mixin_workload_job")
	assert.Contains(t, joined, "Metric: asserts:relation:calls")
}

func TestInterpretServiceResults_SingleCheckFailureIsReported(t *testing.T) {
	// One-failure boundary case — exercises the same branch with a
	// minimal failure set to confirm the count is rendered correctly.
	r := &kg.ServiceDiagnoseResult{
		ServiceName: "api-service",
		Edges:       []kg.EdgeInfo{{Direction: "outgoing", Type: "CALLS", PeerName: "checkout"}},
		Checks: []kg.CheckResult{
			{Name: "Entity lookup", Status: kg.CheckPass},
			{Name: "Relationships", Status: kg.CheckPass},
			{Name: "Insights", Status: kg.CheckPass},
			{Name: "Metric: asserts:mixin_workload_job", Status: kg.CheckPass},
			{Name: "Metric: traces_service_graph (server)", Status: kg.CheckFail, Detail: "no data"},
			{Name: "Metric: traces_service_graph (client)", Status: kg.CheckPass},
			{Name: "Metric: asserts:relation:calls", Status: kg.CheckPass},
			{Name: "Metric: asserts:request:rate5m", Status: kg.CheckPass},
		},
	}
	kg.ComputeServiceSummary(r)
	diagnosis, _ := kg.InterpretServiceResults(r)

	require.NotEmpty(t, diagnosis)
	joined := strings.Join(diagnosis, " ")
	assert.NotContains(t, joined, "looks healthy")
	assert.Contains(t, joined, "1 check(s) failed")
	assert.Contains(t, joined, "Metric: traces_service_graph (server)")
}

// ---------------------------------------------------------------------------
// A.2: Probe without env filter when a metric check returns "no data".
// ---------------------------------------------------------------------------
//
// When a scoped metric check returns zero series, checkMetric now re-probes
// the metric without the env/namespace filter. If the unscoped probe finds
// data, the result is reclassified from FAIL to WARN with a label-pipeline
// hint, instead of silently telling the user "no data" when the data is
// in fact flowing under a different env label value.

// promHandlerScopedEmptyUnscopedFull is an httptest handler whose response
// depends on whether the PromQL expression includes a label filter:
//
//   - Any query that includes a label filter (selectors like {asserts_env=…},
//     {deployment_environment=…}, {client_deployment_environment=…}) returns
//     an empty frame array (simulating "no series match the scope").
//   - A bare `count(<metric>)` with no selector returns a single series with
//     value 42 (simulating "the metric exists outside this scope").
//
// This dispatch lets one handler answer all 5 metric checks plus all 5
// fallback probes for the diagnose pipeline.
func promHandlerScopedEmptyUnscopedFull() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		expr := string(body)
		// A scoped query contains an `=` inside `{}` — match the JSON-escaped
		// `=\"` that the Grafana datasource POST body carries.
		if strings.Contains(expr, `=\"`) {
			writeJSON(w, map[string]any{
				"results": map[string]any{
					"A": map[string]any{
						"frames": []map[string]any{},
					},
				},
			})
			return
		}
		// Unscoped (no selector): return data.
		writeJSON(w, map[string]any{
			"results": map[string]any{
				"A": map[string]any{
					"frames": []map[string]any{
						{
							"schema": map[string]any{
								"fields": []map[string]any{
									{"name": "Time", "type": "time"},
									{"name": "Value", "type": "number"},
								},
							},
							"data": map[string]any{
								"values": []any{
									[]int64{1715100000000},
									[]float64{42},
								},
							},
						},
					},
				},
			},
		})
	}
}

func TestCheckMetric_ReclassifiesNoDataAsLabelMismatchWhenUnscopedHasData(t *testing.T) {
	kgServer := minimalKGServer()
	defer kgServer.Close()

	promMux := http.NewServeMux()
	promMux.HandleFunc("/", promHandlerScopedEmptyUnscopedFull())
	promServer := httptest.NewServer(promMux)
	defer promServer.Close()

	kgClient := newTestClient(t, kgServer)
	promClient := newTestPromClient(t, promServer)
	// Env scope triggers the fallback path.
	scope := kg.NewTestScopeFlags("production", "", "")
	result := kg.RunDiagnose(t.Context(), kgClient, &scope, promClient, "test-prom-uid")

	// Every metric check should be WARN (label-mismatch), not FAIL.
	var warned, failed int
	for _, c := range result.Checks {
		if !strings.HasPrefix(c.Name, "Metric:") {
			continue
		}
		switch c.Status {
		case kg.CheckWarn:
			warned++
			assert.Contains(t, c.Detail, "metric exists",
				"check %q WARN detail should mention the metric exists outside the scope", c.Name)
			assert.Contains(t, c.Recommendation, "asserts_env",
				"check %q WARN recommendation should mention asserts_env label mapping", c.Name)
		case kg.CheckFail:
			failed++
		}
	}
	assert.Equal(t, 5, warned, "all 5 metric checks should be reclassified as WARN when scoped is empty but unscoped has data")
	assert.Equal(t, 0, failed, "no metric check should FAIL when unscoped probe finds data")
}

func TestCheckMetric_NoFallbackWhenScopeUnset(t *testing.T) {
	// Regression check: when no env/namespace is set, scoped and unscoped
	// queries are identical, so no fallback should fire. Empty results
	// should remain FAIL.
	kgServer := minimalKGServer()
	defer kgServer.Close()

	promMux := http.NewServeMux()
	promMux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		// Always return empty.
		writeJSON(w, map[string]any{
			"results": map[string]any{
				"A": map[string]any{
					"frames": []map[string]any{},
				},
			},
		})
	})
	promServer := httptest.NewServer(promMux)
	defer promServer.Close()

	kgClient := newTestClient(t, kgServer)
	promClient := newTestPromClient(t, promServer)
	scope := kg.NewTestScopeFlags("", "", "")
	result := kg.RunDiagnose(t.Context(), kgClient, &scope, promClient, "test-prom-uid")

	var warned, failed int
	for _, c := range result.Checks {
		if !strings.HasPrefix(c.Name, "Metric:") {
			continue
		}
		switch c.Status {
		case kg.CheckWarn:
			warned++
		case kg.CheckFail:
			failed++
		}
	}
	assert.Equal(t, 0, warned, "without scope, no reclassification should occur")
	assert.Equal(t, 5, failed, "all 5 metric checks should FAIL when both scoped and unscoped find nothing")
}

// TestCheckMetric_NoFallbackProbeWhenSelectorEmpty verifies that a check whose
// scoped query happens to carry no label filter (because its scoped selector
// resolves empty under the current env/namespace combination) does not issue
// a no-op unscoped fallback query.
//
// Concretely: when only --namespace is set, traces_target_info has no
// namespace label, so its scoped selector is empty, and its scoped query
// equals the unscoped query. The fallback should not fire.
//
// See PR #746 review feedback: gating used to be a single envOrNs boolean
// shared across all checks; it's now per-check.
func TestCheckMetric_NoFallbackProbeWhenSelectorEmpty(t *testing.T) {
	kgServer := minimalKGServer()
	defer kgServer.Close()

	// Track queries received so we can assert "traces_target_info ran exactly once".
	var (
		mu      sync.Mutex
		queries []string
	)
	promMux := http.NewServeMux()
	promMux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		queries = append(queries, string(body))
		mu.Unlock()
		// Always return empty (mimic "no data" so the fallback path would fire if attached).
		writeJSON(w, map[string]any{
			"results": map[string]any{
				"A": map[string]any{"frames": []map[string]any{}},
			},
		})
	})
	promServer := httptest.NewServer(promMux)
	defer promServer.Close()

	kgClient := newTestClient(t, kgServer)
	promClient := newTestPromClient(t, promServer)
	// Only namespace set. traces_target_info doesn't use namespace, so its
	// scoped selector is empty and no fallback should attach.
	// NewTestScopeFlags(env, site, namespace) — namespace is the third arg.
	scope := kg.NewTestScopeFlags("", "", "demo-ns")
	_ = kg.RunDiagnose(t.Context(), kgClient, &scope, promClient, "test-prom-uid")

	mu.Lock()
	defer mu.Unlock()

	// Count requests that mention traces_target_info.
	var ttiRequests int
	for _, q := range queries {
		if strings.Contains(q, "traces_target_info") {
			ttiRequests++
		}
	}
	// Exactly one: the scoped query. No fallback no-op round-trip.
	assert.Equal(t, 1, ttiRequests,
		"traces_target_info should be queried exactly once when its selector is empty "+
			"(no fallback no-op when scoped == unscoped)")
}

// ---------------------------------------------------------------------------
// Trace context propagation check
// ---------------------------------------------------------------------------

// promResponseHasData returns a Grafana datasource-query response with one
// instant-value frame (count > 0). Mirrors the shape `prometheus.Client.Query`
// expects so that `len(resp.Data.Result) > 0` evaluates to true.
func promResponseHasData() map[string]any {
	return map[string]any{
		"results": map[string]any{
			"A": map[string]any{
				"frames": []map[string]any{
					{
						"schema": map[string]any{
							"fields": []map[string]any{
								{"name": "Time", "type": "time"},
								{"name": "Value", "type": "number"},
							},
						},
						"data": map[string]any{
							"values": []any{
								[]int64{1715100000000},
								[]float64{1},
							},
						},
					},
				},
			},
		},
	}
}

// promResponseEmpty returns a Grafana datasource-query response with no
// frames — the shape `prometheus.Client.Query` returns when a count query
// matches nothing.
func promResponseEmpty() map[string]any {
	return map[string]any{
		"results": map[string]any{
			"A": map[string]any{
				"frames": []map[string]any{},
			},
		},
	}
}

// promHandlerByExpr returns an httptest handler that routes the Prometheus
// datasource POST body to one of three responses based on substrings in the
// `expr` field. The propagation check submits three distinct queries that
// are unambiguous by their selector — this lets us simulate the three
// gates independently.
func promHandlerByExpr(targetInfo, realEdges, phantomEdges map[string]any) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		expr := string(body)
		switch {
		case strings.Contains(expr, "traces_target_info"):
			writeJSON(w, targetInfo)
		case strings.Contains(expr, `client!=\"user\"`):
			writeJSON(w, realEdges)
		case strings.Contains(expr, `client=\"user\"`):
			writeJSON(w, phantomEdges)
		default:
			// All other metric queries the diagnose pipeline issues
			// (asserts:*, raw metric existence, edge-source gap probes,
			// etc.) — return empty so they don't pass and clutter the
			// result. The cases we care about are the three above.
			writeJSON(w, promResponseEmpty())
		}
	}
}

// findCheckByName locates the Trace context propagation check in a result.
// The helper is single-purpose (only one check name has multiple call sites
// that need this lookup pattern); inlining the name avoids the unparam lint
// warning that fires when a parameter only ever takes one value.
func findCheckByName(checks []kg.CheckResult) *kg.CheckResult {
	const name = "Trace context propagation"
	for i := range checks {
		if checks[i].Name == name {
			return &checks[i]
		}
	}
	return nil
}

func TestRunDiagnose_TracePropagationBroken(t *testing.T) {
	kgServer := minimalKGServer()
	defer kgServer.Close()

	// Telemetry signature of broken propagation:
	//   target_info > 0 ; real edges empty ; phantom edges > 0
	promMux := http.NewServeMux()
	promMux.HandleFunc("/", promHandlerByExpr(
		promResponseHasData(), // traces_target_info
		promResponseEmpty(),   // client!="user"
		promResponseHasData(), // client="user"
	))
	promServer := httptest.NewServer(promMux)
	defer promServer.Close()

	kgClient := newTestClient(t, kgServer)
	promClient := newTestPromClient(t, promServer)
	scope := kg.NewTestScopeFlags("prod", "", "")
	result := kg.RunDiagnose(t.Context(), kgClient, &scope, promClient, "test-prom-uid")

	check := findCheckByName(result.Checks)
	require.NotNil(t, check, "expected Trace context propagation check to be present")
	assert.Equal(t, kg.CheckFail, check.Status)
	assert.Contains(t, check.Detail, "phantom")
	assert.Contains(t, check.Recommendation, "traceparent")
	assert.Contains(t, check.Recommendation, "OTEL_PYTHON_DISABLED_INSTRUMENTATIONS")
}

func TestRunDiagnose_TracePropagationHealthy(t *testing.T) {
	kgServer := minimalKGServer()
	defer kgServer.Close()

	// Telemetry signature of healthy propagation:
	//   target_info > 0 ; real edges > 0 ; phantom edges irrelevant
	promMux := http.NewServeMux()
	promMux.HandleFunc("/", promHandlerByExpr(
		promResponseHasData(), // traces_target_info
		promResponseHasData(), // client!="user"  — real edges present
		promResponseEmpty(),   // client="user"   — irrelevant when real edges exist
	))
	promServer := httptest.NewServer(promMux)
	defer promServer.Close()

	kgClient := newTestClient(t, kgServer)
	promClient := newTestPromClient(t, promServer)
	scope := kg.NewTestScopeFlags("prod", "", "")
	result := kg.RunDiagnose(t.Context(), kgClient, &scope, promClient, "test-prom-uid")

	check := findCheckByName(result.Checks)
	assert.Nil(t, check, "Trace context propagation check should NOT appear when real edges exist")
}

func TestRunDiagnose_TracePropagationNoTelemetry(t *testing.T) {
	kgServer := minimalKGServer()
	defer kgServer.Close()

	// No telemetry at all — earlier metric checks will surface this.
	// The propagation check should remain silent (would otherwise
	// double-flag the user with a misleading message).
	promMux := http.NewServeMux()
	promMux.HandleFunc("/", promHandlerByExpr(
		promResponseEmpty(), // traces_target_info empty
		promResponseEmpty(),
		promResponseEmpty(),
	))
	promServer := httptest.NewServer(promMux)
	defer promServer.Close()

	kgClient := newTestClient(t, kgServer)
	promClient := newTestPromClient(t, promServer)
	scope := kg.NewTestScopeFlags("prod", "", "")
	result := kg.RunDiagnose(t.Context(), kgClient, &scope, promClient, "test-prom-uid")

	check := findCheckByName(result.Checks)
	assert.Nil(t, check, "Trace context propagation check should NOT appear when there's no telemetry at all")
}

func TestRunDiagnose_TracePropagationNoEnvSkipped(t *testing.T) {
	kgServer := minimalKGServer()
	defer kgServer.Close()

	// Even with telemetry that would otherwise look like broken propagation,
	// the check should skip itself when no --env scope was provided (the
	// PromQL is per-env, so a global query would be meaningless).
	promMux := http.NewServeMux()
	promMux.HandleFunc("/", promHandlerByExpr(
		promResponseHasData(),
		promResponseEmpty(),
		promResponseHasData(),
	))
	promServer := httptest.NewServer(promMux)
	defer promServer.Close()

	kgClient := newTestClient(t, kgServer)
	promClient := newTestPromClient(t, promServer)
	scope := kg.NewTestScopeFlags("", "", "") // no env
	result := kg.RunDiagnose(t.Context(), kgClient, &scope, promClient, "test-prom-uid")

	check := findCheckByName(result.Checks)
	assert.Nil(t, check, "Trace context propagation check should NOT run without an env scope")
}
