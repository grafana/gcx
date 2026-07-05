package mcpservers_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/grafana/gcx/internal/assistant/assistanthttp"
	"github.com/grafana/gcx/internal/assistant/mcpservers"
	"github.com/grafana/gcx/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"
)

func newTestClient(t *testing.T, handler http.Handler) *mcpservers.Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	cfg := config.NamespacedRESTConfig{
		Config:    rest.Config{Host: server.URL},
		Namespace: "default",
	}
	base, err := assistanthttp.NewClient(cfg)
	require.NoError(t, err)
	return mcpservers.NewClient(base)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		panic(err)
	}
}

func TestListFiltersMCPIntegrations(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/api/plugins/grafana-assistant-app/resources/api/v1/integrations", r.URL.Path)
		assert.Equal(t, "20", r.URL.Query().Get("limit"))

		writeJSON(w, map[string]any{
			"data": map[string]any{
				"integrations": []map[string]any{
					{
						"id":          "mcp-1",
						"name":        "Remote MCP",
						"description": "remote tools",
						"type":        "mcp",
						"enabled":     true,
						"scope":       "user",
						"configuration": map[string]any{
							"url": "https://mcp.example.com/mcp",
						},
					},
					{
						"id":      "other-1",
						"name":    "not mcp",
						"type":    "not-mcp",
						"enabled": true,
					},
				},
			},
		})
	}))

	servers, err := client.List(t.Context(), mcpservers.ListOptions{Limit: 20})
	require.NoError(t, err)
	require.Len(t, servers, 1)
	assert.Equal(t, "mcp-1", servers[0].ID)
	assert.Equal(t, "Remote MCP", servers[0].Name)
	assert.Equal(t, "https://mcp.example.com/mcp", servers[0].URL)
}

func TestGetFallsBackToListForNames(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/plugins/grafana-assistant-app/resources/api/v1/integrations/Remote MCP":
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"message":"invalid integration ID"}`))
		case "/api/plugins/grafana-assistant-app/resources/api/v1/integrations":
			writeJSON(w, map[string]any{
				"data": map[string]any{
					"integrations": []map[string]any{
						{"id": "mcp-1", "name": "Remote MCP", "type": "mcp", "enabled": true},
					},
				},
			})
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))

	server, err := client.Get(t.Context(), "Remote MCP")
	require.NoError(t, err)
	assert.Equal(t, "mcp-1", server.ID)
}

func TestGetErrorsOnAmbiguousName(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/plugins/grafana-assistant-app/resources/api/v1/integrations/Remote MCP":
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"message":"invalid integration ID"}`))
		case "/api/plugins/grafana-assistant-app/resources/api/v1/integrations":
			writeJSON(w, map[string]any{
				"data": map[string]any{
					"integrations": []map[string]any{
						{"id": "mcp-user", "name": "Remote MCP", "type": "mcp", "enabled": true, "scope": "user"},
						{"id": "mcp-tenant", "name": "remote mcp", "type": "mcp", "enabled": true, "scope": "tenant"},
					},
				},
			})
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))

	_, err := client.Get(t.Context(), "Remote MCP")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ambiguous MCP server name")
}

func TestCreatePostsMCPIntegration(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/api/plugins/grafana-assistant-app/resources/api/v1/integrations", r.URL.Path)

		var payload map[string]any
		if !readJSONRequest(t, r, &payload) {
			return
		}
		assert.Equal(t, "mcp", payload["type"])
		assert.Equal(t, "Remote MCP", payload["name"])
		assert.Equal(t, true, payload["enabled"])
		assert.Equal(t, "user", payload["scope"])
		assert.Equal(t, []any{"assistant"}, payload["applications"])

		cfg, ok := payload["configuration"].(map[string]any)
		if !assert.True(t, ok) {
			return
		}
		assert.Equal(t, "https://mcp.example.com/mcp", cfg["url"])

		headers, ok := payload["custom_headers"].([]any)
		if !assert.True(t, ok) {
			return
		}
		if !assert.Len(t, headers, 1) {
			return
		}
		assert.Equal(t, map[string]any{"name": "Authorization", "value": "Bearer token"}, headers[0])

		w.WriteHeader(http.StatusCreated)
		writeJSON(w, map[string]any{
			"data": map[string]any{
				"integration": map[string]any{
					"id":      "mcp-1",
					"name":    "Remote MCP",
					"type":    "mcp",
					"enabled": true,
				},
			},
		})
	}))

	enabled := true
	created, err := client.Create(t.Context(), mcpservers.ServerInput{
		Name:         "Remote MCP",
		URL:          "https://mcp.example.com/mcp",
		Enabled:      &enabled,
		Headers:      []mcpservers.Header{{Name: "Authorization", Value: "Bearer token"}},
		Applications: []string{"assistant"},
	})
	require.NoError(t, err)
	assert.Equal(t, "mcp-1", created.Server.ID)
	assert.Equal(t, "created", created.Operation)
}

func TestCreateReadsBackServerWhenResponseOmitsIntegration(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/plugins/grafana-assistant-app/resources/api/v1/integrations":
			w.WriteHeader(http.StatusCreated)
			writeJSON(w, map[string]any{"data": map[string]any{}})
		case r.Method == http.MethodGet && r.URL.Path == "/api/plugins/grafana-assistant-app/resources/api/v1/integrations/Remote MCP":
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"message":"invalid integration ID"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/plugins/grafana-assistant-app/resources/api/v1/integrations":
			writeJSON(w, map[string]any{
				"data": map[string]any{
					"integrations": []map[string]any{
						{"id": "mcp-1", "name": "Remote MCP", "type": "mcp", "enabled": true, "scope": "user",
							"configuration": map[string]any{"url": "https://mcp.example.com/mcp"}},
					},
				},
			})
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))

	created, err := client.Create(t.Context(), mcpservers.ServerInput{
		Name: "Remote MCP",
		URL:  "https://mcp.example.com/mcp",
	})
	require.NoError(t, err)
	require.NotNil(t, created.Server)
	assert.Equal(t, "mcp-1", created.Server.ID)
	assert.Equal(t, "created", created.Operation)
}

func TestCreateReadBackDisambiguatesSameNameByScope(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/plugins/grafana-assistant-app/resources/api/v1/integrations":
			w.WriteHeader(http.StatusCreated)
			writeJSON(w, map[string]any{"data": map[string]any{}})
		case r.Method == http.MethodGet && r.URL.Path == "/api/plugins/grafana-assistant-app/resources/api/v1/integrations":
			// A user-scoped and tenant-scoped server share the same name.
			// Read-back must pick the just-created tenant server, not error.
			writeJSON(w, map[string]any{
				"data": map[string]any{
					"integrations": []map[string]any{
						{"id": "mcp-user", "name": "Remote MCP", "type": "mcp", "enabled": true, "scope": "user",
							"configuration": map[string]any{"url": "https://mcp.example.com/mcp"}},
						{"id": "mcp-tenant", "name": "Remote MCP", "type": "mcp", "enabled": true, "scope": "tenant",
							"configuration": map[string]any{"url": "https://mcp.example.com/mcp"}},
					},
				},
			})
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))

	created, err := client.Create(t.Context(), mcpservers.ServerInput{
		Name:    "Remote MCP",
		URL:     "https://mcp.example.com/mcp",
		Scope:   "tenant",
		Headers: []mcpservers.Header{{Name: "Authorization", Value: "Bearer token"}},
	})
	require.NoError(t, err)
	require.NotNil(t, created.Server)
	assert.Equal(t, "mcp-tenant", created.Server.ID)
}

func TestCreateFailsWhenResponseOmitsIntegrationAndReadBackFails(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/plugins/grafana-assistant-app/resources/api/v1/integrations":
			w.WriteHeader(http.StatusCreated)
			writeJSON(w, map[string]any{"data": map[string]any{}})
		case r.Method == http.MethodGet && r.URL.Path == "/api/plugins/grafana-assistant-app/resources/api/v1/integrations/Remote MCP":
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"message":"not found"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/plugins/grafana-assistant-app/resources/api/v1/integrations":
			writeJSON(w, map[string]any{
				"data": map[string]any{
					"integrations": []map[string]any{},
				},
			})
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))

	_, err := client.Create(t.Context(), mcpservers.ServerInput{
		Name: "Remote MCP",
		URL:  "https://mcp.example.com/mcp",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to read back created MCP server")
}

func TestUpdateMergesCurrentServerBeforePut(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/plugins/grafana-assistant-app/resources/api/v1/integrations/mcp-1":
			writeJSON(w, map[string]any{
				"data": map[string]any{
					"id":          "mcp-1",
					"name":        "Remote MCP",
					"description": "old description",
					"type":        "mcp",
					"enabled":     true,
					"scope":       "user",
					"applications": []string{
						"assistant",
					},
					"configuration": map[string]any{
						"url":       "https://mcp.example.com/mcp",
						"builtinId": "builtin-1",
						"opaque":    "keep-me",
					},
				},
			})
		case r.Method == http.MethodPut && r.URL.Path == "/api/plugins/grafana-assistant-app/resources/api/v1/integrations/mcp-1":
			assert.Equal(t, "user", r.Header.Get("X-Resource-Scope"))
			var payload map[string]any
			if !readJSONRequest(t, r, &payload) {
				return
			}
			assert.Equal(t, "new description", payload["description"])
			assert.Equal(t, true, payload["enabled"])
			cfg, ok := payload["configuration"].(map[string]any)
			if !assert.True(t, ok) {
				return
			}
			assert.Equal(t, "https://mcp.example.com/mcp", cfg["url"])
			assert.Equal(t, "builtin-1", cfg["builtinId"])
			assert.Equal(t, "keep-me", cfg["opaque"])

			writeJSON(w, map[string]any{
				"data": map[string]any{
					"integration": map[string]any{
						"id":      "mcp-1",
						"name":    "Remote MCP",
						"type":    "mcp",
						"enabled": true,
					},
				},
			})
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))

	updated, err := client.Update(t.Context(), "mcp-1", mcpservers.ServerInput{Description: "new description"})
	require.NoError(t, err)
	assert.Equal(t, "updated", updated.Operation)
	assert.Equal(t, "mcp-1", updated.Server.ID)
}

func TestUpdateExistingTenantServerDoesNotRequireHeaderValues(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/plugins/grafana-assistant-app/resources/api/v1/integrations/mcp-1":
			writeJSON(w, map[string]any{
				"data": map[string]any{
					"id":          "mcp-1",
					"name":        "Tenant MCP",
					"description": "old description",
					"type":        "mcp",
					"enabled":     true,
					"scope":       "tenant",
					"applications": []string{
						"assistant",
					},
					"configuration": map[string]any{
						"url": "https://mcp.example.com/mcp",
					},
				},
			})
		case r.Method == http.MethodPut && r.URL.Path == "/api/plugins/grafana-assistant-app/resources/api/v1/integrations/mcp-1":
			assert.Equal(t, "tenant", r.Header.Get("X-Resource-Scope"))
			var payload map[string]any
			if !readJSONRequest(t, r, &payload) {
				return
			}
			assert.Equal(t, "new description", payload["description"])
			assert.Equal(t, "tenant", payload["scope"])

			writeJSON(w, map[string]any{
				"data": map[string]any{
					"integration": map[string]any{
						"id":      "mcp-1",
						"name":    "Tenant MCP",
						"type":    "mcp",
						"enabled": true,
						"scope":   "tenant",
					},
				},
			})
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))

	updated, err := client.Update(t.Context(), "mcp-1", mcpservers.ServerInput{Description: "new description"})
	require.NoError(t, err)
	assert.Equal(t, "updated", updated.Operation)
	assert.Equal(t, "tenant", updated.Server.Scope)
}

func TestUpdateUserToTenantRequiresAuthHeaderValue(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/plugins/grafana-assistant-app/resources/api/v1/integrations/mcp-1":
			writeJSON(w, map[string]any{
				"data": map[string]any{
					"id":      "mcp-1",
					"name":    "Remote MCP",
					"type":    "mcp",
					"enabled": true,
					"scope":   "user",
					"configuration": map[string]any{
						"url": "https://mcp.example.com/mcp",
					},
				},
			})
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))

	_, err := client.Update(t.Context(), "mcp-1", mcpservers.ServerInput{Scope: "tenant"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--scope tenant requires at least one authentication --header with a value")
}

func TestUpdateUserToTenantRejectsEmailHeaderOnly(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/plugins/grafana-assistant-app/resources/api/v1/integrations/mcp-1":
			writeJSON(w, map[string]any{
				"data": map[string]any{
					"id":      "mcp-1",
					"name":    "Remote MCP",
					"type":    "mcp",
					"enabled": true,
					"scope":   "user",
					"configuration": map[string]any{
						"url": "https://mcp.example.com/mcp",
					},
				},
			})
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))

	_, err := client.Update(t.Context(), "mcp-1", mcpservers.ServerInput{
		Scope:   "tenant",
		Headers: []mcpservers.Header{{Name: "X-CH-Auth-Email", Value: "user@example.com"}},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--scope tenant requires at least one authentication --header with a value")
}

func TestUpdateUserToTenantSucceedsWithAuthHeader(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/plugins/grafana-assistant-app/resources/api/v1/integrations/mcp-1":
			writeJSON(w, map[string]any{
				"data": map[string]any{
					"id":      "mcp-1",
					"name":    "Remote MCP",
					"type":    "mcp",
					"enabled": true,
					"scope":   "user",
					"configuration": map[string]any{
						"url": "https://mcp.example.com/mcp",
					},
				},
			})
		case r.Method == http.MethodPut && r.URL.Path == "/api/plugins/grafana-assistant-app/resources/api/v1/integrations/mcp-1":
			// The scope header carries the current (source) scope so the
			// backend can locate the resource; the body scope promotes it.
			assert.Equal(t, "user", r.Header.Get("X-Resource-Scope"))
			var payload map[string]any
			if !readJSONRequest(t, r, &payload) {
				return
			}
			assert.Equal(t, "tenant", payload["scope"])
			assert.NotEmpty(t, payload["custom_headers"])

			writeJSON(w, map[string]any{
				"data": map[string]any{
					"integration": map[string]any{
						"id":      "mcp-1",
						"name":    "Remote MCP",
						"type":    "mcp",
						"enabled": true,
						"scope":   "tenant",
					},
				},
			})
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))

	updated, err := client.Update(t.Context(), "mcp-1", mcpservers.ServerInput{
		Scope:   "tenant",
		Headers: []mcpservers.Header{{Name: "Authorization", Value: "Bearer token"}},
	})
	require.NoError(t, err)
	assert.Equal(t, "updated", updated.Operation)
	assert.Equal(t, "tenant", updated.Server.Scope)
}

func TestDeleteResolvesNameBeforeDelete(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/plugins/grafana-assistant-app/resources/api/v1/integrations/Remote MCP":
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"message":"invalid integration ID"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/plugins/grafana-assistant-app/resources/api/v1/integrations":
			writeJSON(w, map[string]any{
				"data": map[string]any{
					"integrations": []map[string]any{
						{"id": "mcp-1", "name": "Remote MCP", "type": "mcp", "enabled": true},
					},
				},
			})
		case r.Method == http.MethodDelete && r.URL.Path == "/api/plugins/grafana-assistant-app/resources/api/v1/integrations/mcp-1":
			assert.Equal(t, "user", r.Header.Get("X-Resource-Scope"))
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))

	deleted, err := client.Delete(t.Context(), "Remote MCP")
	require.NoError(t, err)
	assert.Equal(t, "deleted", deleted.Operation)
	assert.Equal(t, "mcp-1", deleted.Server.ID)
}

func TestValidateReturnsOAuthRequired(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/plugins/grafana-assistant-app/resources/api/v1/integrations/mcp-1":
			writeJSON(w, map[string]any{
				"data": map[string]any{
					"id":      "mcp-1",
					"name":    "Remote MCP",
					"type":    "mcp",
					"enabled": true,
					"scope":   "user",
				},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/api/plugins/grafana-assistant-app/resources/api/v1/integrations/mcp-1/validate":
			writeJSON(w, map[string]any{
				"data": map[string]any{
					"result": map[string]any{
						"status":  "oauth_required",
						"message": "OAuth authentication required",
					},
				},
			})
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))

	result, err := client.Validate(t.Context(), "mcp-1")
	require.NoError(t, err)
	assert.Equal(t, mcpservers.ValidationStatusOAuthRequired, result.Status)
	assert.Equal(t, "OAuth authentication required", result.Message)
}

func TestInitiateOAuthPostsIntegrationIDAndScope(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/plugins/grafana-assistant-app/resources/api/v1/integrations/mcp-1":
			writeJSON(w, map[string]any{
				"data": map[string]any{
					"id":      "mcp-1",
					"name":    "Remote MCP",
					"type":    "mcp",
					"enabled": true,
					"scope":   "user",
				},
			})
		case r.Method == http.MethodPost && r.URL.Path == "/api/plugins/grafana-assistant-app/resources/api/v1/integrations/oauth/initiate":
			var payload map[string]any
			if !readJSONRequest(t, r, &payload) {
				return
			}
			assert.Equal(t, "mcp-1", payload["integration_id"])
			assert.Equal(t, "user", payload["scope"])

			writeJSON(w, map[string]any{
				"data": map[string]any{
					"auth_url": "https://github.com/login/oauth/authorize",
					"state":    "state-1",
				},
			})
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))

	result, err := client.InitiateOAuth(t.Context(), "mcp-1")
	require.NoError(t, err)
	assert.Equal(t, "https://github.com/login/oauth/authorize", result.AuthURL)
	assert.Equal(t, "state-1", result.State)
}

func TestParseHeaderRejectsInvalidValue(t *testing.T) {
	_, err := mcpservers.ParseHeader("Authorization")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--header")

	_, err = mcpservers.ParseHeader("=value")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--header")
}

func TestFindMatchesNameURLAndScope(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/api/plugins/grafana-assistant-app/resources/api/v1/integrations", r.URL.Path)
		writeJSON(w, map[string]any{
			"data": map[string]any{
				"integrations": []map[string]any{
					{"id": "mcp-tenant", "name": "Remote MCP", "type": "mcp", "enabled": true, "scope": "tenant",
						"configuration": map[string]any{"url": "https://mcp.example.com/mcp"}},
				},
			},
		})
	}))

	_, err := client.Find(t.Context(), mcpservers.ServerInput{
		Name: "Remote MCP", URL: "https://mcp.example.com/mcp", Scope: "user",
	})
	require.ErrorIs(t, err, mcpservers.ErrNotFound, "same name in a different scope must not match")

	_, err = client.Find(t.Context(), mcpservers.ServerInput{
		Name: "Remote MCP", URL: "https://other.example.com/mcp", Scope: "tenant",
	})
	require.ErrorIs(t, err, mcpservers.ErrNotFound, "same name at a different URL must not match")

	server, err := client.Find(t.Context(), mcpservers.ServerInput{
		Name: "Remote MCP", URL: "https://mcp.example.com/mcp", Scope: "tenant",
	})
	require.NoError(t, err)
	assert.Equal(t, "mcp-tenant", server.ID)
}

func readJSONRequest(t *testing.T, r *http.Request, dst any) bool {
	t.Helper()

	body, err := io.ReadAll(r.Body)
	if err != nil {
		t.Errorf("failed to read request body: %v", err)
		return false
	}
	if err := json.Unmarshal(body, dst); err != nil {
		t.Errorf("failed to decode request body: %v", err)
		return false
	}
	return true
}
