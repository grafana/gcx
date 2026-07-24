package alert_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/gcxerrors"
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
		"groups", "apply", "ns", "-f", file, "--datasource", "my-ds", "--dry-run")
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
		"groups", "apply", "ns", "-f", file, "--datasource", "my-ds")
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
		"groups", "apply", "ns", "-f", file, "--datasource", "my-ds")
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

// runRulerSplit executes `alert ruler <args...>` with stdout and stderr kept
// apart, so a test can assert what reaches the agent-mode stdout document
// versus what is only a diagnostic. Agent mode is enabled before the command
// tree is built because commands resolve their default output format at
// construction time.
func runRulerSplit(t *testing.T, loader alert.GrafanaConfigLoader, args ...string) (string, string, error) {
	t.Helper()
	wasAgent := agent.IsAgentMode()
	agent.SetFlag(true)
	t.Cleanup(func() { agent.SetFlag(wasAgent) })

	cmd := alert.RulerCommands(loader)
	// The real root command silences both, so usage text and the error render
	// never land on stdout. Mirror that here, or the subtree's own Cobra
	// defaults would pollute the document under test.
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)
	cmd.SetIn(strings.NewReader(""))
	cmd.SetArgs(args)
	err := cmd.Execute()
	return stdout.String(), stderr.String(), err
}

// decodeOneJSONDocument asserts that s is exactly one JSON value — the agent
// output contract for a finite command — and returns it decoded.
func decodeOneJSONDocument(t *testing.T, s string) map[string]any {
	t.Helper()
	dec := json.NewDecoder(strings.NewReader(s))
	var doc map[string]any
	require.NoError(t, dec.Decode(&doc), "stdout must be one JSON document, got %q", s)
	require.ErrorIs(t, dec.Decode(new(any)), io.EOF, "stdout must carry exactly one JSON document, got %q", s)
	return doc
}

func TestRulerNamespacesDelete_AgentStdoutIsOneMutationDocument(t *testing.T) {
	loader := newRulerTestEnv(t, "prometheus", func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodDelete, r.Method)
		assert.Equal(t, "/api/ruler/my-ds/api/v1/rules/ns", r.URL.Path)
		w.WriteHeader(http.StatusAccepted)
	})

	stdout, _, err := runRulerSplit(t, loader,
		"namespaces", "delete", "ns", "--datasource", "my-ds", "--force")
	require.NoError(t, err)

	doc := decodeOneJSONDocument(t, stdout)
	assert.Equal(t, "gcx.mutation", doc["type"])
	assert.Equal(t, "deleted", doc["action"])
	target, ok := doc["target"].(map[string]any)
	require.True(t, ok, "target must be an object")
	assert.Equal(t, "ruler-namespace", target["kind"])
	assert.Equal(t, "ns", target["namespace"])
}

func TestRulerGroupsDelete_AgentStdoutIsOneMutationDocument(t *testing.T) {
	loader := newRulerTestEnv(t, "prometheus", func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodDelete, r.Method)
		assert.Equal(t, "/api/ruler/my-ds/api/v1/rules/ns/g1", r.URL.Path)
		w.WriteHeader(http.StatusAccepted)
	})

	stdout, _, err := runRulerSplit(t, loader,
		"groups", "delete", "ns", "g1", "--datasource", "my-ds", "--force")
	require.NoError(t, err)

	doc := decodeOneJSONDocument(t, stdout)
	assert.Equal(t, "gcx.mutation", doc["type"])
	assert.Equal(t, "deleted", doc["action"])
	target, ok := doc["target"].(map[string]any)
	require.True(t, ok, "target must be an object")
	assert.Equal(t, "ruler-rule-group", target["kind"])
	assert.Equal(t, "ns", target["namespace"])
	assert.Equal(t, "g1", target["name"])
}

func TestRulerGroupsApply_AgentStdoutIsOneBatchDocument(t *testing.T) {
	loader := newRulerTestEnv(t, "prometheus", func(w http.ResponseWriter, _ *http.Request) {
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

	stdout, stderr, err := runRulerSplit(t, loader,
		"groups", "apply", "ns", "-f", file, "--datasource", "my-ds")
	require.NoError(t, err)

	doc := decodeOneJSONDocument(t, stdout)
	assert.Equal(t, "gcx.mutation_batch", doc["type"])
	assert.Equal(t, "applied", doc["action"])
	summary, ok := doc["summary"].(map[string]any)
	require.True(t, ok, "summary must be an object")
	assert.InDelta(t, 2, summary["succeeded"], 0)
	assert.Empty(t, doc["failures"], "failures must be an empty list when nothing failed")

	// The per-group receipts are diagnostics: they belong on stderr, never in
	// the stdout document.
	assert.Contains(t, stderr, `Applied group "g1"`)
	assert.Contains(t, stderr, `Applied group "g2"`)
}

func TestRulerGroupsApply_PartialFailureEmitsDocumentAndPartialExit(t *testing.T) {
	loader := newRulerTestEnv(t, "prometheus", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		// Fail only the second group, so the run is a genuine partial failure.
		if strings.Contains(string(body), "g2") {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"message":"ruler rejected group"}`))
			return
		}
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

	stdout, _, err := runRulerSplit(t, loader,
		"groups", "apply", "ns", "-f", file, "--datasource", "my-ds")
	require.Error(t, err)

	// The partial result is still one complete document on stdout, and the
	// error is already reported — no second document.
	doc := decodeOneJSONDocument(t, stdout)
	assert.Equal(t, "gcx.mutation_batch", doc["type"])
	summary, ok := doc["summary"].(map[string]any)
	require.True(t, ok, "summary must be an object")
	assert.InDelta(t, 1, summary["succeeded"], 0)
	assert.InDelta(t, 1, summary["failed"], 0)
	failures, ok := doc["failures"].([]any)
	require.True(t, ok, "failures must be a list")
	require.Len(t, failures, 1)
	failure, ok := failures[0].(map[string]any)
	require.True(t, ok, "failure must be an object")
	failureTarget, ok := failure["target"].(map[string]any)
	require.True(t, ok, "failure target must be an object")
	assert.Equal(t, "g2", failureTarget["name"])

	var emitted *gcxerrors.EmittedError
	require.ErrorAs(t, err, &emitted, "partial failure must not print a second error document")
	assert.Equal(t, gcxerrors.ExitPartialFailure, emitted.Code)
}
