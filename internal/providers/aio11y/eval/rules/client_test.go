package rules_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/providers/aio11y/aio11yhttp"
	"github.com/grafana/gcx/internal/providers/aio11y/eval"
	"github.com/grafana/gcx/internal/providers/aio11y/eval/rules"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"
)

func writeJSON(w http.ResponseWriter, v any) {
	if err := json.NewEncoder(w).Encode(v); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func newTestClient(t *testing.T, handler http.Handler) *rules.Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	cfg := config.NamespacedRESTConfig{
		Config:    rest.Config{Host: srv.URL},
		Namespace: "default",
	}
	base, err := aio11yhttp.NewClient(cfg)
	require.NoError(t, err)
	return rules.NewClient(base)
}

func TestClient_List(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Contains(t, r.URL.Path, "/eval/rules")

		w.Header().Set("Content-Type", "application/json")
		writeJSON(w, map[string]any{
			"items": []eval.RuleDefinition{
				{RuleID: "rule-1", Enabled: true, Selector: "user_visible_turn", SampleRate: 1.0, EvaluatorIDs: []string{"eval-1"}},
				{RuleID: "rule-2", Enabled: false, Selector: "all_assistant_generations", SampleRate: 0.5, EvaluatorIDs: []string{"eval-2"}},
			},
		})
	}))

	items, err := client.List(context.Background())
	require.NoError(t, err)
	require.Len(t, items, 2)
	assert.Equal(t, "rule-1", items[0].RuleID)
	assert.True(t, items[0].Enabled)
	assert.Equal(t, []string{"eval-1"}, items[0].EvaluatorIDs)
	assert.Equal(t, "rule-2", items[1].RuleID)
	assert.False(t, items[1].Enabled)
}

func TestClient_Get(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Contains(t, r.URL.Path, "/eval/rules/rule-1")

		w.Header().Set("Content-Type", "application/json")
		writeJSON(w, eval.RuleDefinition{
			RuleID:       "rule-1",
			Enabled:      true,
			Selector:     "user_visible_turn",
			SampleRate:   1.0,
			EvaluatorIDs: []string{"eval-1", "eval-2"},
		})
	}))

	r, err := client.Get(context.Background(), "rule-1")
	require.NoError(t, err)
	assert.Equal(t, "rule-1", r.RuleID)
	assert.Equal(t, "user_visible_turn", r.Selector)
	assert.Len(t, r.EvaluatorIDs, 2)
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
		assert.Contains(t, r.URL.Path, "/eval/rules")

		var def eval.RuleDefinition
		assert.NoError(t, json.NewDecoder(r.Body).Decode(&def))
		assert.Equal(t, "new-rule", def.RuleID)

		w.WriteHeader(http.StatusCreated)
		writeJSON(w, eval.RuleDefinition{
			RuleID:       "new-rule",
			Enabled:      true,
			Selector:     "user_visible_turn",
			SampleRate:   1.0,
			EvaluatorIDs: []string{"eval-1"},
		})
	}))

	created, err := client.Create(context.Background(), &eval.RuleDefinition{
		RuleID:       "new-rule",
		Enabled:      true,
		Selector:     "user_visible_turn",
		SampleRate:   1.0,
		EvaluatorIDs: []string{"eval-1"},
	})
	require.NoError(t, err)
	assert.Equal(t, "new-rule", created.RuleID)
	assert.True(t, created.Enabled)
}

func TestClient_Update(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPatch, r.Method)
		assert.Contains(t, r.URL.Path, "/eval/rules/rule-1")

		var def eval.RuleDefinition
		assert.NoError(t, json.NewDecoder(r.Body).Decode(&def))
		assert.InDelta(t, 0.5, def.SampleRate, 0.001)

		writeJSON(w, eval.RuleDefinition{
			RuleID:     "rule-1",
			SampleRate: 0.5,
		})
	}))

	updated, err := client.Update(context.Background(), "rule-1", &eval.RuleDefinition{
		RuleID:     "rule-1",
		SampleRate: 0.5,
	})
	require.NoError(t, err)
	assert.Equal(t, "rule-1", updated.RuleID)
	assert.InDelta(t, 0.5, updated.SampleRate, 0.001)
}

func TestClient_Delete(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodDelete, r.Method)
		assert.Contains(t, r.URL.Path, "/eval/rules/rule-1")
		w.WriteHeader(http.StatusNoContent)
	}))

	err := client.Delete(context.Background(), "rule-1")
	require.NoError(t, err)
}
