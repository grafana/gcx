package alert_test

// Agent output contract tests for the alert family (#387).
//
// The contract for finite commands: in agent mode with no explicit -o, stdout
// carries EXACTLY ONE JSON value; the human default stdout stays
// byte-identical to the pre-migration implementation; explicit -o json/yaml
// always wins; confirmation prompts and their "Aborted." note render on
// stderr, never stdout. Alert mutation commands are single-target (no
// partial-failure semantics), so no EmittedError paths exist in this family.

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/providers/alert"
	"github.com/grafana/gcx/internal/testutils"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
	"k8s.io/client-go/rest"
)

// setAgentMode forces agent mode on or off for the duration of the test.
// Must be called BEFORE the command under test is constructed: the agents
// default-format override is resolved in Options.BindFlags at construction.
func setAgentMode(t *testing.T, enabled bool) {
	t.Helper()
	// GCX_AUTO_APPROVE would silently bypass the confirmation paths under
	// test, so pin it off for the test's duration.
	t.Setenv("GCX_AUTO_APPROVE", "0")
	agent.SetFlag(enabled)
	t.Cleanup(agent.ResetForTesting)
}

// fakeGrafanaConfigLoader implements alert.GrafanaConfigLoader for command tests.
type fakeGrafanaConfigLoader struct {
	cfg config.NamespacedRESTConfig
}

func (l fakeGrafanaConfigLoader) LoadGrafanaConfig(context.Context) (config.NamespacedRESTConfig, error) {
	return l.cfg, nil
}

// newAlertFixture starts a mock Grafana server with the given handler and
// returns a loader pointing at it.
func newAlertFixture(t *testing.T, handler http.HandlerFunc) fakeGrafanaConfigLoader {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return fakeGrafanaConfigLoader{cfg: config.NamespacedRESTConfig{
		Config: rest.Config{Host: srv.URL},
	}}
}

// acceptMutations acknowledges any provisioning write with 202 and an empty
// JSON body, recording the last method and path seen.
func acceptMutations(lastMethod, lastPath *string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		*lastMethod = r.Method
		*lastPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{}`))
	}
}

// serveRules answers the Grafana ruler status API with the given groups.
func serveRules(groups []alert.RuleGroup) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, alert.RulesResponse{
			Status: "success",
			Data:   alert.RulesData{Groups: groups},
		})
	}
}

// runCmdSplit executes child under a silenced parent with separate stdout and
// stderr buffers, so contract assertions can check stdout purity. It returns
// (stdout, stderr, execute error).
func runCmdSplit(t *testing.T, child *cobra.Command, args []string, stdin string) (string, string, error) {
	t.Helper()

	parent := &cobra.Command{
		Use:           "test",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	parent.AddCommand(child)

	var outBuf, errBuf bytes.Buffer
	parent.SetOut(&outBuf)
	parent.SetErr(&errBuf)
	parent.SetIn(strings.NewReader(stdin))
	parent.SetArgs(args)

	err := parent.Execute()
	return outBuf.String(), errBuf.String(), err
}

// decodeSingleJSONDocument asserts stdout holds exactly one JSON value
// followed by EOF and returns the decoded value.
func decodeSingleJSONDocument(t *testing.T, stdout string) any {
	t.Helper()

	dec := json.NewDecoder(strings.NewReader(stdout))
	var doc any
	require.NoError(t, dec.Decode(&doc), "stdout must decode as JSON, got: %q", stdout)

	var extra any
	err := dec.Decode(&extra)
	require.ErrorIs(t, err, io.EOF,
		"stdout must contain exactly one JSON value then EOF; second decode returned (%v)\nstdout: %q", extra, stdout)
	return doc
}

// testStatusGroups is the canonical ruler-API fixture: one group with a
// firing rule (carrying one alert instance) and an inactive rule.
func testStatusGroups() []alert.RuleGroup {
	return []alert.RuleGroup{
		{
			Name:           "group-1",
			FolderUID:      "folder-abc",
			Interval:       60,
			LastEvaluation: "2024-01-15T10:00:00Z",
			Rules: []alert.RuleStatus{
				{UID: "uid-1", Name: "Rule 1", State: alert.StateFiring, Alerts: []alert.AlertInstance{
					{State: alert.StateFiring, ActiveAt: "2024-01-15T09:00:00Z", Labels: map[string]string{"severity": "critical"}},
				}},
				{UID: "uid-2", Name: "Rule 2", State: alert.StateInactive},
			},
		},
	}
}

// policyFile writes a minimal notification policy tree and returns its path.
func policyFile(t *testing.T) string {
	t.Helper()
	return testutils.CreateTempFile(t, `{"receiver":"grafana-default-email"}`)
}

// mutationCase describes one migrated single-target mutation command.
type mutationCase struct {
	name       string
	newCmd     func(loader alert.GrafanaConfigLoader) *cobra.Command
	args       func(t *testing.T) []string // forced (no-prompt) invocation, without -o
	wantHuman  string                      // exact human-default stdout
	wantAction string
	wantTarget map[string]any
	wantMethod string
	wantPath   string
}

func mutationCases() []mutationCase {
	return []mutationCase{
		{
			name:       "contact-points delete",
			newCmd:     alert.NewContactPointsDeleteCommandForTest,
			args:       func(*testing.T) []string { return []string{"delete", "cp-1", "--force"} },
			wantHuman:  "✔ Deleted contact point cp-1\n",
			wantAction: "deleted",
			wantTarget: map[string]any{"kind": "contact-point", "uid": "cp-1"},
			wantMethod: http.MethodDelete,
			wantPath:   "/api/v1/provisioning/contact-points/cp-1",
		},
		{
			name:       "mute-timings delete",
			newCmd:     alert.NewMuteTimingsDeleteCommandForTest,
			args:       func(*testing.T) []string { return []string{"delete", "weekends", "--force"} },
			wantHuman:  "✔ Deleted mute timing weekends\n",
			wantAction: "deleted",
			wantTarget: map[string]any{"kind": "mute-timing", "name": "weekends"},
			wantMethod: http.MethodDelete,
			wantPath:   "/api/v1/provisioning/mute-timings/weekends",
		},
		{
			name:       "templates delete",
			newCmd:     alert.NewTemplatesDeleteCommandForTest,
			args:       func(*testing.T) []string { return []string{"delete", "my-template", "--force"} },
			wantHuman:  "✔ Deleted template my-template\n",
			wantAction: "deleted",
			wantTarget: map[string]any{"kind": "template", "name": "my-template"},
			wantMethod: http.MethodDelete,
			wantPath:   "/api/v1/provisioning/templates/my-template",
		},
		{
			name:   "notification-policies set",
			newCmd: alert.NewNotificationPoliciesSetCommandForTest,
			args: func(t *testing.T) []string {
				t.Helper()
				return []string{"set", "--force", "-f", policyFile(t)}
			},
			wantHuman:  "✔ Notification policy updated\n",
			wantAction: "updated",
			wantTarget: map[string]any{"kind": "notification-policy"},
			wantMethod: http.MethodPut,
			wantPath:   "/api/v1/provisioning/policies",
		},
		{
			name:       "notification-policies reset",
			newCmd:     alert.NewNotificationPoliciesResetCommandForTest,
			args:       func(*testing.T) []string { return []string{"reset", "--force"} },
			wantHuman:  "✔ Notification policy reset to default\n",
			wantAction: "reset",
			wantTarget: map[string]any{"kind": "notification-policy"},
			wantMethod: http.MethodDelete,
			wantPath:   "/api/v1/provisioning/policies",
		},
	}
}

// TestAlertMutationAgentMode_SingleJSONDocument verifies each migrated
// mutation emits exactly one gcx.mutation JSON document on stdout in agent
// mode (no explicit -o), and that the mutation actually reached the API.
func TestAlertMutationAgentMode_SingleJSONDocument(t *testing.T) {
	for _, tt := range mutationCases() {
		t.Run(tt.name, func(t *testing.T) {
			setAgentMode(t, true)

			var method, path string
			loader := newAlertFixture(t, acceptMutations(&method, &path))
			stdout, _, err := runCmdSplit(t, tt.newCmd(loader), tt.args(t), "")
			require.NoError(t, err)

			doc, ok := decodeSingleJSONDocument(t, stdout).(map[string]any)
			require.True(t, ok, "mutation result should be a JSON object, got: %q", stdout)
			assert.Equal(t, "gcx.mutation", doc["type"])
			assert.Equal(t, "1", doc["schema_version"])
			assert.Equal(t, tt.wantAction, doc["action"])
			assert.Equal(t, tt.wantTarget, doc["target"])

			assert.Equal(t, tt.wantMethod, method)
			assert.Equal(t, tt.wantPath, path)
		})
	}
}

// TestAlertMutationHumanDefault_ByteIdentical pins the exact human stdout of
// the migrated mutation paths to the bytes the pre-codec implementation
// produced with cmdio.Success.
func TestAlertMutationHumanDefault_ByteIdentical(t *testing.T) {
	for _, tt := range mutationCases() {
		t.Run(tt.name, func(t *testing.T) {
			setAgentMode(t, false)

			var method, path string
			loader := newAlertFixture(t, acceptMutations(&method, &path))
			stdout, _, err := runCmdSplit(t, tt.newCmd(loader), tt.args(t), "")
			require.NoError(t, err)
			assert.Equal(t, tt.wantHuman, stdout)
		})
	}
}

// TestAlertMutationExplicitOutputOverride verifies explicit -o json/yaml/text
// always wins — in human mode and over the agents default in agent mode.
func TestAlertMutationExplicitOutputOverride(t *testing.T) {
	t.Run("delete -o json emits structured document in human mode", func(t *testing.T) {
		setAgentMode(t, false)

		var method, path string
		loader := newAlertFixture(t, acceptMutations(&method, &path))
		stdout, _, err := runCmdSplit(t, alert.NewContactPointsDeleteCommandForTest(loader),
			[]string{"delete", "cp-1", "--force", "-o", "json"}, "")
		require.NoError(t, err)

		doc, ok := decodeSingleJSONDocument(t, stdout).(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "gcx.mutation", doc["type"])
		assert.Equal(t, "deleted", doc["action"])
	})

	t.Run("delete -o yaml beats agents default in agent mode", func(t *testing.T) {
		setAgentMode(t, true)

		var method, path string
		loader := newAlertFixture(t, acceptMutations(&method, &path))
		stdout, _, err := runCmdSplit(t, alert.NewMuteTimingsDeleteCommandForTest(loader),
			[]string{"delete", "weekends", "--force", "-o", "yaml"}, "")
		require.NoError(t, err)

		var doc map[string]any
		require.NoError(t, yaml.Unmarshal([]byte(stdout), &doc))
		assert.Equal(t, "gcx.mutation", doc["type"])
		// YAML, not a JSON document: the JSON decoder must reject it.
		assert.False(t, json.Valid([]byte(stdout)), "-o yaml must not produce JSON, got: %q", stdout)
	})

	t.Run("explicit -o text beats agents default in agent mode", func(t *testing.T) {
		setAgentMode(t, true)

		var method, path string
		loader := newAlertFixture(t, acceptMutations(&method, &path))
		stdout, _, err := runCmdSplit(t, alert.NewTemplatesDeleteCommandForTest(loader),
			[]string{"delete", "my-template", "--force", "-o", "text"}, "")
		require.NoError(t, err)
		assert.Equal(t, "✔ Deleted template my-template\n", stdout)
	})
}

// TestAlertMutationAgentModeGuard pins the destructive-operation guard:
// agent mode without --force must fail fast without touching the API or
// stdout.
func TestAlertMutationAgentModeGuard(t *testing.T) {
	guarded := []struct {
		name   string
		newCmd func(loader alert.GrafanaConfigLoader) *cobra.Command
		args   func(t *testing.T) []string
	}{
		{"contact-points delete", alert.NewContactPointsDeleteCommandForTest,
			func(*testing.T) []string { return []string{"delete", "cp-1"} }},
		{"mute-timings delete", alert.NewMuteTimingsDeleteCommandForTest,
			func(*testing.T) []string { return []string{"delete", "weekends"} }},
		{"templates delete", alert.NewTemplatesDeleteCommandForTest,
			func(*testing.T) []string { return []string{"delete", "my-template"} }},
		{"notification-policies set", alert.NewNotificationPoliciesSetCommandForTest,
			func(t *testing.T) []string {
				t.Helper()
				return []string{"set", "-f", policyFile(t)}
			}},
		{"notification-policies reset", alert.NewNotificationPoliciesResetCommandForTest,
			func(*testing.T) []string { return []string{"reset"} }},
	}

	for _, tt := range guarded {
		t.Run(tt.name, func(t *testing.T) {
			setAgentMode(t, true)

			loader := newAlertFixture(t, func(w http.ResponseWriter, r *http.Request) {
				t.Errorf("unexpected API call: %s %s", r.Method, r.URL.Path)
				w.WriteHeader(http.StatusInternalServerError)
			})
			stdout, _, err := runCmdSplit(t, tt.newCmd(loader), tt.args(t), "")

			require.Error(t, err)
			require.ErrorIs(t, err, providers.ErrAgentModeRequiresForce)
			assert.Empty(t, stdout, "guard rejection must not write to stdout")
		})
	}
}

// TestAlertMutationPromptDiagnosticsStayOffStdout verifies the interactive
// confirmation exchange (prompt and "Aborted.") renders on stderr, keeping
// stdout reserved for the result document.
func TestAlertMutationPromptDiagnosticsStayOffStdout(t *testing.T) {
	t.Run("accepted prompt: stderr prompt, stdout result only", func(t *testing.T) {
		setAgentMode(t, false)

		var method, path string
		loader := newAlertFixture(t, acceptMutations(&method, &path))
		stdout, stderr, err := runCmdSplit(t, alert.NewContactPointsDeleteCommandForTest(loader),
			[]string{"delete", "cp-1"}, "y\n")
		require.NoError(t, err)

		assert.Contains(t, stderr, "Delete contact point cp-1? [y/N]",
			"confirmation prompt belongs on stderr")
		assert.Equal(t, "✔ Deleted contact point cp-1\n", stdout,
			"stdout must carry only the result line")
	})

	t.Run("declined prompt: nothing on stdout, Aborted on stderr, exit 0", func(t *testing.T) {
		setAgentMode(t, false)

		loader := newAlertFixture(t, func(w http.ResponseWriter, r *http.Request) {
			t.Errorf("unexpected API call after declined prompt: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusInternalServerError)
		})
		stdout, stderr, err := runCmdSplit(t, alert.NewContactPointsDeleteCommandForTest(loader),
			[]string{"delete", "cp-1"}, "n\n")
		require.NoError(t, err)

		assert.Empty(t, stdout, "declined prompt must not write to stdout")
		assert.Contains(t, stderr, "Aborted.")
	})
}

// TestGroupsStatusOutputContract covers the empty-result branch that used to
// bypass the codec (styled prose on stdout even for machine formats) and the
// non-empty conformance paths.
func TestGroupsStatusOutputContract(t *testing.T) {
	t.Run("agent mode empty result emits one JSON value ([])", func(t *testing.T) {
		setAgentMode(t, true)

		loader := newAlertFixture(t, serveRules(nil))
		stdout, _, err := runCmdSplit(t, alert.NewGroupsStatusCommandForTest(loader),
			[]string{"status"}, "")
		require.NoError(t, err)

		doc := decodeSingleJSONDocument(t, stdout)
		items, ok := doc.([]any)
		require.True(t, ok, "empty status result should be a JSON array, got %T: %q", doc, stdout)
		assert.Empty(t, items)
	})

	t.Run("explicit -o json empty result emits []", func(t *testing.T) {
		setAgentMode(t, false)

		loader := newAlertFixture(t, serveRules(nil))
		stdout, _, err := runCmdSplit(t, alert.NewGroupsStatusCommandForTest(loader),
			[]string{"status", "-o", "json"}, "")
		require.NoError(t, err)

		doc := decodeSingleJSONDocument(t, stdout)
		items, ok := doc.([]any)
		require.True(t, ok, "empty status result should be a JSON array, got %T: %q", doc, stdout)
		assert.Empty(t, items)
	})

	t.Run("human default empty result stays byte-identical", func(t *testing.T) {
		setAgentMode(t, false)

		loader := newAlertFixture(t, serveRules(nil))
		stdout, _, err := runCmdSplit(t, alert.NewGroupsStatusCommandForTest(loader),
			[]string{"status"}, "")
		require.NoError(t, err)
		assert.Equal(t, "🛈 No alert rule groups found.\n", stdout)
	})

	t.Run("agent mode non-empty result emits one JSON value", func(t *testing.T) {
		setAgentMode(t, true)

		loader := newAlertFixture(t, serveRules(testStatusGroups()))
		stdout, _, err := runCmdSplit(t, alert.NewGroupsStatusCommandForTest(loader),
			[]string{"status"}, "")
		require.NoError(t, err)

		doc := decodeSingleJSONDocument(t, stdout)
		items, ok := doc.([]any)
		require.True(t, ok, "status result should be a JSON array, got %T", doc)
		require.Len(t, items, 1)
	})

	t.Run("human default non-empty result renders the status table", func(t *testing.T) {
		setAgentMode(t, false)

		loader := newAlertFixture(t, serveRules(testStatusGroups()))
		stdout, _, err := runCmdSplit(t, alert.NewGroupsStatusCommandForTest(loader),
			[]string{"status"}, "")
		require.NoError(t, err)

		// Byte-identical to the (unchanged) table codec rendering.
		var want bytes.Buffer
		require.NoError(t, (&alert.GroupsStatusTableCodec{}).Encode(&want, testStatusGroups()))
		assert.Equal(t, want.String(), stdout)
	})
}

// TestInstancesListOutputContract verifies the (already conformant) instances
// list command against the contract: one JSON value in agent mode, unchanged
// human table by default, explicit -o override honored.
func TestInstancesListOutputContract(t *testing.T) {
	t.Run("agent mode emits one JSON value", func(t *testing.T) {
		setAgentMode(t, true)

		loader := newAlertFixture(t, serveRules(testStatusGroups()))
		stdout, _, err := runCmdSplit(t, alert.NewInstancesListCommandForTest(loader),
			[]string{"list"}, "")
		require.NoError(t, err)

		doc := decodeSingleJSONDocument(t, stdout)
		items, ok := doc.([]any)
		require.True(t, ok, "instances result should be a JSON array, got %T", doc)
		require.Len(t, items, 1)
		record, ok := items[0].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "uid-1", record["ruleUid"])
	})

	t.Run("human default renders the instances table", func(t *testing.T) {
		setAgentMode(t, false)

		loader := newAlertFixture(t, serveRules(testStatusGroups()))
		stdout, _, err := runCmdSplit(t, alert.NewInstancesListCommandForTest(loader),
			[]string{"list"}, "")
		require.NoError(t, err)

		var want bytes.Buffer
		require.NoError(t, (&alert.InstancesTableCodec{}).Encode(&want, []alert.AlertInstanceRecord{{
			RuleUID:   "uid-1",
			RuleName:  "Rule 1",
			GroupName: "group-1",
			FolderUID: "folder-abc",
			State:     alert.StateFiring,
			ActiveAt:  "2024-01-15T09:00:00Z",
			Labels:    map[string]string{"severity": "critical"},
		}}))
		assert.Equal(t, want.String(), stdout)
	})

	t.Run("explicit -o yaml beats agents default in agent mode", func(t *testing.T) {
		setAgentMode(t, true)

		loader := newAlertFixture(t, serveRules(testStatusGroups()))
		stdout, _, err := runCmdSplit(t, alert.NewInstancesListCommandForTest(loader),
			[]string{"list", "-o", "yaml"}, "")
		require.NoError(t, err)

		var docs []map[string]any
		require.NoError(t, yaml.Unmarshal([]byte(stdout), &docs))
		require.Len(t, docs, 1)
		assert.Equal(t, "uid-1", docs[0]["ruleUid"])
		assert.False(t, json.Valid([]byte(stdout)), "-o yaml must not produce JSON, got: %q", stdout)
	})
}
