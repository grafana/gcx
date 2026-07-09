package assistant_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/grafana/gcx/internal/assistant/assistanthttp"
	assistantmcp "github.com/grafana/gcx/internal/assistant/mcpservers"
	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/providers/assistant"
	"github.com/grafana/gcx/internal/resources/adapter"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
)

func newTestClient(t *testing.T, handler http.Handler) *assistantmcp.Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	cfg := config.NamespacedRESTConfig{
		Config:    rest.Config{Host: server.URL},
		Namespace: "default",
	}
	base, err := assistanthttp.NewClient(cfg)
	require.NoError(t, err)
	return assistantmcp.NewClient(base)
}

func writeJSON(t *testing.T, w http.ResponseWriter, v any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	require.NoError(t, json.NewEncoder(w).Encode(v))
}

func integration(id, name, scope, url string) map[string]any {
	return map[string]any{
		"id": id, "name": name, "type": "mcp", "enabled": true, "scope": scope,
		"configuration": map[string]any{"url": url, "timeout": "30s"},
	}
}

// TestList_ReturnsBothScopesAsEnvelopes covers AC-004: a stack with a
// user-scoped and a tenant-scoped server must both come back as
// assistant.ext.grafana.app/v1alpha1 MCPServer envelopes from a single List
// call (the underlying integrations list is not scope-filtered).
func TestList_ReturnsBothScopesAsEnvelopes(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, map[string]any{
			"data": map[string]any{
				"integrations": []map[string]any{
					integration("mcp-user", "My Server", "user", "https://mcp.example.com/user"),
					integration("mcp-tenant", "GitHub", "tenant", "https://api.githubcopilot.com/mcp/"),
				},
			},
		})
	}))

	crud := assistant.NewTypedCRUDForClient(client, "default")
	list, err := crud.AsAdapter().List(t.Context(), metav1.ListOptions{})
	require.NoError(t, err)
	require.Len(t, list.Items, 2)

	names := make([]string, 0, 2)
	for _, item := range list.Items {
		assert.Equal(t, assistant.MCPServerAPIVersion, item.GetAPIVersion())
		assert.Equal(t, assistant.MCPServerKind, item.GetKind())
		names = append(names, item.GetName())
	}
	assert.ElementsMatch(t, []string{"user-my-server", "tenant-github"}, names)
}

// TestList_ExhaustsMultiplePagesIncludingMCPEmptyPage covers AC-009/FR-015:
// the adapter's List must go through the client's exhausting ListAll (not
// the single-page List), so a large stack is never truncated and an
// MCP-empty page does not stop paging.
func TestList_ExhaustsMultiplePagesIncludingMCPEmptyPage(t *testing.T) {
	const pageSize = 100

	page0 := make([]map[string]any, 0, pageSize)
	page0 = append(page0, integration("mcp-1", "Server One", "user", "https://mcp.example.com/one"))
	for i := 1; i < pageSize; i++ {
		page0 = append(page0, map[string]any{"id": fmt.Sprintf("other-%d", i), "name": "not mcp", "type": "not-mcp", "enabled": true})
	}

	page1 := make([]map[string]any, 0, pageSize) // MCP-empty page — must not stop paging here.
	for i := range pageSize {
		page1 = append(page1, map[string]any{"id": fmt.Sprintf("filler-%d", i), "name": "not mcp", "type": "not-mcp", "enabled": true})
	}

	page2 := []map[string]any{ // short page — last page.
		integration("mcp-2", "Server Two", "tenant", "https://mcp.example.com/two"),
	}

	pages := [][]map[string]any{page0, page1, page2}

	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
		pageIndex := offset / pageSize
		if !assert.Less(t, pageIndex, len(pages), "unexpected offset %d requested more pages than fixtures provide", offset) {
			return
		}
		writeJSON(t, w, map[string]any{
			"data": map[string]any{"integrations": pages[pageIndex]},
		})
	}))

	crud := assistant.NewTypedCRUDForClient(client, "default")
	list, err := crud.AsAdapter().List(t.Context(), metav1.ListOptions{})
	require.NoError(t, err)
	require.Len(t, list.Items, 2, "must not truncate at the MCP-empty page")

	names := make([]string, 0, 2)
	for _, item := range list.Items {
		names = append(names, item.GetName())
	}
	assert.ElementsMatch(t, []string{"user-server-one", "tenant-server-two"}, names)
}

// TestGet_ResolvesComposedNameAndAnnotatesID covers AC-005 (metadata.name +
// materialized spec) and FR-012 (server ID as a within-stack annotation,
// never used for identity). spec.config must also carry no "url" key, since
// url is already materialized as its own spec field.
func TestGet_ResolvesComposedNameAndAnnotatesID(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, map[string]any{
			"data": map[string]any{
				"integrations": []map[string]any{
					integration("srv-abc123", "GitHub", "tenant", "https://api.githubcopilot.com/mcp/"),
				},
			},
		})
	}))

	crud := assistant.NewTypedCRUDForClient(client, "default")
	obj, err := crud.AsAdapter().Get(t.Context(), "tenant-github", metav1.GetOptions{})
	require.NoError(t, err)

	assert.Equal(t, "tenant-github", obj.GetName())
	assert.Equal(t, "srv-abc123", obj.GetAnnotations()[assistant.MCPServerIDAnnotation])

	spec, ok := obj.Object["spec"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "GitHub", spec["name"])
	assert.Equal(t, "tenant", spec["scope"])
	assert.Equal(t, "https://api.githubcopilot.com/mcp/", spec["url"])
	assert.Equal(t, true, spec["enabled"])

	cfg, ok := spec["config"].(map[string]any)
	require.True(t, ok, "spec.config must be present (timeout key)")
	assert.NotContains(t, cfg, "url", "spec.config must not duplicate spec.url")
	assert.Equal(t, "30s", cfg["timeout"])
}

// TestGet_NotFoundWrapsAdapterErrNotFound ensures an unmatched composite
// name surfaces adapter.ErrNotFound so the push pipeline's upsert falls
// through to Create.
func TestGet_NotFoundWrapsAdapterErrNotFound(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, map[string]any{"data": map[string]any{"integrations": []map[string]any{}}})
	}))

	crud := assistant.NewTypedCRUDForClient(client, "default")
	_, err := crud.Get(t.Context(), "tenant-does-not-exist")
	require.Error(t, err)
	assert.ErrorIs(t, err, adapter.ErrNotFound)
}

// TestCreate_SendsMaterializedSpecAsServerInput covers the create path: the
// manifest's spec fields (scope/name/url/enabled/description/applications/
// config/headers) must reach the backend as the ServerInput payload.
func TestCreate_SendsMaterializedSpecAsServerInput(t *testing.T) {
	var gotBody map[string]any
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			if !assert.NoError(t, json.NewDecoder(r.Body).Decode(&gotBody)) {
				return
			}
			writeJSON(t, w, map[string]any{
				"data": map[string]any{
					"integration": integration("srv-new", "SharedTools", "tenant", "https://mcp.example.com/shared"),
				},
			})
		default:
			writeJSON(t, w, map[string]any{"data": map[string]any{"integrations": []map[string]any{}}})
		}
	}))

	crud := assistant.NewTypedCRUDForClient(client, "default")
	created, err := crud.CreateFn(t.Context(), &assistant.MCPServer{
		Name:    "SharedTools",
		Scope:   "tenant",
		URL:     "https://mcp.example.com/shared",
		Enabled: true,
		Headers: []assistant.MCPServerHeader{{Name: "Authorization", Value: "Bearer secret"}},
	})
	require.NoError(t, err)
	assert.Equal(t, "SharedTools", created.Name)
	assert.Equal(t, "tenant", created.Scope)

	assert.Equal(t, "tenant", gotBody["scope"])
	assert.Equal(t, "SharedTools", gotBody["name"])
	headers, ok := gotBody["custom_headers"].([]any)
	require.True(t, ok)
	require.Len(t, headers, 1)
}

// TestUpdate_ResolvesExistingServerByNaturalKeyAndPreservesHeader covers
// AC-007 (natural-key match, ID annotation ignored) and AC-011/FR-017 (an
// update that does not touch a header must not wipe it): the UpdateFn
// receives only the manifest's spec (no ID), resolves the target via
// (scope, name, url), and updates it -- while a name-only header keeps the
// server's stored value.
func TestUpdate_ResolvesExistingServerByNaturalKeyAndPreservesHeader(t *testing.T) {
	const collectionPath = "/api/plugins/grafana-assistant-app/resources/api/v1/integrations"

	var gotHeaders []map[string]any
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == collectionPath:
			// The adapter's own findServerByKey (ListAll) call.
			writeJSON(t, w, map[string]any{
				"data": map[string]any{
					"integrations": []map[string]any{
						{
							"id": "srv-existing", "name": "GitHub", "type": "mcp", "enabled": true, "scope": "tenant",
							"configuration":  map[string]any{"url": "https://api.githubcopilot.com/mcp/"},
							"custom_headers": []map[string]any{{"name": "Authorization", "value": "stored-secret"}},
						},
					},
				},
			})
		case r.Method == http.MethodGet && r.URL.Path == collectionPath+"/srv-existing":
			// The client's own internal Get(ctx, current.ID) inside Update.
			writeJSON(t, w, map[string]any{
				"data": map[string]any{
					"id": "srv-existing", "name": "GitHub", "type": "mcp", "enabled": true, "scope": "tenant",
					"configuration":  map[string]any{"url": "https://api.githubcopilot.com/mcp/"},
					"custom_headers": []map[string]any{{"name": "Authorization", "value": "stored-secret"}},
				},
			})
		case r.Method == http.MethodPut:
			var body map[string]any
			if !assert.NoError(t, json.NewDecoder(r.Body).Decode(&body)) {
				return
			}
			headers, _ := body["custom_headers"].([]any)
			for _, h := range headers {
				hm, ok := h.(map[string]any)
				if !assert.True(t, ok) {
					return
				}
				gotHeaders = append(gotHeaders, hm)
			}
			writeJSON(t, w, map[string]any{
				"data": map[string]any{
					"integration": integration("srv-existing", "GitHub", "tenant", "https://api.githubcopilot.com/mcp/"),
				},
			})
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))

	crud := assistant.NewTypedCRUDForClient(client, "default")
	updated, err := crud.UpdateFn(t.Context(), "tenant-github", &assistant.MCPServer{
		Name:    "GitHub",
		Scope:   "tenant",
		URL:     "https://api.githubcopilot.com/mcp/",
		Enabled: false,                                                // the field actually being changed
		Headers: []assistant.MCPServerHeader{{Name: "Authorization"}}, // name-only: preserve
	})
	require.NoError(t, err)
	assert.Equal(t, "GitHub", updated.Name)

	require.Len(t, gotHeaders, 1)
	assert.Equal(t, "Authorization", gotHeaders[0]["name"])
	assert.Empty(t, gotHeaders[0]["value"], "preserve intent must not send a value")
}

// TestDelete_ResolvesComposedNameToServerID covers the delete path: DeleteFn
// only receives the composite metadata.name, so it must resolve the target
// server's ID via ListAll + composite-name matching before deleting.
func TestDelete_ResolvesComposedNameToServerID(t *testing.T) {
	const collectionPath = "/api/plugins/grafana-assistant-app/resources/api/v1/integrations"

	var deletedPath string
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodDelete:
			deletedPath = r.URL.Path
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodGet && r.URL.Path == collectionPath:
			// The adapter's own findServerByResourceName (ListAll) call.
			writeJSON(t, w, map[string]any{
				"data": map[string]any{
					"integrations": []map[string]any{
						integration("srv-to-delete", "GitHub", "tenant", "https://api.githubcopilot.com/mcp/"),
					},
				},
			})
		case r.Method == http.MethodGet && r.URL.Path == collectionPath+"/srv-to-delete":
			// The client's own internal Get(ctx, ref) inside Delete.
			writeJSON(t, w, map[string]any{
				"data": integration("srv-to-delete", "GitHub", "tenant", "https://api.githubcopilot.com/mcp/"),
			})
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))

	crud := assistant.NewTypedCRUDForClient(client, "default")
	err := crud.DeleteFn(t.Context(), "tenant-github")
	require.NoError(t, err)
	assert.Contains(t, deletedPath, "srv-to-delete")
}

// TestMCPServerSchema_DoesNotLeakInternalServerIDField guards against the
// unexported serverID carrier field (added purely so MetadataFn can emit
// MCPServerIDAnnotation) ever leaking into the generated JSON Schema.
func TestMCPServerSchema_DoesNotLeakInternalServerIDField(t *testing.T) {
	schema := assistant.MCPServerSchema()
	var parsed map[string]any
	require.NoError(t, json.Unmarshal(schema, &parsed))

	props, ok := parsed["properties"].(map[string]any)
	require.True(t, ok)
	assert.NotContains(t, props, "serverID")
	assert.NotContains(t, props, "serverId")
}
