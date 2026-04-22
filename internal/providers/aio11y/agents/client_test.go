package agents_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/providers/aio11y/agents"
	"github.com/grafana/gcx/internal/providers/aio11y/aio11yhttp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"
)

func writeJSON(w http.ResponseWriter, v any) {
	if err := json.NewEncoder(w).Encode(v); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func newTestClient(t *testing.T, handler http.Handler) *agents.Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	cfg := config.NamespacedRESTConfig{
		Config:    rest.Config{Host: srv.URL},
		Namespace: "default",
	}
	base, err := aio11yhttp.NewClient(cfg)
	require.NoError(t, err)
	return agents.NewClient(base)
}

func TestClient_List(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Contains(t, r.URL.Path, "/query/agents")

		w.Header().Set("Content-Type", "application/json")
		writeJSON(w, map[string]any{
			"items": []agents.Agent{
				{
					AgentName:       "test-agent",
					GenerationCount: 100,
					VersionCount:    3,
					ToolCount:       5,
					LatestSeenAt:    now,
				},
			},
		})
	}))

	items, err := client.List(context.Background(), 0)
	require.NoError(t, err)
	require.Len(t, items, 1)
	assert.Equal(t, "test-agent", items[0].AgentName)
	assert.Equal(t, int64(100), items[0].GenerationCount)
}

func TestClient_Lookup(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "my-agent", r.URL.Query().Get("name"))

		w.Header().Set("Content-Type", "application/json")
		writeJSON(w, map[string]any{
			"agent_name":        "my-agent",
			"effective_version": "sha256:abc123",
			"tool_count":        2,
		})
	}))

	detail, err := client.Lookup(context.Background(), "my-agent", "")
	require.NoError(t, err)
	assert.Equal(t, "my-agent", (*detail)["agent_name"])
}

func TestClient_Versions(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "my-agent", r.URL.Query().Get("name"))

		w.Header().Set("Content-Type", "application/json")
		writeJSON(w, map[string]any{
			"items": []agents.AgentVersion{
				{EffectiveVersion: "sha256:abc", GenerationCount: 50, ToolCount: 2},
				{EffectiveVersion: "sha256:def", GenerationCount: 30, ToolCount: 1},
			},
		})
	}))

	versions, err := client.Versions(context.Background(), "my-agent")
	require.NoError(t, err)
	assert.Len(t, versions, 2)
	assert.Equal(t, "sha256:abc", versions[0].EffectiveVersion)
}
