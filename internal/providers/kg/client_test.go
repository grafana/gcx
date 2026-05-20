package kg_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
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

func TestClient_ListRuleNames(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Contains(t, r.URL.Path, "config/prom-rules")
		writeJSON(w, map[string]any{
			"ruleNames": []string{"rule-1", "rule-2"},
		})
	}))
	defer server.Close()
	client := newTestClient(t, server)
	names, err := client.ListRuleNames(t.Context())
	require.NoError(t, err)
	assert.Equal(t, []string{"rule-1", "rule-2"}, names)
}

// ruleFilesFanOutHandler serves both the list-names endpoint and per-name GETs,
// returning a minimal PrometheusRulesDto for each named rule.
func ruleFilesFanOutHandler(names []string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		const prefix = "/prom-rules/"
		if strings.HasSuffix(r.URL.Path, "/config/prom-rules") {
			writeJSON(w, map[string]any{"ruleNames": names})
			return
		}
		idx := strings.LastIndex(r.URL.Path, prefix)
		name := r.URL.Path[idx+len(prefix):]
		writeJSON(w, map[string]any{
			"name": name,
			"groups": []map[string]any{
				{"name": "g1", "rules": []map[string]any{{"record": name, "expr": "1"}}},
			},
		})
	}
}

func TestClient_ListRules_FanOut(t *testing.T) {
	server := httptest.NewServer(ruleFilesFanOutHandler([]string{"rule-a", "rule-b"}))
	defer server.Close()
	client := newTestClient(t, server)
	files, err := client.ListRules(t.Context())
	require.NoError(t, err)
	require.Len(t, files, 2)
	// Order is preserved from the names list.
	assert.Equal(t, "rule-a", files[0].Name)
	assert.Equal(t, "rule-b", files[1].Name)
	require.Len(t, files[0].Groups, 1)
	assert.Equal(t, "g1", files[0].Groups[0].Name)
}

func TestClient_ListRules_Empty(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, map[string]any{"ruleNames": nil})
	}))
	defer server.Close()
	client := newTestClient(t, server)
	files, err := client.ListRules(t.Context())
	require.NoError(t, err)
	assert.Empty(t, files)
}

func TestClient_ListRules_ListError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("error"))
	}))
	defer server.Close()
	client := newTestClient(t, server)
	_, err := client.ListRules(t.Context())
	require.Error(t, err)
}

func TestClient_GetRule(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Contains(t, r.URL.Path, "prom-rules/my-rule")
		writeJSON(w, map[string]any{
			"name": "my-rule",
			"groups": []map[string]any{
				{"name": "g", "rules": []map[string]any{{"record": "x", "expr": "1"}}},
			},
		})
	}))
	defer server.Close()
	client := newTestClient(t, server)
	f, err := client.GetRule(t.Context(), "my-rule")
	require.NoError(t, err)
	assert.Equal(t, "my-rule", f.Name)
	require.Len(t, f.Groups, 1)
	assert.Equal(t, "g", f.Groups[0].Name)
	require.Len(t, f.Groups[0].Rules, 1)
	assert.Equal(t, "1", f.Groups[0].Rules[0].Expr)
}

// Some backend versions reply 200 with an empty body for a missing rule
// instead of 404. GetRule must surface this as not-found.
func TestClient_GetRule_NotFound_EmptyBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, map[string]any{})
	}))
	defer server.Close()
	client := newTestClient(t, server)
	_, err := client.GetRule(t.Context(), "missing")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
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
	counts, err := client.CountEntityTypes(t.Context(), 0, 0, nil)
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
	page, err := client.Search(t.Context(), kg.SearchRequest{
		FilterCriteria: []kg.EntityMatcher{{EntityType: "Service"}},
	})
	require.NoError(t, err)
	assert.Len(t, page.Entities, 2)
	assert.Equal(t, "svc-1", page.Entities[0].Name)
}

func TestClient_Search_Pagination(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req kg.SearchRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		switch req.PageNum {
		case 0:
			writeJSON(w, map[string]any{
				"data": map[string]any{
					"pageNum":                  0,
					"lastPage":                 false,
					"searchResultsMaxLimitHit": true,
					"entities": []map[string]any{
						{"name": "svc-1", "type": "Service"},
						{"name": "svc-2", "type": "Service"},
					},
				},
			})
		case 1:
			writeJSON(w, map[string]any{
				"data": map[string]any{
					"pageNum":                  1,
					"lastPage":                 true,
					"searchResultsMaxLimitHit": false,
					"entities": []map[string]any{
						{"name": "svc-3", "type": "Service"},
					},
				},
			})
		default:
			t.Fatalf("unexpected pageNum: %d", req.PageNum)
		}
	}))
	defer server.Close()

	client := newTestClient(t, server)

	first, err := client.Search(t.Context(), kg.SearchRequest{
		FilterCriteria: []kg.EntityMatcher{{EntityType: "Service"}},
		PageNum:        0,
	})
	require.NoError(t, err)
	assert.Equal(t, 0, first.PageNum)
	assert.False(t, first.LastPage)
	assert.True(t, first.MaxLimitHit)
	assert.Len(t, first.Entities, 2)
	assert.Equal(t, "svc-1", first.Entities[0].Name)

	second, err := client.Search(t.Context(), kg.SearchRequest{
		FilterCriteria: []kg.EntityMatcher{{EntityType: "Service"}},
		PageNum:        1,
	})
	require.NoError(t, err)
	assert.Equal(t, 1, second.PageNum)
	assert.True(t, second.LastPage)
	assert.False(t, second.MaxLimitHit)
	assert.Len(t, second.Entities, 1)
	assert.Equal(t, "svc-3", second.Entities[0].Name)
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
