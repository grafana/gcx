package alert_test

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/providers/alert"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"
)

// rulerStubLoader is a test double for alert.GrafanaConfigLoader.
type rulerStubLoader struct {
	cfg config.NamespacedRESTConfig
}

func (s *rulerStubLoader) LoadGrafanaConfig(_ context.Context) (config.NamespacedRESTConfig, error) {
	return s.cfg, nil
}

// newRulerTestEnv starts a server that answers the datasource-type lookup with
// dsType and delegates everything else to handler. It returns a loader wired
// to the server.
func newRulerTestEnv(t *testing.T, dsType string, handler http.HandlerFunc) *rulerStubLoader {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/datasources/uid/") {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"uid":"my-ds","name":"my-ds","type":"` + dsType + `"}`))
			return
		}
		handler(w, r)
	}))
	t.Cleanup(srv.Close)
	return &rulerStubLoader{
		cfg: config.NamespacedRESTConfig{
			Config:    rest.Config{Host: srv.URL},
			Namespace: "stacks-test",
		},
	}
}

// runRuler executes `alert <args...>` against the given loader and returns the
// combined output.
func runRuler(t *testing.T, loader alert.GrafanaConfigLoader, args ...string) (string, error) {
	t.Helper()
	cmd := alert.RulerCommands(loader)
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetIn(strings.NewReader(""))
	cmd.SetArgs(args)
	err := cmd.Execute()
	return buf.String(), err
}

func TestRulerCommands_RequireDatasource(t *testing.T) {
	loader := newRulerTestEnv(t, "prometheus", func(w http.ResponseWriter, _ *http.Request) {
		t.Error("no ruler request expected")
	})

	for _, args := range [][]string{
		{"namespaces", "list"},
		{"groups", "list"},
		{"groups", "get", "ns", "g"},
		{"groups", "delete", "ns", "g", "--force"},
		{"namespaces", "delete", "ns", "--force"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			_, err := runRuler(t, loader, args...)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "--datasource is required")
		})
	}
}

func TestRulerCommands_NonRulerDatasourceRejected(t *testing.T) {
	loader := newRulerTestEnv(t, "mysql", func(w http.ResponseWriter, _ *http.Request) {
		t.Error("no ruler request expected")
	})

	_, err := runRuler(t, loader, "namespaces", "list", "--datasource", "my-ds")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no ruler API")
}

func TestRulerGroupsApply_DryRunSendsNoMutation(t *testing.T) {
	loader := newRulerTestEnv(t, "prometheus", func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected ruler request: %s %s", r.Method, r.URL.Path)
	})

	file := filepath.Join(t.TempDir(), "rules.yaml")
	require.NoError(t, os.WriteFile(file, []byte(`groups:
  - name: g1
    rules:
      - alert: A
        expr: up == 0
`), 0o600))

	out, err := runRuler(t, loader,
		"groups", "apply", "-f", file, "--namespace", "ns", "--datasource", "my-ds", "--dry-run")
	require.NoError(t, err)
	assert.Contains(t, out, "would apply group")
}

func TestRulerGroupsApply_InvalidPromQLRejectedBeforeHTTP(t *testing.T) {
	loader := newRulerTestEnv(t, "prometheus", func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected ruler request: %s %s", r.Method, r.URL.Path)
	})

	file := filepath.Join(t.TempDir(), "rules.yaml")
	require.NoError(t, os.WriteFile(file, []byte(`groups:
  - name: g1
    rules:
      - alert: A
        expr: "rate(up[5m"
`), 0o600))

	_, err := runRuler(t, loader,
		"groups", "apply", "-f", file, "--namespace", "ns", "--datasource", "my-ds")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid PromQL")
}

func TestRulerGroupsApply_PostsGroups(t *testing.T) {
	var posted []string
	loader := newRulerTestEnv(t, "prometheus", func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/api/ruler/my-ds/api/v1/rules/ns", r.URL.Path)
		assert.Equal(t, "mimir", r.URL.Query().Get("subtype"))
		posted = append(posted, r.URL.Path)
		w.WriteHeader(http.StatusAccepted)
	})

	file := filepath.Join(t.TempDir(), "rules.yaml")
	require.NoError(t, os.WriteFile(file, []byte(`groups:
  - name: g1
    rules:
      - alert: A
        expr: up == 0
  - name: g2
    rules:
      - record: r:up
        expr: up
`), 0o600))

	out, err := runRuler(t, loader,
		"groups", "apply", "-f", file, "--namespace", "ns", "--datasource", "my-ds")
	require.NoError(t, err)
	assert.Len(t, posted, 2)
	assert.Contains(t, out, `Applied group "g1"`)
	assert.Contains(t, out, `Applied group "g2"`)
}

func TestRulerGroupsDelete_DeclinesWithoutForce(t *testing.T) {
	// Force non-agent behavior: agent mode short-circuits the prompt with an
	// error, and tests may run inside an agent session where env detection
	// already latched at init time.
	wasAgent := agent.IsAgentMode()
	agent.SetFlag(false)
	t.Cleanup(func() { agent.SetFlag(wasAgent) })

	loader := newRulerTestEnv(t, "prometheus", func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected ruler request: %s %s", r.Method, r.URL.Path)
	})

	// Non-TTY stdin answering "n": the confirmation is declined and no delete
	// request is sent.
	provider := alert.RulerCommands(loader)
	buf := &bytes.Buffer{}
	provider.SetOut(buf)
	provider.SetErr(buf)
	provider.SetIn(strings.NewReader("n\n"))
	provider.SetArgs([]string{"groups", "delete", "ns", "g1", "--datasource", "my-ds"})
	require.NoError(t, provider.Execute())
	assert.Contains(t, buf.String(), "Aborted")
	assert.NotContains(t, buf.String(), "Deleted")
}

func TestRulerNamespacesTableCodec_Encode(t *testing.T) {
	codec := &alert.RulerNamespacesTableCodec{}
	assert.Equal(t, "table", string(codec.Format()))

	var buf bytes.Buffer
	err := codec.Encode(&buf, []alert.RulerNamespaceView{
		{Namespace: "ns-a", Groups: 2, Rules: 5},
		{Namespace: "ns-b", Groups: 1, Rules: 1},
	})
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "NAMESPACE")
	assert.Contains(t, output, "ns-a")
	assert.Contains(t, output, "5")
	require.Len(t, strings.Split(strings.TrimSpace(output), "\n"), 3, "header + 2 rows")

	require.Error(t, codec.Encode(&buf, "not a slice"))
}

func TestRulerGroupsTableCodec_Encode(t *testing.T) {
	codec := &alert.RulerGroupsTableCodec{}
	assert.Equal(t, "table", string(codec.Format()))

	var buf bytes.Buffer
	err := codec.Encode(&buf, []alert.RulerGroupView{
		{Namespace: "ns-a", Group: "g1", Interval: "1m", Rules: 3},
		{Namespace: "ns-a", Group: "g2", Rules: 1},
	})
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "GROUP")
	assert.Contains(t, output, "g1")
	assert.Contains(t, output, "1m")
	// Groups without an explicit interval render a placeholder, not a blank cell.
	g2Line := ""
	for line := range strings.SplitSeq(output, "\n") {
		if strings.Contains(line, "g2") {
			g2Line = line
		}
	}
	assert.Contains(t, g2Line, "-")

	require.Error(t, codec.Encode(&buf, 42))
}
