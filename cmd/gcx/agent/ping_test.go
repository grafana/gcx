package agent_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/grafana/gcx/cmd/gcx/agent"
	"github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPingCommand(t *testing.T) {
	for _, tc := range []struct {
		name      string
		args      []string
		agentMode bool
		wantText  string
	}{
		{name: "human default", wantText: "Ping accepted by Grafana\n"},
		{name: "agent default", agentMode: true},
		{name: "explicit json", args: []string{"-o", "json"}},
		{name: "explicit text", agentMode: true, args: []string{"-o", "text"}, wantText: "Ping accepted by Grafana\n"},
		{name: "metadata overrides", args: []string{"--title", `{"nested":true}`, "--host", "@host.txt", "--agent-name", "123", "--body", `Body: "quoted" @file {"literal":true}`}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			testutils.SandboxConfigEnv(t)
			testutils.SetAgentMode(t, tc.agentMode)
			var received map[string]any
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/bootdata" {
					w.WriteHeader(http.StatusNotFound)
					return
				}
				calls++
				assert.Equal(t, http.MethodPost, r.Method)
				assert.Equal(t, "/api/plugins/grafana-irm-app/resources/mobile_notifications/self", r.URL.Path)
				assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
				user, password, ok := r.BasicAuth()
				assert.True(t, ok)
				assert.Equal(t, "test-user", user)
				assert.Equal(t, "test-password", password)
				assert.NoError(t, json.NewDecoder(r.Body).Decode(&received))
				w.WriteHeader(http.StatusNoContent)
			}))
			defer server.Close()
			path := filepath.Join(t.TempDir(), "config.yaml")
			content := fmt.Sprintf(`version: 1
stacks:
  phone:
    grafana:
      server: %s
      org-id: 1
      auth-method: basic
      user: test-user
      password: test-password
contexts:
  phone: {stack: phone}
  other: {stack: phone}
current-context: other
`, server.URL)
			require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
			cmd := agent.Command()
			args := []string{"ping", "--text", "Ready: \"quoted\"\nsecond line 🔔", "--config", path, "--context", "phone"}
			// The override case also exercises structured output.
			if tc.name == "metadata overrides" {
				args = append(args, "-o", "json")
			}
			cmd.SetArgs(append(args, tc.args...))
			var stdout, stderr bytes.Buffer
			cmd.SetOut(&stdout)
			cmd.SetErr(&stderr)
			require.NoError(t, cmd.Execute())
			assert.Equal(t, 1, calls)
			assert.Empty(t, stderr.String())
			assert.Equal(t, "Ready: \"quoted\"\nsecond line 🔔", received["text"])
			assert.Equal(t, "agents", received["inbox"])
			if tc.name == "metadata overrides" {
				assert.Equal(t, "@host.txt", received["host"])
				assert.Equal(t, "123", received["agent"])
				assert.Equal(t, `{"nested":true}`, received["title"])
				assert.Equal(t, `Body: "quoted" @file {"literal":true}`, received["body"])
			} else {
				host, err := os.Hostname()
				require.NoError(t, err)
				assert.Equal(t, host, received["host"])
				assert.Equal(t, "gcx", received["agent"])
				assert.Equal(t, "Agent ping", received["title"])
				assert.NotContains(t, received, "body")
			}
			if tc.wantText != "" {
				assert.Equal(t, tc.wantText, stdout.String())
			} else {
				var result output.SingleMutation
				decoder := json.NewDecoder(&stdout)
				require.NoError(t, decoder.Decode(&result))
				assert.Equal(t, "gcx.mutation", result.Type)
				assert.Equal(t, "1", result.SchemaVersion)
				assert.Equal(t, "accepted", result.Action)
				assert.Equal(t, "self", result.Target.Name)
				assert.ErrorIs(t, decoder.Decode(&result), io.EOF)
			}
		})
	}
}

func TestPingValidationBeforeConfigLoad(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{"missing text", nil, "--text must not be empty"},
		{"positional message", []string{"hello"}, "unknown command"},
		{"blank text", []string{"--text", " \n"}, "--text must not be empty"},
		{"empty title", []string{"--text", "hello", "--title="}, "--title must not be empty"},
		{"empty host", []string{"--text", "hello", "--host="}, "--host must not be empty"},
		{"empty agent", []string{"--text", "hello", "--agent-name= "}, "--agent-name must not be empty"},
		{"empty inbox", []string{"--text", "hello", "--inbox="}, "only agents is supported"},
		{"unsupported inbox", []string{"--text", "hello", "--inbox=alerts"}, "only agents is supported"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			testutils.SandboxConfigEnv(t)
			cmd := agent.Command()
			cmd.SetOut(io.Discard)
			cmd.SetErr(io.Discard)
			args := make([]string, 0, 3+len(tc.args))
			args = append(args, "ping", "--config", filepath.Join(t.TempDir(), "missing.yaml"))
			cmd.SetArgs(append(args, tc.args...))
			require.ErrorContains(t, cmd.Execute(), tc.want)
		})
	}
}
