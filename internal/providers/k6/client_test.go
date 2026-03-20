package k6_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/grafana/grafanactl/internal/providers/k6"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newAuthenticatedClient creates a k6 client pointed at a test server,
// pre-authenticated with orgID=42 and stackID=999.
func newAuthenticatedClient(t *testing.T, handler http.Handler) *k6.Client {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Handle auth endpoint.
		if r.Method == http.MethodPut && r.URL.Path == "/v3/account/grafana-app/start" {
			w.Header().Set("Content-Type", "application/json")
			writeJSON(t, w, map[string]any{
				"organization_id":  "42",
				"v3_grafana_token": "test-k6-token",
			})
			return
		}
		// Forward to test handler.
		handler.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)

	client := k6.NewClient(srv.URL)
	err := client.Authenticate(t.Context(), "test-ap-token", 999)
	require.NoError(t, err)
	assert.Equal(t, 42, client.OrgID())
	assert.Equal(t, "test-k6-token", client.Token())
	return client
}

func TestClient_Authenticate(t *testing.T) {
	tests := []struct {
		name    string
		handler http.HandlerFunc
		wantErr bool
	}{
		{
			name: "successful auth",
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodPut, r.Method)
				assert.Equal(t, "/v3/account/grafana-app/start", r.URL.Path)
				assert.Equal(t, "test-token", r.Header.Get("X-Grafana-Key"))
				assert.Equal(t, "999", r.Header.Get("X-Stack-Id"))
				w.Header().Set("Content-Type", "application/json")
				writeJSON(t, w, map[string]any{
					"organization_id":  "42",
					"v3_grafana_token": "k6-token-abc",
				})
			},
		},
		{
			name: "auth failure",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusForbidden)
				writeJSON(t, w, map[string]string{"message": "forbidden"})
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(tt.handler)
			defer srv.Close()

			client := k6.NewClient(srv.URL)
			err := client.Authenticate(t.Context(), "test-token", 999)

			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, 42, client.OrgID())
			assert.Equal(t, "k6-token-abc", client.Token())
		})
	}
}

func TestClient_ListProjects(t *testing.T) {
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
			client := newAuthenticatedClient(t, tt.handler)
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

func TestClient_GetProject(t *testing.T) {
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
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				writeJSON(t, w, map[string]any{
					"value": []map[string]any{
						{"id": 1, "name": "My Project"},
						{"id": 2, "name": "Other"},
					},
				})
			},
			wantName: "My Project",
		},
		{
			name: "not found",
			id:   999,
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				writeJSON(t, w, map[string]any{"value": []map[string]any{{"id": 1, "name": "X"}}})
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := newAuthenticatedClient(t, tt.handler)
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

func TestClient_CreateProject(t *testing.T) {
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

	client := newAuthenticatedClient(t, handler)
	p, err := client.CreateProject(t.Context(), "New Project")
	require.NoError(t, err)
	assert.Equal(t, 10, p.ID)
	assert.Equal(t, "New Project", p.Name)
}

func TestClient_DeleteProject(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodDelete, r.Method)
		assert.Equal(t, "/cloud/v6/projects/10", r.URL.Path)
		w.WriteHeader(http.StatusNoContent)
	})

	client := newAuthenticatedClient(t, handler)
	err := client.DeleteProject(t.Context(), 10)
	require.NoError(t, err)
}

func TestClient_ListLoadTests(t *testing.T) {
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

	client := newAuthenticatedClient(t, handler)
	tests, err := client.ListLoadTests(t.Context())
	require.NoError(t, err)
	assert.Len(t, tests, 2)
	assert.Equal(t, "My Test", tests[0].Name)
}

func TestClient_GetLoadTest(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/cloud/v6/load_tests/6", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		writeJSON(t, w, map[string]any{"id": 6, "name": "my-load-test", "project_id": 1})
	})

	client := newAuthenticatedClient(t, handler)
	test, err := client.GetLoadTest(t.Context(), 6)
	require.NoError(t, err)
	assert.Equal(t, "my-load-test", test.Name)
	assert.Equal(t, 1, test.ProjectID)
}

func TestClient_ListTestRuns(t *testing.T) {
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

	client := newAuthenticatedClient(t, handler)
	runs, err := client.ListTestRuns(t.Context(), 6)
	require.NoError(t, err)
	assert.Len(t, runs, 1)
	assert.Equal(t, "finished", runs[0].Status)
	assert.Equal(t, 1, runs[0].ResultStatus)
}

func TestClient_ListEnvVars(t *testing.T) {
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

	client := newAuthenticatedClient(t, handler)
	envVars, err := client.ListEnvVars(t.Context())
	require.NoError(t, err)
	assert.Len(t, envVars, 1)
	assert.Equal(t, "MY_VAR", envVars[0].Name)
}

func TestClient_CreateEnvVar(t *testing.T) {
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

	client := newAuthenticatedClient(t, handler)
	ev, err := client.CreateEnvVar(t.Context(), "NEW_VAR", "world", "")
	require.NoError(t, err)
	assert.Equal(t, 4, ev.ID)
	assert.Equal(t, "NEW_VAR", ev.Name)
}

func TestClient_UpdateEnvVar(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPatch, r.Method)
		assert.Equal(t, "/v3/organizations/42/envvars/3", r.URL.Path)
		w.WriteHeader(http.StatusNoContent)
	})

	client := newAuthenticatedClient(t, handler)
	err := client.UpdateEnvVar(t.Context(), 3, "MY_VAR", "updated", "")
	require.NoError(t, err)
}

func TestClient_DeleteEnvVar(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodDelete, r.Method)
		assert.Equal(t, "/v3/organizations/42/envvars/3", r.URL.Path)
		w.WriteHeader(http.StatusNoContent)
	})

	client := newAuthenticatedClient(t, handler)
	err := client.DeleteEnvVar(t.Context(), 3)
	require.NoError(t, err)
}

func writeJSON(t *testing.T, w http.ResponseWriter, v any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		t.Fatalf("writeJSON: %v", err)
	}
}
