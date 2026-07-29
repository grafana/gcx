package stacks_test

// Agent output contract tests for the cloud-stacks family (#387).
//
// The contract for finite commands: in agent mode with no explicit -o, stdout
// carries EXACTLY ONE JSON value; the human default stdout stays
// byte-identical to the pre-migration implementation; explicit -o json/yaml
// always wins. Stack commands are single-target (no partial-failure
// semantics), so no EmittedError paths exist in this family.

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/cloud"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/providers/stacks"
	"github.com/grafana/gcx/internal/testutils"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// setAgentMode forces agent mode on or off for the duration of the test.
// Must be called BEFORE the command under test is constructed: the agents
// default-format override is resolved in Options.BindFlags at construction.
func setAgentMode(t *testing.T, enabled bool) {
	t.Helper()
	agent.SetFlag(enabled)
	t.Cleanup(agent.ResetForTesting)
}

// newCloudFixture starts a mock GCOM server with the given handler and
// returns a ConfigLoader whose cloud api-url points at it.
func newCloudFixture(t *testing.T, handler http.HandlerFunc) *providers.ConfigLoader {
	t.Helper()

	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	cfgPath := testutils.CreateTempFile(t, `
contexts:
  default:
    cloud:
      token: "test-token"
      api-url: "`+srv.URL+`"
current-context: default
`)

	loader := &providers.ConfigLoader{}
	loader.SetConfigFile(cfgPath)
	return loader
}

// serveJSON writes v as the response to every request.
func serveJSON(t *testing.T, v any) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(v); err != nil {
			t.Errorf("mock server: encode: %v", err)
		}
	}
}

// rejectCalls fails the test if the API is contacted at all (dry-run paths
// must never reach the network).
func rejectCalls(t *testing.T) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected API call: %s %s", r.Method, r.URL.Path)
		w.WriteHeader(http.StatusInternalServerError)
	}
}

// runCmdSplit executes child under a silenced parent with separate stdout and
// stderr buffers, so contract assertions can check stdout purity. It returns
// (stdout, stderr, execution error).
func runCmdSplit(t *testing.T, child *cobra.Command, args []string, stdin string) (string, string, error) {
	t.Helper()

	parent := &cobra.Command{
		Use:           "test",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	parent.AddCommand(child)

	var outBuf, errBuf bytes.Buffer
	parent.SetOut(&outBuf)
	parent.SetErr(&errBuf)
	parent.SetIn(strings.NewReader(stdin))
	parent.SetArgs(args)

	err := parent.Execute()
	return outBuf.String(), errBuf.String(), err
}

// decodeSingleJSONDocument asserts stdout holds exactly one JSON value
// followed by EOF and returns the decoded value.
func decodeSingleJSONDocument(t *testing.T, stdout string) any {
	t.Helper()

	dec := json.NewDecoder(strings.NewReader(stdout))
	var doc any
	require.NoError(t, dec.Decode(&doc), "stdout must decode as JSON, got: %q", stdout)

	var extra any
	err := dec.Decode(&extra)
	require.ErrorIs(t, err, io.EOF,
		"stdout must contain exactly one JSON value then EOF; second decode returned (%v)\nstdout: %q", extra, stdout)
	return doc
}

// testStack is the canonical stack fixture served by the mock GCOM server.
func testStack() cloud.StackInfo {
	return cloud.StackInfo{
		ID:     42,
		Slug:   "mystack",
		Name:   "My Stack",
		Status: "active",
		URL:    "https://mystack.grafana.net",
	}
}

// TestAgentMode_SingleJSONDocument verifies that every finite cloud-stacks
// command emits exactly one JSON value on stdout in agent mode (no explicit
// -o), with the expected shape discriminators for mutation results.
func TestAgentMode_SingleJSONDocument(t *testing.T) {
	tests := []struct {
		name    string
		newCmd  func(loader *providers.ConfigLoader) *cobra.Command
		handler func(t *testing.T) http.HandlerFunc
		args    []string
		check   func(t *testing.T, doc any)
	}{
		{
			name:   "list",
			newCmd: stacks.NewTestListCommandWithLoader,
			handler: func(t *testing.T) http.HandlerFunc {
				t.Helper()
				return serveJSON(t, map[string]any{"items": []cloud.StackInfo{testStack()}})
			},
			args: []string{"list", "--org", "myorg"},
			check: func(t *testing.T, doc any) {
				t.Helper()
				items, ok := doc.([]any)
				require.True(t, ok, "list result should be a JSON array, got %T", doc)
				require.Len(t, items, 1)
			},
		},
		{
			name:   "get",
			newCmd: stacks.NewTestGetCommandWithLoader,
			handler: func(t *testing.T) http.HandlerFunc {
				t.Helper()
				return serveJSON(t, testStack())
			},
			args: []string{"get", "mystack"},
			check: func(t *testing.T, doc any) {
				t.Helper()
				obj, ok := doc.(map[string]any)
				require.True(t, ok, "get result should be a JSON object, got %T", doc)
				assert.Equal(t, "mystack", obj["slug"])
			},
		},
		{
			name:   "list-regions",
			newCmd: stacks.NewTestListRegionsCommandWithLoader,
			handler: func(t *testing.T) http.HandlerFunc {
				t.Helper()
				return serveJSON(t, map[string]any{"items": []cloud.Region{{Slug: "us", Name: "US Central"}}})
			},
			args: []string{"list-regions"},
			check: func(t *testing.T, doc any) {
				t.Helper()
				items, ok := doc.([]any)
				require.True(t, ok, "list-regions result should be a JSON array, got %T", doc)
				require.Len(t, items, 1)
			},
		},
		{
			name:   "create echoes created stack",
			newCmd: stacks.NewTestCreateCommandWithLoader,
			handler: func(t *testing.T) http.HandlerFunc {
				t.Helper()
				return serveJSON(t, testStack())
			},
			args: []string{"create", "--name", "My Stack", "--slug", "mystack"},
			check: func(t *testing.T, doc any) {
				t.Helper()
				obj, ok := doc.(map[string]any)
				require.True(t, ok, "create result should be a JSON object, got %T", doc)
				assert.Equal(t, "mystack", obj["slug"])
			},
		},
		{
			name:    "create --dry-run emits structured preview",
			newCmd:  stacks.NewTestCreateCommandWithLoader,
			handler: rejectCalls,
			args:    []string{"create", "--name", "My Stack", "--slug", "mystack", "--region", "us", "--dry-run"},
			check: func(t *testing.T, doc any) {
				t.Helper()
				obj, ok := doc.(map[string]any)
				require.True(t, ok, "dry-run preview should be a JSON object, got %T", doc)
				assert.Equal(t, "gcx.stacks.dry_run", obj["type"])
				assert.Equal(t, "1", obj["schema_version"])
				assert.Equal(t, "created", obj["action"])
				assert.Equal(t, "POST", obj["method"])
				assert.Equal(t, "/api/instances", obj["endpoint"])
				assert.Equal(t, true, obj["dry_run"])
				req, ok := obj["request"].(map[string]any)
				require.True(t, ok, "preview must carry the request body")
				assert.Equal(t, "mystack", req["slug"])
			},
		},
		{
			name:   "update echoes updated stack",
			newCmd: stacks.NewTestUpdateCommandWithLoader,
			handler: func(t *testing.T) http.HandlerFunc {
				t.Helper()
				return serveJSON(t, testStack())
			},
			args: []string{"update", "mystack", "--name", "New Name"},
			check: func(t *testing.T, doc any) {
				t.Helper()
				obj, ok := doc.(map[string]any)
				require.True(t, ok, "update result should be a JSON object, got %T", doc)
				assert.Equal(t, "mystack", obj["slug"])
			},
		},
		{
			name:    "update --dry-run emits structured preview",
			newCmd:  stacks.NewTestUpdateCommandWithLoader,
			handler: rejectCalls,
			args:    []string{"update", "mystack", "--name", "New Name", "--dry-run"},
			check: func(t *testing.T, doc any) {
				t.Helper()
				obj, ok := doc.(map[string]any)
				require.True(t, ok, "dry-run preview should be a JSON object, got %T", doc)
				assert.Equal(t, "gcx.stacks.dry_run", obj["type"])
				assert.Equal(t, "updated", obj["action"])
				assert.Equal(t, "/api/instances/mystack", obj["endpoint"])
				assert.Equal(t, true, obj["dry_run"])
			},
		},
		{
			name:   "delete --force emits mutation result",
			newCmd: stacks.NewTestDeleteCommandWithLoader,
			handler: func(t *testing.T) http.HandlerFunc {
				t.Helper()
				return serveJSON(t, map[string]any{})
			},
			args: []string{"delete", "mystack", "--force"},
			check: func(t *testing.T, doc any) {
				t.Helper()
				obj, ok := doc.(map[string]any)
				require.True(t, ok, "delete result should be a JSON object, got %T", doc)
				assert.Equal(t, "gcx.mutation", obj["type"])
				assert.Equal(t, "1", obj["schema_version"])
				assert.Equal(t, "deleted", obj["action"])
				assert.Equal(t, true, obj["changed"])
				target, ok := obj["target"].(map[string]any)
				require.True(t, ok, "delete result must carry the target")
				assert.Equal(t, "stack", target["kind"])
				assert.Equal(t, "mystack", target["name"])
			},
		},
		{
			name:    "delete --dry-run emits mutation result",
			newCmd:  stacks.NewTestDeleteCommandWithLoader,
			handler: rejectCalls,
			args:    []string{"delete", "mystack", "--dry-run"},
			check: func(t *testing.T, doc any) {
				t.Helper()
				obj, ok := doc.(map[string]any)
				require.True(t, ok, "delete dry-run result should be a JSON object, got %T", doc)
				assert.Equal(t, "gcx.mutation", obj["type"])
				assert.Equal(t, "deleted", obj["action"])
				assert.Equal(t, true, obj["dry_run"])
				assert.NotContains(t, obj, "changed", "dry-run cannot claim a state change")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setAgentMode(t, true)

			loader := newCloudFixture(t, tt.handler(t))
			stdout, _, err := runCmdSplit(t, tt.newCmd(loader), tt.args, "")
			require.NoError(t, err)

			doc := decodeSingleJSONDocument(t, stdout)
			tt.check(t, doc)
		})
	}
}

// TestHumanDefault_ByteIdentical pins the exact human stdout of the migrated
// paths to the bytes the pre-codec implementation produced.
func TestHumanDefault_ByteIdentical(t *testing.T) {
	tests := []struct {
		name    string
		newCmd  func(loader *providers.ConfigLoader) *cobra.Command
		handler func(t *testing.T) http.HandlerFunc
		args    []string
		want    string
	}{
		{
			name:    "delete --dry-run",
			newCmd:  stacks.NewTestDeleteCommandWithLoader,
			handler: rejectCalls,
			args:    []string{"delete", "mystack", "--dry-run"},
			want: "Dry run: DELETE /api/instances/mystack\n" +
				"\n" +
				"Stack \"mystack\" would be permanently deleted. No changes were made.\n",
		},
		{
			name:   "delete --force success",
			newCmd: stacks.NewTestDeleteCommandWithLoader,
			handler: func(t *testing.T) http.HandlerFunc {
				t.Helper()
				return serveJSON(t, map[string]any{})
			},
			args: []string{"delete", "mystack", "--force"},
			want: "✔ Stack \"mystack\" deleted successfully.\n",
		},
		{
			name:    "create --dry-run",
			newCmd:  stacks.NewTestCreateCommandWithLoader,
			handler: rejectCalls,
			args:    []string{"create", "--name", "My Stack", "--slug", "mystack", "--region", "us", "--dry-run"},
			want: "Dry run: POST /api/instances\n" +
				"\n" +
				"{\n" +
				"  \"name\": \"My Stack\",\n" +
				"  \"slug\": \"mystack\",\n" +
				"  \"region\": \"us\"\n" +
				"}\n",
		},
		{
			name:    "update --dry-run",
			newCmd:  stacks.NewTestUpdateCommandWithLoader,
			handler: rejectCalls,
			args:    []string{"update", "mystack", "--name", "New Name", "--dry-run"},
			want: "Dry run: POST /api/instances/mystack\n" +
				"\n" +
				"{\n" +
				"  \"name\": \"New Name\"\n" +
				"}\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setAgentMode(t, false)

			loader := newCloudFixture(t, tt.handler(t))
			stdout, _, err := runCmdSplit(t, tt.newCmd(loader), tt.args, "")
			require.NoError(t, err)
			assert.Equal(t, tt.want, stdout)
		})
	}
}

// TestExplicitOutputOverride verifies explicit -o json/yaml always wins —
// including on the dry-run paths that used to bypass the codec system, and
// over the agents default in agent mode.
func TestExplicitOutputOverride(t *testing.T) {
	t.Run("delete -o json emits structured document in human mode", func(t *testing.T) {
		setAgentMode(t, false)

		loader := newCloudFixture(t, serveJSON(t, map[string]any{}))
		stdout, _, err := runCmdSplit(t, stacks.NewTestDeleteCommandWithLoader(loader),
			[]string{"delete", "mystack", "--force", "-o", "json"}, "")
		require.NoError(t, err)

		doc, ok := decodeSingleJSONDocument(t, stdout).(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "gcx.mutation", doc["type"])
		assert.Equal(t, "deleted", doc["action"])
	})

	t.Run("delete --dry-run -o yaml emits structured document", func(t *testing.T) {
		setAgentMode(t, false)

		loader := newCloudFixture(t, rejectCalls(t))
		stdout, _, err := runCmdSplit(t, stacks.NewTestDeleteCommandWithLoader(loader),
			[]string{"delete", "mystack", "--dry-run", "-o", "yaml"}, "")
		require.NoError(t, err)

		var doc map[string]any
		require.NoError(t, yaml.Unmarshal([]byte(stdout), &doc))
		assert.Equal(t, "gcx.mutation", doc["type"])
		assert.Equal(t, true, doc["dry_run"])
	})

	t.Run("create --dry-run -o json emits structured preview", func(t *testing.T) {
		setAgentMode(t, false)

		loader := newCloudFixture(t, rejectCalls(t))
		stdout, _, err := runCmdSplit(t, stacks.NewTestCreateCommandWithLoader(loader),
			[]string{"create", "--name", "My Stack", "--slug", "mystack", "--dry-run", "-o", "json"}, "")
		require.NoError(t, err)

		doc, ok := decodeSingleJSONDocument(t, stdout).(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "gcx.stacks.dry_run", doc["type"])
		assert.Equal(t, "created", doc["action"])
	})

	t.Run("explicit -o yaml beats agents default in agent mode", func(t *testing.T) {
		setAgentMode(t, true)

		loader := newCloudFixture(t, serveJSON(t, testStack()))
		stdout, _, err := runCmdSplit(t, stacks.NewTestGetCommandWithLoader(loader),
			[]string{"get", "mystack", "-o", "yaml"}, "")
		require.NoError(t, err)

		var doc map[string]any
		require.NoError(t, yaml.Unmarshal([]byte(stdout), &doc))
		assert.Equal(t, "mystack", doc["slug"])
		// YAML, not a JSON document: the JSON decoder must reject it.
		assert.False(t, json.Valid([]byte(stdout)), "-o yaml must not produce JSON, got: %q", stdout)
	})

	t.Run("explicit -o text beats agents default for delete", func(t *testing.T) {
		setAgentMode(t, true)

		loader := newCloudFixture(t, rejectCalls(t))
		stdout, _, err := runCmdSplit(t, stacks.NewTestDeleteCommandWithLoader(loader),
			[]string{"delete", "mystack", "--dry-run", "-o", "text"}, "")
		require.NoError(t, err)
		assert.Equal(t, "Dry run: DELETE /api/instances/mystack\n"+
			"\n"+
			"Stack \"mystack\" would be permanently deleted. No changes were made.\n", stdout)
	})
}

// TestDeleteAgentModeGuard pins the destructive-operation guard: agent mode
// without --force must fail fast without touching the API or stdout.
func TestDeleteAgentModeGuard(t *testing.T) {
	setAgentMode(t, true)

	loader := newCloudFixture(t, rejectCalls(t))
	stdout, _, err := runCmdSplit(t, stacks.NewTestDeleteCommandWithLoader(loader),
		[]string{"delete", "mystack"}, "")

	require.Error(t, err)
	require.ErrorIs(t, err, providers.ErrAgentModeRequiresForce)
	assert.Empty(t, stdout, "guard rejection must not write to stdout")
}

// TestMutationDiagnosticsStayOffStdout verifies the interactive confirmation
// prompt renders on stderr, keeping stdout reserved for the result document.
func TestMutationDiagnosticsStayOffStdout(t *testing.T) {
	setAgentMode(t, false)

	loader := newCloudFixture(t, serveJSON(t, map[string]any{}))
	stdout, stderr, err := runCmdSplit(t, stacks.NewTestDeleteCommandWithLoader(loader),
		[]string{"delete", "mystack"}, "mystack\n")
	require.NoError(t, err)

	assert.Contains(t, stderr, "WARNING", "confirmation prompt belongs on stderr")
	assert.Contains(t, stderr, "Type the stack slug to confirm")
	assert.Equal(t, "✔ Stack \"mystack\" deleted successfully.\n", stdout,
		"stdout must carry only the result line")
}
