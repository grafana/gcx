package loki_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/datasources/loki"
	"github.com/grafana/gcx/internal/providers"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Expression resolution itself (positional arg vs --expr, both/neither
// provided) is exercised by dsquery.ExprOpts's own TestResolveExpr — statsOpts
// embeds that type directly, so no separate unit test is needed here. This
// test just confirms the CLI wiring surfaces that error end-to-end.
func TestStatsCmd_ExprFlagAndPositionalBothProvidedIsError(t *testing.T) {
	cmd := loki.StatsCmd(nil)
	cmd.SetArgs([]string{`{job="x"}`, "--expr", `{job="x"}`})
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected an error when both a positional arg and --expr are provided")
	}
}

func TestStatsCmd_NoSelectorFoundReturnsError(t *testing.T) {
	cmd := loki.StatsCmd(nil)
	cmd.SetArgs([]string{"--expr", "vector(1)"})
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected an error for an expression with no stream selector")
	}
}

func TestStatsCmd_Construction(t *testing.T) {
	cmd := loki.StatsCmd(nil)
	if cmd.Use != "stats [EXPR]" {
		t.Errorf("Use = %q, want %q", cmd.Use, "stats [EXPR]")
	}
	if cmd.Annotations[agent.AnnotationTokenCost] != "small" {
		t.Errorf("expected %s annotation to be \"small\", got %q", agent.AnnotationTokenCost, cmd.Annotations[agent.AnnotationTokenCost])
	}
	for _, flag := range []string{"datasource", "expr", "from", "to", "since", "output"} {
		if cmd.Flags().Lookup(flag) == nil {
			t.Errorf("expected --%s flag to be registered", flag)
		}
	}
}

func writeStatsTestConfig(t *testing.T, serverURL string) string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "gcx-loki-stats-config-*.yaml")
	require.NoError(t, err)
	_, err = f.WriteString(`
contexts:
  default:
    grafana:
      server: "` + serverURL + `"
      token: "test-token"
      org-id: 1
      tls:
        insecure-skip-verify: true
current-context: default
`)
	require.NoError(t, err)
	require.NoError(t, f.Close())
	return f.Name()
}

// execStatsCmd executes `stats` against a fake index-stats response and
// returns stdout/stderr separately, so the header-routing test can assert on
// each stream in isolation the way a script piping only stdout would see it.
func execStatsCmd(t *testing.T, args ...string) (string, string, error) {
	t.Helper()

	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/bootdata" {
			http.Error(w, `{"message":"not a cloud stack"}`, http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, writeErr := w.Write([]byte(`{"streams":6,"chunks":316,"bytes":37748736,"entries":88531}`))
		assert.NoError(t, writeErr)
	}))
	defer srv.Close()

	cfgFile := writeStatsTestConfig(t, srv.URL)
	loader := &providers.ConfigLoader{}
	loader.SetConfigFile(cfgFile)

	cmd := loki.StatsCmd(loader)
	root := &cobra.Command{Use: "test"}
	root.AddCommand(cmd)

	var outBuf, errBuf bytes.Buffer
	root.SetOut(&outBuf)
	root.SetErr(&errBuf)
	root.SetArgs(append([]string{"stats", "-d", "loki-uid", `{app="foo"}`}, args...))

	err := root.Execute()
	return outBuf.String(), errBuf.String(), err
}

func TestStatsCmd_BytesHeaderRoutesByFormat(t *testing.T) {
	t.Run("table: header on stdout, above the table, nothing on stderr", func(t *testing.T) {
		stdout, stderr, err := execStatsCmd(t, "-o", "table")
		require.NoError(t, err)
		assert.Contains(t, stdout, "36 MiB would be scanned")
		assert.Contains(t, stdout, "Bytes")
		assert.Empty(t, stderr)
	})

	t.Run("json: stdout is clean valid JSON, header goes to stderr instead", func(t *testing.T) {
		stdout, stderr, err := execStatsCmd(t, "-o", "json")
		require.NoError(t, err)
		assert.NotContains(t, stdout, "would be scanned")
		assert.Contains(t, stdout, `"bytes"`)
		assert.Contains(t, stderr, "36 MiB would be scanned")
	})
}
