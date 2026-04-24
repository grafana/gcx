package judge_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/providers/aio11y/aio11yhttp"
	"github.com/grafana/gcx/internal/providers/aio11y/eval"
	"github.com/grafana/gcx/internal/providers/aio11y/eval/judge"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"
)

func writeJSON(w http.ResponseWriter, v any) {
	if err := json.NewEncoder(w).Encode(v); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func newTestClient(t *testing.T, handler http.Handler) *judge.Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	cfg := config.NamespacedRESTConfig{
		Config:    rest.Config{Host: srv.URL},
		Namespace: "default",
	}
	base, err := aio11yhttp.NewClient(cfg)
	require.NoError(t, err)
	return judge.NewClient(base)
}

func TestClient_ListProviders(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Contains(t, r.URL.Path, "/eval/judge/providers")

		w.Header().Set("Content-Type", "application/json")
		writeJSON(w, map[string]any{
			"providers": []eval.JudgeProvider{
				{ID: "openai", Name: "OpenAI", Type: "openai"},
				{ID: "anthropic", Name: "Anthropic", Type: "anthropic"},
			},
		})
	}))

	providers, err := client.ListProviders(context.Background())
	require.NoError(t, err)
	require.Len(t, providers, 2)
	assert.Equal(t, "openai", providers[0].ID)
	assert.Equal(t, "anthropic", providers[1].ID)
}

func TestClient_ListModels(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Contains(t, r.URL.Path, "/eval/judge/models")
		assert.Equal(t, "openai", r.URL.Query().Get("provider"))

		w.Header().Set("Content-Type", "application/json")
		writeJSON(w, map[string]any{
			"models": []eval.JudgeModel{
				{ID: "gpt-4o", Name: "GPT-4o", Provider: "openai", ContextWindow: 128000},
				{ID: "gpt-4o-mini", Name: "GPT-4o Mini", Provider: "openai", ContextWindow: 128000},
			},
		})
	}))

	models, err := client.ListModels(context.Background(), "openai")
	require.NoError(t, err)
	require.Len(t, models, 2)
	assert.Equal(t, "gpt-4o", models[0].ID)
	assert.Equal(t, 128000, models[0].ContextWindow)
}

func TestClient_ListModels_NoProvider(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Empty(t, r.URL.Query().Get("provider"))

		w.Header().Set("Content-Type", "application/json")
		writeJSON(w, map[string]any{
			"models": []eval.JudgeModel{
				{ID: "gpt-4o", Name: "GPT-4o", Provider: "openai", ContextWindow: 128000},
			},
		})
	}))

	models, err := client.ListModels(context.Background(), "")
	require.NoError(t, err)
	require.Len(t, models, 1)
}
