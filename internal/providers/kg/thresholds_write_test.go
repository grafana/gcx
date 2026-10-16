package kg_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/providers/kg"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type recordedRequest struct {
	Method      string
	Path        string
	ContentType string
	Body        string
}

// recordingServer captures every request so tests can assert on the wire shape,
// and on the absence of a request when a command must not write.
func recordingServer(t *testing.T) (*httptest.Server, *[]recordedRequest) {
	t.Helper()
	var got []recordedRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		got = append(got, recordedRequest{Method: r.Method, Path: r.URL.Path, ContentType: r.Header.Get("Content-Type"), Body: string(b)})
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)
	return server, &got
}

// runThresholds runs the command in pinned human mode with auto-approve off, so
// prompt-dependent tests behave the same in CI.
func runThresholds(t *testing.T, server *httptest.Server, stdin string, args ...string) (string, error) {
	t.Helper()
	pinHumanMode(t)
	return runThresholdsAsIs(t, server, stdin, args...)
}

// runThresholdsAsIs leaves the agent-mode flag as the caller set it.
func runThresholdsAsIs(t *testing.T, server *httptest.Server, stdin string, args ...string) (string, error) {
	t.Helper()
	t.Setenv("GCX_AUTO_APPROVE", "0")
	cmd := kg.NewThresholdsCommand(thresholdsLoader(server))
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetIn(strings.NewReader(stdin))
	cmd.SetArgs(args)
	err := cmd.Execute()
	return buf.String(), err
}

func TestThresholdsUpsertCommand(t *testing.T) {
	t.Run("posts the identity triple as JSON", func(t *testing.T) {
		server, got := recordingServer(t)
		out, err := runThresholds(t, server, "", "upsert",
			"--record", "asserts:resource:rate:critical", "--expr", "200e6",
			"--label", "asserts_severity=critical", "--label", "asserts_source=node_exporter")
		require.NoError(t, err)

		require.Len(t, *got, 1)
		req := (*got)[0]
		assert.Equal(t, http.MethodPost, req.Method)
		assert.True(t, strings.HasSuffix(req.Path, "/v1/config/threshold-rule"), "path: %s", req.Path)
		assert.Equal(t, "application/json", req.ContentType)

		var body map[string]any
		require.NoError(t, json.Unmarshal([]byte(req.Body), &body))
		assert.Equal(t, map[string]any{
			"record": "asserts:resource:rate:critical",
			"expr":   "200e6",
			"labels": map[string]any{"asserts_severity": "critical", "asserts_source": "node_exporter"},
		}, body)
		assert.Contains(t, out, "asserts:resource:rate:critical")
	})

	t.Run("labels are optional", func(t *testing.T) {
		server, got := recordingServer(t)
		_, err := runThresholds(t, server, "", "upsert", "--record", "r", "--expr", "1")
		require.NoError(t, err)
		require.Len(t, *got, 1)
		assert.JSONEq(t, `{"record":"r","expr":"1"}`, (*got)[0].Body)
	})

	for name, args := range map[string][]string{
		"missing record": {"upsert", "--expr", "1"},
		"missing expr":   {"upsert", "--record", "r"},
	} {
		t.Run(name+" is rejected before any request", func(t *testing.T) {
			server, got := recordingServer(t)
			_, err := runThresholds(t, server, "", args...)
			require.Error(t, err)
			assert.Empty(t, *got)
		})
	}

	t.Run("backend errors surface", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, `{"message":"forbidden"}`, http.StatusForbidden)
		}))
		defer server.Close()
		_, err := runThresholds(t, server, "", "upsert", "--record", "r", "--expr", "1")
		require.Error(t, err)
	})
}

func TestThresholdsDeleteCommand(t *testing.T) {
	args := []string{"delete", "--record", "r", "--expr", "1", "--label", "a=b"}

	t.Run("posts the identity triple to the delete endpoint with --force", func(t *testing.T) {
		server, got := recordingServer(t)
		out, err := runThresholds(t, server, "", append(args, "--force")...)
		require.NoError(t, err)

		require.Len(t, *got, 1)
		req := (*got)[0]
		assert.Equal(t, http.MethodPost, req.Method)
		assert.True(t, strings.HasSuffix(req.Path, "/v1/config/threshold-rule/delete"), "path: %s", req.Path)
		assert.JSONEq(t, `{"record":"r","expr":"1","labels":{"a":"b"}}`, req.Body)
		assert.Contains(t, out, "deleted")
	})

	t.Run("declining the prompt sends nothing", func(t *testing.T) {
		server, got := recordingServer(t)
		_, err := runThresholds(t, server, "n\n", args...)
		require.NoError(t, err)
		assert.Empty(t, *got)
	})

	t.Run("accepting the prompt deletes", func(t *testing.T) {
		server, got := recordingServer(t)
		_, err := runThresholds(t, server, "y\n", args...)
		require.NoError(t, err)
		assert.Len(t, *got, 1)
	})

	t.Run("requires record and expr", func(t *testing.T) {
		server, got := recordingServer(t)
		_, err := runThresholds(t, server, "", "delete", "--record", "r", "--force")
		require.Error(t, err)
		assert.Empty(t, *got)
	})
}

func writeTempYAML(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "thresholds.yaml")
	require.NoError(t, os.WriteFile(p, []byte(content), 0o600))
	return p
}

const bareThresholds = `groups:
- name: custom_thresholds
  rules:
  - record: asserts:request:cardinality:limit
    expr: "200"
`

func TestThresholdsUpdateCommand(t *testing.T) {
	t.Run("uploads a bare document as YAML after confirmation", func(t *testing.T) {
		server, got := recordingServer(t)
		out, err := runThresholds(t, server, "y\n", "update", "-f", writeTempYAML(t, bareThresholds))
		require.NoError(t, err)

		require.Len(t, *got, 1)
		req := (*got)[0]
		assert.Equal(t, http.MethodPost, req.Method)
		assert.True(t, strings.HasSuffix(req.Path, "/v1/config/threshold-rules/"), "path: %s", req.Path)
		assert.Equal(t, "application/x-yaml", req.ContentType)
		assert.Contains(t, req.Body, "asserts:request:cardinality:limit")
		assert.Contains(t, out, "replaced")
	})

	t.Run("unwraps the kind Thresholds envelope from get -o yaml", func(t *testing.T) {
		server, got := recordingServer(t)
		envelope := "apiVersion: kg.ext.grafana.app/v1alpha1\nkind: Thresholds\nmetadata:\n  name: custom_thresholds\n  namespace: stack-123\nspec:\n" +
			"  groups:\n  - name: custom_thresholds\n    rules:\n    - record: r\n      expr: \"1\"\n"
		_, err := runThresholds(t, server, "", "update", "--force", "-f", writeTempYAML(t, envelope))
		require.NoError(t, err)

		require.Len(t, *got, 1)
		body := (*got)[0].Body
		assert.Contains(t, body, "groups:")
		assert.NotContains(t, body, "apiVersion")
		assert.NotContains(t, body, "kind:")
	})

	t.Run("declining the prompt sends nothing", func(t *testing.T) {
		server, got := recordingServer(t)
		_, err := runThresholds(t, server, "n\n", "update", "-f", writeTempYAML(t, bareThresholds))
		require.NoError(t, err)
		assert.Empty(t, *got)
	})

	rejected := map[string]string{
		"a prom-rules document":   "apiVersion: kg.ext.grafana.app/v1alpha1\nkind: Rule\nspec:\n  groups:\n  - name: g\n",
		"an empty groups list":    "groups: []\n",
		"a document with no body": "foo: bar\n",
		"invalid YAML":            "groups: [unclosed\n",
	}
	for name, content := range rejected {
		t.Run("rejects "+name+" before any request", func(t *testing.T) {
			server, got := recordingServer(t)
			_, err := runThresholds(t, server, "y\n", "update", "--force", "-f", writeTempYAML(t, content))
			require.Error(t, err)
			assert.Empty(t, *got)
		})
	}
}

func TestThresholdsDestructiveWritesRequireForceInAgentMode(t *testing.T) {
	for name, args := range map[string][]string{
		"delete": {"delete", "--record", "r", "--expr", "1"},
		"update": {"update", "-f", writeTempYAML(t, bareThresholds)},
	} {
		t.Run(name, func(t *testing.T) {
			server, got := recordingServer(t)
			setAgentMode(t)
			_, err := runThresholdsAsIs(t, server, "", args...)
			require.ErrorIs(t, err, providers.ErrAgentModeRequiresForce)
			assert.Empty(t, *got)
		})
	}
}

func TestThresholdsUpdateRejectsNonStringKind(t *testing.T) {
	server, got := recordingServer(t)
	_, err := runThresholds(t, server, "", "update", "--force", "-f", writeTempYAML(t, "kind: 5\n"+bareThresholds))
	require.Error(t, err)
	assert.Empty(t, *got)
}

func TestThresholdsDeletePromptNamesLabels(t *testing.T) {
	server, _ := recordingServer(t)
	out, err := runThresholds(t, server, "n\n", "delete", "--record", "r", "--expr", "1", "--label", "b=2", "--label", "a=1")
	require.NoError(t, err)
	assert.Contains(t, out, "labels a=1,b=2")
}
