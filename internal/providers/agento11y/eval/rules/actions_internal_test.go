package rules

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/providers/agento11y/agento11yhttp"
	"github.com/grafana/gcx/internal/providers/agento11y/eval"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"
)

func TestApplyActionFlags(t *testing.T) {
	rule := &eval.RuleDefinition{}
	require.NoError(t, applyActionFlags(rule, []string{"pass-a", "pass-b"}, []string{"fail-a"}))
	require.NotNil(t, rule.Actions)
	require.Len(t, *rule.Actions, 2)
	assert.Equal(t, "all_evaluators_pass", (*rule.Actions)[0].Condition.Kind)
	assert.Equal(t, []string{"pass-a", "pass-b"}, (*rule.Actions)[0].ActionConfig.CollectionIDs)
	assert.Equal(t, "all_evaluators_fail", (*rule.Actions)[1].Condition.Kind)

	withActions := &eval.RuleDefinition{Actions: rule.Actions}
	assert.ErrorContains(t, applyActionFlags(withActions, []string{"another"}, nil), "cannot be combined")
}

func TestReadActionFileDefaultsEnabled(t *testing.T) {
	action, err := readActionFile("-", bytes.NewBufferString(`
condition:
  kind: all_evaluators_pass
action_config:
  kind: add_to_collection
  collection_ids: [review]
`))
	require.NoError(t, err)
	assert.True(t, action.Enabled)
	assert.Equal(t, "all_evaluators_pass", action.Condition.Kind)
	assert.Equal(t, []string{"review"}, action.ActionConfig.CollectionIDs)
}

func TestReconcileActions(t *testing.T) {
	var requests []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Method+" "+r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		switch r.Method {
		case http.MethodGet:
			assert.NoError(t, json.NewEncoder(w).Encode(map[string]any{"items": []eval.RuleAction{
				{ActionID: "keep", RuleID: "rule-1"},
				{ActionID: "stale", RuleID: "rule-1"},
			}}))
		case http.MethodPost:
			assert.NoError(t, json.NewEncoder(w).Encode(eval.RuleAction{ActionID: "created", RuleID: "rule-1"}))
		case http.MethodPatch:
			assert.NoError(t, json.NewEncoder(w).Encode(eval.RuleAction{ActionID: "keep", RuleID: "rule-1"}))
		case http.MethodDelete:
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	t.Cleanup(srv.Close)

	base, err := agento11yhttp.NewClient(config.NamespacedRESTConfig{Config: rest.Config{Host: srv.URL}})
	require.NoError(t, err)
	client := NewClient(base)

	existing, err := reconcileActions(t.Context(), client, "rule-1", nil)
	require.NoError(t, err)
	require.Len(t, existing, 2)
	assert.Equal(t, []string{"GET /api/plugins/grafana-agento11y-app/resources/eval/rules/rule-1/actions"}, requests)

	requests = nil
	desired := []eval.RuleAction{
		{Condition: eval.RuleActionCondition{Kind: "all_evaluators_pass"}, ActionConfig: eval.RuleActionConfig{Kind: "add_to_collection", CollectionIDs: []string{"review"}}, Enabled: true},
		{ActionID: "keep", Condition: eval.RuleActionCondition{Kind: "all_evaluators_fail"}, ActionConfig: eval.RuleActionConfig{Kind: "add_to_collection", CollectionIDs: []string{"review"}}, Enabled: true},
	}
	result, err := reconcileActions(t.Context(), client, "rule-1", &desired)
	require.NoError(t, err)
	require.Len(t, result, 2)
	assert.Equal(t, []string{
		"GET /api/plugins/grafana-agento11y-app/resources/eval/rules/rule-1/actions",
		"POST /api/plugins/grafana-agento11y-app/resources/eval/rules/rule-1/actions",
		"PATCH /api/plugins/grafana-agento11y-app/resources/eval/rules/rule-1/actions/keep",
		"DELETE /api/plugins/grafana-agento11y-app/resources/eval/rules/rule-1/actions/stale",
	}, requests)
}

func TestActionCreateCommand(t *testing.T) {
	tests := []struct {
		name         string
		args         []string
		stdin        string
		wantErr      string
		wantRequests int
		wantStdout   string
		wantStderr   string
	}{
		{name: "filename required", args: []string{"rule-1"}, wantErr: "--filename/-f is required"},
		{
			name: "short filename flag and yaml output",
			args: []string{"rule-1", "-f", "-", "-o", "yaml"},
			stdin: `condition: {kind: all_evaluators_fail}
action_config: {kind: add_to_collection, collection_ids: [review]}
`,
			wantRequests: 1,
			wantStdout:   "action_id: ra-created",
			wantStderr:   "Action ra-created created",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			requests := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/bootdata" {
					assert.NoError(t, json.NewEncoder(w).Encode(map[string]any{"user": map[string]any{"orgId": 1}}))
					return
				}
				requests++
				assert.Equal(t, http.MethodPost, r.Method)
				assert.Equal(t, "/api/plugins/grafana-agento11y-app/resources/eval/rules/rule-1/actions", r.URL.Path)
				w.Header().Set("Content-Type", "application/json")
				assert.NoError(t, json.NewEncoder(w).Encode(eval.RuleAction{ActionID: "ra-created", RuleID: "rule-1", Enabled: true}))
			}))
			t.Cleanup(srv.Close)

			cfgPath := t.TempDir() + "/config.yaml"
			cfg := fmt.Sprintf("version: 1\nstacks:\n  default:\n    grafana:\n      server: %q\n      token: test-token\n      org-id: 1\ncontexts:\n  default:\n    stack: default\ncurrent-context: default\n", srv.URL)
			require.NoError(t, os.WriteFile(cfgPath, []byte(cfg), 0o600))
			loader := &providers.ConfigLoader{}
			loader.SetConfigFile(cfgPath)

			cmd := actionCreateCommand(loader)
			root := &cobra.Command{Use: "test"}
			root.AddCommand(cmd)
			var stdout, stderr bytes.Buffer
			root.SetOut(&stdout)
			root.SetErr(&stderr)
			root.SetIn(strings.NewReader(tc.stdin))
			root.SetArgs(append([]string{"create"}, tc.args...))

			err := root.Execute()
			if tc.wantErr != "" {
				require.ErrorContains(t, err, tc.wantErr)
			} else {
				require.NoError(t, err)
			}
			assert.Equal(t, tc.wantRequests, requests)
			assert.Contains(t, stdout.String(), tc.wantStdout)
			assert.Contains(t, stderr.String(), tc.wantStderr)
		})
	}
}
