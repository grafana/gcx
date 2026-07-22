package kg_test

// These tests pin the agent output contract for the kg command family:
//   - in agent mode (no explicit -o), a finite command writes EXACTLY ONE
//     JSON value to stdout (json.Decoder + io.EOF check);
//   - the default human stdout stays byte-identical to the pre-migration
//     one-liners (hardcoded expected lines);
//   - partial failures return *gcxerrors.EmittedError with ExitPartialFailure
//     after the complete result document was written;
//   - explicit -o json/yaml always wins.

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/fatih/color"
	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/gcxerrors"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/providers/kg"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// forceNoColor pins styled output to its plain-text form so the hardcoded
// expected lines are stable regardless of the test environment.
func forceNoColor(t *testing.T) {
	t.Helper()
	old := color.NoColor
	color.NoColor = true
	t.Cleanup(func() { color.NoColor = old })
}

// setAgentMode enables agent-mode detection for the duration of the test. It
// must be called BEFORE constructing the command under test: the agents
// default format is resolved when the command binds its flags.
func setAgentMode(t *testing.T) {
	t.Helper()
	agent.SetFlag(true)
	t.Cleanup(func() { agent.SetFlag(false) })
}

// pinHumanMode forces agent-mode detection off for the duration of the test
// so ambient env detection (e.g. CLAUDECODE=1 when the suite runs inside an
// agent harness) cannot flip output defaults; agent-mode subtests opt back in
// via setAgentMode. Mirrors cmd/gcx/resources/pull_edit_format_test.go.
func pinHumanMode(t *testing.T) {
	t.Helper()
	agent.SetFlag(false)
	t.Cleanup(func() { agent.SetFlag(false) })
}

// decodeSingleJSON asserts stdout holds exactly one JSON value followed by
// EOF, and returns that value.
func decodeSingleJSON(t *testing.T, out []byte) any {
	t.Helper()
	dec := json.NewDecoder(bytes.NewReader(out))
	var first any
	require.NoError(t, dec.Decode(&first), "stdout must be valid JSON:\n%s", string(out))
	var second any
	err := dec.Decode(&second)
	require.ErrorIs(t, err, io.EOF, "stdout must contain exactly one JSON value:\n%s", string(out))
	return first
}

// runKgCommand executes a kg command built by build with the given args and
// stdin, returning stdout, stderr, and the execution error.
func runKgCommand(t *testing.T, build func() *cobra.Command, args []string, stdin string) (string, string, error) {
	t.Helper()
	cmd := build()
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetIn(bytes.NewBufferString(stdin))
	cmd.SetArgs(args)
	err := cmd.Execute()
	return stdout.String(), stderr.String(), err
}

// okHandler accepts every request; deletes and YAML uploads only check the
// status code, so a bare 200 satisfies all single-mutation endpoints.
func okHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }
}

const twoSuppressionsYAML = `disabledAlertConfigs:
  - name: first
    matchLabels:
      alertname: A
  - name: second
    matchLabels:
      alertname: B
`

// TestKgSingleMutations_OutputContract covers every kg mutation that emits a
// cmdio.SingleMutation document: byte-identical human default line, exactly
// one JSON document in agent mode, and explicit -o json/yaml override.
func TestKgSingleMutations_OutputContract(t *testing.T) {
	forceNoColor(t)
	pinHumanMode(t)

	tests := []struct {
		name           string
		build          func(loader *kg.FakeWriteLoader) *cobra.Command
		args           []string
		stdin          string
		wantHumanLine  string
		wantAction     string
		wantTargetName string
	}{
		{
			name:          "prom-rules upsert",
			build:         func(l *kg.FakeWriteLoader) *cobra.Command { return kg.NewRulesCommand(l) },
			args:          []string{"upsert", "-f", "-"},
			stdin:         "groups: []\n",
			wantHumanLine: "✔ Knowledge Graph rules uploaded\n",
			wantAction:    "upserted",
		},
		{
			name:           "prom-rules delete",
			build:          func(l *kg.FakeWriteLoader) *cobra.Command { return kg.NewRulesCommand(l) },
			args:           []string{"delete", "my-rule", "--force"},
			wantHumanLine:  "✔ Knowledge Graph rule \"my-rule\" deleted\n",
			wantAction:     "deleted",
			wantTargetName: "my-rule",
		},
		{
			name:          "model-rules upsert",
			build:         func(l *kg.FakeWriteLoader) *cobra.Command { return kg.NewModelRulesCommand(l) },
			args:          []string{"upsert", "-f", "-"},
			stdin:         "name: m\n",
			wantHumanLine: "✔ Model rules uploaded\n",
			wantAction:    "upserted",
		},
		{
			name:           "model-rules delete",
			build:          func(l *kg.FakeWriteLoader) *cobra.Command { return kg.NewModelRulesCommand(l) },
			args:           []string{"delete", "my-model", "--force"},
			wantHumanLine:  "✔ Model rules \"my-model\" deleted\n",
			wantAction:     "deleted",
			wantTargetName: "my-model",
		},
		{
			name:           "suppressions delete",
			build:          func(l *kg.FakeWriteLoader) *cobra.Command { return kg.NewSuppressionsCommand(l) },
			args:           []string{"delete", "my-suppression", "--force"},
			wantHumanLine:  "✔ Suppression \"my-suppression\" deleted\n",
			wantAction:     "deleted",
			wantTargetName: "my-suppression",
		},
		{
			name:           "entities delete",
			build:          func(l *kg.FakeWriteLoader) *cobra.Command { return kg.NewEntitiesDeleteCommand(l) },
			args:           []string{"Service--checkout", "--domain", "myapp", "--force"},
			wantHumanLine:  "✔ entity Service--checkout deleted\n",
			wantAction:     "deleted",
			wantTargetName: "checkout",
		},
		{
			name:  "relationships delete",
			build: func(l *kg.FakeWriteLoader) *cobra.Command { return kg.NewRelationshipsDeleteCommand(l) },
			args: []string{"--type", "CALLS", "--from", "myapp/Service/checkout",
				"--to", "myapp/Service/cart", "--force"},
			wantHumanLine:  "✔ relationship \"CALLS\" deleted\n",
			wantAction:     "deleted",
			wantTargetName: "CALLS",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name+" human default", func(t *testing.T) {
			server := httptest.NewServer(okHandler())
			defer server.Close()
			stdout, _, err := runKgCommand(t, func() *cobra.Command { return tc.build(writeLoaderFor(server)) }, tc.args, tc.stdin)
			require.NoError(t, err)
			assert.Equal(t, tc.wantHumanLine, stdout, "default human stdout must stay byte-identical")
		})

		t.Run(tc.name+" agent mode", func(t *testing.T) {
			setAgentMode(t)
			server := httptest.NewServer(okHandler())
			defer server.Close()
			stdout, _, err := runKgCommand(t, func() *cobra.Command { return tc.build(writeLoaderFor(server)) }, tc.args, tc.stdin)
			require.NoError(t, err)
			doc, ok := decodeSingleJSON(t, []byte(stdout)).(map[string]any)
			require.True(t, ok, "agent-mode document must be an object")
			assert.Equal(t, "gcx.mutation", doc["type"])
			assert.Equal(t, "1", doc["schema_version"])
			assert.Equal(t, tc.wantAction, doc["action"])
			if tc.wantTargetName != "" {
				target, _ := doc["target"].(map[string]any)
				require.NotNil(t, target)
				assert.Equal(t, tc.wantTargetName, target["name"])
			}
		})

		t.Run(tc.name+" explicit -o json", func(t *testing.T) {
			server := httptest.NewServer(okHandler())
			defer server.Close()
			stdout, _, err := runKgCommand(t, func() *cobra.Command { return tc.build(writeLoaderFor(server)) },
				append(append([]string{}, tc.args...), "-o", "json"), tc.stdin)
			require.NoError(t, err)
			doc, ok := decodeSingleJSON(t, []byte(stdout)).(map[string]any)
			require.True(t, ok)
			assert.Equal(t, "gcx.mutation", doc["type"])
		})

		t.Run(tc.name+" explicit -o yaml", func(t *testing.T) {
			server := httptest.NewServer(okHandler())
			defer server.Close()
			stdout, _, err := runKgCommand(t, func() *cobra.Command { return tc.build(writeLoaderFor(server)) },
				append(append([]string{}, tc.args...), "-o", "yaml"), tc.stdin)
			require.NoError(t, err)
			var doc map[string]any
			require.NoError(t, yaml.Unmarshal([]byte(stdout), &doc))
			assert.Equal(t, "gcx.mutation", doc["type"])
		})
	}
}

// TestKgPromRulesDelete_ConfirmGuard pins the new destructive-delete guard on
// `kg prom-rules delete`: interactive decline aborts without a request (with
// the prompt on stderr, never stdout), and agent mode without --force is
// rejected before any request.
func TestKgPromRulesDelete_ConfirmGuard(t *testing.T) {
	forceNoColor(t)
	pinHumanMode(t)

	t.Run("interactive decline", func(t *testing.T) {
		t.Setenv("GCX_AUTO_APPROVE", "0")
		called := false
		server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }))
		defer server.Close()
		stdout, stderr, err := runKgCommand(t,
			func() *cobra.Command { return kg.NewRulesCommand(writeLoaderFor(server)) },
			[]string{"delete", "my-rule"}, "n\n")
		require.NoError(t, err)
		assert.False(t, called, "declined confirmation must not issue DELETE")
		assert.Empty(t, stdout, "prompt and abort note must not contaminate stdout")
		assert.Contains(t, stderr, `Delete Knowledge Graph rule "my-rule"? [y/N]`)
		assert.Contains(t, stderr, "Aborted.")
	})

	t.Run("agent mode requires --force", func(t *testing.T) {
		t.Setenv("GCX_AUTO_APPROVE", "0")
		setAgentMode(t)
		called := false
		server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }))
		defer server.Close()
		stdout, _, err := runKgCommand(t,
			func() *cobra.Command { return kg.NewRulesCommand(writeLoaderFor(server)) },
			[]string{"delete", "my-rule"}, "")
		require.ErrorIs(t, err, providers.ErrAgentModeRequiresForce)
		assert.False(t, called)
		assert.Empty(t, stdout)
	})
}

// TestKgDeletePromptsOnStderr pins that the guarded delete prompts write to
// stderr (previously stdout) for the pre-existing ConfirmDestructive callers.
func TestKgDeletePromptsOnStderr(t *testing.T) {
	forceNoColor(t)
	pinHumanMode(t)
	t.Setenv("GCX_AUTO_APPROVE", "0")

	tests := []struct {
		name  string
		build func(loader *kg.FakeWriteLoader) *cobra.Command
		args  []string
	}{
		{
			name:  "model-rules delete",
			build: func(l *kg.FakeWriteLoader) *cobra.Command { return kg.NewModelRulesCommand(l) },
			args:  []string{"delete", "m"},
		},
		{
			name:  "suppressions delete",
			build: func(l *kg.FakeWriteLoader) *cobra.Command { return kg.NewSuppressionsCommand(l) },
			args:  []string{"delete", "s"},
		},
		{
			name:  "entities delete",
			build: func(l *kg.FakeWriteLoader) *cobra.Command { return kg.NewEntitiesDeleteCommand(l) },
			args:  []string{"Service--checkout", "--domain", "myapp"},
		},
		{
			name:  "relationships delete",
			build: func(l *kg.FakeWriteLoader) *cobra.Command { return kg.NewRelationshipsDeleteCommand(l) },
			args:  []string{"--type", "CALLS", "--from", "myapp/Service/a", "--to", "myapp/Service/b"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			called := false
			server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }))
			defer server.Close()
			stdout, stderr, err := runKgCommand(t,
				func() *cobra.Command { return tc.build(writeLoaderFor(server)) }, tc.args, "n\n")
			require.NoError(t, err)
			assert.False(t, called, "declined confirmation must not issue DELETE")
			assert.Empty(t, stdout, "prompt and abort note must not contaminate stdout")
			assert.Contains(t, stderr, "[y/N]")
			assert.Contains(t, stderr, "Aborted.")
		})
	}
}

// suppressionsUpsertHandler accepts the single-suppression write endpoint,
// failing every request after the first okCount with a 500.
func suppressionsUpsertHandler(okCount int) http.HandlerFunc {
	seen := 0
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "disabled-alert") {
			seen++
			if seen > okCount {
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte(`{"message":"boom"}`))
				return
			}
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}
}

func TestKgSuppressionsUpsert_OutputContract(t *testing.T) {
	forceNoColor(t)
	pinHumanMode(t)

	t.Run("success human default", func(t *testing.T) {
		server := httptest.NewServer(suppressionsUpsertHandler(2))
		defer server.Close()
		stdout, _, err := runKgCommand(t,
			func() *cobra.Command { return kg.NewSuppressionsCommand(writeLoaderFor(server)) },
			[]string{"upsert", "-f", "-"}, twoSuppressionsYAML)
		require.NoError(t, err)
		assert.Equal(t, "✔ 2 suppression(s) upserted\n", stdout,
			"default human stdout must stay byte-identical")
	})

	t.Run("success agent mode", func(t *testing.T) {
		setAgentMode(t)
		server := httptest.NewServer(suppressionsUpsertHandler(2))
		defer server.Close()
		stdout, _, err := runKgCommand(t,
			func() *cobra.Command { return kg.NewSuppressionsCommand(writeLoaderFor(server)) },
			[]string{"upsert", "-f", "-"}, twoSuppressionsYAML)
		require.NoError(t, err)
		doc, ok := decodeSingleJSON(t, []byte(stdout)).(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "gcx.mutation_batch", doc["type"])
		summary, _ := doc["summary"].(map[string]any)
		require.NotNil(t, summary)
		assert.InDelta(t, 2, summary["succeeded"], 0)
		assert.InDelta(t, 0, summary["failed"], 0)
		failures, ok := doc["failures"].([]any)
		require.True(t, ok, "failures must serialize as [] on success")
		assert.Empty(t, failures)
	})

	t.Run("explicit -o json on the real write path", func(t *testing.T) {
		// Previously -o json was silently ignored outside --dry-run.
		server := httptest.NewServer(suppressionsUpsertHandler(2))
		defer server.Close()
		stdout, _, err := runKgCommand(t,
			func() *cobra.Command { return kg.NewSuppressionsCommand(writeLoaderFor(server)) },
			[]string{"upsert", "-f", "-", "-o", "json"}, twoSuppressionsYAML)
		require.NoError(t, err)
		doc, ok := decodeSingleJSON(t, []byte(stdout)).(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "gcx.mutation_batch", doc["type"])
	})

	t.Run("partial failure agent mode", func(t *testing.T) {
		setAgentMode(t)
		server := httptest.NewServer(suppressionsUpsertHandler(1))
		defer server.Close()
		// Three entries: first succeeds, second fails, third never attempted.
		yamlIn := twoSuppressionsYAML + "  - name: third\n    matchLabels:\n      alertname: C\n"
		stdout, stderr, err := runKgCommand(t,
			func() *cobra.Command { return kg.NewSuppressionsCommand(writeLoaderFor(server)) },
			[]string{"upsert", "-f", "-"}, yamlIn)

		var emitted *gcxerrors.EmittedError
		require.ErrorAs(t, err, &emitted, "partial failure must return EmittedError, got %T (%v)", err, err)
		assert.Equal(t, gcxerrors.ExitPartialFailure, emitted.Code)

		doc, ok := decodeSingleJSON(t, []byte(stdout)).(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "gcx.mutation_batch", doc["type"])
		summary, _ := doc["summary"].(map[string]any)
		require.NotNil(t, summary)
		assert.InDelta(t, 1, summary["succeeded"], 0)
		assert.InDelta(t, 1, summary["failed"], 0)
		assert.InDelta(t, 1, summary["skipped"], 0)
		failures, _ := doc["failures"].([]any)
		require.Len(t, failures, 1)
		failure, _ := failures[0].(map[string]any)
		target, _ := failure["target"].(map[string]any)
		require.NotNil(t, target)
		assert.Equal(t, "second", target["name"])
		assert.Contains(t, stderr, "failed to upsert suppression")
	})

	t.Run("partial failure human default", func(t *testing.T) {
		server := httptest.NewServer(suppressionsUpsertHandler(1))
		defer server.Close()
		stdout, stderr, err := runKgCommand(t,
			func() *cobra.Command { return kg.NewSuppressionsCommand(writeLoaderFor(server)) },
			[]string{"upsert", "-f", "-"}, twoSuppressionsYAML)

		var emitted *gcxerrors.EmittedError
		require.ErrorAs(t, err, &emitted)
		assert.Equal(t, gcxerrors.ExitPartialFailure, emitted.Code)
		assert.Equal(t, "⚠ 1 suppression(s) upserted, 1 failed (0 skipped)\n", stdout)
		assert.Contains(t, stderr, `failed to upsert suppression "second" (1/2 succeeded)`)
	})

	t.Run("total failure emits no result document", func(t *testing.T) {
		setAgentMode(t)
		server := httptest.NewServer(suppressionsUpsertHandler(0))
		defer server.Close()
		stdout, _, err := runKgCommand(t,
			func() *cobra.Command { return kg.NewSuppressionsCommand(writeLoaderFor(server)) },
			[]string{"upsert", "-f", "-"}, twoSuppressionsYAML)
		require.Error(t, err)
		var emitted *gcxerrors.EmittedError
		assert.NotErrorAs(t, err, &emitted,
			"a total failure must use the standard error path, not EmittedError")
		assert.Empty(t, stdout, "no result document when nothing succeeded — the reporter owns the error document")
	})
}

// entitiesUpsertHandler serves the entity write endpoint, failing every
// request after the first okCount with a 500.
func entitiesUpsertHandler(okCount int) http.HandlerFunc {
	seen := 0
	return func(w http.ResponseWriter, r *http.Request) {
		seen++
		if seen > okCount {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"error":"boom"}`))
			return
		}
		var body kg.EntityWriteRequest
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.WriteHeader(http.StatusCreated)
		writeJSON(w, kg.EntityWriteResponse{Domain: body.Domain, Type: body.Type, Name: body.Name})
	}
}

const twoEntitiesYAML = "- domain: myapp\n  type: Service\n  name: checkout\n" +
	"- domain: myapp\n  type: Service\n  name: cart\n"

func TestKgEntitiesUpsert_OutputContract(t *testing.T) {
	forceNoColor(t)
	pinHumanMode(t)

	t.Run("single success human default stays the echoed object", func(t *testing.T) {
		server := httptest.NewServer(entitiesUpsertHandler(1))
		defer server.Close()
		stdout, _, err := runKgCommand(t,
			func() *cobra.Command { return kg.NewEntitiesCreateCommand(writeLoaderFor(server)) },
			[]string{"--domain", "myapp", "--type", "Service", "--name", "checkout"}, "")
		require.NoError(t, err)
		//nolint:testifylint // byte-identical output (not JSON equivalence) is the contract under test
		assert.Equal(t, "{\n  \"domain\": \"myapp\",\n  \"type\": \"Service\",\n  \"name\": \"checkout\"\n}\n",
			stdout, "single-entity human default must stay byte-identical")
	})

	t.Run("single success agent mode is one object document", func(t *testing.T) {
		setAgentMode(t)
		server := httptest.NewServer(entitiesUpsertHandler(1))
		defer server.Close()
		stdout, _, err := runKgCommand(t,
			func() *cobra.Command { return kg.NewEntitiesCreateCommand(writeLoaderFor(server)) },
			[]string{"--domain", "myapp", "--type", "Service", "--name", "checkout"}, "")
		require.NoError(t, err)
		doc, ok := decodeSingleJSON(t, []byte(stdout)).(map[string]any)
		require.True(t, ok, "single-request result must stay an object")
		assert.Equal(t, "checkout", doc["name"])
	})

	t.Run("array input agent mode is one array document", func(t *testing.T) {
		setAgentMode(t)
		server := httptest.NewServer(entitiesUpsertHandler(2))
		defer server.Close()
		stdout, _, err := runKgCommand(t,
			func() *cobra.Command { return kg.NewEntitiesCreateCommand(writeLoaderFor(server)) },
			[]string{"-f", "-"}, twoEntitiesYAML)
		require.NoError(t, err)
		docs, ok := decodeSingleJSON(t, []byte(stdout)).([]any)
		require.True(t, ok, "array input must produce one array document, not N concatenated documents")
		require.Len(t, docs, 2)
	})

	t.Run("array partial failure agent mode", func(t *testing.T) {
		setAgentMode(t)
		server := httptest.NewServer(entitiesUpsertHandler(1))
		defer server.Close()
		stdout, stderr, err := runKgCommand(t,
			func() *cobra.Command { return kg.NewEntitiesCreateCommand(writeLoaderFor(server)) },
			[]string{"-f", "-"}, twoEntitiesYAML)

		var emitted *gcxerrors.EmittedError
		require.ErrorAs(t, err, &emitted, "mid-loop failure after a success must be EmittedError, got %T (%v)", err, err)
		assert.Equal(t, gcxerrors.ExitPartialFailure, emitted.Code)

		docs, ok := decodeSingleJSON(t, []byte(stdout)).([]any)
		require.True(t, ok, "partial failure must still emit exactly one array document")
		require.Len(t, docs, 1, "only the succeeded entries are echoed")
		assert.Contains(t, stderr, "failed to upsert entity")
	})

	t.Run("first entry failure uses the standard error path", func(t *testing.T) {
		setAgentMode(t)
		server := httptest.NewServer(entitiesUpsertHandler(0))
		defer server.Close()
		stdout, _, err := runKgCommand(t,
			func() *cobra.Command { return kg.NewEntitiesCreateCommand(writeLoaderFor(server)) },
			[]string{"-f", "-"}, twoEntitiesYAML)
		require.Error(t, err)
		var emitted *gcxerrors.EmittedError
		assert.NotErrorAs(t, err, &emitted)
		assert.Empty(t, stdout)
	})
}

func TestKgRelationshipsUpsert_OutputContract(t *testing.T) {
	forceNoColor(t)
	pinHumanMode(t)

	relHandler := func(okCount int) http.HandlerFunc {
		seen := 0
		return func(w http.ResponseWriter, r *http.Request) {
			seen++
			if seen > okCount {
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte(`{"error":"boom"}`))
				return
			}
			writeJSON(w, kg.RelationshipWriteResponse{Type: "CALLS"})
		}
	}
	const twoRelsYAML = "- domain: myapp\n  type: CALLS\n" +
		"  from: {domain: myapp, type: Service, name: checkout}\n" +
		"  to: {domain: myapp, type: Service, name: cart}\n" +
		"- domain: myapp\n  type: CALLS\n" +
		"  from: {domain: myapp, type: Service, name: cart}\n" +
		"  to: {domain: myapp, type: Service, name: db}\n"

	t.Run("array input agent mode is one array document", func(t *testing.T) {
		setAgentMode(t)
		server := httptest.NewServer(relHandler(2))
		defer server.Close()
		stdout, _, err := runKgCommand(t,
			func() *cobra.Command { return kg.NewRelationshipsCreateCommand(writeLoaderFor(server)) },
			[]string{"-f", "-"}, twoRelsYAML)
		require.NoError(t, err)
		docs, ok := decodeSingleJSON(t, []byte(stdout)).([]any)
		require.True(t, ok, "array input must produce one array document")
		require.Len(t, docs, 2)
	})

	t.Run("array partial failure agent mode", func(t *testing.T) {
		setAgentMode(t)
		server := httptest.NewServer(relHandler(1))
		defer server.Close()
		stdout, stderr, err := runKgCommand(t,
			func() *cobra.Command { return kg.NewRelationshipsCreateCommand(writeLoaderFor(server)) },
			[]string{"-f", "-"}, twoRelsYAML)

		var emitted *gcxerrors.EmittedError
		require.ErrorAs(t, err, &emitted)
		assert.Equal(t, gcxerrors.ExitPartialFailure, emitted.Code)
		docs, ok := decodeSingleJSON(t, []byte(stdout)).([]any)
		require.True(t, ok)
		require.Len(t, docs, 1)
		assert.Contains(t, stderr, "failed to upsert relationship")
	})

	t.Run("single success agent mode is one object document", func(t *testing.T) {
		setAgentMode(t)
		server := httptest.NewServer(relHandler(1))
		defer server.Close()
		stdout, _, err := runKgCommand(t,
			func() *cobra.Command { return kg.NewRelationshipsCreateCommand(writeLoaderFor(server)) },
			[]string{"--type", "CALLS", "--domain", "myapp",
				"--from", "myapp/Service/checkout", "--to", "myapp/Service/cart"}, "")
		require.NoError(t, err)
		doc, ok := decodeSingleJSON(t, []byte(stdout)).(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "CALLS", doc["type"])
	})
}

func TestKgRelabelRulesGet_NotConfigured(t *testing.T) {
	forceNoColor(t)
	pinHumanMode(t)

	noContent := func() http.HandlerFunc {
		return func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }
	}

	t.Run("human default keeps empty stdout with stderr note", func(t *testing.T) {
		server := httptest.NewServer(noContent())
		defer server.Close()
		stdout, stderr, err := runKgCommand(t,
			func() *cobra.Command { return kg.NewRelabelRulesCommand(writeLoaderFor(server)) },
			[]string{"get"}, "")
		require.NoError(t, err)
		assert.Empty(t, stdout, "human default for a missing group has always been empty stdout")
		assert.Contains(t, stderr, "No generated relabel rules configured.")
	})

	t.Run("agent mode emits exactly one JSON value", func(t *testing.T) {
		setAgentMode(t)
		server := httptest.NewServer(noContent())
		defer server.Close()
		stdout, stderr, err := runKgCommand(t,
			func() *cobra.Command { return kg.NewRelabelRulesCommand(writeLoaderFor(server)) },
			[]string{"get"}, "")
		require.NoError(t, err)
		doc := decodeSingleJSON(t, []byte(stdout))
		assert.Nil(t, doc, "the not-configured group encodes as JSON null, never empty stdout")
		assert.Contains(t, stderr, "No generated relabel rules configured.")
	})

	t.Run("agent mode configured group is one document", func(t *testing.T) {
		setAgentMode(t)
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(w, map[string]any{"name": "generated", "rules": []any{}})
		}))
		defer server.Close()
		stdout, _, err := runKgCommand(t,
			func() *cobra.Command { return kg.NewRelabelRulesCommand(writeLoaderFor(server)) },
			[]string{"get"}, "")
		require.NoError(t, err)
		doc, ok := decodeSingleJSON(t, []byte(stdout)).(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "generated", doc["name"])
	})
}

func TestKgInsights_OutputContract(t *testing.T) {
	forceNoColor(t)
	pinHumanMode(t)

	chartServer := func() *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !strings.HasSuffix(r.URL.Path, "entity-metric") {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"thresholds":[],"metrics":[]}`))
		}))
	}
	sourcesServer := func() *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !strings.HasSuffix(r.URL.Path, "source-metrics") {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[{"metricName":"up","labels":[]}]`))
		}))
	}
	const chartRequestYAML = "startTime: 1\nendTime: 2\nlabels:\n  alertname: X\n"

	t.Run("chart human default stays plain JSON", func(t *testing.T) {
		server := chartServer()
		defer server.Close()
		stdout, _, err := runKgCommand(t,
			func() *cobra.Command { return kg.NewInsightsCommand(writeLoaderFor(server)) },
			[]string{"chart", "-f", "-"}, chartRequestYAML)
		require.NoError(t, err)
		//nolint:testifylint // byte-identical output (not JSON equivalence) is the contract under test
		assert.Equal(t, "{\n  \"thresholds\": [],\n  \"metrics\": []\n}\n", stdout,
			"human default must stay the exact JSON the command always printed")
	})

	t.Run("chart agent mode emits one document", func(t *testing.T) {
		setAgentMode(t)
		server := chartServer()
		defer server.Close()
		stdout, _, err := runKgCommand(t,
			func() *cobra.Command { return kg.NewInsightsCommand(writeLoaderFor(server)) },
			[]string{"chart", "-f", "-"}, chartRequestYAML)
		require.NoError(t, err)
		doc, ok := decodeSingleJSON(t, []byte(stdout)).(map[string]any)
		require.True(t, ok)
		assert.Contains(t, doc, "metrics")
	})

	t.Run("chart explicit -o yaml", func(t *testing.T) {
		server := chartServer()
		defer server.Close()
		stdout, _, err := runKgCommand(t,
			func() *cobra.Command { return kg.NewInsightsCommand(writeLoaderFor(server)) },
			[]string{"chart", "-f", "-", "-o", "yaml"}, chartRequestYAML)
		require.NoError(t, err)
		var doc map[string]any
		require.NoError(t, yaml.Unmarshal([]byte(stdout), &doc))
		assert.Contains(t, doc, "metrics")
	})

	t.Run("sources human default stays plain JSON", func(t *testing.T) {
		server := sourcesServer()
		defer server.Close()
		stdout, _, err := runKgCommand(t,
			func() *cobra.Command { return kg.NewInsightsCommand(writeLoaderFor(server)) },
			[]string{"sources", "-f", "-"}, chartRequestYAML)
		require.NoError(t, err)
		//nolint:testifylint // byte-identical output (not JSON equivalence) is the contract under test
		assert.Equal(t, "[\n  {\n    \"metricName\": \"up\",\n    \"labels\": []\n  }\n]\n", stdout,
			"human default must stay the exact JSON the command always printed")
	})

	t.Run("sources agent mode emits one document", func(t *testing.T) {
		setAgentMode(t)
		server := sourcesServer()
		defer server.Close()
		stdout, _, err := runKgCommand(t,
			func() *cobra.Command { return kg.NewInsightsCommand(writeLoaderFor(server)) },
			[]string{"sources", "-f", "-"}, chartRequestYAML)
		require.NoError(t, err)
		docs, ok := decodeSingleJSON(t, []byte(stdout)).([]any)
		require.True(t, ok)
		require.Len(t, docs, 1)
	})
}

func TestKgMetaAll_SectionErrorsInPayload(t *testing.T) {
	forceNoColor(t)
	pinHumanMode(t)

	failAll := func() http.HandlerFunc {
		return func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"message":"section down"}`))
		}
	}

	t.Run("agent mode records section failures in the document", func(t *testing.T) {
		setAgentMode(t)
		server := httptest.NewServer(failAll())
		defer server.Close()
		stdout, stderr, err := runKgCommand(t,
			func() *cobra.Command { return kg.NewDescribeAllCommand(writeLoaderFor(server)) },
			nil, "")
		require.NoError(t, err, "meta all stays best-effort (exit 0)")
		doc, ok := decodeSingleJSON(t, []byte(stdout)).(map[string]any)
		require.True(t, ok)
		errsMap, ok := doc["errors"].(map[string]any)
		require.True(t, ok, "failed sections must be legible from stdout, not only stderr")
		for _, section := range []string{"schema", "scopes", "logs", "traces", "profiles"} {
			assert.Contains(t, errsMap, section)
		}
		assert.Contains(t, doc, "metricGuide", "locally-defined section still present")
		assert.Contains(t, stderr, "failed to load")
	})

	t.Run("human default text does not render the errors map", func(t *testing.T) {
		server := httptest.NewServer(failAll())
		defer server.Close()
		stdout, stderr, err := runKgCommand(t,
			func() *cobra.Command { return kg.NewDescribeAllCommand(writeLoaderFor(server)) },
			nil, "")
		require.NoError(t, err)
		assert.NotContains(t, stdout, "section down", "human text output is unchanged")
		assert.Contains(t, stderr, "warning: schema failed to load")
	})
}

// TestEncodeDiagnoseResult pins the diagnose exit-code fix: the single result
// document is written through the codec, and failed checks map to
// ExitPartialFailure via EmittedError (never a second stdout document).
func TestEncodeDiagnoseResult(t *testing.T) {
	forceNoColor(t)
	pinHumanMode(t)

	newIO := func(t *testing.T) *cmdio.Options {
		t.Helper()
		opts := &cmdio.Options{}
		opts.RegisterCustomCodec("table", &kg.DiagnoseTableCodec{})
		opts.DefaultFormat("table")
		opts.BindFlags(pflag.NewFlagSet("diagnose", pflag.ContinueOnError))
		require.NoError(t, opts.Validate())
		return opts
	}
	failing := kg.DiagnoseResult{
		Checks: []kg.CheckResult{
			{Name: "ok", Status: kg.CheckPass},
			{Name: "broken", Status: kg.CheckFail, Detail: "boom"},
		},
		Summary: kg.DiagnoseSummary{Total: 2, Passed: 1, Failed: 1},
	}
	healthy := kg.DiagnoseResult{
		Checks:  []kg.CheckResult{{Name: "ok", Status: kg.CheckPass}},
		Summary: kg.DiagnoseSummary{Total: 1, Passed: 1},
	}
	warned := kg.DiagnoseResult{
		Checks:  []kg.CheckResult{{Name: "meh", Status: kg.CheckWarn}},
		Summary: kg.DiagnoseSummary{Total: 1, Warned: 1},
	}

	t.Run("healthy exits clean", func(t *testing.T) {
		var stdout bytes.Buffer
		err := kg.EncodeDiagnoseResult(&stdout, newIO(t), healthy, healthy.Summary.Failed, healthy.Summary.Total)
		require.NoError(t, err)
		assert.NotEmpty(t, stdout.String())
	})

	t.Run("warned checks do not change the exit code", func(t *testing.T) {
		var stdout bytes.Buffer
		err := kg.EncodeDiagnoseResult(&stdout, newIO(t), warned, warned.Summary.Failed, warned.Summary.Total)
		require.NoError(t, err)
	})

	t.Run("failed checks return EmittedError with ExitPartialFailure", func(t *testing.T) {
		var stdout bytes.Buffer
		err := kg.EncodeDiagnoseResult(&stdout, newIO(t), failing, failing.Summary.Failed, failing.Summary.Total)
		var emitted *gcxerrors.EmittedError
		require.ErrorAs(t, err, &emitted)
		assert.Equal(t, gcxerrors.ExitPartialFailure, emitted.Code)
		assert.NotEmpty(t, stdout.String(), "the verdict document must already be on stdout")
	})

	t.Run("agent mode writes exactly one JSON document then exits 4", func(t *testing.T) {
		setAgentMode(t)
		var stdout bytes.Buffer
		opts := &cmdio.Options{ErrWriter: io.Discard}
		opts.RegisterCustomCodec("table", &kg.DiagnoseTableCodec{})
		opts.DefaultFormat("table")
		opts.BindFlags(pflag.NewFlagSet("diagnose", pflag.ContinueOnError))
		require.NoError(t, opts.Validate())

		err := kg.EncodeDiagnoseResult(&stdout, opts, failing, failing.Summary.Failed, failing.Summary.Total)
		var emitted *gcxerrors.EmittedError
		require.ErrorAs(t, err, &emitted)
		assert.Equal(t, gcxerrors.ExitPartialFailure, emitted.Code)

		doc, ok := decodeSingleJSON(t, stdout.Bytes()).(map[string]any)
		require.True(t, ok)
		summary, _ := doc["summary"].(map[string]any)
		require.NotNil(t, summary)
		assert.InDelta(t, 1, summary["failed"], 0)
	})
}

// TestKgOpenLinkReceipt pins the `kg open` stdout receipt shape. The full
// command is not executed here because it launches a host browser; the
// receipt document is validated through the same codec path.
func TestKgOpenLinkReceipt(t *testing.T) {
	var stdout bytes.Buffer
	opts := &cmdio.Options{OutputFormat: "agents", ErrWriter: io.Discard}
	require.NoError(t, opts.Encode(&stdout, kg.NewKGOpenLinkForTest("https://example.grafana.net/a/grafana-asserts-app")))
	doc, ok := decodeSingleJSON(t, stdout.Bytes()).(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "gcx.kg.deeplink", doc["type"])
	assert.Equal(t, "1", doc["schema_version"])
	assert.Equal(t, "https://example.grafana.net/a/grafana-asserts-app", doc["url"])
}
