package errortracking_test

import (
	"bytes"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/grafana/gcx/cmd/gcx/root"
	"github.com/grafana/gcx/internal/agent"
	internalconfig "github.com/grafana/gcx/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const etPrefix = "/apis/error-tracking.ext.grafana.app/v0alpha1/namespaces/stacks-123/"

type fakeET struct {
	t      *testing.T
	cfg    string
	url    string
	stderr bytes.Buffer

	mu     sync.Mutex
	routes map[string]http.HandlerFunc
	calls  []etCall
}

type etCall struct {
	Route string
	Query url.Values
	Body  string
}

func newFakeET(t *testing.T) *fakeET {
	t.Helper()
	f := &fakeET{t: t, routes: map[string]http.HandlerFunc{}}
	srv := httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(srv.Close)

	dir := scratchEnv(t)
	f.url = srv.URL
	f.cfg = write(t, dir, "config.yaml", "")
	f.useToken("test-token")
	return f
}

func (f *fakeET) useToken(token string) { f.writeConfig(token, "") }

func (f *fakeET) writeConfig(token, defaultProject string) {
	f.t.Helper()
	cfg := `version: 1
current-context: test
contexts:
  test:
    stack: test
stacks:
  test:
    grafana:
      server: ` + f.url + `
      token: ` + token + `
`
	if defaultProject != "" {
		cfg += "    providers:\n      errortracking:\n        project: \"" + defaultProject + "\"\n"
	}
	require.NoError(f.t, os.WriteFile(f.cfg, []byte(cfg), 0o600))
}

func scratchEnv(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("USERPROFILE", dir)
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv(internalconfig.ConfigFileEnvVar, "")
	t.Setenv("GCX_KEYCHAIN", "off")
	t.Setenv("GCX_TELEMETRY", "disabled")
	t.Setenv("DO_NOT_TRACK", "1")
	t.Setenv("GCX_NO_UPDATE_NOTIFIER", "1")
	t.Setenv("GCX_AGENT_MODE", "false")
	agent.ResetForTesting()
	t.Cleanup(agent.ResetForTesting)
	for _, kv := range os.Environ() {
		if k, _, _ := strings.Cut(kv, "="); strings.HasPrefix(k, "GRAFANA_") {
			t.Setenv(k, "")
			os.Unsetenv(k)
		}
	}
	t.Chdir(dir)
	return dir
}

func (f *fakeET) serve(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/apis/iam.grafana.app/v0alpha1/whoami":
		_, _ = io.WriteString(w, `{"namespace":"stacks-123"}`)
		return
	case "/bootdata":
		_, _ = io.WriteString(w, `{"settings":{"namespace":"stacks-123"}}`)
		return
	}
	body, _ := io.ReadAll(r.Body)
	route := r.Method + " " + strings.TrimPrefix(r.URL.EscapedPath(), etPrefix)
	f.mu.Lock()
	f.calls = append(f.calls, etCall{Route: route, Query: r.URL.Query(), Body: string(body)})
	h := f.routes[route]
	f.mu.Unlock()
	if h == nil {
		f.t.Errorf("unexpected request %s %s", r.Method, r.URL)
		http.NotFound(w, r)
		return
	}
	h(w, r)
}

func (f *fakeET) handle(route string, h http.HandlerFunc) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.routes[route] = h
}

func (f *fakeET) reply(route string, status int, body string) {
	f.handle(route, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	})
}

func (f *fakeET) run(args ...string) (string, error) {
	f.t.Helper()
	return f.runArgs("", false, args...)
}

func (f *fakeET) runArgs(stdin string, bare bool, args ...string) (string, error) {
	f.t.Helper()
	cmd := root.Command("test")
	var out bytes.Buffer
	f.stderr.Reset()
	cmd.SetIn(strings.NewReader(stdin))
	cmd.SetOut(&out)
	cmd.SetErr(&f.stderr)
	if !bare {
		args = append([]string{"--config", f.cfg}, args...)
	}
	cmd.SetArgs(append([]string{"error-tracking"}, args...))
	ctx := f.t.Context()
	err := cmd.ExecuteContext(ctx)
	return out.String(), err
}

func (f *fakeET) takeCalls() []etCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	calls := f.calls
	f.calls = nil
	return calls
}

type cliCase struct {
	name   string
	stdin  string
	args   []string
	calls  []string
	bare   bool
	query  url.Values
	json   string
	has    []string
	hasNot []string
	stderr string
	err    string
	exit   int
	check  func(t *testing.T, out string, err error, calls []etCall)
}

func (f *fakeET) runCases(t *testing.T, groups ...[]cliCase) {
	t.Helper()
	f.mu.Lock()
	base := maps.Clone(f.routes)
	f.mu.Unlock()
	baseCfg, err := os.ReadFile(f.cfg)
	require.NoError(t, err)
	outer := f.t
	for _, tc := range slices.Concat(groups...) {
		t.Run(tc.name, func(t *testing.T) {
			f.mu.Lock()
			f.routes = maps.Clone(base)
			f.calls = nil
			f.mu.Unlock()
			require.NoError(t, os.WriteFile(f.cfg, baseCfg, 0o600))
			agent.SetFlag(false)
			f.t = t
			defer func() { f.t = outer }()
			out, err := f.runArgs(tc.stdin, tc.bare, tc.args...)
			calls := f.takeCalls()
			if tc.err == "" && tc.exit == 0 {
				require.NoError(t, err)
			}
			if err != nil {
				require.NotZero(t, tc.exit, "an error case asserts its exit code: %v", err)
			}
			if tc.err != "" {
				require.Error(t, err)
				assert.Contains(t, rendered(t, err), tc.err)
			}
			if tc.exit != 0 {
				assert.Equal(t, tc.exit, exitCode(err), "%v", err)
			}
			switch {
			case len(tc.calls) == 0:
				assert.Empty(t, routesOf(calls), "no call")
			default:
				assert.Equal(t, tc.calls, routesOf(calls))
			}
			if tc.query != nil {
				require.NotEmpty(t, calls)
				last := calls[len(calls)-1]
				if tc.query != nil {
					assert.Equal(t, tc.query, last.Query)
				}
			}
			if tc.json != "" {
				assert.JSONEq(t, tc.json, out)
			}
			for _, s := range tc.has {
				assert.Contains(t, out, s)
			}
			for _, s := range tc.hasNot {
				assert.NotContains(t, out, s)
			}
			if tc.stderr != "" {
				assert.Contains(t, f.stderr.String(), tc.stderr)
			}
			if tc.check != nil {
				tc.check(t, out, err, calls)
			}
		})
	}
}

func routesOf(calls []etCall) []string {
	out := make([]string, len(calls))
	for i, c := range calls {
		out[i] = c.Route
	}
	return out
}
