package kg_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
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

func TestDiagnoseTextCodec_Encode(t *testing.T) {
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

	codec := &kg.DiagnoseTextCodec{}
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

func TestServiceDiagnoseTextCodec(t *testing.T) {
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

	codec := &kg.ServiceDiagnoseTextCodec{}
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

func TestLabelsDiagnoseTextCodec(t *testing.T) {
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

	codec := &kg.LabelsDiagnoseTextCodec{}
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
