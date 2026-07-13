package kg_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
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

func TestClient_ListEntityScopes(t *testing.T) {
	t.Run("passes explicit time window as query params", func(t *testing.T) {
		var gotStart, gotEnd string
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, http.MethodGet, r.Method)
			assert.Contains(t, r.URL.Path, "entity_scope")
			gotStart = r.URL.Query().Get("start")
			gotEnd = r.URL.Query().Get("end")
			writeJSON(w, map[string]any{
				"scopeValues": map[string][]string{
					"env":       {"prod"},
					"namespace": {"loki-prod-029"},
				},
			})
		}))
		defer server.Close()

		client := newTestClient(t, server)
		scopes, err := client.ListEntityScopes(t.Context(), 1000, 2000)
		require.NoError(t, err)
		assert.Equal(t, "1000", gotStart)
		assert.Equal(t, "2000", gotEnd)
		assert.Equal(t, []string{"prod"}, scopes["env"])
		assert.Equal(t, []string{"loki-prod-029"}, scopes["namespace"])
	})

	t.Run("defaults to a one-hour window when unset", func(t *testing.T) {
		var gotStart, gotEnd int64
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotStart, _ = strconv.ParseInt(r.URL.Query().Get("start"), 10, 64)
			gotEnd, _ = strconv.ParseInt(r.URL.Query().Get("end"), 10, 64)
			writeJSON(w, map[string]any{"scopeValues": map[string][]string{}})
		}))
		defer server.Close()

		client := newTestClient(t, server)
		_, err := client.ListEntityScopes(t.Context(), 0, 0)
		require.NoError(t, err)
		assert.Positive(t, gotStart)
		assert.Positive(t, gotEnd)
		assert.Equal(t, int64(3600000), gotEnd-gotStart, "default window should span one hour")
	})
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

func TestClient_GetRelabelRules(t *testing.T) {
	tests := []struct {
		name       string
		ruleType   kg.RelabelRuleType
		status     int
		body       any
		wantPath   string
		wantNil    bool
		wantErr    bool
		wantErrMsg string
	}{
		{
			name:     "prologue ok",
			ruleType: kg.RelabelRuleTypePrologue,
			status:   http.StatusOK,
			body: map[string]any{
				"name":  "prologue",
				"rules": []map[string]any{{"sourceLabels": []string{"deployment_environment"}, "targetLabel": "asserts_env"}},
			},
			wantPath: "/v2/config/relabel-rules/prologue",
		},
		{
			name:     "epilogue ok",
			ruleType: kg.RelabelRuleTypeEpilogue,
			status:   http.StatusOK,
			body:     map[string]any{"name": "epilogue", "rules": []map[string]any{}},
			wantPath: "/v2/config/relabel-rules/epilogue",
		},
		{
			name:     "generated ok",
			ruleType: kg.RelabelRuleTypeGenerated,
			status:   http.StatusOK,
			body:     map[string]any{"name": "generated", "order": 100},
			wantPath: "/v2/config/relabel-rules/generated",
		},
		{
			name:     "204 returns nil map without error",
			ruleType: kg.RelabelRuleTypePrologue,
			status:   http.StatusNoContent,
			wantPath: "/v2/config/relabel-rules/prologue",
			wantNil:  true,
		},
		{
			name:     "server error surfaces APIError",
			ruleType: kg.RelabelRuleTypeEpilogue,
			status:   http.StatusInternalServerError,
			body:     map[string]any{"message": "boom"},
			wantPath: "/v2/config/relabel-rules/epilogue",
			wantErr:  true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodGet, r.Method)
				assert.Contains(t, r.URL.Path, tt.wantPath)
				if tt.status == http.StatusNoContent {
					w.WriteHeader(http.StatusNoContent)
					return
				}
				w.WriteHeader(tt.status)
				if tt.body != nil {
					writeJSON(w, tt.body)
				}
			}))
			defer server.Close()

			client := newTestClient(t, server)
			got, err := client.GetRelabelRules(t.Context(), tt.ruleType)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			if tt.wantNil {
				assert.Nil(t, got)
				return
			}
			assert.NotNil(t, got)
		})
	}
}

func TestClient_GetRelabelRules_InvalidType(t *testing.T) {
	// No server contact expected — invalid type rejected client-side.
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		t.Fatal("server should not be called for invalid rule type")
	}))
	defer server.Close()

	client := newTestClient(t, server)
	_, err := client.GetRelabelRules(t.Context(), kg.RelabelRuleType("bogus"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid relabel rule type")
}

func TestClient_LookupEntity_NotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	client := newTestClient(t, server)
	entity, err := client.LookupEntity(t.Context(), "Service", "nonexistent", nil, "", 0, 0)
	require.NoError(t, err)
	assert.Nil(t, entity)
}

func TestClient_LookupEntity_SendsDomain(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "myapp", r.URL.Query().Get("domain"))
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	client := newTestClient(t, server)
	_, err := client.LookupEntity(t.Context(), "Service", "checkout", nil, "myapp", 0, 0)
	require.NoError(t, err)
}

// When a domain filter is set and the domain-honoring LookupEntity finds no
// match, discoverEntityScope must NOT fall back to the domain-blind search
// (which could surface an entity from another domain) — it returns "not found"
// instead. Asserted by failing the test if the search endpoint is ever called.
func TestDiscoverEntityScope_DomainSet_NoSearchFallback(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/v1/search") {
			t.Errorf("search fallback must not run when --domain is set; got request to %s", r.URL.Path)
		}
		// entity lookup: no match in the requested domain.
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	client := newTestClient(t, server)
	scope, err := kg.DiscoverEntityScope(client, "Service", "checkout", "myapp", 0, 0)
	require.NoError(t, err)
	assert.Nil(t, scope)
}

// Without a domain filter, discoverEntityScope still falls back to a name-exact
// search and returns the single match's scope.
func TestDiscoverEntityScope_NoDomain_SearchFallback(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/v1/search") {
			writeJSON(w, map[string]any{"data": map[string]any{
				"entities": []kg.SearchResult{{Type: "Service", Name: "checkout", Scope: map[string]string{"env": "prod"}}},
				"lastPage": true,
			}})
			return
		}
		// entity lookup: no direct match, force the search fallback.
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	client := newTestClient(t, server)
	scope, err := kg.DiscoverEntityScope(client, "Service", "checkout", "", 0, 0)
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"env": "prod"}, scope)
}

func TestClient_ListModelRuleNames(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.True(t, strings.HasSuffix(r.URL.Path, "/v1/config/model-rules"),
			"expected path to end with /v1/config/model-rules (no trailing slash), got %q", r.URL.Path)
		writeJSON(w, kg.ModelRuleNames{RuleNames: []string{"alpha", "beta"}})
	}))
	defer server.Close()

	client := newTestClient(t, server)
	names, err := client.ListModelRuleNames(t.Context())
	require.NoError(t, err)
	assert.Equal(t, []string{"alpha", "beta"}, names)
}

func TestClient_ListModelRules_FansOut(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		path := r.URL.EscapedPath()
		switch {
		case strings.HasSuffix(path, "/v1/config/model-rules"):
			writeJSON(w, kg.ModelRuleNames{RuleNames: []string{"alpha", "beta"}})
		case strings.HasSuffix(path, "/v1/config/model-rules/alpha"):
			writeJSON(w, map[string]any{"name": "alpha"})
		case strings.HasSuffix(path, "/v1/config/model-rules/beta"):
			writeJSON(w, map[string]any{"name": "beta"})
		default:
			t.Fatalf("unexpected path: %s", path)
		}
	}))
	defer server.Close()

	client := newTestClient(t, server)
	rules, err := client.ListModelRules(t.Context())
	require.NoError(t, err)
	require.Len(t, rules, 2)
	names := []string{rules[0].Name, rules[1].Name}
	assert.ElementsMatch(t, []string{"alpha", "beta"}, names)
}

func TestClient_GetModelRules(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.True(t, strings.HasSuffix(r.URL.EscapedPath(), "/v1/config/model-rules/my%20rules"),
			"expected URL-escaped name in path, got %q", r.URL.EscapedPath())
		writeJSON(w, map[string]any{
			"name":     "my rules",
			"entities": []map[string]any{{"type": "Service"}},
		})
	}))
	defer server.Close()

	client := newTestClient(t, server)
	rules, err := client.GetModelRules(t.Context(), "my rules")
	require.NoError(t, err)
	assert.Equal(t, "my rules", rules.Name)
	assert.JSONEq(t, `[{"type":"Service"}]`, string(rules.Entities))
}

func TestClient_GetModelRules_NotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"message":"Rule x not found"}`))
	}))
	defer server.Close()

	client := newTestClient(t, server)
	_, err := client.GetModelRules(t.Context(), "x")
	require.Error(t, err)
}

func TestClient_GetModelRulesSchema(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.True(t, strings.HasSuffix(r.URL.Path, "/v1/config/model-rules/schema"),
			"unexpected path %q", r.URL.Path)
		writeJSON(w, map[string]any{
			"$schema": "https://json-schema.org/draft/2020-12/schema",
			"type":    "object",
			"properties": map[string]any{
				"name": map[string]any{"type": "string"},
			},
		})
	}))
	defer server.Close()

	client := newTestClient(t, server)
	schema, err := client.GetModelRulesSchema(t.Context())
	require.NoError(t, err)
	assert.Equal(t, "object", schema["type"])
}

func TestClient_GetPromRulesSchema(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.True(t, strings.HasSuffix(r.URL.Path, "/v1/config/prom-rules/schema"),
			"unexpected path %q", r.URL.Path)
		writeJSON(w, map[string]any{
			"$schema": "https://json-schema.org/draft/2020-12/schema",
			"type":    "object",
			"properties": map[string]any{
				"groups": map[string]any{"type": "array"},
			},
		})
	}))
	defer server.Close()

	client := newTestClient(t, server)
	schema, err := client.GetPromRulesSchema(t.Context())
	require.NoError(t, err)
	assert.Equal(t, "object", schema["type"])
}

func TestClient_DeleteModelRules(t *testing.T) {
	called := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		assert.Equal(t, http.MethodDelete, r.Method)
		assert.True(t, strings.HasSuffix(r.URL.Path, "/v1/config/model-rules/my-rules"))
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	client := newTestClient(t, server)
	require.NoError(t, client.DeleteModelRules(t.Context(), "my-rules"))
	assert.True(t, called)
}

func TestClient_UpsertEntity(t *testing.T) {
	tests := []struct {
		name        string
		status      int
		wantCreated bool
		wantErr     bool
	}{
		{name: "201 created", status: http.StatusCreated, wantCreated: true},
		{name: "200 updated", status: http.StatusOK, wantCreated: false},
		{name: "409 conflict", status: http.StatusConflict, wantErr: true},
		{name: "422 validation", status: http.StatusUnprocessableEntity, wantErr: true},
		{name: "500 server", status: http.StatusInternalServerError, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodPost, r.Method)
				assert.Contains(t, r.URL.Path, "/apis/kg.grafana.com/v1alpha1/namespaces/stack-123/entities")
				var got kg.EntityWriteRequest
				assert.NoError(t, json.NewDecoder(r.Body).Decode(&got))
				assert.Equal(t, "myapp", got.Domain)
				if tt.status >= 400 {
					w.WriteHeader(tt.status)
					writeJSON(w, map[string]string{"message": "boom"})
					return
				}
				w.WriteHeader(tt.status)
				writeJSON(w, kg.EntityWriteResponse{Domain: "myapp", Type: "Service", Name: "checkout"})
			}))
			defer server.Close()
			client := newTestClient(t, server)
			resp, created, err := client.UpsertEntity(t.Context(), kg.EntityWriteRequest{
				Domain: "myapp", Type: "Service", Name: "checkout", TTLSeconds: new(int64(-1)),
			})
			if tt.wantErr {
				require.Error(t, err)
				var apiErr *kg.APIError
				require.ErrorAs(t, err, &apiErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantCreated, created)
			assert.Equal(t, "checkout", resp.Name)
		})
	}
}

func TestClient_DeleteEntity(t *testing.T) {
	t.Run("sends identity as query params on collection path", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, http.MethodDelete, r.Method)
			assert.Equal(t, "/apis/kg.grafana.com/v1alpha1/namespaces/stack-123/entities", r.URL.Path)
			q := r.URL.Query()
			assert.Equal(t, "myapp", q.Get("domain"))
			assert.Equal(t, "Service", q.Get("type"))
			assert.Equal(t, "checkout", q.Get("name"))
			assert.Equal(t, "prod", q.Get("scope[env]"))
			w.WriteHeader(http.StatusNoContent)
		}))
		defer server.Close()
		client := newTestClient(t, server)
		require.NoError(t, client.DeleteEntity(t.Context(), "myapp", "Service", "checkout", map[string]string{"env": "prod"}))
	})
	t.Run("allows slash in name via query params", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, http.MethodDelete, r.Method)
			assert.Equal(t, "/apis/kg.grafana.com/v1alpha1/namespaces/stack-123/entities", r.URL.Path)
			assert.Equal(t, "foo/bar", r.URL.Query().Get("name"))
			w.WriteHeader(http.StatusNoContent)
		}))
		defer server.Close()
		client := newTestClient(t, server)
		require.NoError(t, client.DeleteEntity(t.Context(), "myapp", "Service", "foo/bar", nil))
	})
	t.Run("404 surfaces APIError", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		}))
		defer server.Close()
		client := newTestClient(t, server)
		err := client.DeleteEntity(t.Context(), "myapp", "Service", "missing", nil)
		var apiErr *kg.APIError
		require.ErrorAs(t, err, &apiErr)
		assert.Equal(t, http.StatusNotFound, apiErr.StatusCode)
	})
}

func TestClient_DeleteRelationship(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodDelete, r.Method)
		assert.Equal(t, "/apis/kg.grafana.com/v1alpha1/namespaces/stack-123/relationships", r.URL.Path)
		q := r.URL.Query()
		assert.Equal(t, "CALLS", q.Get("type"))
		assert.Equal(t, "myapp", q.Get("from.domain"))
		assert.Equal(t, "Service", q.Get("from.type"))
		assert.Equal(t, "checkout", q.Get("from.name"))
		assert.Equal(t, "prod", q.Get("from.scope[env]"))
		assert.Equal(t, "cart", q.Get("to.name"))
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	client := newTestClient(t, server)
	err := client.DeleteRelationship(t.Context(), "CALLS",
		kg.EntityRef{Domain: "myapp", Type: "Service", Name: "checkout", Scope: map[string]string{"env": "prod"}},
		kg.EntityRef{Domain: "myapp", Type: "Service", Name: "cart"})
	require.NoError(t, err)
}

func TestClient_UpsertRelationship(t *testing.T) {
	t.Run("200 written", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, http.MethodPost, r.Method)
			assert.Contains(t, r.URL.Path, "/namespaces/stack-123/relationships")
			var got kg.RelationshipWriteRequest
			assert.NoError(t, json.NewDecoder(r.Body).Decode(&got))
			assert.Equal(t, "CALLS", got.Type)
			assert.Equal(t, "checkout", got.From.Name)
			writeJSON(w, kg.RelationshipWriteResponse{Type: "CALLS"})
		}))
		defer server.Close()
		client := newTestClient(t, server)
		resp, err := client.UpsertRelationship(t.Context(), kg.RelationshipWriteRequest{
			Domain: "myapp", Type: "CALLS",
			From:       kg.EntityRef{Domain: "myapp", Type: "Service", Name: "checkout"},
			To:         kg.EntityRef{Domain: "myapp", Type: "Service", Name: "cart"},
			TTLSeconds: new(int64(-1)),
		})
		require.NoError(t, err)
		assert.Equal(t, "CALLS", resp.Type)
	})
	t.Run("404 from/to not found", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		}))
		defer server.Close()
		client := newTestClient(t, server)
		_, err := client.UpsertRelationship(t.Context(), kg.RelationshipWriteRequest{Domain: "myapp", Type: "CALLS", TTLSeconds: new(int64(-1))})
		var apiErr *kg.APIError
		require.ErrorAs(t, err, &apiErr)
	})
}

func TestClient_ValidateSuppressions(t *testing.T) {
	t.Run("valid returns nil", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, http.MethodPost, r.Method)
			assert.Contains(t, r.URL.Path, "config/disabled-alerts-validate")
			assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
			var got kg.Suppressions
			assert.NoError(t, json.NewDecoder(r.Body).Decode(&got))
			if assert.Len(t, got.DisabledAlertConfigs, 1) {
				assert.Equal(t, "my-suppression", got.DisabledAlertConfigs[0].Name)
			}
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()
		client := newTestClient(t, server)
		err := client.ValidateSuppressions(t.Context(), &kg.Suppressions{
			DisabledAlertConfigs: []kg.Suppression{
				{Name: "my-suppression", MatchLabels: map[string]string{"alertname": "ErrorRatioBreach"}},
			},
		})
		require.NoError(t, err)
	})

	t.Run("422 returns field errors", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnprocessableEntity)
			_, _ = w.Write([]byte(`{
				"status": "UNPROCESSABLE_ENTITY",
				"message": "Invalid disabled alert configuration file",
				"subErrors": [
					{"@type": "ApiValidationError", "field": "disabledAlertConfigs[0].name", "message": "Missing Rule Name"},
					{"@type": "ApiValidationError", "field": "disabledAlertConfigs[0].matchLabels", "message": "Missing Assertion"}
				]
			}`))
		}))
		defer server.Close()
		client := newTestClient(t, server)
		err := client.ValidateSuppressions(t.Context(), &kg.Suppressions{
			DisabledAlertConfigs: []kg.Suppression{{Name: ""}},
		})
		var verr *kg.SuppressionsValidationError
		require.ErrorAs(t, err, &verr)
		assert.Equal(t, http.StatusUnprocessableEntity, verr.StatusCode)
		assert.Equal(t, "Invalid disabled alert configuration file", verr.Message)
		require.Len(t, verr.Errors, 2)
		assert.Equal(t, "disabledAlertConfigs[0].name", verr.Errors[0].Field)
		assert.Equal(t, "Missing Rule Name", verr.Errors[0].Message)
		// Error() renders each field error.
		assert.Contains(t, verr.Error(), "disabledAlertConfigs[0].name: Missing Rule Name")
		assert.Contains(t, verr.Error(), "disabledAlertConfigs[0].matchLabels: Missing Assertion")
	})

	t.Run("500 returns generic APIError", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"message": "boom"}`))
		}))
		defer server.Close()
		client := newTestClient(t, server)
		err := client.ValidateSuppressions(t.Context(), &kg.Suppressions{
			DisabledAlertConfigs: []kg.Suppression{{Name: "x"}},
		})
		var apiErr *kg.APIError
		require.ErrorAs(t, err, &apiErr)
		assert.Equal(t, http.StatusInternalServerError, apiErr.HTTPStatusCode())
	})
}
