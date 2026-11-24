package rules //nolint:testpackage // Tests drive unexported command constructors through the shared loader seam.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/providers/agento11y/eval"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	rulesAPI   = "/api/plugins/grafana-agento11y-app/resources/eval/rules"
	ruleFileV1 = "rule_id: r1\nselector: user_visible_turn\nsample_rate: 1\nevaluator_ids: [e1]\nenabled: true\n"
)

// fakeBackend serves the rules API from handler and records every request it
// receives (as "METHOD path-below-the-rules-API").
type fakeBackend struct {
	mu       sync.Mutex
	requests []string
	bodies   map[string]map[string]any
}

func (f *fakeBackend) log() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.requests...)
}

type runResult struct {
	backend *fakeBackend
	stdout  string
	stderr  string
	err     error
}

func runRules(t *testing.T, handler func(w http.ResponseWriter, method, path string), args []string, stdin string) runResult {
	t.Helper()
	fb := &fakeBackend{bodies: map[string]map[string]any{}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/bootdata" {
			assert.NoError(t, json.NewEncoder(w).Encode(map[string]any{"user": map[string]any{"orgId": 1}}))
			return
		}
		key := r.Method + " " + strings.TrimPrefix(r.URL.Path, rulesAPI)
		fb.mu.Lock()
		fb.requests = append(fb.requests, key)
		if r.Body != nil {
			var body map[string]any
			if json.NewDecoder(r.Body).Decode(&body) == nil {
				fb.bodies[key] = body
			}
		}
		fb.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		handler(w, r.Method, strings.TrimPrefix(r.URL.Path, rulesAPI))
	}))
	t.Cleanup(srv.Close)

	cfgPath := t.TempDir() + "/config.yaml"
	cfg := fmt.Sprintf("version: 1\nstacks:\n  default:\n    grafana:\n      server: %q\n      token: test-token\n      org-id: 1\ncontexts:\n  default:\n    stack: default\ncurrent-context: default\n", srv.URL)
	require.NoError(t, os.WriteFile(cfgPath, []byte(cfg), 0o600))
	loader := &providers.ConfigLoader{}
	loader.SetConfigFile(cfgPath)

	root := &cobra.Command{Use: "test", SilenceUsage: true, SilenceErrors: true}
	root.AddCommand(Commands(loader))
	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetIn(strings.NewReader(stdin))
	root.SetArgs(append([]string{"rules"}, args...))
	err := root.Execute()
	return runResult{backend: fb, stdout: stdout.String(), stderr: stderr.String(), err: err}
}

func writeJSON(t *testing.T, w http.ResponseWriter, v any) {
	t.Helper()
	assert.NoError(t, json.NewEncoder(w).Encode(v))
}

func existingActions() []eval.RuleAction {
	return []eval.RuleAction{
		{ActionID: "keep", RuleID: "r1", Condition: eval.RuleActionCondition{Kind: "all_evaluators_fail"}, ActionConfig: eval.RuleActionConfig{Kind: "add_to_collection", CollectionIDs: []string{"a"}}, Enabled: true},
		{ActionID: "pass1", RuleID: "r1", Condition: eval.RuleActionCondition{Kind: "all_evaluators_pass"}, ActionConfig: eval.RuleActionConfig{Kind: "add_to_collection", CollectionIDs: []string{"old"}}, Enabled: true},
		{ActionID: "other", RuleID: "r1", Condition: eval.RuleActionCondition{Kind: "score_below"}, ActionConfig: eval.RuleActionConfig{Kind: "add_to_collection", CollectionIDs: []string{"b"}}, Enabled: true},
	}
}

func TestRulesUpdate_ActionDeletionIsExplicit(t *testing.T) {
	fileWithKeep := ruleFileV1 + "actions:\n  - action_id: keep\n    condition: {kind: all_evaluators_fail}\n    action_config: {kind: add_to_collection, collection_ids: [a]}\n    enabled: true\n"
	tests := []struct {
		name      string
		args      []string
		file      string
		want      []string
		wantError string
	}{
		{
			name: "unlisted actions are left alone without --prune",
			args: []string{"update", "r1", "-f", "-"}, file: fileWithKeep,
			want: []string{"PATCH /r1", "GET /r1/actions", "PATCH /r1/actions/keep"},
		},
		{
			name: "--prune deletes only actions missing from the file",
			args: []string{"update", "r1", "-f", "-", "--prune"}, file: fileWithKeep,
			want: []string{"PATCH /r1", "GET /r1/actions", "PATCH /r1/actions/keep", "DELETE /r1/actions/pass1", "DELETE /r1/actions/other"},
		},
		{
			name: "collection flag updates its own action and deletes nothing",
			args: []string{"update", "r1", "-f", "-", "--on-pass-collection", "new"}, file: ruleFileV1,
			want: []string{"PATCH /r1", "GET /r1/actions", "PATCH /r1/actions/pass1"},
		},
		{
			name: "--prune cannot be combined with collection flags",
			args: []string{"update", "r1", "-f", "-", "--prune", "--on-pass-collection", "new"}, file: ruleFileV1,
			wantError: "--prune cannot be combined",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			res := runRules(t, func(w http.ResponseWriter, method, path string) {
				switch {
				case method == http.MethodPatch && path == "/r1":
					writeJSON(t, w, eval.RuleDefinition{RuleID: "r1", Selector: "user_visible_turn", Enabled: true})
				case method == http.MethodGet && path == "/r1/actions":
					writeJSON(t, w, map[string]any{"items": existingActions()})
				case method == http.MethodPatch && strings.HasPrefix(path, "/r1/actions/"):
					writeJSON(t, w, eval.RuleAction{ActionID: strings.TrimPrefix(path, "/r1/actions/"), RuleID: "r1"})
				case method == http.MethodDelete:
					w.WriteHeader(http.StatusNoContent)
				default:
					http.NotFound(w, nil)
				}
			}, tc.args, tc.file)
			if tc.wantError != "" {
				require.ErrorContains(t, res.err, tc.wantError)
				assert.Empty(t, res.backend.log())
				return
			}
			require.NoError(t, res.err)
			assert.ElementsMatch(t, tc.want, res.backend.log())
			if len(tc.want) == 3 && strings.Contains(tc.want[2], "pass1") {
				cfg, _ := res.backend.bodies["PATCH /r1/actions/pass1"]["action_config"].(map[string]any)
				assert.Equal(t, []any{"new"}, cfg["collection_ids"])
			}
		})
	}
}

func TestRulesRead_MissingActionsEndpointDoesNotFail(t *testing.T) {
	for _, args := range [][]string{{"get", "r1", "-o", "json"}, {"list", "-o", "json"}} {
		t.Run(args[0], func(t *testing.T) {
			rule := eval.RuleDefinition{RuleID: "r1", Selector: "user_visible_turn", Enabled: true}
			res := runRules(t, func(w http.ResponseWriter, method, path string) {
				switch {
				case strings.HasSuffix(path, "/actions"):
					http.NotFound(w, nil)
				case path == "/r1":
					writeJSON(t, w, rule)
				default:
					writeJSON(t, w, map[string]any{"items": []eval.RuleDefinition{rule}})
				}
			}, args, "")
			require.NoError(t, res.err)
			assert.Contains(t, res.stdout, "r1")
			assert.NotContains(t, res.stdout, `"actions"`)
			assert.Contains(t, res.stderr, "could not read actions for 1 rule(s)")
		})
	}
}

func TestRulesCreate_ActionFailureReportsCreatedRule(t *testing.T) {
	file := ruleFileV1 + "actions:\n  - condition: {kind: all_evaluators_fail}\n    action_config: {kind: add_to_collection, collection_ids: [a]}\n"
	res := runRules(t, func(w http.ResponseWriter, method, path string) {
		switch {
		case method == http.MethodPost && path == "":
			writeJSON(t, w, eval.RuleDefinition{RuleID: "r1", Selector: "user_visible_turn", Enabled: true})
		case method == http.MethodGet && path == "/r1/actions":
			writeJSON(t, w, map[string]any{"items": []eval.RuleAction{}})
		default:
			http.Error(w, "boom", http.StatusInternalServerError)
		}
	}, []string{"create", "-f", "-"}, file)
	require.ErrorContains(t, res.err, "rule r1 was created but its actions were not")
	assert.ErrorContains(t, res.err, "do not retry create")
}

func TestActionUpdate_OmittedEnabledIsNotSent(t *testing.T) {
	file := "condition: {kind: all_evaluators_fail}\naction_config: {kind: add_to_collection, collection_ids: [a]}\n"
	res := runRules(t, func(w http.ResponseWriter, _, _ string) {
		writeJSON(t, w, eval.RuleAction{ActionID: "keep", RuleID: "r1"})
	}, []string{"actions", "update", "r1", "keep", "-f", "-"}, file)
	require.NoError(t, res.err)
	body := res.backend.bodies["PATCH /r1/actions/keep"]
	require.NotNil(t, body)
	assert.NotContains(t, body, "enabled")
}
