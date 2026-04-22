package evaluators_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/providers/aio11y/eval"
	"github.com/grafana/gcx/internal/providers/aio11y/eval/evaluators"
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

func newTestClient(t *testing.T, handler http.Handler) *evaluators.Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	cfg := config.NamespacedRESTConfig{
		Config:    rest.Config{Host: srv.URL},
		Namespace: "default",
	}
	base, err := aio11yhttp.NewClient(cfg)
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

func TestClient_Create(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Contains(t, r.URL.Path, "/eval/evaluators")

		var def eval.EvaluatorDefinition
		assert.NoError(t, json.NewDecoder(r.Body).Decode(&def))
		assert.Equal(t, "new-eval", def.EvaluatorID)

		w.WriteHeader(http.StatusCreated)
		writeJSON(w, eval.EvaluatorDefinition{
			EvaluatorID: "new-eval",
			Version:     "1.0",
			Kind:        "llm_judge",
		})
	}))

	created, err := client.Create(context.Background(), &eval.EvaluatorDefinition{
		EvaluatorID: "new-eval",
		Kind:        "llm_judge",
	})
	require.NoError(t, err)
	assert.Equal(t, "new-eval", created.EvaluatorID)
	assert.Equal(t, "1.0", created.Version)
}

func TestClient_Delete(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodDelete, r.Method)
		assert.Contains(t, r.URL.Path, "/eval/evaluators/eval-1")
		w.WriteHeader(http.StatusNoContent)
	}))

	err := client.Delete(context.Background(), "eval-1")
	require.NoError(t, err)
}

func TestClient_RunTest(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Contains(t, r.URL.Path, "/eval:test")

		var req eval.EvalTestRequest
		assert.NoError(t, json.NewDecoder(r.Body).Decode(&req))
		assert.Equal(t, "llm_judge", req.Kind)
		assert.Equal(t, "gen-1", req.GenerationID)

		passed := true
		writeJSON(w, eval.EvalTestResponse{
			GenerationID:    "gen-1",
			ExecutionTimeMs: 150,
			Scores: []eval.EvalTestScore{
				{Key: "quality", Type: "number", Value: 0.9, Passed: &passed},
			},
		})
	}))

	resp, err := client.RunTest(context.Background(), &eval.EvalTestRequest{
		Kind:         "llm_judge",
		GenerationID: "gen-1",
	})
	require.NoError(t, err)
	assert.Equal(t, "gen-1", resp.GenerationID)
	assert.Equal(t, int64(150), resp.ExecutionTimeMs)
	require.Len(t, resp.Scores, 1)
	assert.Equal(t, "quality", resp.Scores[0].Key)
}
