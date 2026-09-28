//nolint:testpackage // White-box: the codec and the next steps are unexported.
package login

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fatih/color"
	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/docs"
	internallogin "github.com/grafana/gcx/internal/login"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// agentModeForTest pins agent mode on or off, whatever harness runs the tests.
func agentModeForTest(t *testing.T, value string) {
	t.Helper()
	t.Setenv("GCX_AGENT_MODE", value)
	agent.ResetForTesting()
	t.Cleanup(agent.ResetForTesting)
}

// runSignupOutput is runSignup that also returns stdout, where the signup
// summary goes.
func runSignupOutput(t *testing.T, args ...string) (string, string, error) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	cmd := SignupCommand()
	cmd.SilenceErrors = true
	cmd.SilenceUsage = true
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs(args)
	err := cmd.ExecuteContext(t.Context())
	return stdout.String(), stderr.String(), err
}

// plainColor turns fatih/color off for the test, so golden output carries no
// escape codes whatever the terminal running the tests.
func plainColor(t *testing.T) {
	t.Helper()
	previous := color.NoColor
	color.NoColor = true
	t.Cleanup(func() { color.NoColor = previous })
}

func TestSignupTextCodec(t *testing.T) {
	plainColor(t)

	tests := []struct {
		name      string
		agentMode string
		result    LoginResult
		want      string
	}{
		{
			name:      "with version",
			agentMode: "false",
			result:    LoginResult{ContextName: "default", Server: "https://mystack.grafana.net", GrafanaVersion: "12.0.0", Cloud: true},
			want: "✔ Connected to your Grafana Cloud stack\n" +
				"\n" +
				"  Stack:    https://mystack.grafana.net\n" +
				"  Context:  default\n" +
				"  Version:  12.0.0\n",
		},
		{
			name:      "without version",
			agentMode: "false",
			result:    LoginResult{ContextName: "prod", Server: "https://prod.grafana.net", Cloud: true},
			want: "✔ Connected to your Grafana Cloud stack\n" +
				"\n" +
				"  Stack:    https://prod.grafana.net\n" +
				"  Context:  prod\n",
		},
		{
			name:      "agent mode has no check mark",
			agentMode: "true",
			result:    LoginResult{ContextName: "default", Server: "https://mystack.grafana.net", Cloud: true},
			want: "Connected to your Grafana Cloud stack\n" +
				"\n" +
				"  Stack:    https://mystack.grafana.net\n" +
				"  Context:  default\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			agentModeForTest(t, tt.agentMode)

			var buf bytes.Buffer
			require.NoError(t, (&signupTextCodec{}).Encode(&buf, tt.result))
			assert.Equal(t, tt.want, buf.String())
		})
	}

	t.Run("unsupported type", func(t *testing.T) {
		err := (&signupTextCodec{}).Encode(&bytes.Buffer{}, "not a result")
		assert.ErrorContains(t, err, "unsupported type string")
	})
}

func TestSignupNextSteps(t *testing.T) {
	cloudSteps := "Manage SLOs, Synthetic Monitoring, k6 and more with a Cloud Access Policy token"
	tests := []struct {
		name       string
		agentMode  string
		configFile string
		result     LoginResult
		want       []signupNextStep
	}{
		{
			name:   "new Grafana Cloud stack",
			result: LoginResult{ContextName: "default", Server: "https://mystack.grafana.net", Cloud: true},
			want: []signupNextStep{
				{Summary: "Connect your first app or service", Link: "https://mystack.grafana.net/connections/add-new-connection"},
				{Summary: "Check the connection anytime", Command: "gcx config check --context default"},
				{Summary: cloudSteps, Command: "gcx cloud login --context default --cloud-token <token>", Link: docs.HumanURL(docs.AccessPolicies)},
			},
		},
		{
			name:       "commands keep the context and the config file",
			configFile: "/tmp/my config.yaml",
			result:     LoginResult{ContextName: "my stack", Server: "https://mystack.grafana.net/", Cloud: true},
			want: []signupNextStep{
				{Summary: "Connect your first app or service", Link: "https://mystack.grafana.net/connections/add-new-connection"},
				{Summary: "Check the connection anytime", Command: "gcx config check --context 'my stack' --config '/tmp/my config.yaml'"},
				{Summary: cloudSteps, Command: "gcx cloud login --context 'my stack' --cloud-token <token> --config '/tmp/my config.yaml'", Link: docs.HumanURL(docs.AccessPolicies)},
			},
		},
		{
			name:   "no page link without https, no Cloud step with a Cloud token",
			result: LoginResult{ContextName: "local", Server: "http://localhost:3000", Cloud: true, HasCloudToken: true},
			want: []signupNextStep{
				{Summary: "Check the connection anytime", Command: "gcx config check --context local"},
			},
		},
		{
			name:      "agents get the Markdown docs page",
			agentMode: "true",
			result:    LoginResult{ContextName: "default", Server: "https://mystack.grafana.net", Cloud: true},
			want: []signupNextStep{
				{Summary: "Connect your first app or service", Link: "https://mystack.grafana.net/connections/add-new-connection"},
				{Summary: "Check the connection anytime", Command: "gcx config check --context default"},
				{Summary: cloudSteps, Command: "gcx cloud login --context default --cloud-token <token>", Link: docs.AccessPolicies},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			agentMode := tt.agentMode
			if agentMode == "" {
				agentMode = "false"
			}
			agentModeForTest(t, agentMode)
			flags := &loginOpts{signup: true}
			flags.Config.ConfigFile = tt.configFile
			assert.Equal(t, tt.want, signupNextSteps(flags, tt.result))
		})
	}
}

// TestSignupPrintsTheSummaryAndNextSteps pins what a person sees after a
// signup: the success summary on stdout, only once the connection is saved,
// and the next steps on stderr in place of gcx login's advisory prose.
func TestSignupPrintsTheSummaryAndNextSteps(t *testing.T) {
	signupEnvironment(t, "false")
	plainColor(t)
	browser := stubSignupBrowser(t, nil)
	path := filepath.Join(t.TempDir(), "config.yaml")

	stdout, stderr, err := runSignupOutput(t, "--config", path)
	require.NoError(t, err, stderr)

	assert.Contains(t, stdout, "✔ Connected to your Grafana Cloud stack")
	assert.Contains(t, stdout, "Stack:    "+browser.stack)
	assert.Contains(t, stdout, "Context:  default")
	assert.Contains(t, stdout, "Version:  12.0.0")
	assert.NotContains(t, stdout, "Logged in to")

	assert.Contains(t, stderr, "Approved in the browser. Checking the connection to "+browser.stack+"...")
	assert.NotContains(t, stderr, "Signed in to", "the summary is signup's one success line")
	assert.Contains(t, stderr, "Next steps")
	assert.Contains(t, stderr, "gcx config check --context default --config "+path)
	assert.Contains(t, stderr, "gcx cloud login --context default --cloud-token <token> --config "+path)
	assert.Contains(t, stderr, docs.HumanURL(docs.AccessPolicies))
	assert.NotContains(t, stderr, "Verify access anytime")
	assert.NotContains(t, stderr, "Add one with")
	assert.NotContains(t, stdout+stderr, "\x1b[")
}

// TestSignupStructuredOutputMatchesLogin pins that structured output is the
// LoginResult gcx login returns, with the next steps as hints on stderr.
func TestSignupStructuredOutputMatchesLogin(t *testing.T) {
	tests := []struct {
		name      string
		agentMode string
		args      []string
		decode    func([]byte, any) error
		wantHint  string
		wantDocs  string
	}{
		{name: "agent default", agentMode: "true", decode: json.Unmarshal, wantHint: `"class":"hint"`, wantDocs: docs.AccessPolicies},
		{name: "json", agentMode: "false", args: []string{"-o", "json"}, decode: json.Unmarshal, wantHint: "hint: ", wantDocs: docs.HumanURL(docs.AccessPolicies)},
		{name: "yaml", agentMode: "false", args: []string{"-o", "yaml"}, decode: yaml.Unmarshal, wantHint: "hint: ", wantDocs: docs.HumanURL(docs.AccessPolicies)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			signupEnvironment(t, tt.agentMode)
			browser := stubSignupBrowser(t, nil)
			path := filepath.Join(t.TempDir(), "config.yaml")

			stdout, stderr, err := runSignupOutput(t, append([]string{"--config", path}, tt.args...)...)
			require.NoError(t, err, stderr)

			var got LoginResult
			require.NoError(t, tt.decode([]byte(stdout), &got), stdout)
			assert.Equal(t, LoginResult{
				ContextName:    "default",
				Server:         browser.stack,
				AuthMethod:     "oauth",
				Cloud:          true,
				GrafanaVersion: "12.0.0",
			}, got)
			assert.NotContains(t, stdout, "Next steps")
			assert.NotContains(t, stderr, "Next steps")

			// Agent JSONL escapes the < and > of the token placeholder.
			var cloudHint string
			for line := range strings.SplitSeq(stderr, "\n") {
				if strings.Contains(line, "gcx cloud login --context default --cloud-token ") {
					cloudHint = line
				}
			}
			assert.Contains(t, cloudHint, tt.wantHint, stderr)
			assert.Contains(t, cloudHint, tt.wantDocs, "the hint must say where the token is created")
		})
	}
}

// TestSignupAgentModeTextIsPlainASCII pins the agent mode charset rule for the
// text signup prints: an agent that asks for -o text still gets plain ASCII.
func TestSignupAgentModeTextIsPlainASCII(t *testing.T) {
	signupEnvironment(t, "true")
	stubSignupBrowser(t, nil)
	path := filepath.Join(t.TempDir(), "config.yaml")

	stdout, stderr, err := runSignupOutput(t, "--config", path, "-o", "text")
	require.NoError(t, err, stderr)
	require.Contains(t, stdout, "Connected to your Grafana Cloud stack")
	require.Contains(t, stderr, "Next steps")

	for _, stream := range []struct{ name, text string }{{"stdout", stdout}, {"stderr", stderr}} {
		for i := range len(stream.text) {
			if stream.text[i] >= 0x80 {
				t.Fatalf("%s has a non-ASCII byte at %d: %q", stream.name, i, stream.text)
			}
		}
	}
}

// TestPrintSignupResultRendersEveryStep covers the rendering of each kind of
// step, including the page link that only an https stack gets: a plain list in
// text mode, and one hint per step otherwise.
func TestPrintSignupResultRendersEveryStep(t *testing.T) {
	plainColor(t)
	result := internallogin.Result{ContextName: "default", AuthMethod: "oauth", IsCloud: true}

	for _, tc := range []struct {
		format string
		want   []string
	}{
		{format: "text", want: []string{
			"Next steps\n",
			"  Connect your first app or service\n    https://mystack.grafana.net/connections/add-new-connection\n",
			"  Check the connection anytime\n    gcx config check --context default\n",
			"    gcx cloud login --context default --cloud-token <token>\n    " + docs.HumanURL(docs.AccessPolicies) + "\n",
		}},
		{format: "json", want: []string{
			"hint: Connect your first app or service: https://mystack.grafana.net/connections/add-new-connection\n",
			"hint: Check the connection anytime: gcx config check --context default\n",
			"token: " + docs.HumanURL(docs.AccessPolicies) + ": gcx cloud login --context default --cloud-token <token>\n",
		}},
	} {
		t.Run(tc.format, func(t *testing.T) {
			agentModeForTest(t, "false")
			flags := &loginOpts{signup: true}
			fs := pflag.NewFlagSet("test", pflag.ContinueOnError)
			flags.bindConfigAndOutputFlags(fs, &signupTextCodec{})
			require.NoError(t, fs.Parse([]string{"-o", tc.format}))
			require.NoError(t, flags.IO.Validate())

			var stdout, stderr bytes.Buffer
			cmd := &cobra.Command{}
			cmd.SetOut(&stdout)
			cmd.SetErr(&stderr)
			require.NoError(t, printSignupResult(cmd, flags, "https://mystack.grafana.net", result))
			for _, want := range tc.want {
				assert.Contains(t, stderr.String(), want)
			}
		})
	}
}
