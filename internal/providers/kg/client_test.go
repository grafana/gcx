package kg_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/providers/kg"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"
)

func newTestClient(t *testing.T, server *httptest.Server) *kg.Client {
	t.Helper()
	cfg := config.NamespacedRESTConfig{
		Config:    rest.Config{Host: server.URL},
		Namespace: "stack-123",
	}
	c, err := kg.NewClient(cfg)
	require.NoError(t, err)
	return c
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		panic(err)
	}
}

func TestClient_GetStatus(t *testing.T) {
	tests := []struct {
		name    string
		handler http.HandlerFunc
		wantErr bool
	}{
		{
			name: "returns status",
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodGet, r.Method)
				assert.Contains(t, r.URL.Path, "v1/stack/status")
				writeJSON(w, kg.Status{Status: "complete", Enabled: true})
			},
		},
		{
			name: "handles error",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte("internal error"))
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(tt.handler)
			defer server.Close()
			client := newTestClient(t, server)
			status, err := client.GetStatus(t.Context())
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, "complete", status.Status)
			assert.True(t, status.Enabled)
		})
	}
}

func TestClient_ListRules(t *testing.T) {
	tests := []struct {
		name    string
		handler http.HandlerFunc
		wantLen int
		wantErr bool
	}{
		{
			name: "returns rules",
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodGet, r.Method)
				assert.Contains(t, r.URL.Path, "config/prom-rules")
				writeJSON(w, map[string]any{
					"rules": []map[string]any{
						{"name": "rule-1", "expr": "sum(rate(x[5m]))"},
						{"name": "rule-2", "record": "metric:name"},
					},
				})
			},
			wantLen: 2,
		},
		{
			name: "returns empty on nil rules",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				writeJSON(w, map[string]any{"rules": nil})
			},
			wantLen: 0,
		},
		{
			name: "handles error",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte("error"))
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(tt.handler)
			defer server.Close()
			client := newTestClient(t, server)
			rules, err := client.ListRules(t.Context())
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Len(t, rules, tt.wantLen)
		})
	}
}

func TestClient_GetRule(t *testing.T) {
	tests := []struct {
		name     string
		ruleName string
		handler  http.HandlerFunc
		wantErr  bool
	}{
		{
			name:     "returns rule",
			ruleName: "my-rule",
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Contains(t, r.URL.Path, "prom-rules/my-rule")
				writeJSON(w, map[string]any{
					"rules": []map[string]any{
						{"name": "my-rule", "expr": "sum(rate(x[5m]))"},
					},
				})
			},
		},
		{
			name:     "rule not found",
			ruleName: "missing",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				writeJSON(w, map[string]any{"rules": []any{}})
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(tt.handler)
			defer server.Close()
			client := newTestClient(t, server)
			rule, err := client.GetRule(t.Context(), tt.ruleName)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, "my-rule", rule.Name)
		})
	}
}

func TestClient_CountEntityTypes(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Contains(t, r.URL.Path, "entity_type/count")
		writeJSON(w, map[string]int64{
			"Service":   42,
			"Namespace": 5,
		})
	}))
	defer server.Close()

	client := newTestClient(t, server)
	counts, err := client.CountEntityTypes(t.Context())
	require.NoError(t, err)
	assert.Equal(t, int64(42), counts["Service"])
	assert.Equal(t, int64(5), counts["Namespace"])
}

func TestClient_UploadPromRules(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPut, r.Method)
		assert.Contains(t, r.URL.Path, "config/prom-rules")
		assert.Equal(t, "application/x-yaml", r.Header.Get("Content-Type"))
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := newTestClient(t, server)
	err := client.UploadPromRules(t.Context(), "groups:\n- name: test\n  rules: []")
	require.NoError(t, err)
}

func TestClient_Search(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Contains(t, r.URL.Path, "v1/search")
		writeJSON(w, map[string]any{
			"data": map[string]any{
				"entities": []map[string]any{
					{"name": "svc-1", "type": "Service"},
					{"name": "svc-2", "type": "Service"},
				},
			},
		})
	}))
	defer server.Close()

	client := newTestClient(t, server)
	results, err := client.Search(t.Context(), kg.SearchRequest{
		FilterCriteria: []kg.EntityMatcher{{EntityType: "Service"}},
	})
	require.NoError(t, err)
	assert.Len(t, results, 2)
	assert.Equal(t, "svc-1", results[0].Name)
}

func TestClient_CypherSearch(t *testing.T) {
	tests := []struct {
		name        string
		req         kg.CypherSearchRequest
		handler     http.HandlerFunc
		wantErr     bool
		checkResult func(t *testing.T, resp *kg.CypherSearchResponse)
	}{
		{
			name: "sends correct path and request body",
			req: kg.CypherSearchRequest{
				CypherQuery:  "MATCH (s:Service) RETURN s LIMIT 10",
				TimeCriteria: &kg.TimeCriteria{Start: 1000, End: 2000},
				PageNum:      0,
			},
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodPost, r.Method)
				assert.Contains(t, r.URL.Path, "v1/search/cypher")

				var body kg.CypherSearchRequest
				assert.NoError(t, json.NewDecoder(r.Body).Decode(&body))
				assert.Equal(t, "MATCH (s:Service) RETURN s LIMIT 10", body.CypherQuery)
				assert.Equal(t, int64(1000), body.TimeCriteria.Start)
				assert.Equal(t, int64(2000), body.TimeCriteria.End)

				writeJSON(w, kg.CypherSearchResponse{
					Entities: []kg.CypherEntity{
						{Type: "Service", Name: "svc-1", Scope: map[string]any{"env": "prod"}},
						{Type: "Service", Name: "svc-2"},
					},
					Edges:    []kg.CypherEdge{},
					LastPage: true,
				})
			},
			checkResult: func(t *testing.T, resp *kg.CypherSearchResponse) {
				t.Helper()
				assert.Len(t, resp.Entities, 2)
				assert.Equal(t, "svc-1", resp.Entities[0].Name)
				assert.Equal(t, "prod", resp.Entities[0].Scope["env"])
				assert.True(t, resp.LastPage)
			},
		},
		{
			name: "sends scope criteria when set",
			req: kg.CypherSearchRequest{
				CypherQuery:   "MATCH (s:Service) RETURN s",
				TimeCriteria:  &kg.TimeCriteria{Start: 1000, End: 2000},
				ScopeCriteria: &kg.ScopeCriteria{NameAndValues: map[string][]string{"env": {"prod-us-east-0"}}},
			},
			handler: func(w http.ResponseWriter, r *http.Request) {
				var body kg.CypherSearchRequest
				assert.NoError(t, json.NewDecoder(r.Body).Decode(&body))
				assert.NotNil(t, body.ScopeCriteria)
				assert.Equal(t, []string{"prod-us-east-0"}, body.ScopeCriteria.NameAndValues["env"])
				writeJSON(w, kg.CypherSearchResponse{})
			},
		},
		{
			name: "sends withInsights flag",
			req: kg.CypherSearchRequest{
				CypherQuery:  "MATCH (s:Service) RETURN s",
				TimeCriteria: &kg.TimeCriteria{Start: 1000, End: 2000},
				WithInsights: true,
			},
			handler: func(w http.ResponseWriter, r *http.Request) {
				var body kg.CypherSearchRequest
				assert.NoError(t, json.NewDecoder(r.Body).Decode(&body))
				assert.True(t, body.WithInsights)
				writeJSON(w, kg.CypherSearchResponse{})
			},
		},
		{
			name: "returns empty entities and edges on empty response",
			req:  kg.CypherSearchRequest{CypherQuery: "MATCH (s:Service) RETURN s"},
			handler: func(w http.ResponseWriter, _ *http.Request) {
				writeJSON(w, kg.CypherSearchResponse{Entities: []kg.CypherEntity{}, Edges: []kg.CypherEdge{}, LastPage: true})
			},
			checkResult: func(t *testing.T, resp *kg.CypherSearchResponse) {
				t.Helper()
				assert.Empty(t, resp.Entities)
				assert.Empty(t, resp.Edges)
				assert.True(t, resp.LastPage)
			},
		},
		{
			name: "returns edges with source and destination",
			req:  kg.CypherSearchRequest{CypherQuery: "MATCH (s:Service)-[r]->(d) RETURN s, d"},
			handler: func(w http.ResponseWriter, _ *http.Request) {
				writeJSON(w, kg.CypherSearchResponse{
					Entities: []kg.CypherEntity{
						{Type: "Service", Name: "caller"},
						{Type: "Service", Name: "callee"},
					},
					Edges: []kg.CypherEdge{
						{Type: "CALLS", SourceName: "caller", SourceType: "Service", DestinationName: "callee", DestinationType: "Service"},
					},
				})
			},
			checkResult: func(t *testing.T, resp *kg.CypherSearchResponse) {
				t.Helper()
				assert.Len(t, resp.Edges, 1)
				assert.Equal(t, "CALLS", resp.Edges[0].Type)
				assert.Equal(t, "caller", resp.Edges[0].SourceName)
				assert.Equal(t, "callee", resp.Edges[0].DestinationName)
			},
		},
		{
			name: "handles server error",
			req:  kg.CypherSearchRequest{CypherQuery: "MATCH (s:Service) RETURN s"},
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte(`{"message":"internal error"}`))
			},
			wantErr: true,
		},
		{
			name: "handles 400 validation error",
			req:  kg.CypherSearchRequest{CypherQuery: "INVALID CYPHER"},
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"message":"invalid cypher query"}`))
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(tt.handler)
			defer server.Close()
			client := newTestClient(t, server)
			resp, err := client.CypherSearch(t.Context(), tt.req)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			if tt.checkResult != nil {
				tt.checkResult(t, resp)
			}
		})
	}
}

func TestClient_LookupEntity_NotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	client := newTestClient(t, server)
	entity, err := client.LookupEntity(t.Context(), "Service", "nonexistent", nil, 0, 0)
	require.NoError(t, err)
	assert.Nil(t, entity)
}
