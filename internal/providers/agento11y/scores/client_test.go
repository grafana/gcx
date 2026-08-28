package scores_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/providers/agento11y/agento11yhttp"
	"github.com/grafana/gcx/internal/providers/agento11y/scores"
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
	base, err := agento11yhttp.NewClient(cfg)
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

	items, err := client.ListByGeneration(context.Background(), "gen-1", 100)
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

func TestClient_ListByRule(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Contains(t, r.URL.Path, "/eval/rules/rule-1/scores")
		assert.Equal(t, "eval-1", r.URL.Query().Get("evaluator_id"))
		assert.Equal(t, "false", r.URL.Query().Get("passed"))
		assert.Equal(t, "2026-04-01T00:00:00Z", r.URL.Query().Get("from"))
		assert.Equal(t, "2026-04-02T00:00:00Z", r.URL.Query().Get("to"))
		assert.Equal(t, []string{"agent-a", "agent-b"}, r.URL.Query()["agent_name"])
		assert.Equal(t, []string{"gpt-4"}, r.URL.Query()["model"])
		assert.Equal(t, []string{"openai"}, r.URL.Query()["provider"])
		assert.Equal(t, []string{"fail"}, r.URL.Query()["score_value"])
		assert.Equal(t, "0.1", r.URL.Query().Get("min_value"))
		assert.Equal(t, "0.9", r.URL.Query().Get("max_value"))
		assert.Equal(t, "created_at", r.URL.Query().Get("sort_by"))
		assert.Equal(t, "desc", r.URL.Query().Get("sort_dir"))
		assert.Equal(t, "50", r.URL.Query().Get("limit"))

		w.Header().Set("Content-Type", "application/json")
		writeJSON(w, map[string]any{
			"items": []scores.Score{
				{
					ScoreID:           "s-1",
					GenerationID:      "gen-1",
					ConversationID:    "conv-1",
					ConversationTitle: "Help with billing",
					EvaluatorID:       "eval-1",
					EvaluatorVersion:  "1",
					RuleID:            "rule-1",
					ScoreKey:          "relevance",
					ScoreType:         "number",
					Value:             scores.ScoreValue{Number: new(0.2)},
					Passed:            new(false),
					Explanation:       "Off topic",
					AgentName:         "agent-a",
					GenModel:          "gpt-4",
					GenProvider:       "openai",
					CreatedAt:         now,
				},
			},
		})
	}))

	passed := false
	minV, maxV := 0.1, 0.9
	items, hasMore, err := client.ListByRule(context.Background(), "rule-1", scores.ListScoresOptions{
		Limit:       50,
		EvaluatorID: "eval-1",
		Passed:      &passed,
		From:        "2026-04-01T00:00:00Z",
		To:          "2026-04-02T00:00:00Z",
		AgentNames:  []string{"agent-a", "agent-b"},
		Models:      []string{"gpt-4"},
		Providers:   []string{"openai"},
		ScoreValues: []string{"fail"},
		MinValue:    &minV,
		MaxValue:    &maxV,
		SortBy:      "created_at",
		SortDir:     "desc",
	})
	require.NoError(t, err)
	require.Len(t, items, 1)
	assert.False(t, hasMore)
	assert.Equal(t, "relevance", items[0].ScoreKey)
	assert.Equal(t, "Help with billing", items[0].ConversationTitle)
	assert.Equal(t, "agent-a", items[0].AgentName)
	assert.Equal(t, "gpt-4", items[0].GenModel)
	assert.Equal(t, "openai", items[0].GenProvider)
	assert.False(t, *items[0].Passed)
}

func TestClient_ListByRule_Empty(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Contains(t, r.URL.Path, "/eval/rules/rule-1/scores")
		assert.Equal(t, "100", r.URL.Query().Get("limit"))
		assert.Equal(t, "created_at", r.URL.Query().Get("sort_by"))
		assert.Equal(t, "desc", r.URL.Query().Get("sort_dir"))
		w.Header().Set("Content-Type", "application/json")
		writeJSON(w, map[string]any{"items": []any{}})
	}))

	// The client trusts validated input: the CLI always supplies the sort
	// defaults, so pass them here rather than relying on client-side defaulting.
	items, hasMore, err := client.ListByRule(context.Background(), "rule-1", scores.ListScoresOptions{
		Limit: 100, SortBy: "created_at", SortDir: "desc",
	})
	require.NoError(t, err)
	assert.Empty(t, items)
	assert.False(t, hasMore)
}

func TestClient_ListByRule_ZeroLimitFetchesAllPages(t *testing.T) {
	pages := 0
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "500", r.URL.Query().Get("limit"))
		pages++
		w.Header().Set("Content-Type", "application/json")
		if pages == 1 {
			writeJSON(w, map[string]any{
				"items":       []scores.Score{{ScoreID: "s-1", ScoreKey: "a"}},
				"next_cursor": "page-2",
			})
			return
		}
		writeJSON(w, map[string]any{
			"items": []scores.Score{{ScoreID: "s-2", ScoreKey: "b"}},
		})
	}))

	items, hasMore, err := client.ListByRule(context.Background(), "rule-1", scores.ListScoresOptions{Limit: 0})
	require.NoError(t, err)
	require.Len(t, items, 2)
	assert.False(t, hasMore)
	assert.Equal(t, 2, pages)
}

func TestClient_ListByRule_HasMoreWhenTruncated(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "2", r.URL.Query().Get("limit"))
		w.Header().Set("Content-Type", "application/json")
		writeJSON(w, map[string]any{
			"items": []scores.Score{
				{ScoreID: "s-1", ScoreKey: "a"},
				{ScoreID: "s-2", ScoreKey: "b"},
				{ScoreID: "s-3", ScoreKey: "c"},
			},
			"next_cursor": "page-2",
		})
	}))

	items, hasMore, err := client.ListByRule(context.Background(), "rule-1", scores.ListScoresOptions{Limit: 2})
	require.NoError(t, err)
	require.Len(t, items, 2)
	assert.True(t, hasMore)
}

func TestClient_ListByRule_CompleteAtLimit(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "2", r.URL.Query().Get("limit"))
		w.Header().Set("Content-Type", "application/json")
		writeJSON(w, map[string]any{
			"items": []scores.Score{
				{ScoreID: "s-1", ScoreKey: "a"},
				{ScoreID: "s-2", ScoreKey: "b"},
			},
		})
	}))

	items, hasMore, err := client.ListByRule(context.Background(), "rule-1", scores.ListScoresOptions{Limit: 2})
	require.NoError(t, err)
	require.Len(t, items, 2)
	assert.False(t, hasMore, "exactly limit items with no next page is complete")
}

func TestClient_ListByRule_ZeroLimitBoundedByHardCap(t *testing.T) {
	hardCap := scores.ScoreHardCap()
	// The cap bounds total ROWS: a server that keeps returning full non-empty
	// pages plus a cursor would otherwise page until the cursor runs out; with
	// --limit 0 the fetch stops once the accumulated rows reach the hard cap.
	// (It is not a request-count guard — an empty page with a cursor would not
	// grow the row count; that pre-existing ListAll behavior is out of scope.)
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "500", r.URL.Query().Get("limit"))
		page := make([]scores.Score, 500)
		for i := range page {
			page[i] = scores.Score{ScoreID: "s", ScoreKey: "a"}
		}
		w.Header().Set("Content-Type", "application/json")
		writeJSON(w, map[string]any{"items": page, "next_cursor": "more"})
	}))

	items, hasMore, err := client.ListByRule(context.Background(), "rule-1", scores.ListScoresOptions{
		Limit: 0, SortBy: "created_at", SortDir: "desc",
	})
	require.NoError(t, err)
	assert.True(t, hasMore)
	assert.Len(t, items, hardCap, "fetch is bounded by the hard cap")
}
