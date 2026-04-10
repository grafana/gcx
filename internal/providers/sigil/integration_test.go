package sigil_test

// Integration tests for the sigil provider pipeline.
// Each test spins up an httptest server mimicking the plugin API,
// wires a real client, fetches data, then encodes through the real
// table codec — verifying the full path from HTTP to rendered output.

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/providers/sigil/agents"
	"github.com/grafana/gcx/internal/providers/sigil/conversations"
	"github.com/grafana/gcx/internal/providers/sigil/eval"
	"github.com/grafana/gcx/internal/providers/sigil/eval/evaluators"
	"github.com/grafana/gcx/internal/providers/sigil/eval/rules"
	"github.com/grafana/gcx/internal/providers/sigil/eval/templates"
	"github.com/grafana/gcx/internal/providers/sigil/sigilhttp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"
)

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func newBase(t *testing.T, handler http.Handler) *sigilhttp.Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	base, err := sigilhttp.NewClient(config.NamespacedRESTConfig{
		Config:    rest.Config{Host: srv.URL},
		Namespace: "default",
	})
	require.NoError(t, err)
	return base
}

// fakePluginMux returns a mux mimicking the grafana-sigil-app plugin API.
func fakePluginMux() *http.ServeMux {
	mux := http.NewServeMux()

	mux.HandleFunc("/api/plugins/grafana-sigil-app/resources/query/conversations",
		func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(w, map[string]any{
				"items": []map[string]any{
					{"id": "conv-1", "title": "Debug session", "generation_count": 12, "last_generation_at": "2026-04-02T18:00:00Z"},
					{"id": "conv-2", "title": "", "generation_count": 1},
				},
			})
		})

	mux.HandleFunc("/api/plugins/grafana-sigil-app/resources/query/conversations/conv-1",
		func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(w, map[string]any{
				"conversation_id": "conv-1",
				"generations": []map[string]any{
					{"generation_id": "gen-1", "model": map[string]string{"name": "claude-3", "provider": "anthropic"}},
				},
			})
		})

	mux.HandleFunc("/api/plugins/grafana-sigil-app/resources/query/conversations/search",
		func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(w, map[string]any{
				"conversations": []map[string]any{
					{"conversation_id": "conv-1", "conversation_title": "Debug session",
						"generation_count": 12, "models": []string{"claude-3"}, "agents": []string{"my-agent"},
						"last_generation_at": "2026-04-02T18:00:00Z"},
				},
				"has_more": false,
			})
		})

	mux.HandleFunc("/api/plugins/grafana-sigil-app/resources/query/agents",
		func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(w, map[string]any{
				"items": []map[string]any{
					{"agent_name": "my-agent", "version_count": 2, "generation_count": 100,
						"tool_count": 3, "latest_seen_at": "2026-04-02T18:00:00Z",
						"token_estimate": map[string]int{"total": 500}},
				},
			})
		})

	mux.HandleFunc("/api/plugins/grafana-sigil-app/resources/query/agents/lookup",
		func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, map[string]any{
				"agent_name": r.URL.Query().Get("name"), "effective_version": "sha256:abc123",
				"system_prompt": "You are helpful.", "tools": []any{},
			})
		})

	mux.HandleFunc("/api/plugins/grafana-sigil-app/resources/query/agents/versions",
		func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(w, map[string]any{
				"items": []map[string]any{
					{"effective_version": "sha256:abc123", "generation_count": 50,
						"tool_count": 0, "first_seen_at": "2026-03-01T00:00:00Z",
						"last_seen_at": "2026-04-02T00:00:00Z", "token_estimate": map[string]int{"total": 500}},
				},
			})
		})

	mux.HandleFunc("/api/plugins/grafana-sigil-app/resources/eval/evaluators",
		func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(w, map[string]any{
				"items": []map[string]any{
					{"evaluator_id": "eval-1", "version": "1.0", "kind": "llm_judge", "description": "Quality check"},
				},
			})
		})

	mux.HandleFunc("/api/plugins/grafana-sigil-app/resources/eval/evaluators/eval-1",
		func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(w, map[string]any{
				"evaluator_id": "eval-1", "version": "1.0", "kind": "llm_judge",
				"description": "Quality check",
				"config":      map[string]any{"model": "gpt-4"},
				"output_keys": []map[string]any{{"key": "quality", "type": "number"}},
			})
		})

	mux.HandleFunc("/api/plugins/grafana-sigil-app/resources/eval/rules",
		func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(w, map[string]any{
				"items": []map[string]any{
					{"rule_id": "rule-1", "enabled": true, "selector": "user_visible_turn",
						"sample_rate": 1.0, "evaluator_ids": []string{"eval-1"}},
				},
			})
		})

	mux.HandleFunc("/api/plugins/grafana-sigil-app/resources/eval:test",
		func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost {
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}
			passed := true
			writeJSON(w, map[string]any{
				"generation_id":     "gen-1",
				"conversation_id":   "conv-1",
				"execution_time_ms": 150,
				"scores": []map[string]any{
					{"key": "quality", "type": "number", "value": 0.9, "passed": passed, "explanation": "Good quality"},
				},
			})
		})

	mux.HandleFunc("/api/plugins/grafana-sigil-app/resources/eval/templates",
		func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(w, map[string]any{
				"items": []map[string]any{
					{"template_id": "tpl-1", "scope": "global", "kind": "llm_judge",
						"latest_version": "2026-04-01", "description": "Quality template"},
				},
			})
		})

	mux.HandleFunc("/api/plugins/grafana-sigil-app/resources/eval/templates/tpl-1/versions",
		func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(w, map[string]any{
				"items": []map[string]any{
					{"template_id": "tpl-1", "version": "2026-04-01", "changelog": "Initial release",
						"created_by": "admin"},
				},
			})
		})

	return mux
}

func TestIntegration_ConversationsListToTable(t *testing.T) {
	base := newBase(t, fakePluginMux())
	client := conversations.NewClient(base)

	items, err := client.List(context.Background(), 0)
	require.NoError(t, err)
	require.Len(t, items, 2)

	var buf bytes.Buffer
	codec := &conversations.TableCodec{}
	require.NoError(t, codec.Encode(&buf, items))

	output := buf.String()
	assert.Contains(t, output, "conv-1")
	assert.Contains(t, output, "Debug session")
	assert.Contains(t, output, "12")
	assert.Contains(t, output, "conv-2")
	assert.Contains(t, output, "-") // empty title
}

func TestIntegration_ConversationsGetDecodes(t *testing.T) {
	base := newBase(t, fakePluginMux())
	client := conversations.NewClient(base)

	detail, err := client.Get(context.Background(), "conv-1")
	require.NoError(t, err)

	// Verify model is decoded as an object (this caught a real bug)
	gens, ok := (*detail)["generations"].([]any)
	require.True(t, ok)
	require.Len(t, gens, 1)

	gen, ok := gens[0].(map[string]any)
	require.True(t, ok)

	model, ok := gen["model"].(map[string]any)
	require.True(t, ok, "model should be an object, not a string")
	assert.Equal(t, "claude-3", model["name"])
}

func TestIntegration_ConversationsSearchToTable(t *testing.T) {
	base := newBase(t, fakePluginMux())
	client := conversations.NewClient(base)

	resp, err := client.Search(context.Background(), conversations.SearchRequest{})
	require.NoError(t, err)

	var buf bytes.Buffer
	codec := &conversations.SearchTableCodec{}
	require.NoError(t, codec.Encode(&buf, resp.Conversations))

	output := buf.String()
	assert.Contains(t, output, "conv-1")
	assert.Contains(t, output, "claude-3")

	// agents column only in wide mode
	var wideBuf bytes.Buffer
	wideCodec := &conversations.SearchTableCodec{Wide: true}
	require.NoError(t, wideCodec.Encode(&wideBuf, resp.Conversations))
	assert.Contains(t, wideBuf.String(), "my-agent")
}

func TestIntegration_AgentsListToTable(t *testing.T) {
	base := newBase(t, fakePluginMux())
	client := agents.NewClient(base)

	items, err := client.List(context.Background(), 0)
	require.NoError(t, err)
	require.Len(t, items, 1)

	var buf bytes.Buffer
	codec := &agents.ListTableCodec{}
	require.NoError(t, codec.Encode(&buf, items))

	output := buf.String()
	assert.Contains(t, output, "my-agent")
	assert.Contains(t, output, "100")
	assert.Contains(t, output, "3")
}

func TestIntegration_AgentsLookupDecodes(t *testing.T) {
	base := newBase(t, fakePluginMux())
	client := agents.NewClient(base)

	detail, err := client.Lookup(context.Background(), "my-agent", "")
	require.NoError(t, err)
	assert.Equal(t, "my-agent", (*detail)["agent_name"])
	assert.Equal(t, "sha256:abc123", (*detail)["effective_version"])
}

func TestIntegration_AgentsVersionsToTable(t *testing.T) {
	base := newBase(t, fakePluginMux())
	client := agents.NewClient(base)

	items, err := client.Versions(context.Background(), "my-agent")
	require.NoError(t, err)

	var buf bytes.Buffer
	codec := &agents.VersionsTableCodec{}
	require.NoError(t, codec.Encode(&buf, items))

	assert.Contains(t, buf.String(), "sha256:abc123")
	assert.Contains(t, buf.String(), "50")
}

func TestIntegration_EvaluatorsListToTable(t *testing.T) {
	base := newBase(t, fakePluginMux())
	client := evaluators.NewClient(base)

	items, err := client.List(context.Background())
	require.NoError(t, err)
	require.Len(t, items, 1)
	assert.Equal(t, "eval-1", items[0].EvaluatorID)

	var buf bytes.Buffer
	codec := &evaluators.TableCodec{}
	require.NoError(t, codec.Encode(&buf, items))

	output := buf.String()
	assert.Contains(t, output, "eval-1")
	assert.Contains(t, output, "llm_judge")
}

func TestIntegration_RulesListToTable(t *testing.T) {
	base := newBase(t, fakePluginMux())
	client := rules.NewClient(base)

	items, err := client.List(context.Background())
	require.NoError(t, err)
	require.Len(t, items, 1)
	assert.Equal(t, "rule-1", items[0].RuleID)

	var buf bytes.Buffer
	codec := &rules.TableCodec{}
	require.NoError(t, codec.Encode(&buf, items))

	output := buf.String()
	assert.Contains(t, output, "rule-1")
	assert.Contains(t, output, "user_visible_turn")
	assert.Contains(t, output, "eval-1")
}

func TestIntegration_EvalTestRunTest(t *testing.T) {
	base := newBase(t, fakePluginMux())
	client := evaluators.NewClient(base)

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

func TestIntegration_EvalTestToTable(t *testing.T) {
	base := newBase(t, fakePluginMux())
	client := evaluators.NewClient(base)

	resp, err := client.RunTest(context.Background(), &eval.EvalTestRequest{
		Kind:         "llm_judge",
		GenerationID: "gen-1",
	})
	require.NoError(t, err)

	var buf bytes.Buffer
	codec := &evaluators.TestTableCodec{}
	require.NoError(t, codec.Encode(&buf, resp))

	output := buf.String()
	assert.Contains(t, output, "quality")
	assert.Contains(t, output, "gen-1")
	assert.Contains(t, output, "150ms")
}

func TestIntegration_TemplatesListToTable(t *testing.T) {
	base := newBase(t, fakePluginMux())
	client := templates.NewClient(base)

	items, err := client.List(context.Background(), "")
	require.NoError(t, err)
	require.Len(t, items, 1)
	assert.Equal(t, "tpl-1", items[0].TemplateID)

	var buf bytes.Buffer
	codec := &templates.TableCodec{}
	require.NoError(t, codec.Encode(&buf, items))

	output := buf.String()
	assert.Contains(t, output, "tpl-1")
	assert.Contains(t, output, "global")
	assert.Contains(t, output, "llm_judge")
}

func TestIntegration_TemplateVersionsToTable(t *testing.T) {
	base := newBase(t, fakePluginMux())
	client := templates.NewClient(base)

	versions, err := client.ListVersions(context.Background(), "tpl-1")
	require.NoError(t, err)
	require.Len(t, versions, 1)

	var buf bytes.Buffer
	codec := &templates.VersionsTableCodec{}
	require.NoError(t, codec.Encode(&buf, versions))

	output := buf.String()
	assert.Contains(t, output, "2026-04-01")
	assert.Contains(t, output, "Initial release")
}
