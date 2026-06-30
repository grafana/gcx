package integrations_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/grafana/gcx/internal/assistant/assistanthttp"
	"github.com/grafana/gcx/internal/assistant/integrations"
	"github.com/grafana/gcx/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"
)

func newTestClient(t *testing.T, handler http.Handler) *integrations.Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	cfg := config.NamespacedRESTConfig{
		Config:    rest.Config{Host: server.URL},
		Namespace: "default",
	}
	base, err := assistanthttp.NewClient(cfg)
	require.NoError(t, err)
	return integrations.NewClient(base)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		panic(err)
	}
}

func boolPtr(b bool) *bool { return &b } //nolint:modernize // new(bool) gives *false, not a pointer to the given value.

func TestList(t *testing.T) {
	tests := []struct {
		name      string
		opts      integrations.ListOptions
		handler   http.HandlerFunc
		wantCount int
		wantErr   bool
	}{
		{
			name: "success",
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodGet, r.Method)
				assert.Contains(t, r.URL.Path, "/integrations")
				writeJSON(w, map[string]any{
					"data": map[string]any{
						"integrations": []integrations.Integration{
							{ID: "int-1", Name: "mcp-1", Type: "mcp", Scope: "user", Enabled: boolPtr(true)},    //nolint:modernize
							{ID: "int-2", Name: "mcp-2", Type: "mcp", Scope: "tenant", Enabled: boolPtr(false)}, //nolint:modernize
						},
						"pagination": integrations.Pagination{Total: 2, Limit: 20, Offset: 0},
					},
				})
			},
			wantCount: 2,
		},
		{
			name: "filter by scope",
			opts: integrations.ListOptions{Scope: "user"},
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "user", r.URL.Query().Get("scope"))
				writeJSON(w, map[string]any{
					"data": map[string]any{
						"integrations": []integrations.Integration{
							{ID: "int-1", Name: "mcp-1", Type: "mcp", Scope: "user", Enabled: boolPtr(true)}, //nolint:modernize
						},
						"pagination": integrations.Pagination{Total: 1, Limit: 20, Offset: 0},
					},
				})
			},
			wantCount: 1,
		},
		{
			name: "enabled only",
			opts: integrations.ListOptions{EnabledOnly: true},
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "true", r.URL.Query().Get("enabled_only"))
				writeJSON(w, map[string]any{
					"data": map[string]any{
						"integrations": []integrations.Integration{},
						"pagination":   integrations.Pagination{Total: 0, Limit: 20, Offset: 0},
					},
				})
			},
			wantCount: 0,
		},
		{
			name: "pagination params",
			opts: integrations.ListOptions{Limit: 10, Offset: 20},
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "10", r.URL.Query().Get("limit"))
				assert.Equal(t, "20", r.URL.Query().Get("offset"))
				writeJSON(w, map[string]any{
					"data": map[string]any{
						"integrations": []integrations.Integration{},
						"pagination":   integrations.Pagination{Total: 30, Limit: 10, Offset: 20},
					},
				})
			},
			wantCount: 0,
		},
		{
			name: "null list",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				writeJSON(w, map[string]any{
					"data": map[string]any{
						"integrations": nil,
						"pagination":   integrations.Pagination{Total: 0, Limit: 20, Offset: 0},
					},
				})
			},
			wantCount: 0,
		},
		{
			name: "server error",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte("internal error"))
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := newTestClient(t, tt.handler)
			items, _, err := client.List(t.Context(), tt.opts)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Len(t, items, tt.wantCount)
		})
	}
}

func TestGet(t *testing.T) {
	tests := []struct {
		name    string
		handler http.HandlerFunc
		wantErr bool
	}{
		{
			name: "success",
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodGet, r.Method)
				assert.Contains(t, r.URL.Path, "/integrations/int-1")
				writeJSON(w, map[string]any{
					"data": integrations.Integration{
						ID: "int-1", Name: "mcp-1", Type: "mcp", Scope: "user",
						Enabled: boolPtr(true), CreatedBy: "admin", UpdatedBy: "admin", //nolint:modernize
						CreatedAt: time.Now(), ModifiedAt: time.Now(),
					},
				})
			},
		},
		{
			name: "not found",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write([]byte("not found"))
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := newTestClient(t, tt.handler)
			item, err := client.Get(t.Context(), "int-1")
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, "int-1", item.ID)
			assert.Equal(t, "mcp-1", item.Name)
		})
	}
}

func TestCreate(t *testing.T) {
	tests := []struct {
		name    string
		handler http.HandlerFunc
		wantErr bool
	}{
		{
			name: "success",
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodPost, r.Method)
				assert.Contains(t, r.URL.Path, "/integrations")

				body, _ := io.ReadAll(r.Body)
				var req integrations.CreateRequest
				assert.NoError(t, json.Unmarshal(body, &req))
				assert.Equal(t, "test-mcp", req.Name)
				assert.Equal(t, "mcp", req.Type)
				assert.Equal(t, "user", req.Scope)

				writeJSON(w, map[string]any{
					"data": integrations.Integration{
						ID: "int-new", Name: "test-mcp", Type: "mcp", Scope: "user",
						Enabled: boolPtr(true), CreatedBy: "admin", UpdatedBy: "admin", //nolint:modernize
						CreatedAt: time.Now(), ModifiedAt: time.Now(),
					},
				})
			},
		},
		{
			name: "server error",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte("bad request"))
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := newTestClient(t, tt.handler)
			enabled := true
			resp, err := client.Create(t.Context(), integrations.CreateRequest{
				Name:         "test-mcp",
				Type:         "mcp",
				Scope:        "user",
				Enabled:      &enabled,
				Applications: []string{"all"},
			})
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, "int-new", resp.ID)
		})
	}
}

func TestUpdate(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPut, r.Method)
		assert.Contains(t, r.URL.Path, "/integrations/int-1")

		body, _ := io.ReadAll(r.Body)
		var req integrations.UpdateRequest
		assert.NoError(t, json.Unmarshal(body, &req))
		assert.Equal(t, "user", req.Scope)
		assert.Equal(t, "renamed", req.Name)

		writeJSON(w, map[string]any{
			"data": integrations.Integration{
				ID: "int-1", Name: "renamed", Type: "mcp", Scope: "user",
				Enabled: boolPtr(true), CreatedBy: "admin", UpdatedBy: "admin", //nolint:modernize
				CreatedAt: time.Now(), ModifiedAt: time.Now(),
			},
		})
	}))

	resp, err := client.Update(t.Context(), "int-1", integrations.UpdateRequest{
		Scope: "user",
		Name:  "renamed",
	})
	require.NoError(t, err)
	assert.Equal(t, "renamed", resp.Name)
}

func TestDelete(t *testing.T) {
	callCount := 0
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount == 1 {
			// First call: GET to discover scope
			assert.Equal(t, http.MethodGet, r.Method)
			writeJSON(w, map[string]any{
				"data": integrations.Integration{
					ID: "int-1", Name: "mcp-1", Type: "mcp", Scope: "tenant",
					Enabled: boolPtr(true), CreatedBy: "admin", UpdatedBy: "admin", //nolint:modernize
					CreatedAt: time.Now(), ModifiedAt: time.Now(),
				},
			})
			return
		}
		// Second call: DELETE with scope header
		assert.Equal(t, http.MethodDelete, r.Method)
		assert.Equal(t, "tenant", r.Header.Get("X-Resource-Scope"))
		w.WriteHeader(http.StatusNoContent)
	}))

	err := client.Delete(t.Context(), "int-1")
	require.NoError(t, err)
	assert.Equal(t, 2, callCount)
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		handler http.HandlerFunc
		wantErr bool
	}{
		{
			name: "success with tools",
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodGet, r.Method)
				assert.Contains(t, r.URL.Path, "/integrations/int-1/validate")
				writeJSON(w, map[string]any{
					"data": map[string]any{
						"result": integrations.ValidationResult{
							Status:  "success",
							Message: "Connected",
							Tools: []integrations.MCPTool{
								{Name: "search", Description: "Search for things"},
								{Name: "create", Description: "Create things"},
							},
						},
					},
				})
			},
		},
		{
			name: "failed validation",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				writeJSON(w, map[string]any{
					"data": map[string]any{
						"result": integrations.ValidationResult{
							Status: "failed",
							Error:  "connection refused",
						},
					},
				})
			},
		},
		{
			name: "server error",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte("internal error"))
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := newTestClient(t, tt.handler)
			result, err := client.Validate(t.Context(), "int-1")
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.NotEmpty(t, result.Status)
		})
	}
}

func TestGet_InvalidJSON(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("{invalid"))
	}))

	_, err := client.Get(t.Context(), "int-1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to decode")
}

func TestList_InvalidJSON(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("{invalid"))
	}))

	_, _, err := client.List(t.Context(), integrations.ListOptions{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to decode")
}
