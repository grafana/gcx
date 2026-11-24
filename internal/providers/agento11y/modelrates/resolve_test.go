package modelrates_test

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

	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/providers/agento11y/modelrates"
	"github.com/grafana/gcx/internal/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolve(t *testing.T) {
	for _, at := range []string{"", "2026-10-01T01:02:03.123400Z"} {
		t.Run(at, func(t *testing.T) {
			client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodPost, r.Method)
				assert.Equal(t, "/api/plugins/grafana-agento11y-app/resources/model-rates:resolve", r.URL.Path)
				var body modelrates.ResolveRequest
				if !assert.NoError(t, json.NewDecoder(r.Body).Decode(&body)) {
					return
				}
				assert.Equal(t, []modelrates.ResolveItem{{Provider: "synthetic", Model: "test", At: at}}, body.Items)
				writeJSON(w, map[string]any{"items": []map[string]any{{"provider": "synthetic", "model": "test", "at": at, "source": "tenant", "pricing": map[string]any{"prompt_usd_per_token": 0.000001, "future_dimension": 7}}}})
			}))
			result, err := client.Resolve(context.Background(), modelrates.ResolveItem{Provider: "synthetic", Model: "test", At: at})
			require.NoError(t, err)
			assert.Equal(t, "tenant", result.Source)
			assert.Contains(t, string(result.Pricing), "future_dimension")
		})
	}
}

func TestResolveRejectsInvalidFlags(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		wantErr string
	}{
		{name: "missing provider", args: []string{"resolve"}, wantErr: "--provider is required"},
		{name: "missing model", args: []string{"resolve", "--provider", "p"}, wantErr: "--model is required"},
		{name: "bad at", args: []string{"resolve", "--provider", "p", "--model", "m", "--at", "bad"}, wantErr: "--at must be RFC3339"},
		{name: "empty at", args: []string{"resolve", "--provider", "p", "--model", "m", "--at", ""}, wantErr: "--at cannot be empty"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := modelrates.Commands(&providers.ConfigLoader{})
			cmd.SilenceUsage = true
			cmd.SilenceErrors = true
			cmd.SetArgs(tt.args)
			require.ErrorContains(t, cmd.Execute(), tt.wantErr)
		})
	}
}

func runResolve(t *testing.T, handler http.Handler, args ...string) string {
	t.Helper()
	home := testutils.SandboxConfigEnv(t)
	srv := httptest.NewServer(handler)
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

	cmd := modelrates.Commands(&providers.ConfigLoader{})
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs(args)
	require.NoError(t, cmd.ExecuteContext(context.Background()), "stderr: %s", stderr.String())
	return stdout.String()
}

// resolve prints the one ResolvedRate, not the batch envelope the backend uses.
func TestResolveCommand_EncodesSingleRate(t *testing.T) {
	out := runResolve(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req modelrates.ResolveRequest
		assert.NoError(t, json.NewDecoder(r.Body).Decode(&req))
		assert.Equal(t, []modelrates.ResolveItem{{Provider: "openai", Model: "gpt-5.5", At: "2026-10-01T00:00:00Z"}}, req.Items)
		writeJSON(w, map[string]any{"items": []map[string]any{{"provider": "openai", "model": "gpt-5.5", "at": "2026-10-01T00:00:00Z", "source": "catalog", "pricing": map[string]any{"prompt_usd_per_token": 0.000002}}}})
	}), "resolve", "--provider", "openai", "--model", "gpt-5.5", "--at", "2026-10-01T00:00:00Z", "-o", "json")

	var rate map[string]any
	require.NoError(t, json.Unmarshal([]byte(out), &rate))
	assert.Equal(t, "catalog", rate["source"])
	assert.Equal(t, "gpt-5.5", rate["model"])
	assert.NotContains(t, rate, "items")
}

// A model nothing prices is an answer (empty source, null pricing), not an error.
func TestResolveCommand_UnpricedModel(t *testing.T) {
	out := runResolve(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, map[string]any{"items": []map[string]any{{"provider": "acme", "model": "x", "at": "2026-10-01T00:00:00Z", "source": "", "pricing": nil}}})
	}), "resolve", "--provider", "acme", "--model", "x", "-o", "json")

	var rate map[string]any
	require.NoError(t, json.Unmarshal([]byte(out), &rate))
	assert.Empty(t, rate["source"])
	assert.Nil(t, rate["pricing"])
}

func TestResolve_UnexpectedResultCount(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, map[string]any{"items": []any{}})
	}))
	_, err := client.Resolve(context.Background(), modelrates.ResolveItem{Provider: "p", Model: "m"})
	require.ErrorContains(t, err, "0 results for 1 requested")
}
