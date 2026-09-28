//nolint:testpackage // White-box: the codec and the next steps are unexported.
package login

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/fatih/color"
	"github.com/grafana/gcx/internal/agent"
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
			// The Grafana version stays in structured output only.
			name:      "human",
			agentMode: "false",
			result:    LoginResult{ContextName: "default", Server: "https://mystack.grafana.net", GrafanaVersion: "12.0.0", Cloud: true},
			want: "✔ You're connected to Grafana Cloud\n" +
				"\n" +
				"  Stack     https://mystack.grafana.net\n" +
				"  Context   default\n",
		},
		{
			name:      "agent mode has no check mark",
			agentMode: "true",
			result:    LoginResult{ContextName: "default", Server: "https://mystack.grafana.net", Cloud: true},
			want: "You're connected to Grafana Cloud\n" +
				"\n" +
				"  Stack     https://mystack.grafana.net\n" +
				"  Context   default\n",
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

func TestStackBrowserURL(t *testing.T) {
	tests := []struct {
		name   string
		server string
		want   string
	}{
		{name: "Grafana Cloud stack", server: "https://mystack.grafana.net", want: "https://mystack.grafana.net"},
		{name: "kept as saved", server: "https://grafana.example.com/grafana", want: "https://grafana.example.com/grafana"},
		{name: "plain http", server: "http://localhost:3000"},
		{name: "not a URL, as when the context name stands in", server: "default"},
		{name: "empty", server: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, stackBrowserURL(tt.server))
		})
	}
}

// TestSignupPrintsTheSummaryWithoutAdvice pins what a person sees after a
// signup: the success summary on stdout, only once the connection is saved,
// and none of gcx login's advisory prose. Signup asks for no further
// credential. The stub stack is plain http, so it gets no page to open;
// TestPrintSignupResultRendersTheStep covers that step.
func TestSignupPrintsTheSummaryWithoutAdvice(t *testing.T) {
	signupEnvironment(t, "false")
	plainColor(t)
	browser := stubSignupBrowser(t, nil)
	path := filepath.Join(t.TempDir(), "config.yaml")

	stdout, stderr, err := runSignupOutput(t, "--config", path)
	require.NoError(t, err, stderr)

	assert.Contains(t, stdout, "✔ You're connected to Grafana Cloud")
	assert.Contains(t, stdout, "  Stack     "+browser.stack+"\n")
	assert.Contains(t, stdout, "  Context   default\n")
	assert.NotContains(t, stdout, "Version")
	assert.NotContains(t, stdout, "Logged in to")

	assert.Contains(t, stderr, "Approved in the browser. Checking the connection to "+browser.stack+"...")
	assert.NotContains(t, stderr, "Signed in to", "the summary is signup's one success line")
	assert.NotContains(t, stderr, "Next steps")
	for _, advice := range []string{"gcx config check", "gcx cloud login", "Cloud Access Policy", "add-new-connection", "Verify access anytime", "Add one with"} {
		assert.NotContains(t, stdout+stderr, advice)
	}
	assert.NotContains(t, stdout+stderr, "\x1b[")
}

// TestSignupStructuredOutputMatchesLogin pins that structured output is the
// LoginResult gcx login returns, and that no advice leaks into it.
func TestSignupStructuredOutputMatchesLogin(t *testing.T) {
	tests := []struct {
		name      string
		agentMode string
		args      []string
		decode    func([]byte, any) error
	}{
		{name: "agent default", agentMode: "true", decode: json.Unmarshal},
		{name: "json", agentMode: "false", args: []string{"-o", "json"}, decode: json.Unmarshal},
		{name: "yaml", agentMode: "false", args: []string{"-o", "yaml"}, decode: yaml.Unmarshal},
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
			assert.NotContains(t, stderr, "gcx cloud login")
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
	require.Contains(t, stdout, "You're connected to Grafana Cloud")

	for _, stream := range []struct{ name, text string }{{"stdout", stdout}, {"stderr", stderr}} {
		for i := range len(stream.text) {
			if stream.text[i] >= 0x80 {
				t.Fatalf("%s has a non-ASCII byte at %d: %q", stream.name, i, stream.text)
			}
		}
	}
}

// TestPrintSignupResultRendersTheStep covers the rendering of the way back to
// the stack: a plain list on stderr in text mode, a hint on stderr otherwise,
// never on stdout, and nothing at all, not even the heading, when a server has
// no page to open. Agent mode text stays plain ASCII.
func TestPrintSignupResultRendersTheStep(t *testing.T) {
	plainColor(t)
	result := internallogin.Result{ContextName: "default", AuthMethod: "oauth", IsCloud: true}
	const stack = "https://mystack.grafana.net"

	for _, tc := range []struct {
		name      string
		agentMode string
		format    string
		server    string
		want      string
		notWant   string
	}{
		{name: "text", agentMode: "false", format: "text", server: stack, want: "\nNext steps\n  Open Grafana\n    " + stack + "\n"},
		{name: "json", agentMode: "false", format: "json", server: stack, want: "hint: Open Grafana: " + stack + "\n"},
		{name: "agent", agentMode: "true", format: "agents", server: stack, want: `{"class":"hint","summary":"Open Grafana: ` + stack + `"}`},
		{name: "agent text", agentMode: "true", format: "text", server: stack, want: "\nNext steps\n  Open Grafana\n    " + stack + "\n"},
		{name: "text without a page", agentMode: "false", format: "text", server: "http://localhost:3000", notWant: "Next steps"},
		{name: "json without a page", agentMode: "false", format: "json", server: "http://localhost:3000", notWant: "hint:"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			agentModeForTest(t, tc.agentMode)
			flags := &loginOpts{signup: true}
			fs := pflag.NewFlagSet("test", pflag.ContinueOnError)
			flags.bindConfigAndOutputFlags(fs, &signupTextCodec{})
			require.NoError(t, fs.Parse([]string{"-o", tc.format}))
			require.NoError(t, flags.IO.Validate())

			var stdout, stderr bytes.Buffer
			cmd := &cobra.Command{}
			cmd.SetOut(&stdout)
			cmd.SetErr(&stderr)
			require.NoError(t, printSignupResult(cmd, flags, tc.server, result))
			if tc.want != "" {
				assert.Contains(t, stderr.String(), tc.want)
			}
			assert.NotContains(t, stdout.String(), "Open Grafana", "stdout carries the result only")
			if tc.notWant != "" {
				assert.NotContains(t, stderr.String(), tc.notWant)
			}
			if tc.agentMode == "true" {
				for i, b := range []byte(stdout.String() + stderr.String()) {
					require.Less(t, b, byte(0x80), "non-ASCII byte at %d", i)
				}
			}
		})
	}
}
