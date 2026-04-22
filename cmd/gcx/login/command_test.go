// White-box tests: most targets (structuredMissingFieldsError,
// structuredClarificationError, loginTextCodec, loginOpts.Validate) are
// unexported. Using the _test package would require exporting them solely
// for tests, which the project avoids.
//
//nolint:testpackage // see comment above
package login

import (
	"bytes"
	"strings"
	"testing"

	configcmd "github.com/grafana/gcx/cmd/gcx/config"
	"github.com/grafana/gcx/cmd/gcx/fail"
	"github.com/grafana/gcx/internal/agent"
	internallogin "github.com/grafana/gcx/internal/login"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestStructuredMissingFieldsError verifies that structuredMissingFieldsError
// maps ErrNeedInput.Fields into a DetailedError whose suggestions mention
// the expected flags/env vars for each field.
func TestStructuredMissingFieldsError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		err            *internallogin.ErrNeedInput
		wantSummary    string
		wantDetailSubs []string
		wantSuggestSub []string
	}{
		{
			name:           "missing_server_only",
			err:            &internallogin.ErrNeedInput{Fields: []string{"server"}},
			wantSummary:    "Login requires additional input",
			wantDetailSubs: []string{"server"},
			wantSuggestSub: []string{"--server", "GRAFANA_SERVER"},
		},
		{
			name:           "missing_grafana_auth",
			err:            &internallogin.ErrNeedInput{Fields: []string{"grafana-auth"}},
			wantSummary:    "Login requires additional input",
			wantDetailSubs: []string{"grafana-auth"},
			wantSuggestSub: []string{"--token"},
		},
		{
			name:           "missing_cloud_token",
			err:            &internallogin.ErrNeedInput{Fields: []string{"cloud-token"}},
			wantSummary:    "Login requires additional input",
			wantDetailSubs: []string{"cloud-token"},
			wantSuggestSub: []string{"--cloud-token", "--yes"},
		},
		{
			name: "multiple_fields_with_hint",
			err: &internallogin.ErrNeedInput{
				Fields: []string{"server", "grafana-auth"},
				Hint:   "connect to your Grafana instance first",
			},
			wantSummary:    "Login requires additional input",
			wantDetailSubs: []string{"server", "grafana-auth", "connect to your Grafana instance first"},
			wantSuggestSub: []string{"--server", "--token"},
		},
		{
			name:           "unknown_field_fallback",
			err:            &internallogin.ErrNeedInput{Fields: []string{"some_custom_field"}},
			wantSummary:    "Login requires additional input",
			wantDetailSubs: []string{"some_custom_field"},
			wantSuggestSub: []string{"--some-custom-field"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := structuredMissingFieldsError(tt.err)
			require.Error(t, err)

			var det fail.DetailedError
			require.ErrorAs(t, err, &det, "expected fail.DetailedError, got %T", err)

			assert.Equal(t, tt.wantSummary, det.Summary)
			for _, sub := range tt.wantDetailSubs {
				assert.Contains(t, det.Details, sub, "details should mention %q", sub)
			}
			joined := strings.Join(det.Suggestions, "\n")
			for _, sub := range tt.wantSuggestSub {
				assert.Contains(t, joined, sub, "suggestions should mention %q", sub)
			}
		})
	}
}

// TestStructuredClarificationError verifies that structuredClarificationError
// returns the right DetailedError variant for each Field ("allow-override",
// "save-unvalidated", ambiguous target).
func TestStructuredClarificationError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		err            *internallogin.ErrNeedClarification
		wantSummary    string
		wantDetailSubs []string
		wantSuggestSub []string
	}{
		{
			name: "allow_override",
			err: &internallogin.ErrNeedClarification{
				Field:    "allow-override",
				Question: "Context \"prod\" already exists. Overwrite?",
			},
			wantSummary:    "Login would overwrite an existing context",
			wantDetailSubs: []string{"prod", "Overwrite"},
			wantSuggestSub: []string{"--yes"},
		},
		{
			name: "save_unvalidated",
			err: &internallogin.ErrNeedClarification{
				Field:    "save-unvalidated",
				Question: "Connectivity failed: context deadline exceeded.",
			},
			wantSummary:    "Connectivity validation failed",
			wantDetailSubs: []string{"Connectivity failed"},
			wantSuggestSub: []string{"Re-run interactively", "server URL"},
		},
		{
			name: "ambiguous_cloud_vs_onprem",
			err: &internallogin.ErrNeedClarification{
				Field:    "target",
				Question: "Is this a Grafana Cloud instance or an on-premises Grafana?",
				Choices:  []string{"cloud", "on-prem"},
			},
			wantSummary:    "Login requires clarification",
			wantDetailSubs: []string{"Cloud", "on-prem"},
			wantSuggestSub: []string{"--cloud", "--yes"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := structuredClarificationError(tt.err)
			require.Error(t, err)

			var det fail.DetailedError
			require.ErrorAs(t, err, &det, "expected fail.DetailedError, got %T", err)

			assert.Equal(t, tt.wantSummary, det.Summary)
			for _, sub := range tt.wantDetailSubs {
				assert.Contains(t, det.Details, sub, "details should mention %q", sub)
			}
			joined := strings.Join(det.Suggestions, "\n")
			for _, sub := range tt.wantSuggestSub {
				assert.Contains(t, joined, sub, "suggestions should mention %q", sub)
			}
		})
	}
}

// TestLoginOptsValidate covers the F4 arg-vs-flag conflict detection.
// Validate() must error when positional CONTEXT_NAME and --context are both
// set, and succeed otherwise.
func TestLoginOptsValidate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		args          []string
		contextFlag   string
		wantErr       bool
		wantErrSubstr string
	}{
		{
			name:          "conflict_positional_and_flag",
			args:          []string{"prod"},
			contextFlag:   "staging",
			wantErr:       true,
			wantErrSubstr: "conflicting context specification",
		},
		{
			name:        "only_positional",
			args:        []string{"prod"},
			contextFlag: "",
			wantErr:     false,
		},
		{
			name:        "only_flag",
			args:        []string{},
			contextFlag: "staging",
			wantErr:     false,
		},
		{
			name:        "neither",
			args:        []string{},
			contextFlag: "",
			wantErr:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			opts := &loginOpts{
				Config: configcmd.Options{Context: tt.contextFlag},
			}
			// Bind flags into a throwaway FlagSet so IO.Validate() has a
			// populated flag set to inspect (otherwise --json handling is
			// a no-op, which is fine for these cases).
			opts.IO.RegisterCustomCodec("text", &loginTextCodec{})
			opts.IO.DefaultFormat("text")
			opts.IO.BindFlags(pflag.NewFlagSet("test", pflag.ContinueOnError))

			err := opts.Validate(tt.args)
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErrSubstr)
				return
			}
			require.NoError(t, err)
		})
	}
}

// TestPrintResult_TextCodec is a golden comparison that confirms the text
// codec produces the expected multi-line summary for representative
// LoginResult fixtures, and that the CAP-token advisory goes to stderr
// (never stdout) so JSON/YAML consumers receive a clean stream.
//
// Intentionally not run with t.Parallel(): subtests mutate process-level env
// vars via t.Setenv (to disable agent-mode detection that would otherwise
// flip the default codec to json), and t.Setenv is incompatible with parallel
// tests.
func TestPrintResult_TextCodec(t *testing.T) {
	tests := []struct {
		name           string
		server         string
		result         internallogin.Result
		wantStdout     string
		wantStderrSubs []string
		noStderr       bool
	}{
		{
			name:   "cloud_with_cap_token",
			server: "https://mystack.grafana.net",
			result: internallogin.Result{
				ContextName:    "mystack",
				AuthMethod:     "oauth",
				IsCloud:        true,
				HasCloudToken:  true,
				GrafanaVersion: "12.0.0",
				StackSlug:      "mystack",
			},
			wantStdout: `Logged in to https://mystack.grafana.net
  Context:     mystack
  Auth method: oauth
  Version:     12.0.0
  Grafana Cloud: yes
  Stack:       mystack
`,
			noStderr: true,
		},
		{
			name:   "cloud_without_cap_token_emits_advisory_on_stderr",
			server: "https://stack.grafana.net",
			result: internallogin.Result{
				ContextName:   "stack",
				AuthMethod:    "token",
				IsCloud:       true,
				HasCloudToken: false,
				StackSlug:     "stack",
			},
			wantStdout: `Logged in to https://stack.grafana.net
  Context:     stack
  Auth method: token
  Grafana Cloud: yes
  Stack:       stack
`,
			wantStderrSubs: []string{
				"Note: Cloud API commands require a Cloud Access Policy token.",
				"gcx login --context stack",
			},
		},
		{
			name:   "onprem_no_advisory",
			server: "https://grafana.local",
			result: internallogin.Result{
				ContextName:    "local",
				AuthMethod:     "token",
				IsCloud:        false,
				GrafanaVersion: "11.5.0",
			},
			wantStdout: `Logged in to https://grafana.local
  Context:     local
  Auth method: token
  Version:     11.5.0
  Grafana Cloud: no
`,
			noStderr: true,
		},
		{
			name:   "empty_server_falls_back_to_context_name",
			server: "",
			result: internallogin.Result{
				ContextName: "prod",
				AuthMethod:  "token",
				IsCloud:     false,
			},
			wantStdout: `Logged in to prod
  Context:     prod
  Auth method: token
  Grafana Cloud: no
`,
			noStderr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// No t.Parallel() — this subtest sets env vars via t.Setenv to
			// disable agent-mode detection, which would otherwise flip the
			// default output format to "json" and break the golden-text
			// comparisons. t.Setenv is incompatible with t.Parallel().
			disableAgentMode(t)

			cmd := &cobra.Command{}
			var stdout, stderr bytes.Buffer
			cmd.SetOut(&stdout)
			cmd.SetErr(&stderr)

			ioOpts := &cmdio.Options{}
			ioOpts.RegisterCustomCodec("text", &loginTextCodec{})
			ioOpts.DefaultFormat("text")
			fs := pflag.NewFlagSet("test", pflag.ContinueOnError)
			ioOpts.BindFlags(fs)
			require.NoError(t, ioOpts.Validate())

			err := printResult(cmd, ioOpts, tt.server, tt.result)
			require.NoError(t, err)

			assert.Equal(t, tt.wantStdout, stdout.String(), "stdout mismatch")
			if tt.noStderr {
				assert.Empty(t, stderr.String(), "expected no stderr output")
			} else {
				for _, sub := range tt.wantStderrSubs {
					assert.Contains(t, stderr.String(), sub, "stderr should contain %q", sub)
				}
			}
		})
	}
}

// disableAgentMode forces agent.IsAgentMode() to return false for the
// duration of the test, regardless of the host env (e.g. `CLAUDECODE=1`
// during local agent sessions). Without this, cmdio.BindFlags would flip
// the default output format to "json" and break golden text comparisons.
//
// It unsets every known agent env var, sets GCX_AGENT_MODE=false (which
// takes priority over all others), and calls agent.ResetForTesting to
// re-derive the cached package-level state. On cleanup, it restores the
// original env and re-detects so subsequent tests in the process see the
// original agent-mode value.
func disableAgentMode(t *testing.T) {
	t.Helper()
	// t.Setenv handles both set-and-restore for us. Clearing every known
	// agent env var covers CLAUDECODE, CURSOR_AGENT, etc. in one pass.
	for _, v := range []string{
		"GCX_AGENT_MODE",
		"CLAUDECODE",
		"CLAUDE_CODE",
		"CURSOR_AGENT",
		"GITHUB_COPILOT",
		"AMAZON_Q",
		"OPENCODE",
	} {
		t.Setenv(v, "")
	}
	// GCX_AGENT_MODE=false is the authoritative override.
	t.Setenv("GCX_AGENT_MODE", "false")
	agent.ResetForTesting()
	t.Cleanup(agent.ResetForTesting)
}
