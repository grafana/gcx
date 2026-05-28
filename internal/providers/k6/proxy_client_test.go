package k6_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/providers/k6"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newAuthenticatedProxyClient creates a k6 client pointed at a test server.
// The server responds to the plugin /organization endpoint with orgID=42
// (lazily fetched by env var methods) and forwards cloud calls under the
// plugin /cloud prefix to the supplied handler.
func newAuthenticatedProxyClient(t *testing.T, handler http.Handler) *k6.ProxyClient {
	t.Helper()

	const proxyPrefix = "/api/plugins/k6-app/resources/cloud"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Handle plugin /organization endpoint (used lazily by env var methods).
		if r.Method == http.MethodGet && r.URL.Path == "/api/plugins/k6-app/resources/organization" {
			w.Header().Set("Content-Type", "application/json")
			writeJSON(t, w, map[string]any{"organization_id": 42})
			return
		}
		// Handle plugin /v3/account/me Token() endpoint.
		if r.Method == http.MethodGet && r.URL.Path == proxyPrefix+"/v3/account/me" {
			w.Header().Set("Content-Type", "application/json")
			writeJSON(t, w, map[string]any{"token": map[string]any{"key": "test-k6-token"}})
			return
		}
		// Strip the plugin /cloud prefix so handlers can match on the
		// underlying k6 API path (e.g. /cloud/v6/projects).
		if strings.HasPrefix(r.URL.Path, proxyPrefix) {
			r2 := r.Clone(r.Context())
			r2.URL.Path = strings.TrimPrefix(r.URL.Path, proxyPrefix)
			handler.ServeHTTP(w, r2)
			return
		}
		t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
	}))
	t.Cleanup(srv.Close)

	authClient := &http.Client{Transport: &bearerInjector{token: "test-k6-token"}}
	return k6.NewProxyClient(context.Background(), srv.URL, authClient)
}

// bearerInjector is a RoundTripper that injects a Bearer Authorization
// header on every request, mimicking a refresh-aware transport.
type bearerInjector struct {
	token string
	base  http.RoundTripper
}

func (b *bearerInjector) RoundTrip(req *http.Request) (*http.Response, error) {
	clone := req.Clone(req.Context())
	clone.Header.Set("Authorization", "Bearer "+b.token)
	base := b.base
	if base == nil {
		base = http.DefaultTransport
	}
	return base.RoundTrip(clone)
}

// TestProxyClient_Token_FetchesFromAccountMe verifies that Token in OAuth
// proxy mode resolves to /v3/account/me .token.key, routing the call through
// the plugin proxy without Bearer / X-Stack-Id headers (the auth client's
// transport injects auth, the plugin resolves the stack).
func TestProxyClient_Token_FetchesFromAccountMe(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/api/plugins/k6-app/resources/cloud/v3/account/me", r.URL.Path)
		assert.Equal(t, "Bearer gat_test-oauth-token", r.Header.Get("Authorization"))
		assert.Empty(t, r.Header.Get("X-Stack-Id"))
		w.Header().Set("Content-Type", "application/json")
		writeJSON(t, w, map[string]any{
			"token": map[string]any{"key": "k6-v3-from-me"},
		})
	}))
	defer srv.Close()

	authClient := &http.Client{Transport: &bearerInjector{token: "gat_test-oauth-token"}}
	client := k6.NewProxyClient(context.Background(), srv.URL, authClient)
	token, err := client.Token(t.Context())
	require.NoError(t, err)
	assert.Equal(t, "k6-v3-from-me", token)
}

// TestProxyClient_Token_Memoised confirms that repeated Token calls in proxy
// mode hit /v3/account/me only once.
func TestProxyClient_Token_Memoised(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/json")
		writeJSON(t, w, map[string]any{"token": map[string]any{"key": "k6-v3"}})
	}))
	defer srv.Close()

	client := k6.NewProxyClient(context.Background(), srv.URL, &http.Client{Transport: &bearerInjector{token: "tok"}})
	for range 3 {
		_, err := client.Token(t.Context())
		require.NoError(t, err)
	}
	assert.Equal(t, 1, calls, "expected /v3/account/me to be called once, got %d", calls)
}

// TestProxyClient_Token_EmptyKey surfaces an error when /v3/account/me
// returns a successful response with no token.key, rather than letting
// callers print an empty token.
func TestProxyClient_Token_EmptyKey(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		writeJSON(t, w, map[string]any{"token": map[string]any{"key": ""}})
	}))
	defer srv.Close()

	client := k6.NewProxyClient(context.Background(), srv.URL, &http.Client{Transport: &bearerInjector{token: "tok"}})
	_, err := client.Token(t.Context())
	require.Error(t, err)
}

// TestProxyClient_RoutesResourceCallsThroughProxy verifies that regular API
// calls hit the plugin proxy path (not api.k6.io) and omit Bearer +
// X-Stack-Id — relying entirely on the auth client's transport for
// credential injection.
func TestProxyClient_RoutesResourceCallsThroughProxy(t *testing.T) {
	var listRecorded struct {
		path string
		auth string
		stk  string
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/plugins/k6-app/resources/cloud/cloud/v6/projects":
			listRecorded.path = r.URL.Path
			listRecorded.auth = r.Header.Get("Authorization")
			listRecorded.stk = r.Header.Get("X-Stack-Id")
			writeJSON(t, w, map[string]any{
				"value": []map[string]any{{"id": 1, "name": "p"}},
			})
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()

	authClient := &http.Client{Transport: &bearerInjector{token: "gat_test-oauth-token"}}
	client := k6.NewProxyClient(context.Background(), srv.URL, authClient)

	projects, err := client.ListProjects(t.Context())
	require.NoError(t, err)
	require.Len(t, projects, 1)

	assert.Equal(t, "/api/plugins/k6-app/resources/cloud/cloud/v6/projects", listRecorded.path)
	// Bearer must come from the auth client's transport, not from a v3 token.
	assert.Equal(t, "Bearer gat_test-oauth-token", listRecorded.auth)
	assert.Empty(t, listRecorded.stk, "X-Stack-Id must be omitted in proxy mode")
}

func TestProxyClient_ListProjects(t *testing.T) {
	tests := []struct {
		name    string
		handler http.HandlerFunc
		wantLen int
		wantErr bool
	}{
		{
			name: "returns projects",
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodGet, r.Method)
				assert.Equal(t, "/cloud/v6/projects", r.URL.Path)
				assert.Equal(t, "Bearer test-k6-token", r.Header.Get("Authorization"))
				w.Header().Set("Content-Type", "application/json")
				writeJSON(t, w, map[string]any{
					"value": []map[string]any{
						{"id": 1, "name": "My Project"},
						{"id": 2, "name": "Other Project"},
					},
				})
			},
			wantLen: 2,
		},
		{
			name: "handles empty list",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				writeJSON(t, w, map[string]any{"value": []any{}})
			},
			wantLen: 0,
		},
		{
			name: "propagates error",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
				writeJSON(t, w, map[string]string{"error": "internal error"})
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := newAuthenticatedProxyClient(t, tt.handler)
			projects, err := client.ListProjects(t.Context())

			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Len(t, projects, tt.wantLen)
		})
	}
}

func TestProxyClient_GetProject(t *testing.T) {
	tests := []struct {
		name     string
		id       int
		handler  http.HandlerFunc
		wantName string
		wantErr  bool
	}{
		{
			name: "returns project by ID",
			id:   1,
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "/cloud/v6/projects/1", r.URL.Path)
				w.Header().Set("Content-Type", "application/json")
				writeJSON(t, w, map[string]any{"id": 1, "name": "My Project"})
			},
			wantName: "My Project",
		},
		{
			name: "not found",
			id:   999,
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "/cloud/v6/projects/999", r.URL.Path)
				w.WriteHeader(http.StatusNotFound)
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := newAuthenticatedProxyClient(t, tt.handler)
			p, err := client.GetProject(t.Context(), tt.id)

			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.wantName, p.Name)
		})
	}
}

func TestProxyClient_CreateProject(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/cloud/v6/projects", r.URL.Path)
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		assert.Equal(t, "New Project", body["name"])
		w.WriteHeader(http.StatusCreated)
		w.Header().Set("Content-Type", "application/json")
		writeJSON(t, w, map[string]any{"id": 10, "name": "New Project"})
	})

	client := newAuthenticatedProxyClient(t, handler)
	p, err := client.CreateProject(t.Context(), "New Project")
	require.NoError(t, err)
	assert.Equal(t, 10, p.ID)
	assert.Equal(t, "New Project", p.Name)
}

func TestProxyClient_DeleteProject(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodDelete, r.Method)
		assert.Equal(t, "/cloud/v6/projects/10", r.URL.Path)
		w.WriteHeader(http.StatusNoContent)
	})

	client := newAuthenticatedProxyClient(t, handler)
	err := client.DeleteProject(t.Context(), 10)
	require.NoError(t, err)
}

func TestProxyClient_ListLoadTests(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/cloud/v6/load_tests", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		writeJSON(t, w, map[string]any{
			"value": []map[string]any{
				{"id": 5, "name": "My Test", "project_id": 1},
				{"id": 6, "name": "Other Test", "project_id": 2},
			},
		})
	})

	client := newAuthenticatedProxyClient(t, handler)
	tests, err := client.ListLoadTests(t.Context())
	require.NoError(t, err)
	assert.Len(t, tests, 2)
	assert.Equal(t, "My Test", tests[0].Name)
}

func TestProxyClient_ListLoadTests_WithLimit(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		// When limit=5 is passed, $top should be 5 instead of the default 100
		assert.Equal(t, "5", r.URL.Query().Get("$top"))
		assert.Equal(t, "0", r.URL.Query().Get("$skip"))
		w.Header().Set("Content-Type", "application/json")
		writeJSON(t, w, map[string]any{
			"value": []map[string]any{
				{"id": 1, "name": "Test 1", "project_id": 1},
				{"id": 2, "name": "Test 2", "project_id": 1},
			},
		})
	})

	client := newAuthenticatedProxyClient(t, handler)
	tests, err := client.ListLoadTestsWithLimit(t.Context(), 5)
	require.NoError(t, err)
	assert.Len(t, tests, 2)
}

func TestProxyClient_GetLoadTest(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/cloud/v6/load_tests/6", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		writeJSON(t, w, map[string]any{"id": 6, "name": "my-load-test", "project_id": 1})
	})

	client := newAuthenticatedProxyClient(t, handler)
	test, err := client.GetLoadTest(t.Context(), 6)
	require.NoError(t, err)
	assert.Equal(t, "my-load-test", test.Name)
	assert.Equal(t, 1, test.ProjectID)
}

func TestProxyClient_ListTestRuns(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/cloud/v6/load_tests/6/test_runs", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		writeJSON(t, w, map[string]any{
			"value": []map[string]any{
				{"id": 101, "load_test_id": 6, "status": "finished", "result_status": 1, "created": "2026-01-01T00:00:00Z"},
			},
		})
	})

	client := newAuthenticatedProxyClient(t, handler)
	runs, err := client.ListTestRuns(t.Context(), 6)
	require.NoError(t, err)
	assert.Len(t, runs, 1)
	assert.Equal(t, "finished", runs[0].Status)
	assert.Equal(t, 1, runs[0].ResultStatus)
}

func TestProxyClient_ListEnvVars(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/v3/organizations/42/envvars", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		writeJSON(t, w, map[string]any{
			"envvars": []map[string]any{
				{"id": 3, "name": "MY_VAR", "value": "hello"},
			},
		})
	})

	client := newAuthenticatedProxyClient(t, handler)
	envVars, err := client.ListEnvVars(t.Context())
	require.NoError(t, err)
	assert.Len(t, envVars, 1)
	assert.Equal(t, "MY_VAR", envVars[0].Name)
}

func TestProxyClient_CreateEnvVar(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/v3/organizations/42/envvars", r.URL.Path)
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		assert.Equal(t, "NEW_VAR", body["name"])
		assert.Equal(t, "world", body["value"])
		w.WriteHeader(http.StatusCreated)
		w.Header().Set("Content-Type", "application/json")
		writeJSON(t, w, map[string]any{
			"envvar": map[string]any{"id": 4, "name": "NEW_VAR", "value": "world"},
		})
	})

	client := newAuthenticatedProxyClient(t, handler)
	ev, err := client.CreateEnvVar(t.Context(), "NEW_VAR", "world", "")
	require.NoError(t, err)
	assert.Equal(t, 4, ev.ID)
	assert.Equal(t, "NEW_VAR", ev.Name)
}

func TestProxyClient_UpdateEnvVar(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPatch, r.Method)
		assert.Equal(t, "/v3/organizations/42/envvars/3", r.URL.Path)
		w.WriteHeader(http.StatusNoContent)
	})

	client := newAuthenticatedProxyClient(t, handler)
	err := client.UpdateEnvVar(t.Context(), 3, "MY_VAR", "updated", "")
	require.NoError(t, err)
}

func TestProxyClient_DeleteEnvVar(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodDelete, r.Method)
		assert.Equal(t, "/v3/organizations/42/envvars/3", r.URL.Path)
		w.WriteHeader(http.StatusNoContent)
	})

	client := newAuthenticatedProxyClient(t, handler)
	err := client.DeleteEnvVar(t.Context(), 3)
	require.NoError(t, err)
}

func TestProxyClient_GetProjectByName(t *testing.T) {
	projectListHandler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		writeJSON(t, w, map[string]any{
			"value": []map[string]any{
				{"id": 1, "name": "My Project"},
				{"id": 2, "name": "Other"},
			},
		})
	})

	client := newAuthenticatedProxyClient(t, projectListHandler)

	// Found by name.
	p, err := client.GetProjectByName(t.Context(), "My Project")
	require.NoError(t, err)
	require.NotNil(t, p)
	assert.Equal(t, 1, p.ID)

	// Not found returns error.
	_, err = client.GetProjectByName(t.Context(), "Missing")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestProxyClient_ListLoadTestsByProject(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify server-side filtering is requested
		assert.Equal(t, "1", r.URL.Query().Get("project_id"))
		w.Header().Set("Content-Type", "application/json")
		// Mock returns only project 1's tests (server-side filtered)
		writeJSON(t, w, map[string]any{
			"value": []map[string]any{
				{"id": 5, "name": "Test A", "project_id": 1},
				{"id": 7, "name": "Test C", "project_id": 1},
			},
		})
	})

	client := newAuthenticatedProxyClient(t, handler)
	tests, err := client.ListLoadTestsByProject(t.Context(), 1)
	require.NoError(t, err)
	assert.Len(t, tests, 2)
	assert.Equal(t, "Test A", tests[0].Name)
	assert.Equal(t, "Test C", tests[1].Name)
}

func TestProxyClient_CreateLoadTest(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/cloud/v6/projects/1/load_tests", r.URL.Path)
		assert.Contains(t, r.Header.Get("Content-Type"), "multipart/form-data")
		w.WriteHeader(http.StatusCreated)
		writeJSON(t, w, map[string]any{"id": 10, "name": "New Test", "project_id": 1})
	})

	client := newAuthenticatedProxyClient(t, handler)
	lt, err := client.CreateLoadTest(t.Context(), "New Test", 1, "export default function() {}")
	require.NoError(t, err)
	assert.Equal(t, 10, lt.ID)
	assert.Equal(t, "New Test", lt.Name)
}

func TestProxyClient_UpdateLoadTest(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPatch {
			assert.Equal(t, "/cloud/v6/load_tests/5", r.URL.Path)
			w.WriteHeader(http.StatusNoContent)
			return
		}
		t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
	})

	client := newAuthenticatedProxyClient(t, handler)
	err := client.UpdateLoadTest(t.Context(), 5, "Updated Name", "")
	require.NoError(t, err)
}

func TestProxyClient_UpdateLoadTestScript(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPut, r.Method)
		assert.Equal(t, "/cloud/v6/load_tests/5/script", r.URL.Path)
		assert.Equal(t, "application/octet-stream", r.Header.Get("Content-Type"))
		w.WriteHeader(http.StatusNoContent)
	})

	client := newAuthenticatedProxyClient(t, handler)
	err := client.UpdateLoadTestScript(t.Context(), 5, "export default function() { console.log('hi'); }")
	require.NoError(t, err)
}

func TestProxyClient_GetLoadTestScript(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/cloud/v6/load_tests/5/script", r.URL.Path)
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("export default function() {}"))
	})

	client := newAuthenticatedProxyClient(t, handler)
	script, err := client.GetLoadTestScript(t.Context(), 5)
	require.NoError(t, err)
	assert.Equal(t, "export default function() {}", script)
}

func TestProxyClient_GetLoadTestByName(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		writeJSON(t, w, map[string]any{
			"value": []map[string]any{
				{"id": 5, "name": "alpha", "project_id": 1},
				{"id": 6, "name": "beta", "project_id": 1},
			},
		})
	})

	client := newAuthenticatedProxyClient(t, handler)

	lt, err := client.GetLoadTestByName(t.Context(), 1, "beta")
	require.NoError(t, err)
	require.NotNil(t, lt)
	assert.Equal(t, 6, lt.ID)

	_, err = client.GetLoadTestByName(t.Context(), 1, "gamma")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestProxyClient_ListSchedules(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/cloud/v6/schedules", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		writeJSON(t, w, map[string]any{
			"value": []map[string]any{
				{"id": 10, "load_test_id": 5, "starts": "2026-06-01T10:00:00Z"},
			},
		})
	})

	client := newAuthenticatedProxyClient(t, handler)
	schedules, err := client.ListSchedules(t.Context())
	require.NoError(t, err)
	assert.Len(t, schedules, 1)
	assert.Equal(t, 10, schedules[0].ID)
	assert.Equal(t, 5, schedules[0].LoadTestID)
}

func TestProxyClient_GetSchedule(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/cloud/v6/schedules/10", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		writeJSON(t, w, map[string]any{
			"id": 10, "load_test_id": 5, "starts": "2026-06-01T10:00:00Z",
		})
	})

	client := newAuthenticatedProxyClient(t, handler)
	s, err := client.GetSchedule(t.Context(), 10)
	require.NoError(t, err)
	assert.Equal(t, 10, s.ID)
}

func TestProxyClient_CreateSchedule(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/cloud/v6/load_tests/5/schedule", r.URL.Path)
		w.WriteHeader(http.StatusCreated)
		writeJSON(t, w, map[string]any{
			"id": 10, "load_test_id": 5, "starts": "2026-06-01T10:00:00Z",
		})
	})

	client := newAuthenticatedProxyClient(t, handler)
	s, err := client.CreateSchedule(t.Context(), 5, k6.ScheduleRequest{
		Starts: "2026-06-01T10:00:00Z",
	})
	require.NoError(t, err)
	assert.Equal(t, 10, s.ID)
}

func TestProxyClient_UpdateScheduleByID(t *testing.T) {
	t.Run("200 OK with body", func(t *testing.T) {
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, http.MethodPut, r.Method)
			assert.Equal(t, "/cloud/v6/schedules/10", r.URL.Path)
			writeJSON(t, w, map[string]any{
				"id": 10, "load_test_id": 5, "starts": "2026-07-01T12:00:00Z",
			})
		})

		client := newAuthenticatedProxyClient(t, handler)
		s, err := client.UpdateScheduleByID(t.Context(), 10, k6.ScheduleRequest{
			Starts: "2026-07-01T12:00:00Z",
		})
		require.NoError(t, err)
		assert.Equal(t, "2026-07-01T12:00:00Z", s.Starts)
	})

	t.Run("204 No Content re-fetches", func(t *testing.T) {
		calls := 0
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls++
			if calls == 1 {
				assert.Equal(t, http.MethodPut, r.Method)
				w.WriteHeader(http.StatusNoContent)
				return
			}
			// Re-fetch via GetSchedule
			assert.Equal(t, http.MethodGet, r.Method)
			assert.Equal(t, "/cloud/v6/schedules/10", r.URL.Path)
			writeJSON(t, w, map[string]any{
				"id": 10, "load_test_id": 5, "starts": "2026-07-01T12:00:00Z",
			})
		})

		client := newAuthenticatedProxyClient(t, handler)
		s, err := client.UpdateScheduleByID(t.Context(), 10, k6.ScheduleRequest{
			Starts: "2026-07-01T12:00:00Z",
		})
		require.NoError(t, err)
		require.NotNil(t, s)
		assert.Equal(t, 10, s.ID)
		assert.Equal(t, 2, calls)
	})
}

func TestProxyClient_DeleteScheduleByLoadTest(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodDelete, r.Method)
		assert.Equal(t, "/cloud/v6/load_tests/5/schedule", r.URL.Path)
		w.WriteHeader(http.StatusNoContent)
	})

	client := newAuthenticatedProxyClient(t, handler)
	err := client.DeleteScheduleByLoadTest(t.Context(), 5)
	require.NoError(t, err)
}

func TestProxyClient_ListLoadZones(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/cloud/v6/load_zones", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		writeJSON(t, w, map[string]any{
			"value": []map[string]any{
				{"id": 1, "name": "my-plz", "k6_load_zone_id": "k6-plz-123"},
			},
		})
	})

	client := newAuthenticatedProxyClient(t, handler)
	zones, err := client.ListLoadZones(t.Context())
	require.NoError(t, err)
	assert.Len(t, zones, 1)
	assert.Equal(t, "my-plz", zones[0].Name)
}

func TestProxyClient_CreateLoadZone(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/cloud-resources/v1/load-zones", r.URL.Path)
		w.WriteHeader(http.StatusCreated)
		writeJSON(t, w, map[string]any{
			"name": "my-plz", "k6_load_zone_id": "k6-plz-123",
		})
	})

	client := newAuthenticatedProxyClient(t, handler)
	resp, err := client.CreateLoadZone(t.Context(), k6.PLZCreateRequest{
		K6LoadZoneID: "k6-plz-123",
		ProviderID:   "aws",
		PodTiers:     k6.PLZPodTiers{CPU: "1", Memory: "2Gi"},
	})
	require.NoError(t, err)
	assert.Equal(t, "my-plz", resp.Name)
}

func TestProxyClient_DeleteLoadZone(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodDelete, r.Method)
		assert.Equal(t, "/cloud-resources/v1/load-zones/my-plz", r.URL.Path)
		w.WriteHeader(http.StatusNoContent)
	})

	client := newAuthenticatedProxyClient(t, handler)
	err := client.DeleteLoadZone(t.Context(), "my-plz")
	require.NoError(t, err)
}

func TestProxyClient_ListAllowedProjects(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/cloud/v6/load_zones/1/allowed_projects", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		writeJSON(t, w, map[string]any{
			"value": []map[string]any{{"id": 10, "name": "proj-a"}},
		})
	})

	client := newAuthenticatedProxyClient(t, handler)
	projects, err := client.ListAllowedProjects(t.Context(), 1)
	require.NoError(t, err)
	assert.Len(t, projects, 1)
	assert.Equal(t, 10, projects[0].ID)
}

func TestProxyClient_UpdateAllowedProjects(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPut, r.Method)
		assert.Equal(t, "/cloud/v6/load_zones/1/allowed_projects", r.URL.Path)
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		assert.NotNil(t, body["project_ids"])
		w.WriteHeader(http.StatusOK)
	})

	client := newAuthenticatedProxyClient(t, handler)
	err := client.UpdateAllowedProjects(t.Context(), 1, []int{10, 20})
	require.NoError(t, err)
}

func TestProxyClient_ListAllowedLoadZones(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/cloud/v6/projects/1/allowed_load_zones", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		writeJSON(t, w, map[string]any{
			"value": []map[string]any{{"id": 100, "name": "zone-a"}},
		})
	})

	client := newAuthenticatedProxyClient(t, handler)
	zones, err := client.ListAllowedLoadZones(t.Context(), 1)
	require.NoError(t, err)
	assert.Len(t, zones, 1)
	assert.Equal(t, 100, zones[0].ID)
}

func TestProxyClient_UpdateAllowedLoadZones(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPut, r.Method)
		assert.Equal(t, "/cloud/v6/projects/1/allowed_load_zones", r.URL.Path)
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		assert.NotNil(t, body["load_zone_ids"])
		w.WriteHeader(http.StatusOK)
	})

	client := newAuthenticatedProxyClient(t, handler)
	err := client.UpdateAllowedLoadZones(t.Context(), 1, []int{100, 200})
	require.NoError(t, err)
}

func writeJSON(t *testing.T, w http.ResponseWriter, v any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		t.Fatalf("writeJSON: %v", err)
	}
}
