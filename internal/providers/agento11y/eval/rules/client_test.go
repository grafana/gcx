package rules_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/providers/agento11y/agento11yhttp"
	"github.com/grafana/gcx/internal/providers/agento11y/eval"
	"github.com/grafana/gcx/internal/providers/agento11y/eval/rules"
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

func newTestClient(t *testing.T, handler http.Handler) *rules.Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	cfg := config.NamespacedRESTConfig{
		Config:    rest.Config{Host: srv.URL},
		Namespace: "default",
	}
	base, err := agento11yhttp.NewClient(cfg)
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

func TestClient_Get_Error(t *testing.T) {
	for _, status := range []int{http.StatusNotFound, http.StatusForbidden, http.StatusInternalServerError} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				http.Error(w, http.StatusText(status), status)
			}))
			item, err := client.Get(t.Context(), "nonexistent")
			require.Error(t, err)
			assert.Nil(t, item)
			assert.Equal(t, status == http.StatusNotFound, errors.Is(err, adapter.ErrNotFound))
			if status == http.StatusNotFound {
				assert.Contains(t, err.Error(), "nonexistent")
			}
		})
	}
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

func TestClient_Create_ConversationRule(t *testing.T) {
	idle := 10
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Contains(t, r.URL.Path, "/eval/rules")

		// The body the backend actually receives must carry min_idle_seconds,
		// otherwise the backend rejects conversation rules. This is the bug
		// being guarded: gcx previously dropped the field entirely.
		body, err := io.ReadAll(r.Body)
		assert.NoError(t, err)
		assert.Contains(t, string(body), `"min_idle_seconds":10`)

		var def eval.RuleDefinition
		assert.NoError(t, json.Unmarshal(body, &def))
		assert.Equal(t, "conversation", def.Selector)
		if assert.NotNil(t, def.MinIdleSeconds) {
			assert.Equal(t, 10, *def.MinIdleSeconds)
		}

		w.WriteHeader(http.StatusCreated)
		writeJSON(w, def)
	}))

	created, err := client.Create(context.Background(), &eval.RuleDefinition{
		RuleID:         "online.conversation.report_compiler.response_quality",
		Enabled:        true,
		Selector:       "conversation",
		MinIdleSeconds: &idle,
		SampleRate:     1.0,
		EvaluatorIDs:   []string{"custom.response_quality.v1"},
	})
	require.NoError(t, err)
	require.NotNil(t, created.MinIdleSeconds)
	assert.Equal(t, 10, *created.MinIdleSeconds)
}

// TestClient_ExecutionModeAndFilterableTagKeysRoundTrip pins that a rule's
// execution_mode and filterable_tag_keys reach the server on create and
// update, and come back from get and list. gcx previously dropped both, so a
// sequential rule was created as parallel and its tag keys were lost.
func TestClient_ExecutionModeAndFilterableTagKeysRoundTrip(t *testing.T) {
	stored := map[string]any{
		"rule_id":             "gated_quality",
		"enabled":             true,
		"selector":            "user_visible_turn",
		"sample_rate":         1.0,
		"evaluator_ids":       []string{"cheap_gate", "quality_judge"},
		"execution_mode":      "sequential",
		"filterable_tag_keys": []string{"environment", "team"},
	}

	var bodies []map[string]any
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.Method {
		case http.MethodPost, http.MethodPatch:
			var body map[string]any
			assert.NoError(t, json.NewDecoder(r.Body).Decode(&body))
			bodies = append(bodies, body)
			writeJSON(w, stored)
		case http.MethodGet:
			if strings.HasSuffix(r.URL.Path, "/eval/rules") {
				writeJSON(w, map[string]any{"items": []any{stored}})
				return
			}
			writeJSON(w, stored)
		default:
			http.Error(w, "unexpected method", http.StatusMethodNotAllowed)
		}
	}))

	rule := &eval.RuleDefinition{
		RuleID:            "gated_quality",
		Enabled:           true,
		Selector:          "user_visible_turn",
		SampleRate:        1.0,
		EvaluatorIDs:      []string{"cheap_gate", "quality_judge"},
		ExecutionMode:     "sequential",
		FilterableTagKeys: []string{"environment", "team"},
	}

	created, err := client.Create(t.Context(), rule)
	require.NoError(t, err)
	got, err := client.Get(t.Context(), "gated_quality")
	require.NoError(t, err)
	listed, err := client.List(t.Context())
	require.NoError(t, err)
	require.Len(t, listed, 1)
	updated, err := client.Update(t.Context(), "gated_quality", got)
	require.NoError(t, err)

	require.Len(t, bodies, 2)
	for i, body := range bodies {
		assert.Equal(t, "sequential", body["execution_mode"], "request %d", i)
		assert.Equal(t, []any{"environment", "team"}, body["filterable_tag_keys"], "request %d", i)
	}
	for name, r := range map[string]*eval.RuleDefinition{"create": created, "get": got, "list": &listed[0], "update": updated} {
		assert.Equal(t, "sequential", r.ExecutionMode, name)
		assert.Equal(t, []string{"environment", "team"}, r.FilterableTagKeys, name)
	}
}

func TestClient_Update(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPatch, r.Method)
		assert.Contains(t, r.URL.Path, "/eval/rules/rule-1")

		raw, err := io.ReadAll(r.Body)
		assert.NoError(t, err)
		// The id is in the URL path; it must NOT be in the PATCH body. The
		// server decodes with DisallowUnknownFields and its update DTO has no
		// rule_id field, so a body carrying rule_id fails with
		// `unknown field "rule_id"`. Even though the caller passes RuleID, the
		// client must strip it before sending.
		assert.NotContains(t, string(raw), "rule_id", "PATCH body must not carry rule_id")

		var def eval.RuleDefinition
		assert.NoError(t, json.Unmarshal(raw, &def))
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

// TestClient_UpdateFilterableTagKeys pins how a rule file's
// filterable_tag_keys reaches the PATCH body. The server keeps the stored keys
// when the field is absent and replaces them when it is present, so an empty
// list must be sent rather than dropped, or update can't clear the keys.
func TestClient_UpdateFilterableTagKeys(t *testing.T) {
	tests := []struct {
		name     string
		file     string
		wantSent bool
		want     []any
	}{
		{name: "yaml empty list clears", file: "rule_id: r\nfilterable_tag_keys: []\n", wantSent: true, want: []any{}},
		{name: "json empty list clears", file: `{"rule_id":"r","filterable_tag_keys":[]}`, wantSent: true, want: []any{}},
		{name: "yaml keys replace", file: "rule_id: r\nfilterable_tag_keys: [team]\n", wantSent: true, want: []any{"team"}},
		{name: "absent keeps stored", file: "rule_id: r\nsample_rate: 0.5\n"},
		{name: "null keeps stored", file: "rule_id: r\nfilterable_tag_keys: null\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var body map[string]any
			client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.NoError(t, json.NewDecoder(r.Body).Decode(&body))
				writeJSON(w, eval.RuleDefinition{RuleID: "r"})
			}))

			rule, err := rules.ReadRuleFile("-", strings.NewReader(tt.file))
			require.NoError(t, err)
			_, err = client.Update(t.Context(), "r", rule)
			require.NoError(t, err)

			got, sent := body["filterable_tag_keys"]
			assert.Equal(t, tt.wantSent, sent)
			if tt.wantSent {
				assert.Equal(t, tt.want, got)
			}
		})
	}
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
