package kg_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/grafana/gcx/internal/providers/kg"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunDiagnose_AllHealthy(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/plugins/grafana-asserts-app/resources/asserts/api-server/v1/stack/status":
			writeJSON(w, kg.Status{
				Status:  "complete",
				Enabled: true,
				SanityCheckResults: []kg.SanityCheckResult{
					{CheckName: "traces_service_graph", DataPresent: true},
				},
			})
		case r.URL.Path == "/api/plugins/grafana-asserts-app/resources/asserts/api-server/v1/entity_type/count":
			writeJSON(w, map[string]int64{"Service": 10, "Pod": 20})
		case r.URL.Path == "/api/plugins/grafana-asserts-app/resources/asserts/api-server/v1/entity_scope":
			writeJSON(w, map[string]any{"scopeValues": map[string][]string{
				"env":       {"production"},
				"site":      {"us-east-1"},
				"namespace": {"default"},
			}})
		case r.URL.Path == "/api/plugins/grafana-asserts-app/resources/asserts/api-server/v2/config/log":
			writeJSON(w, kg.LogConfigsResponse{LogDrilldownConfigs: []kg.LogDrilldownConfig{{Name: "default-loki"}}})
		case r.URL.Path == "/api/plugins/grafana-asserts-app/resources/asserts/api-server/v2/config/trace":
			writeJSON(w, kg.TraceConfigsResponse{TraceDrilldownConfigs: []kg.TraceDrilldownConfig{{Name: "default-tempo"}}})
		case r.URL.Path == "/api/plugins/grafana-asserts-app/resources/asserts/api-server/v2/config/profile":
			writeJSON(w, kg.ProfileConfigsResponse{ProfileDrilldownConfigs: []kg.ProfileDrilldownConfig{{Name: "default-pyroscope"}}})
		default:
			http.NotFound(w, r)
		}
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	client := newTestClient(t, server)
	scope := kg.NewTestScopeFlags("", "", "")
	result := kg.RunDiagnose(t.Context(), client, &scope)

	assert.Equal(t, 7, result.Summary.Total)
	assert.Equal(t, 7, result.Summary.Passed)
	assert.Equal(t, 0, result.Summary.Failed)
	assert.Equal(t, 0, result.Summary.Warned)
}

func TestRunDiagnose_StackDisabled(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/plugins/grafana-asserts-app/resources/asserts/api-server/v1/stack/status":
			writeJSON(w, kg.Status{Status: "not_initialized", Enabled: false})
		case r.URL.Path == "/api/plugins/grafana-asserts-app/resources/asserts/api-server/v1/entity_type/count":
			writeJSON(w, map[string]int64{})
		case r.URL.Path == "/api/plugins/grafana-asserts-app/resources/asserts/api-server/v1/entity_scope":
			writeJSON(w, map[string]any{"scopeValues": map[string][]string{}})
		case r.URL.Path == "/api/plugins/grafana-asserts-app/resources/asserts/api-server/v2/config/log":
			writeJSON(w, kg.LogConfigsResponse{})
		case r.URL.Path == "/api/plugins/grafana-asserts-app/resources/asserts/api-server/v2/config/trace":
			writeJSON(w, kg.TraceConfigsResponse{})
		case r.URL.Path == "/api/plugins/grafana-asserts-app/resources/asserts/api-server/v2/config/profile":
			writeJSON(w, kg.ProfileConfigsResponse{})
		default:
			http.NotFound(w, r)
		}
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	client := newTestClient(t, server)
	scope := kg.NewTestScopeFlags("", "", "")
	result := kg.RunDiagnose(t.Context(), client, &scope)

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
		switch {
		case r.URL.Path == "/api/plugins/grafana-asserts-app/resources/asserts/api-server/v1/stack/status":
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
		case r.URL.Path == "/api/plugins/grafana-asserts-app/resources/asserts/api-server/v1/entity_type/count":
			writeJSON(w, map[string]int64{"Service": 5})
		case r.URL.Path == "/api/plugins/grafana-asserts-app/resources/asserts/api-server/v1/entity_scope":
			writeJSON(w, map[string]any{"scopeValues": map[string][]string{"env": {"prod"}}})
		case r.URL.Path == "/api/plugins/grafana-asserts-app/resources/asserts/api-server/v2/config/log":
			writeJSON(w, kg.LogConfigsResponse{})
		case r.URL.Path == "/api/plugins/grafana-asserts-app/resources/asserts/api-server/v2/config/trace":
			writeJSON(w, kg.TraceConfigsResponse{})
		case r.URL.Path == "/api/plugins/grafana-asserts-app/resources/asserts/api-server/v2/config/profile":
			writeJSON(w, kg.ProfileConfigsResponse{})
		default:
			http.NotFound(w, r)
		}
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	client := newTestClient(t, server)
	scope := kg.NewTestScopeFlags("", "", "")
	result := kg.RunDiagnose(t.Context(), client, &scope)

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
		switch {
		case r.URL.Path == "/api/plugins/grafana-asserts-app/resources/asserts/api-server/v1/stack/status":
			writeJSON(w, kg.Status{Status: "complete", Enabled: true})
		case r.URL.Path == "/api/plugins/grafana-asserts-app/resources/asserts/api-server/v1/entity_type/count":
			writeJSON(w, map[string]int64{})
		case r.URL.Path == "/api/plugins/grafana-asserts-app/resources/asserts/api-server/v1/entity_scope":
			writeJSON(w, map[string]any{"scopeValues": map[string][]string{"env": {"prod"}}})
		case r.URL.Path == "/api/plugins/grafana-asserts-app/resources/asserts/api-server/v2/config/log":
			writeJSON(w, kg.LogConfigsResponse{})
		case r.URL.Path == "/api/plugins/grafana-asserts-app/resources/asserts/api-server/v2/config/trace":
			writeJSON(w, kg.TraceConfigsResponse{})
		case r.URL.Path == "/api/plugins/grafana-asserts-app/resources/asserts/api-server/v2/config/profile":
			writeJSON(w, kg.ProfileConfigsResponse{})
		default:
			http.NotFound(w, r)
		}
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	client := newTestClient(t, server)
	scope := kg.NewTestScopeFlags("", "", "")
	result := kg.RunDiagnose(t.Context(), client, &scope)

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
	assert.Contains(t, output, "env: production")
	assert.Contains(t, output, "✓ PASS")
	assert.Contains(t, output, "✗ FAIL")
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
