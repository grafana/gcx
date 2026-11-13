package metrics_test

// These tests pin the pre-GA agent output contract for the adaptive metrics
// command family: in agent mode a finite command emits EXACTLY ONE JSON value
// on stdout, the exit signal agrees with the outcome (partial failure =
// EmittedError carrying ExitPartialFailure), default human stdout stays
// byte-identical to the pre-migration output (empty for the stderr-prose
// mutation commands, nothing for empty lists), and explicit -o json/yaml
// always wins.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/gcxerrors"
	"github.com/grafana/gcx/internal/providers"
	metrics "github.com/grafana/gcx/internal/providers/metrics/adaptive"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newContractLoader starts one fake server answering both GCOM stack
// discovery and the Adaptive Metrics API (via handler), and returns a
// ConfigLoader wired to it through a temp config file so the real command
// tree runs end-to-end.
func newContractLoader(t *testing.T, handler http.HandlerFunc) *providers.ConfigLoader {
	t.Helper()

	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	mux.HandleFunc("/api/instances/", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"id":1,"hmInstancePromId":42,"hmInstancePromUrl":%q,"hlInstanceId":42,"hlInstanceUrl":%q,"htInstanceId":42,"htInstanceUrl":%q}`,
			srv.URL, srv.URL, srv.URL)
	})
	mux.HandleFunc("/", handler)

	cfgPath := filepath.Join(t.TempDir(), "config.yaml")
	cfg := fmt.Sprintf("contexts:\n  default:\n    cloud:\n      token: test-token\n      stack: teststack\n      api-url: %s\ncurrent-context: default\n", srv.URL)
	require.NoError(t, os.WriteFile(cfgPath, []byte(cfg), 0o600))

	loader := &providers.ConfigLoader{}
	loader.SetConfigFile(cfgPath)
	return loader
}

type cmdResult struct {
	stdout string
	stderr string
	err    error
}

// runAdaptiveCmd executes the real adaptive metrics command tree with the
// given agent-mode state. Agent mode must be set before the tree is built
// because output defaults are resolved at flag-binding time.
func runAdaptiveCmd(t *testing.T, loader *providers.ConfigLoader, agentMode bool, args ...string) cmdResult {
	t.Helper()

	prior := agent.IsAgentMode()
	agent.SetFlag(agentMode)
	t.Cleanup(func() { agent.SetFlag(prior) })

	root := metrics.Commands(loader)
	// Mirror the production root (cmd/gcx/root): errors and usage are
	// reported by the caller, never printed into the output streams.
	root.SilenceUsage = true
	root.SilenceErrors = true
	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetIn(strings.NewReader(""))
	root.SetArgs(args)
	err := root.Execute()
	return cmdResult{stdout: stdout.String(), stderr: stderr.String(), err: err}
}

// decodeOneJSONValue asserts stdout carries exactly one JSON value followed
// by EOF, and returns the decoded value.
func decodeOneJSONValue(t *testing.T, stdout string) any {
	t.Helper()
	dec := json.NewDecoder(strings.NewReader(stdout))
	var first any
	require.NoError(t, dec.Decode(&first), "stdout is not valid JSON: %q", stdout)
	var second any
	require.ErrorIs(t, dec.Decode(&second), io.EOF, "stdout must contain exactly one JSON value: %q", stdout)
	return first
}

const rulesV2Prefix = "/adaptive-metrics/api/v2/segments/default/rules"

// fakeMetricsAPI serves the Adaptive Metrics endpoints exercised by the
// delete and list commands.
func fakeMetricsAPI() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, rulesV2Prefix+"/"):
			if r.Method == http.MethodDelete {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, `{"metric":%q,"action":"drop"}`, strings.TrimPrefix(r.URL.Path, rulesV2Prefix+"/"))
		case r.URL.Path == rulesV2Prefix:
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"rules":[]}`)
		case r.URL.Path == "/aggregations/recommendations":
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, "[]")
		case r.URL.Path == "/aggregations/rules/segments" && r.Method == http.MethodGet:
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, "[]")
		case r.URL.Path == "/aggregations/rules/segments" && r.Method == http.MethodDelete:
			w.WriteHeader(http.StatusNoContent)
		case r.URL.Path == "/v1/recommendations/segmented_exemptions":
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, "[]")
		case strings.HasPrefix(r.URL.Path, "/v1/recommendations/exemptions"):
			if r.Method == http.MethodDelete {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"result":[]}`)
		default:
			http.NotFound(w, r)
		}
	}
}

func newApplyFake(t *testing.T) *fakeRules {
	t.Helper()
	removeA := fakeRec{Action: "remove", CurrentSeries: 10}
	return newFakeRules(t).
		seed("default", ruleA, ruleB).
		recommend("default", "a_metric", removeA).
		recommend("default", "b_metric", fakeRec{Action: "keep"})
}

func TestRecommendationsApply_OutputContract(t *testing.T) {
	tests := []struct {
		name            string
		agentMode       bool
		args            []string
		warnings        []fakeApplyWarning
		wantExitCode    int // 0 = plain nil error expected
		wantStdoutEmpty bool
		wantSucceeded   float64
		wantFailed      float64
		wantSkipped     float64
		wantDryRun      bool
	}{
		{
			name:          "agent success emits one batch doc",
			agentMode:     true,
			args:          []string{"recommendations", "apply", "a_metric", "b_metric", "--force"},
			wantSucceeded: 1,
			wantSkipped:   1,
		},
		{
			name:          "agent dry-run emits one doc with dry_run",
			agentMode:     true,
			args:          []string{"recommendations", "apply", "a_metric", "--dry-run"},
			wantSucceeded: 1,
			wantDryRun:    true,
		},
		{
			name:          "agent all emits one batch doc",
			agentMode:     true,
			args:          []string{"recommendations", "apply", "--all", "--force"},
			wantSucceeded: 1,
			wantSkipped:   1,
		},
		{
			name:          "agent all dry-run emits one doc with dry_run",
			agentMode:     true,
			args:          []string{"recommendations", "apply", "--all", "--dry-run"},
			wantSucceeded: 1,
			wantDryRun:    true,
		},
		{
			name:          "agent all with skipped changes emits one doc and exit 4",
			agentMode:     true,
			args:          []string{"recommendations", "apply", "--all", "--force"},
			warnings:      []fakeApplyWarning{{Code: "rule_limit_reached", Skipped: 1}},
			wantExitCode:  gcxerrors.ExitPartialFailure,
			wantSucceeded: 0,
			wantFailed:    1,
			wantSkipped:   1,
		},
		{
			name:            "human default stdout stays empty",
			args:            []string{"recommendations", "apply", "a_metric", "--force"},
			wantStdoutEmpty: true,
		},
		{
			name:            "human all with skipped changes stdout empty and exit 4",
			args:            []string{"recommendations", "apply", "--all", "--force"},
			warnings:        []fakeApplyWarning{{Code: "policy_limited", Skipped: 1}},
			wantExitCode:    gcxerrors.ExitPartialFailure,
			wantStdoutEmpty: true,
		},
		{
			name:            "human dry-run stdout stays empty",
			args:            []string{"recommendations", "apply", "a_metric", "--dry-run"},
			wantStdoutEmpty: true,
		},
		{
			name:          "human explicit -o json wins",
			args:          []string{"recommendations", "apply", "a_metric", "--force", "-o", "json"},
			wantSucceeded: 1,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newApplyFake(t)
			f.applyWarnings = tc.warnings
			res := runAdaptiveCmd(t, f.loader(t, fakeConfig{grafana: true}), tc.agentMode, tc.args...)

			if tc.wantExitCode != 0 {
				var emitted *gcxerrors.EmittedError
				require.ErrorAs(t, res.err, &emitted,
					"error = %T (%v), want *gcxerrors.EmittedError", res.err, res.err)
				assert.Equal(t, tc.wantExitCode, emitted.Code)
			} else {
				require.NoError(t, res.err, "stderr: %s", res.stderr)
			}

			if tc.wantStdoutEmpty {
				assert.Empty(t, res.stdout, "default human stdout must stay byte-identical (empty)")
				return
			}

			doc, ok := decodeOneJSONValue(t, res.stdout).(map[string]any)
			require.True(t, ok, "stdout document must be an object")
			assert.Equal(t, "gcx.mutation_batch", doc["type"])
			assert.Equal(t, "1", doc["schema_version"])
			assert.Equal(t, "applied", doc["action"])

			summary, ok := doc["summary"].(map[string]any)
			require.True(t, ok, "document must carry a summary object")
			assert.InDelta(t, tc.wantSucceeded, summary["succeeded"], 0)
			assert.InDelta(t, tc.wantFailed, summary["failed"], 0)
			if tc.wantSkipped > 0 {
				assert.InDelta(t, tc.wantSkipped, summary["skipped"], 0)
			}
			if tc.wantFailed > 0 {
				failures, ok := doc["failures"].([]any)
				require.True(t, ok, "failures must be present")
				require.Len(t, failures, int(tc.wantFailed))
				failure, ok := failures[0].(map[string]any)
				require.True(t, ok, "failure entry must be an object")
				assert.NotEmpty(t, failure["error"])
			}
			if tc.wantDryRun {
				assert.Equal(t, true, doc["dry_run"])
			} else {
				assert.Nil(t, doc["dry_run"])
			}
		})
	}
}

// TestRecommendationsApply_TotalFailure pins the total-failure contract:
// when nothing was applied, the command keeps the classified single-error
// path — no success-shaped batch document on stdout and no EmittedError (the
// reporter owns the error document and the exit code).
func TestRecommendationsApply_TotalFailure(t *testing.T) {
	for _, tc := range []struct {
		name     string
		args     []string
		failPost bool
		want     string
	}{
		{name: "listed metrics rejected by the server", args: []string{"recommendations", "apply", "a_metric", "--force"}, failPost: true, want: "boom"},
		{name: "all rejected by the server", args: []string{"recommendations", "apply", "--all", "--force"}, failPost: true, want: "boom"},
		{name: "metric without a recommendation", args: []string{"recommendations", "apply", "nope", "--force"}, want: "no recommendation found"},
	} {
		for _, agentMode := range []bool{true, false} {
			t.Run(fmt.Sprintf("%s agent=%v", tc.name, agentMode), func(t *testing.T) {
				f := newApplyFake(t)
				if tc.failPost {
					f.fail(viaProxy, "POST", 500, `{"code":"internal","error":"boom"}`)
				}
				res := runAdaptiveCmd(t, f.loader(t, fakeConfig{grafana: true}), agentMode, tc.args...)

				require.Error(t, res.err)
				var emitted *gcxerrors.EmittedError
				assert.NotErrorAs(t, res.err, &emitted,
					"total failure must use the standard error path, not EmittedError")
				assert.Empty(t, res.stdout,
					"no success-shaped batch document when nothing was applied — the reporter owns the error document")
				assert.Contains(t, res.err.Error(), tc.want)
			})
		}
	}
}

func TestAdaptiveLists_EmptyStillEmitsOneDoc(t *testing.T) {
	lists := []struct {
		name string
		args []string
	}{
		{name: "recommendations list", args: []string{"recommendations", "list"}},
		{name: "rules list", args: []string{"rules", "list"}},
		{name: "segments list", args: []string{"segments", "list"}},
		{name: "exemptions list", args: []string{"exemptions", "list"}},
		{name: "exemptions list --all-segments", args: []string{"exemptions", "list", "--all-segments"}},
	}

	for _, tc := range lists {
		t.Run(tc.name, func(t *testing.T) {
			t.Run("agent mode emits exactly one empty array", func(t *testing.T) {
				loader := newContractLoader(t, fakeMetricsAPI())
				res := runAdaptiveCmd(t, loader, true, tc.args...)
				require.NoError(t, res.err, "stderr: %s", res.stderr)
				doc := decodeOneJSONValue(t, res.stdout)
				arr, ok := doc.([]any)
				require.True(t, ok, "stdout document must be an array, got %T", doc)
				assert.Empty(t, arr)
			})

			t.Run("human default stdout stays empty", func(t *testing.T) {
				loader := newContractLoader(t, fakeMetricsAPI())
				res := runAdaptiveCmd(t, loader, false, tc.args...)
				require.NoError(t, res.err, "stderr: %s", res.stderr)
				assert.Empty(t, res.stdout, "default human stdout for an empty list must stay byte-identical (empty)")
			})

			t.Run("human explicit -o json emits one empty array", func(t *testing.T) {
				loader := newContractLoader(t, fakeMetricsAPI())
				res := runAdaptiveCmd(t, loader, false, append(append([]string{}, tc.args...), "-o", "json")...)
				require.NoError(t, res.err, "stderr: %s", res.stderr)
				doc := decodeOneJSONValue(t, res.stdout)
				_, ok := doc.([]any)
				require.True(t, ok, "stdout document must be an array, got %T", doc)
			})
		})
	}
}

func TestAdaptiveDeletes_MutationDocContract(t *testing.T) {
	deletes := []struct {
		name         string
		args         []string
		wantName     string // expected target.name ("" = unset)
		wantID       string // expected target.id ("" = unset)
		wantKind     string
		wantStderrIn string
	}{
		{
			name:         "rules delete",
			args:         []string{"rules", "delete", "foo"},
			wantName:     "foo",
			wantKind:     "rule",
			wantStderrIn: "Deleted rule for foo.",
		},
		{
			name:         "segments delete",
			args:         []string{"segments", "delete", "seg1"},
			wantID:       "seg1",
			wantKind:     "segment",
			wantStderrIn: "Deleted segment seg1.",
		},
		{
			name:         "exemptions delete",
			args:         []string{"exemptions", "delete", "ex1"},
			wantID:       "ex1",
			wantKind:     "exemption",
			wantStderrIn: "Deleted exemption ex1.",
		},
	}

	for _, tc := range deletes {
		t.Run(tc.name, func(t *testing.T) {
			forceArgs := append(append([]string{}, tc.args...), "--force")

			t.Run("agent mode emits one mutation doc", func(t *testing.T) {
				loader := newContractLoader(t, fakeMetricsAPI())
				res := runAdaptiveCmd(t, loader, true, forceArgs...)
				require.NoError(t, res.err, "stderr: %s", res.stderr)

				doc, ok := decodeOneJSONValue(t, res.stdout).(map[string]any)
				require.True(t, ok, "stdout document must be an object")
				assert.Equal(t, "gcx.mutation", doc["type"])
				assert.Equal(t, "1", doc["schema_version"])
				assert.Equal(t, "deleted", doc["action"])
				assert.Equal(t, true, doc["changed"])

				target, ok := doc["target"].(map[string]any)
				require.True(t, ok, "target must be an object")
				assert.Equal(t, tc.wantKind, target["kind"])
				if tc.wantName != "" {
					assert.Equal(t, tc.wantName, target["name"])
				}
				if tc.wantID != "" {
					assert.Equal(t, tc.wantID, target["id"])
				}
			})

			t.Run("human default stdout stays empty", func(t *testing.T) {
				loader := newContractLoader(t, fakeMetricsAPI())
				res := runAdaptiveCmd(t, loader, false, forceArgs...)
				require.NoError(t, res.err, "stderr: %s", res.stderr)
				assert.Empty(t, res.stdout, "default human stdout must stay byte-identical (empty)")
				assert.Contains(t, res.stderr, tc.wantStderrIn)
			})

			t.Run("explicit -o yaml wins", func(t *testing.T) {
				loader := newContractLoader(t, fakeMetricsAPI())
				res := runAdaptiveCmd(t, loader, false, append(append([]string{}, forceArgs...), "-o", "yaml")...)
				require.NoError(t, res.err, "stderr: %s", res.stderr)
				assert.Contains(t, res.stdout, "type: gcx.mutation")
				assert.Contains(t, res.stdout, "action: deleted")
			})

			t.Run("agent mode without --force is rejected before mutation", func(t *testing.T) {
				loader := newContractLoader(t, fakeMetricsAPI())
				res := runAdaptiveCmd(t, loader, true, tc.args...)
				require.ErrorIs(t, res.err, providers.ErrAgentModeRequiresForce)
				assert.Empty(t, res.stdout)
			})
		})
	}
}
