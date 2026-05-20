package guards_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/providers/aio11y/aio11yhttp"
	"github.com/grafana/gcx/internal/providers/aio11y/eval"
	"github.com/grafana/gcx/internal/providers/aio11y/eval/guards"
	"github.com/grafana/gcx/internal/resources/adapter"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"
)

func writeJSON(w http.ResponseWriter, v any) {
	if err := json.NewEncoder(w).Encode(v); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func newTestClient(t *testing.T, handler http.Handler) *guards.Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	cfg := config.NamespacedRESTConfig{
		Config:    rest.Config{Host: srv.URL},
		Namespace: "default",
	}
	base, err := aio11yhttp.NewClient(cfg)
	require.NoError(t, err)
	return guards.NewClient(base)
}

func TestClient_List(t *testing.T) {
	// Two-page list: first response returns a next_cursor, second response is empty.
	pageCalls := 0
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Contains(t, r.URL.Path, "/eval/hook-rules")

		w.Header().Set("Content-Type", "application/json")
		pageCalls++
		if pageCalls == 1 {
			writeJSON(w, map[string]any{
				"items": []eval.HookRuleDefinition{
					{RuleID: "guard-1", Enabled: true, Phase: "preflight", Selector: "all", ActionOnFail: "deny", ShortCircuit: true, EvaluatorIDs: []string{"eval-1"}},
				},
				"next_cursor": "next-cursor-value",
			})
			return
		}
		// Subsequent pages: no items, no cursor → list terminates.
		writeJSON(w, map[string]any{
			"items": []eval.HookRuleDefinition{
				{RuleID: "guard-2", Enabled: false, Phase: "postflight", Selector: "user_visible_turn", ActionOnFail: "warn"},
			},
		})
	}))

	items, err := client.List(context.Background())
	require.NoError(t, err)
	require.Len(t, items, 2)
	assert.Equal(t, "guard-1", items[0].RuleID)
	assert.True(t, items[0].Enabled)
	assert.Equal(t, "preflight", items[0].Phase)
	assert.Equal(t, "deny", items[0].ActionOnFail)
	assert.Equal(t, "guard-2", items[1].RuleID)
	assert.False(t, items[1].Enabled)
	assert.Equal(t, 2, pageCalls, "expected pagination across two pages")
}

func TestClient_Get(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Contains(t, r.URL.Path, "/eval/hook-rules/guard-1")

		w.Header().Set("Content-Type", "application/json")
		writeJSON(w, eval.HookRuleDefinition{
			RuleID:       "guard-1",
			Enabled:      true,
			Phase:        "preflight",
			Priority:     5,
			Selector:     "all",
			ActionOnFail: "deny",
			ShortCircuit: true,
			EvaluatorIDs: []string{"eval-1", "eval-2"},
		})
	}))

	r, err := client.Get(context.Background(), "guard-1")
	require.NoError(t, err)
	assert.Equal(t, "guard-1", r.RuleID)
	assert.Equal(t, "preflight", r.Phase)
	assert.Equal(t, 5, r.Priority)
	assert.Len(t, r.EvaluatorIDs, 2)
}

func TestClient_Get_NotFound(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	}))

	_, err := client.Get(context.Background(), "nonexistent")
	require.Error(t, err)
	require.ErrorIs(t, err, guards.ErrNotFound)
	require.ErrorIs(t, err, adapter.ErrNotFound)
}

func TestClient_Create(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Contains(t, r.URL.Path, "/eval/hook-rules")

		var def eval.HookRuleDefinition
		assert.NoError(t, json.NewDecoder(r.Body).Decode(&def))
		assert.Equal(t, "new-guard", def.RuleID)

		w.WriteHeader(http.StatusCreated)
		writeJSON(w, eval.HookRuleDefinition{
			RuleID:       "new-guard",
			Enabled:      true,
			Phase:        "preflight",
			Selector:     "all",
			ActionOnFail: "warn",
			ShortCircuit: true,
			EvaluatorIDs: []string{"eval-1"},
		})
	}))

	created, err := client.Create(context.Background(), &eval.HookRuleDefinition{
		RuleID:       "new-guard",
		Enabled:      true,
		Phase:        "preflight",
		Selector:     "all",
		ActionOnFail: "warn",
		ShortCircuit: true,
		EvaluatorIDs: []string{"eval-1"},
	})
	require.NoError(t, err)
	assert.Equal(t, "new-guard", created.RuleID)
	assert.True(t, created.Enabled)
}

func TestClient_Update_UsesPUT(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPut, r.Method, "hook-rules Update must be PUT (full replace), not PATCH")
		assert.Contains(t, r.URL.Path, "/eval/hook-rules/guard-1")

		var def eval.HookRuleDefinition
		assert.NoError(t, json.NewDecoder(r.Body).Decode(&def))
		assert.Equal(t, "warn", def.ActionOnFail)

		writeJSON(w, eval.HookRuleDefinition{
			RuleID:       "guard-1",
			ActionOnFail: "warn",
		})
	}))

	updated, err := client.Update(context.Background(), "guard-1", &eval.HookRuleDefinition{
		RuleID:       "guard-1",
		ActionOnFail: "warn",
	})
	require.NoError(t, err)
	assert.Equal(t, "guard-1", updated.RuleID)
	assert.Equal(t, "warn", updated.ActionOnFail)
}

func TestClient_Update_AcceptsCreated(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPut, r.Method)
		w.WriteHeader(http.StatusCreated)
		writeJSON(w, eval.HookRuleDefinition{RuleID: "guard-1"})
	}))

	updated, err := client.Update(context.Background(), "guard-1", &eval.HookRuleDefinition{RuleID: "guard-1"})
	require.NoError(t, err)
	assert.Equal(t, "guard-1", updated.RuleID)
}

func TestClient_Delete_NoContent(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodDelete, r.Method)
		assert.Contains(t, r.URL.Path, "/eval/hook-rules/guard-1")
		w.WriteHeader(http.StatusNoContent)
	}))

	err := client.Delete(context.Background(), "guard-1")
	require.NoError(t, err)
}

func TestClient_Delete_OK(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	err := client.Delete(context.Background(), "guard-1")
	require.NoError(t, err)
}
