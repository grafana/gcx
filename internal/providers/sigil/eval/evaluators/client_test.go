package evaluators_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/providers/sigil/eval"
	"github.com/grafana/gcx/internal/providers/sigil/eval/evaluators"
	"github.com/grafana/gcx/internal/providers/sigil/sigilhttp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"
)

func writeJSON(w http.ResponseWriter, v any) {
	if err := json.NewEncoder(w).Encode(v); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func newTestClient(t *testing.T, handler http.Handler) *evaluators.Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	cfg := config.NamespacedRESTConfig{
		Config:    rest.Config{Host: srv.URL},
		Namespace: "default",
	}
	base, err := sigilhttp.NewClient(cfg)
	require.NoError(t, err)
	return evaluators.NewClient(base)
}

func TestClient_List(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Contains(t, r.URL.Path, "/eval/evaluators")

		w.Header().Set("Content-Type", "application/json")
		writeJSON(w, map[string]any{
			"items": []eval.EvaluatorDefinition{
				{EvaluatorID: "eval-1", Version: "1.0", Kind: "llm_judge", Description: "Quality check"},
				{EvaluatorID: "eval-2", Version: "2.0", Kind: "regex", Description: "Pattern match"},
			},
		})
	}))

	items, err := client.List(context.Background())
	require.NoError(t, err)
	require.Len(t, items, 2)
	assert.Equal(t, "eval-1", items[0].EvaluatorID)
	assert.Equal(t, "llm_judge", items[0].Kind)
	assert.Equal(t, "eval-2", items[1].EvaluatorID)
}

func TestClient_Get(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Contains(t, r.URL.Path, "/eval/evaluators/eval-1")

		w.Header().Set("Content-Type", "application/json")
		writeJSON(w, eval.EvaluatorDefinition{
			EvaluatorID: "eval-1",
			Version:     "1.0",
			Kind:        "llm_judge",
			Description: "Quality check",
		})
	}))

	e, err := client.Get(context.Background(), "eval-1")
	require.NoError(t, err)
	assert.Equal(t, "eval-1", e.EvaluatorID)
	assert.Equal(t, "1.0", e.Version)
}

func TestClient_Get_NotFound(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	}))

	_, err := client.Get(context.Background(), "nonexistent")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "404")
}
