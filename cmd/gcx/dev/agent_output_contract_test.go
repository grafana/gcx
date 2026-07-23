//nolint:testpackage // white-box: subcommands and receipt helpers are unexported
package dev

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fatih/color"
	"github.com/grafana/gcx/cmd/gcx/fail"
	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/gcxerrors"
	cmdio "github.com/grafana/gcx/internal/output"
	model "github.com/grafana/gcx/internal/resources"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// These tests pin the agent output contract for the dev command family
// (#387): in agent mode with no explicit -o, every finite command emits
// exactly one JSON value on stdout; the human default output stays
// byte-identical to the pre-migration implementation; explicit -o always
// wins over the agent-mode default; partial failures carry exit 4 via
// EmittedError without a second stdout document.

// setAgentMode toggles agent mode for the duration of the test. It must run
// BEFORE the command is constructed — the shared output flags resolve their
// default format at BindFlags time.
func setAgentMode(t *testing.T, enabled bool) {
	t.Helper()
	agent.SetFlag(enabled)
	t.Cleanup(func() { agent.SetFlag(false) })
}

// disableColor forces deterministic plain rendering for byte-identical
// assertions regardless of the environment the tests run in.
func disableColor(t *testing.T) {
	t.Helper()
	prev := color.NoColor
	color.NoColor = true
	t.Cleanup(func() { color.NoColor = prev })
}

func runCommand(t *testing.T, cmd *cobra.Command, args ...string) (string, string, error) {
	t.Helper()
	cmd.SilenceErrors = true
	cmd.SilenceUsage = true
	var stdout, stderr bytes.Buffer
	cmd.SetIn(&bytes.Buffer{})
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return stdout.String(), stderr.String(), err
}

// decodeSingleJSONValue asserts stdout holds exactly one JSON value followed
// by EOF, and returns it.
func decodeSingleJSONValue(t *testing.T, stdout string) any {
	t.Helper()
	dec := json.NewDecoder(strings.NewReader(stdout))
	var first any
	if err := dec.Decode(&first); err != nil {
		t.Fatalf("stdout is not valid JSON: %v\nstdout:\n%s", err, stdout)
	}
	var second any
	if err := dec.Decode(&second); !errors.Is(err, io.EOF) {
		t.Fatalf("stdout must contain exactly one JSON value, second decode = %v\nstdout:\n%s", err, stdout)
	}
	return first
}

func requireEmitted(t *testing.T, err error, wantCode int) *gcxerrors.EmittedError {
	t.Helper()
	var emitted *gcxerrors.EmittedError
	require.ErrorAs(t, err, &emitted, "want *gcxerrors.EmittedError, got %T (%v)", err, err)
	require.Equal(t, wantCode, emitted.Code)
	return emitted
}

func TestGenerate_HumanDefaultByteIdentical(t *testing.T) {
	disableColor(t)
	setAgentMode(t, false)
	tmp := t.TempDir()

	first := filepath.Join(tmp, "dashboards", "svc-overview.go")
	second := filepath.Join(tmp, "alerts", "high-cpu.go")

	stdout, stderr, err := runCommand(t, generateCmd(), first, second)
	require.NoError(t, err)

	// Exact pre-migration output: one Success line per file (in argument
	// order) plus the Info summary line.
	want := "✔ Generated " + filepath.Join(tmp, "dashboards", "svc_overview.go") + "\n" +
		"✔ Generated " + filepath.Join(tmp, "alerts", "high_cpu.go") + "\n" +
		"🛈 Generated 2 file(s).\n"
	require.Equal(t, want, stdout)
	require.Empty(t, stderr)
}

func TestGenerate_PartialFailure(t *testing.T) {
	disableColor(t)

	tests := []struct {
		name      string
		agentMode bool
	}{
		{name: "human default", agentMode: false},
		{name: "agent default", agentMode: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			setAgentMode(t, tc.agentMode)
			tmp := t.TempDir()

			good := filepath.Join(tmp, "dashboards", "ok.go")
			bad := filepath.Join(tmp, "mystery", "nope.go")

			stdout, stderr, err := runCommand(t, generateCmd(), good, bad)

			// Partial failure carries exit 4 without a second stdout document.
			emitted := requireEmitted(t, err, gcxerrors.ExitPartialFailure)
			var partial *gcxerrors.PartialFailureError
			require.ErrorAs(t, emitted, &partial)

			// Per-file error lines are diagnostics: stderr, not stdout.
			require.Contains(t, stderr, "✘ "+bad+": cannot infer resource type")

			if tc.agentMode {
				doc := decodeSingleJSONValue(t, stdout)
				obj, ok := doc.(map[string]any)
				require.True(t, ok, "document is %T, want object", doc)
				require.Equal(t, "gcx.artifact_receipt", obj["type"])
				failures, ok := obj["failures"].([]any)
				require.True(t, ok)
				require.Len(t, failures, 1)
			} else {
				want := "✔ Generated " + filepath.Join(tmp, "dashboards", "ok.go") + "\n" +
					"🛈 Generated 1 file(s), 1 failed.\n"
				require.Equal(t, want, stdout)
			}
		})
	}
}

func TestGenerate_AgentModeSingleJSONDocument(t *testing.T) {
	setAgentMode(t, true)
	tmp := t.TempDir()

	stdout, _, err := runCommand(t, generateCmd(), filepath.Join(tmp, "dashboards", "svc.go"))
	require.NoError(t, err)

	doc := decodeSingleJSONValue(t, stdout)
	obj, ok := doc.(map[string]any)
	require.True(t, ok, "document is %T, want object", doc)
	require.Equal(t, "gcx.artifact_receipt", obj["type"])
	require.Equal(t, "1", obj["schema_version"])
	require.Equal(t, "generated", obj["action"])
	files, ok := obj["files"].([]any)
	require.True(t, ok)
	require.Len(t, files, 1)
	file, ok := files[0].(map[string]any)
	require.True(t, ok)
	require.Equal(t, filepath.Join(tmp, "dashboards", "svc.go"), file["path"])
	require.Equal(t, "dashboard", file["kind"])
}

func TestGenerate_ExplicitOutputOverridesAgentDefault(t *testing.T) {
	setAgentMode(t, true)
	tmp := t.TempDir()

	stdout, _, err := runCommand(t, generateCmd(), "-o", "yaml", filepath.Join(tmp, "dashboards", "svc.go"))
	require.NoError(t, err)

	assert.Contains(t, stdout, "type: gcx.artifact_receipt")
	assert.Contains(t, stdout, "action: generated")
}

func mustResource(t *testing.T, object map[string]any) *model.Resource {
	t.Helper()
	res, err := model.FromUnstructured(&unstructured.Unstructured{Object: object})
	require.NoError(t, err)
	return res
}

func TestImportResources_ReceiptAndDiagnostics(t *testing.T) {
	disableColor(t)
	// The destination basename doubles as the generated Go package name, so
	// it must be a valid identifier (t.TempDir() basenames like "001" are not).
	tmp := filepath.Join(t.TempDir(), "imported")

	okDashboard := mustResource(t, map[string]any{
		"apiVersion": "dashboard.grafana.app/v1",
		"kind":       "Dashboard",
		"metadata":   map[string]any{"name": "ok-dash"},
		"spec":       map[string]any{"title": "OK", "schemaVersion": float64(36)},
	})
	// No converter is registered for this kind: an expected capability gap,
	// counted as skipped, not failed.
	noConverter := mustResource(t, map[string]any{
		"apiVersion": "playlist.grafana.app/v1",
		"kind":       "Playlist",
		"metadata":   map[string]any{"name": "some-playlist"},
		"spec":       map[string]any{"title": "P"},
	})
	// Spec that cannot unmarshal into the dashboard schema: a real
	// conversion failure.
	badDashboard := mustResource(t, map[string]any{
		"apiVersion": "dashboard.grafana.app/v1",
		"kind":       "Dashboard",
		"metadata":   map[string]any{"name": "bad-dash"},
		"spec":       map[string]any{"title": []any{"not", "a", "string"}},
	})

	var warn bytes.Buffer
	receipt, err := importResources(model.NewResources(okDashboard, noConverter, badDashboard), tmp, &warn)
	require.NoError(t, err)

	require.Equal(t, cmdio.MutationSummary{Succeeded: 1, Failed: 1, Skipped: 1}, receipt.Summary)
	require.Equal(t, "gcx.artifact_receipt", receipt.Type)
	require.Equal(t, "imported", receipt.Action)
	require.Equal(t, tmp, receipt.Dir)

	require.Len(t, receipt.Files, 1)
	require.Equal(t, filepath.Join(tmp, "ok_dash.go"), receipt.Files[0].Path)
	require.Equal(t, "Dashboard", receipt.Files[0].Kind)

	require.Len(t, receipt.Failures, 1)
	require.Equal(t, "bad-dash", receipt.Failures[0].Target.Name)

	// Per-resource notes are diagnostics on the warn stream, with the exact
	// pre-migration rendering.
	require.Contains(t, warn.String(), "🛈 Skipping resource 'Playlist.some-playlist': no converter found for Playlist.playlist.grafana.app/v1\n")
	require.Contains(t, warn.String(), "🛈 Skipping resource 'Dashboard.bad-dash': ")
}

func TestImportReceiptCodec_HumanDefaultByteIdentical(t *testing.T) {
	disableColor(t)

	receipt := cmdio.NewArtifactReceipt("imported", "go")
	receipt.Dir = "imported"
	receipt.Summary.Succeeded = 2

	var buf bytes.Buffer
	require.NoError(t, (&importReceiptCodec{}).Encode(&buf, receipt))

	// Exact pre-migration summary line.
	require.Equal(t, "✔ Imported 2 resources in imported\n", buf.String())
}

func TestImportOpts_AgentModeSingleJSONDocument(t *testing.T) {
	tests := []struct {
		name       string
		agentMode  bool
		args       []string
		wantFormat string
	}{
		{name: "agent default is agents codec", agentMode: true, wantFormat: "agents"},
		{name: "human default is text codec", agentMode: false, wantFormat: "text"},
		{name: "explicit -o yaml wins in agent mode", agentMode: true, args: []string{"--output", "yaml"}, wantFormat: "yaml"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			setAgentMode(t, tc.agentMode)

			opts := &importOpts{}
			flags := pflag.NewFlagSet("import", pflag.ContinueOnError)
			opts.setup(flags)
			require.NoError(t, flags.Parse(tc.args))
			require.NoError(t, opts.Validate())
			require.Equal(t, tc.wantFormat, opts.IO.OutputFormat)

			receipt := cmdio.NewArtifactReceipt("imported", "go")
			receipt.Dir = "imported"
			receipt.Summary.Succeeded = 1
			receipt.Files = append(receipt.Files, cmdio.ArtifactFile{Path: "imported/ok.go", Kind: "Dashboard"})

			var stdout bytes.Buffer
			opts.IO.ErrWriter = io.Discard
			require.NoError(t, opts.IO.Encode(&stdout, receipt))

			if tc.wantFormat == "agents" {
				doc := decodeSingleJSONValue(t, stdout.String())
				obj, ok := doc.(map[string]any)
				require.True(t, ok, "document is %T, want object", doc)
				require.Equal(t, "gcx.artifact_receipt", obj["type"])
			}
		})
	}
}

func TestScaffold_NonInteractiveGuard(t *testing.T) {
	// Under `go test`, stdin is not a terminal, so the interactivity guard
	// must fail fast instead of opening the huh TUI.
	tests := []struct {
		name        string
		agentMode   bool
		args        []string
		wantMissing []string
	}{
		{name: "no flags", args: nil, wantMissing: []string{"--project", "--go-module-path"}},
		{name: "only project", args: []string{"--project", "demo"}, wantMissing: []string{"--go-module-path"}},
		{name: "agent mode no flags", agentMode: true, args: nil, wantMissing: []string{"--project", "--go-module-path"}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			setAgentMode(t, tc.agentMode)

			stdout, _, err := runCommand(t, scaffoldCmd(), tc.args...)

			var usageErr *fail.UsageError
			require.ErrorAs(t, err, &usageErr, "want *fail.UsageError, got %T (%v)", err, err)
			for _, missing := range tc.wantMissing {
				assert.Contains(t, usageErr.Message, missing)
			}
			require.Empty(t, stdout)
		})
	}
}

func TestScaffold_FlagsProvidedByteIdentical(t *testing.T) {
	disableColor(t)
	setAgentMode(t, false)
	t.Chdir(t.TempDir())

	stdout, _, err := runCommand(t, scaffoldCmd(),
		"--project", "My Dashboards",
		"--go-module-path", "github.com/example/my-dashboards")
	require.NoError(t, err)

	// Exact pre-migration Success line (scaffold is protocol-exempt:
	// interactive/artifact — output is intentionally unchanged).
	require.Equal(t, "✔ Project scaffolded in my-dashboards.\n", stdout)
	require.DirExists(t, "my-dashboards")
	require.FileExists(t, filepath.Join("my-dashboards", "go.mod"))
}

// TestGenerate_TotalFailure_RawErrorNotPartial pins the zero-success
// convention: when every input fails there is no receipt on stdout and the
// raw error takes the standard path (exit 1 via reportError), never exit 4 —
// a "partial" code on a complete failure would mislead consumers.
func TestGenerate_TotalFailure_RawErrorNotPartial(t *testing.T) {
	setAgentMode(t, true)
	tmp := t.TempDir()

	bad := filepath.Join(tmp, "mystery", "nope.go")
	stdout, _, err := runCommand(t, generateCmd(), bad)

	require.Error(t, err)
	var emitted *gcxerrors.EmittedError
	require.NotErrorAs(t, err, &emitted,
		"total failure must be a raw error (exit 1), not EmittedError (exit 4)")
	require.Empty(t, strings.TrimSpace(stdout),
		"no receipt on stdout — reportError owns the error document")
}
