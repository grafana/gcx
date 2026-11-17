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

	"github.com/grafana/gcx/internal/gcxerrors"
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

// runThresholdsAsIs leaves the agent-mode flag as the caller set it. It still
// pins GCX_AUTO_APPROVE=0, so tests of the auto-approve bypass build the command themselves.
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
		requireCancelled(t, err)
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
		requireCancelled(t, err)
		assert.Empty(t, *got)
	})

	rejected := map[string]string{
		"a prom-rules document":    "apiVersion: kg.ext.grafana.app/v1alpha1\nkind: Rule\nspec:\n  groups:\n  - name: g\n",
		"an empty groups list":     "groups: []\n",
		"a document with no body":  "foo: bar\n",
		"invalid YAML":             "groups: [unclosed\n",
		"a misspelled key":         "groups:\n- name: g\n  rules:\n  - record: r\n    expr: \"1\"\n    lables: {a: b}\n",
		"an unquoted numeric expr": "groups:\n- name: g\n  rules:\n  - record: r\n    expr: 200e6\n",
		"a second YAML document":   bareThresholds + "---\ngroups:\n- name: second\n  rules:\n  - record: r2\n    expr: \"2\"\n",
		"unknown envelope keys":    "kind: Thresholds\nbogus: 1\nspec:\n  groups:\n  - name: g\n",
		"a kind with no spec":      "kind: Thresholds\n",
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

// requireCancelled asserts err is the ExitCancelled error a declined prompt returns.
func requireCancelled(t *testing.T, err error) {
	t.Helper()
	var detailed *gcxerrors.DetailedError
	require.ErrorAs(t, err, &detailed)
	require.NotNil(t, detailed.ExitCode)
	assert.Equal(t, gcxerrors.ExitCancelled, *detailed.ExitCode)
}

func TestThresholdsUpdateAllowsTrailingEmptyDocument(t *testing.T) {
	server, got := recordingServer(t)
	_, err := runThresholds(t, server, "", "update", "--force", "-f", writeTempYAML(t, bareThresholds+"---\n"))
	require.NoError(t, err)
	assert.Len(t, *got, 1)
}

func TestThresholdsDeletePromptNamesLabels(t *testing.T) {
	server, _ := recordingServer(t)
	out, err := runThresholds(t, server, "n\n", "delete", "--record", "r", "--expr", "1", "--label", "b=2", "--label", "a=1")
	requireCancelled(t, err)
	assert.Contains(t, out, "labels a=1,b=2")
}

func TestThresholdsUpdateFromStdinNeedsForce(t *testing.T) {
	server, got := recordingServer(t)
	_, err := runThresholds(t, server, bareThresholds, "update", "-f", "-")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--force")
	assert.Empty(t, *got)

	_, err = runThresholds(t, server, bareThresholds, "update", "-f", "-", "--force")
	require.NoError(t, err)
	assert.Len(t, *got, 1)
}

func TestThresholdsUpdateFromStdinHonoursAutoApprove(t *testing.T) {
	server, got := recordingServer(t)
	pinHumanMode(t)
	t.Setenv("GCX_AUTO_APPROVE", "1")
	cmd := kg.NewThresholdsCommand(thresholdsLoader(server))
	var stderr bytes.Buffer
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&stderr)
	cmd.SetIn(strings.NewReader(bareThresholds))
	cmd.SetArgs([]string{"update", "-f", "-"})
	require.NoError(t, cmd.Execute())
	assert.Len(t, *got, 1)
	assert.NotContains(t, stderr.String(), "[y/N]")
}

func TestThresholdsUpdateSendsBareDocumentVerbatim(t *testing.T) {
	server, got := recordingServer(t)
	doc := "# keep my comment\ngroups:\n- name: custom_thresholds\n  rules:\n  - record: r\n    expr: \"0.10\"\n"
	_, err := runThresholds(t, server, "", "update", "--force", "-f", writeTempYAML(t, doc))
	require.NoError(t, err)
	require.Len(t, *got, 1)
	assert.Equal(t, doc, (*got)[0].Body)
}

func TestThresholdsRejectBlankIdentity(t *testing.T) {
	server, got := recordingServer(t)
	_, err := runThresholds(t, server, "", "upsert", "--record", "  ", "--expr", "1")
	require.Error(t, err)
	_, err = runThresholds(t, server, "", "upsert", "--record", "r", "--expr", " ")
	require.Error(t, err)
	assert.Empty(t, *got)
}

func TestThresholdsDeletePromptQuotesAmbiguousLabelValues(t *testing.T) {
	server, _ := recordingServer(t)
	out, err := runThresholds(t, server, "n\n", "delete", "--record", "r", "--expr", "1", "--label", `"k=a,b"`)
	requireCancelled(t, err)
	assert.Contains(t, out, `labels k="a,b"`)
}
