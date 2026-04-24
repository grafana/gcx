package templates_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/providers/aio11y/aio11yhttp"
	"github.com/grafana/gcx/internal/providers/aio11y/eval"
	"github.com/grafana/gcx/internal/providers/aio11y/eval/templates"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"
)

func writeJSON(w http.ResponseWriter, v any) {
	if err := json.NewEncoder(w).Encode(v); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func newTestClient(t *testing.T, handler http.Handler) *templates.Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	cfg := config.NamespacedRESTConfig{
		Config:    rest.Config{Host: srv.URL},
		Namespace: "default",
	}
	base, err := aio11yhttp.NewClient(cfg)
	require.NoError(t, err)
	return templates.NewClient(base)
}

func TestClient_List(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Contains(t, r.URL.Path, "/eval/templates")

		writeJSON(w, map[string]any{
			"items": []eval.TemplateDefinition{
				{TemplateID: "tpl-1", Scope: "global", Kind: "llm_judge"},
				{TemplateID: "tpl-2", Scope: "tenant", Kind: "regex"},
			},
		})
	}))

	items, err := client.List(context.Background(), "")
	require.NoError(t, err)
	require.Len(t, items, 2)
	assert.Equal(t, "tpl-1", items[0].TemplateID)
	assert.Equal(t, "global", items[0].Scope)
}

func TestClient_List_WithScope(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "global", r.URL.Query().Get("scope"))

		writeJSON(w, map[string]any{
			"items": []eval.TemplateDefinition{
				{TemplateID: "tpl-1", Scope: "global", Kind: "llm_judge"},
			},
		})
	}))

	items, err := client.List(context.Background(), "global")
	require.NoError(t, err)
	require.Len(t, items, 1)
}

func TestClient_Get(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Contains(t, r.URL.Path, "/eval/templates/tpl-1")

		writeJSON(w, map[string]any{
			"template_id": "tpl-1",
			"kind":        "llm_judge",
			"config":      map[string]any{"model": "gpt-4"},
		})
	}))

	detail, err := client.Get(context.Background(), "tpl-1")
	require.NoError(t, err)
	assert.Equal(t, "tpl-1", (*detail)["template_id"])
	assert.Equal(t, "llm_judge", (*detail)["kind"])
}

func TestClient_List_WithLimit(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, map[string]any{
			"items": []eval.TemplateDefinition{
				{TemplateID: "tpl-1", Scope: "global", Kind: "llm_judge"},
				{TemplateID: "tpl-2", Scope: "tenant", Kind: "regex"},
				{TemplateID: "tpl-3", Scope: "global", Kind: "llm_judge"},
			},
		})
	}))

	items, err := client.List(context.Background(), "", 2)
	require.NoError(t, err)
	require.Len(t, items, 2)
	assert.Equal(t, "tpl-1", items[0].TemplateID)
	assert.Equal(t, "tpl-2", items[1].TemplateID)
}

func TestClient_ListVersions(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Contains(t, r.URL.Path, "/eval/templates/tpl-1/versions")

		writeJSON(w, map[string]any{
			"items": []eval.TemplateVersion{
				{TemplateID: "tpl-1", Version: "2026-04-01", Changelog: "Initial release"},
			},
		})
	}))

	versions, err := client.ListVersions(context.Background(), "tpl-1")
	require.NoError(t, err)
	require.Len(t, versions, 1)
	assert.Equal(t, "2026-04-01", versions[0].Version)
}
