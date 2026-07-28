package collections_test

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/grafana/gcx/internal/providers/aio11y/eval/collections"
	"github.com/grafana/gcx/internal/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCommands_NilLoader_EndToEnd is the teeth for the nil-loader contract:
// unlike the flag-validation tests in commands_test.go, which return before
// RunE ever touches the loader, this runs a verb all the way through
// ConfigLoader.LoadGrafanaConfig and an HTTP round-trip. A nil loader must
// behave like a zero-value one (default config discovery); removing the
// nil-receiver handling in providers.ConfigLoader fails this test.
func TestCommands_NilLoader_EndToEnd(t *testing.T) {
	home := testutils.SandboxConfigEnv(t)

	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"items": []}`))
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

	cmd := collections.Commands(nil)
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetIn(strings.NewReader(""))
	cmd.SetArgs([]string{"list", "-o", "json"})

	require.NoError(t, cmd.ExecuteContext(context.Background()), "stderr: %s", stderr.String())
	assert.Positive(t, hits.Load(), "nil-loader command must route via default config discovery")
}
