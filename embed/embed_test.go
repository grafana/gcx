package embed_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/grafana/gcx/embed"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// isolateHost points every location gcx could write to at empty directories
// and plants process-level credentials that must never be used.
func isolateHost(t *testing.T) []string {
	t.Helper()
	envs := []string{"HOME", "XDG_CONFIG_HOME", "XDG_STATE_HOME", "XDG_CACHE_HOME", "XDG_DATA_HOME", "TMPDIR"}
	dirs := make([]string, 0, len(envs))
	for _, env := range envs {
		dir := t.TempDir()
		t.Setenv(env, dir)
		dirs = append(dirs, dir)
	}
	t.Setenv("GRAFANA_SERVER", "http://process-env.invalid")
	t.Setenv("GRAFANA_TOKEN", "process-env-token")
	t.Setenv("GCX_CONFIG", filepath.Join(dirs[0], "config.yaml"))
	return dirs
}

func assertNothingWritten(t *testing.T, dirs []string) {
	t.Helper()
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		require.NoError(t, err)
		assert.Empty(t, entries, "embedded gcx wrote to %s", dir)
	}
}

type tenant struct {
	token  string
	server *httptest.Server

	mu   sync.Mutex
	seen []string // Authorization headers received
}

func newTenant(t *testing.T, i int) *tenant {
	t.Helper()
	tn := &tenant{token: fmt.Sprintf("tenant-%d-token", i)}
	tn.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tn.mu.Lock()
		tn.seen = append(tn.seen, r.Header.Get("Authorization"))
		tn.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"tenant": i, "path": r.URL.Path})
	}))
	t.Cleanup(tn.server.Close)
	return tn
}

func TestRunIsolatesConcurrentTenants(t *testing.T) {
	dirs := isolateHost(t)

	const tenants = 20
	commands := []string{
		"api /api/health",
		"gcx api /api/search -o json",
		"resources get dashboards",
		"slo definitions list",
		"config view",
	}

	ts := make([]*tenant, tenants)
	for i := range ts {
		ts[i] = newTenant(t, i)
	}

	var wg sync.WaitGroup
	results := make([][]embed.Result, tenants)
	for i, tn := range ts {
		results[i] = make([]embed.Result, len(commands))
		for j, command := range commands {
			wg.Go(func() {
				res, err := embed.Run(t.Context(), command, embed.Options{
					Grafana: embed.Grafana{URL: tn.server.URL, Token: tn.token, StackID: int64(i + 1)},
				})
				assert.NoError(t, err, command)
				results[i][j] = res
			})
		}
	}
	wg.Wait()

	for i, tn := range ts {
		require.NotEmpty(t, tn.seen, "tenant %d received no requests", i)
		for _, auth := range tn.seen {
			assert.Equal(t, "Bearer "+tn.token, auth, "tenant %d received another tenant's credentials", i)
		}

		health := results[i][0]
		assert.Equal(t, 0, health.ExitCode, health.Stdout+health.Stderr)
		assert.JSONEq(t, fmt.Sprintf(`{"tenant":%d,"path":"/api/health"}`, i), health.Stdout)

		for j, res := range results[i] {
			assert.NotContains(t, res.Stdout+res.Stderr, "process-env", "command %q saw process environment", commands[j])
		}
	}

	assertNothingWritten(t, dirs)
}

func TestRunRefusesHostAccess(t *testing.T) {
	dirs := isolateHost(t)
	tn := newTenant(t, 0)
	opts := embed.Options{Grafana: embed.Grafana{URL: tn.server.URL, Token: tn.token, StackID: 1}}

	for _, command := range []string{
		"login",
		"config set contexts.x.stack y",
		"resources get dashboards --config /etc/passwd",
		"resources push -p /etc",
		"resources pull -p " + dirs[0],
		"dev serve",
	} {
		t.Run(command, func(t *testing.T) {
			res, err := embed.Run(t.Context(), command, opts)
			require.NoError(t, err)
			assert.NotEqual(t, 0, res.ExitCode, "stdout: %s\nstderr: %s", res.Stdout, res.Stderr)
		})
	}

	assertNothingWritten(t, dirs)
}

func TestRunReadsStdin(t *testing.T) {
	isolateHost(t)
	tn := newTenant(t, 0)

	res, err := embed.Run(t.Context(), "resources validate -f -", embed.Options{
		Grafana: embed.Grafana{URL: tn.server.URL, Token: tn.token, StackID: 1},
		Stdin:   "not: [valid",
	})
	require.NoError(t, err)
	assert.NotContains(t, res.Stdout, "Not available when gcx is embedded", "stdin must be readable via -f -")
}

func TestRunRejectsShellSyntax(t *testing.T) {
	_, err := embed.Run(t.Context(), "slo definitions list | jq .", embed.Options{Grafana: embed.Grafana{URL: "http://x"}})
	require.Error(t, err)
}
