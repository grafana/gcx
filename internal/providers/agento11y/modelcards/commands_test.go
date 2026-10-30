package modelcards_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/grafana/gcx/internal/providers/agento11y/modelcards"
	"github.com/grafana/gcx/internal/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCommands_InvalidInput(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "get without key", args: []string{"get"}, want: "a model key, or --source with --source-model-id, is required"},
		{name: "get blank key", args: []string{"get", " "}, want: "is required"},
		{name: "get key and source", args: []string{"get", "openai/x", "--source", "openrouter", "--source-model-id", "x"}, want: "not both"},
		{name: "get source alone", args: []string{"get", "--source", "openrouter"}, want: "must be given together"},
		{name: "get source model id alone", args: []string{"get", "--source-model-id", "x"}, want: "must be given together"},
		{name: "list negative limit", args: []string{"list", "--limit", "-1"}, want: "--limit cannot be negative"},
		{name: "list negative context", args: []string{"list", "--min-context-length", "-1"}, want: "--min-context-length cannot be negative"},
		{name: "list bad sort", args: []string{"list", "--sort", "price"}, want: "--sort must be one of"},
		{name: "list bad order", args: []string{"list", "--order", "up"}, want: "--order must be asc or desc"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := modelcards.Commands(nil)
			cmd.SilenceUsage = true
			cmd.SilenceErrors = true
			cmd.SetArgs(tt.args)
			require.ErrorContains(t, cmd.Execute(), tt.want)
		})
	}
}

// A limited list that leaves cards behind must say so on stderr while stdout
// stays a plain list.
func TestList_WarnsWhenTruncated(t *testing.T) {
	home := testutils.SandboxConfigEnv(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, map[string]any{
			"data":        []map[string]any{{"model_key": "openai/a"}},
			"next_cursor": "more",
			"freshness":   map[string]any{"stale": false},
		})
	}))
	t.Cleanup(srv.Close)

	cfgPath := filepath.Join(home, ".config", "gcx", "config.yaml")
	require.NoError(t, os.MkdirAll(filepath.Dir(cfgPath), 0o755))
	cfg := fmt.Sprintf(`version: 1
stacks:
  main:
    grafana:
      server: %s
      token: test-token
      stack-id: 11111
contexts:
  default:
    stack: main
current-context: default
`, srv.URL)
	require.NoError(t, os.WriteFile(cfgPath, []byte(cfg), 0o600))

	cmd := modelcards.Commands(nil)
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{"list", "--limit", "1", "-o", "json"})
	require.NoError(t, cmd.ExecuteContext(context.Background()), "stderr: %s", stderr.String())

	var cards []map[string]any
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &cards))
	assert.Len(t, cards, 1)
	assert.Contains(t, stderr.String(), "truncated")
	assert.Contains(t, stderr.String(), "--limit")
}
