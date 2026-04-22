package scores_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/providers/aio11y/aio11yhttp"
	"github.com/grafana/gcx/internal/providers/aio11y/scores"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"
)

func writeJSON(w http.ResponseWriter, v any) {
	if err := json.NewEncoder(w).Encode(v); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func newTestClient(t *testing.T, handler http.Handler) *scores.Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	cfg := config.NamespacedRESTConfig{
		Config:    rest.Config{Host: srv.URL},
		Namespace: "default",
	}
	base, err := aio11yhttp.NewClient(cfg)
	require.NoError(t, err)
	return scores.NewClient(base)
}

func TestClient_ListByGeneration(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Contains(t, r.URL.Path, "/query/generations/gen-1/scores")

		w.Header().Set("Content-Type", "application/json")
		writeJSON(w, map[string]any{
			"items": []scores.Score{
				{
					ScoreID:          "s-1",
					GenerationID:     "gen-1",
					EvaluatorID:      "eval-1",
					EvaluatorVersion: "1",
					ScoreKey:         "relevance",
					ScoreType:        "number",
					Value:            scores.ScoreValue{Number: new(0.95)},
					Passed:           new(true),
					CreatedAt:        now,
				},
				{
					ScoreID:          "s-2",
					GenerationID:     "gen-1",
					EvaluatorID:      "eval-2",
					EvaluatorVersion: "1",
					ScoreKey:         "harmful",
					ScoreType:        "bool",
					Value:            scores.ScoreValue{Bool: new(false)},
					Passed:           new(true),
					CreatedAt:        now,
				},
			},
		})
	}))

	items, err := client.ListByGeneration(context.Background(), "gen-1", 0)
	require.NoError(t, err)
	require.Len(t, items, 2)
	assert.Equal(t, "relevance", items[0].ScoreKey)
	assert.Equal(t, "0.95", items[0].Value.Display())
	assert.True(t, *items[0].Passed)
	assert.Equal(t, "harmful", items[1].ScoreKey)
	assert.Equal(t, "false", items[1].Value.Display())
}

func TestClient_ListByGeneration_Empty(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		writeJSON(w, map[string]any{"items": []any{}})
	}))

	items, err := client.ListByGeneration(context.Background(), "gen-1", 0)
	require.NoError(t, err)
	assert.Empty(t, items)
}
