package dashboards_test

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/gcxerrors"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/providers/dashboards"
	"github.com/grafana/gcx/internal/resources/dynamic"
	"github.com/grafana/gcx/internal/testutils"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// ---------------------------------------------------------------------------
// decodeManifest tests
// ---------------------------------------------------------------------------

func TestDecodeManifest(t *testing.T) {
	validJSON := `{"apiVersion":"dashboard.grafana.app/v2","kind":"Dashboard","metadata":{"name":"my-dash"},"spec":{"title":"My Dashboard"}}`
	validYAML := "apiVersion: dashboard.grafana.app/v2\nkind: Dashboard\nmetadata:\n  name: my-dash\nspec:\n  title: My Dashboard\n"

	tests := []struct {
		name        string
		input       string
		wantErr     bool
		errContains string
		wantName    string // non-empty means check GetName()
	}{
		{
			name:     "valid JSON dashboard",
			input:    validJSON,
			wantErr:  false,
			wantName: "my-dash",
		},
		{
			name:     "valid YAML dashboard",
			input:    validYAML,
			wantErr:  false,
			wantName: "my-dash",
		},
		{
			// Starts with '{' so JSON branch is taken; malformed JSON surfaces parse error.
			name:        "invalid JSON — malformed",
			input:       `{not valid json`,
			wantErr:     true,
			errContains: "failed to parse JSON manifest",
		},
		{
			// Does NOT start with '{' or '[', falls through to YAML; YAML codec returns EOF.
			name:        "invalid content — neither JSON nor YAML",
			input:       "!!! not yaml or json !!!",
			wantErr:     true,
			errContains: "neither valid JSON nor YAML",
		},
		{
			// Empty string: no JSON prefix, YAML codec returns EOF.
			name:        "empty content",
			input:       "",
			wantErr:     true,
			errContains: "neither valid JSON nor YAML",
		},
		{
			// Whitespace-only: same code path as empty.
			name:        "whitespace only",
			input:       "   \n\t  ",
			wantErr:     true,
			errContains: "neither valid JSON nor YAML",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			obj, err := dashboards.DecodeManifestForTest(strings.NewReader(tt.input))

			if tt.wantErr {
				if err == nil {
					t.Fatalf("decodeManifest() expected error, got nil (obj=%v)", obj)
				}
				if tt.errContains != "" && !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("decodeManifest() error = %q, want it to contain %q", err.Error(), tt.errContains)
				}
				return
			}

			if err != nil {
				t.Fatalf("decodeManifest() unexpected error: %v", err)
			}
			if obj == nil {
				t.Fatal("decodeManifest() returned nil object, want non-nil")
			}
			if tt.wantName != "" && obj.GetName() != tt.wantName {
				t.Errorf("decodeManifest() GetName() = %q, want %q", obj.GetName(), tt.wantName)
			}
		})
	}
}

func TestDecodeManifest_ClassicDashboard(t *testing.T) {
	// A distinctive query string proves the error does not echo the input.
	const query = `rate(secret_query_marker_total[5m])`
	export := `{
  "__inputs": [{"name": "DS_PROMETHEUS", "type": "datasource", "pluginId": "prometheus"}],
  "uid": "example-dashboard",
  "title": "Example dashboard",
  "schemaVersion": 39,
  "panels": [{"type": "timeseries", "targets": [{"datasource": "${DS_PROMETHEUS}", "expr": "` + query + `"}]}]
}`

	tests := []struct {
		name            string
		input           string
		wantSource      string
		wantName        string
		wantInputsHint  bool
		wantSuggestions int
	}{
		{
			name:            "UI export JSON",
			input:           export,
			wantSource:      "dashboard export (JSON model)",
			wantName:        "name: example-dashboard",
			wantInputsHint:  true,
			wantSuggestions: 3,
		},
		{
			name:            "UI export YAML without uid",
			input:           "title: Example dashboard\nschemaVersion: 39\npanels: []\n",
			wantSource:      "dashboard export (JSON model)",
			wantName:        "name: <dashboard-uid>",
			wantSuggestions: 2,
		},
		{
			name:            "dashboard API response",
			input:           `{"meta": {"slug": "example"}, "dashboard": {"uid": "api-dash", "title": "API", "panels": []}}`,
			wantSource:      "dashboard API response",
			wantName:        "name: api-dash",
			wantSuggestions: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := dashboards.DecodeManifestForTest(strings.NewReader(tt.input))

			usageErr, ok := errors.AsType[*gcxerrors.UsageError](err)
			if !ok {
				t.Fatalf("decodeManifest() error = %v (%T), want *gcxerrors.UsageError", err, err)
			}
			msg := usageErr.Error()
			for _, want := range []string{tt.wantSource, "not a dashboard resource manifest", "kind: Dashboard", tt.wantName} {
				if !strings.Contains(msg, want) {
					t.Errorf("message missing %q:\n%s", want, msg)
				}
			}
			if strings.Contains(msg, query) || strings.Contains(msg, "Object 'Kind' is missing") {
				t.Errorf("message echoes the input or decoder error:\n%s", msg)
			}
			if len(usageErr.Suggestions) != tt.wantSuggestions {
				t.Errorf("got %d suggestions, want %d: %q", len(usageErr.Suggestions), tt.wantSuggestions, usageErr.Suggestions)
			}
			hasInputsHint := strings.Contains(strings.Join(usageErr.Suggestions, "\n"), "__inputs")
			if hasInputsHint != tt.wantInputsHint {
				t.Errorf("__inputs suggestion present = %v, want %v", hasInputsHint, tt.wantInputsHint)
			}
		})
	}
}

func TestDecodeManifest_NotClassicDashboard(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		errContains string // empty means decoding succeeds
	}{
		{
			// A manifest that only lacks kind keeps the decoder's diagnostic.
			name:        "manifest missing kind",
			input:       `{"apiVersion": "dashboard.grafana.app/v1beta1", "metadata": {"name": "x"}, "spec": {"title": "X", "panels": []}}`,
			errContains: "Object 'Kind' is missing",
		},
		{
			name:        "object without dashboard fields",
			input:       `{"title": "Just a title"}`,
			errContains: "Object 'Kind' is missing",
		},
		{
			name:  "valid manifest with dashboard fields in spec",
			input: "apiVersion: dashboard.grafana.app/v1beta1\nkind: Dashboard\nmetadata:\n  name: x\nspec:\n  title: X\n  schemaVersion: 39\n  panels: []\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := dashboards.DecodeManifestForTest(strings.NewReader(tt.input))

			if _, ok := errors.AsType[*gcxerrors.UsageError](err); ok {
				t.Fatalf("decodeManifest() returned a classic-dashboard usage error: %v", err)
			}
			if tt.errContains == "" {
				if err != nil {
					t.Fatalf("decodeManifest() unexpected error: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.errContains) {
				t.Errorf("decodeManifest() error = %v, want it to contain %q", err, tt.errContains)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// readManifest tests
// ---------------------------------------------------------------------------

func TestReadManifest(t *testing.T) {
	validJSON := `{"apiVersion":"dashboard.grafana.app/v2","kind":"Dashboard","metadata":{"name":"file-dash"},"spec":{"title":"File Dashboard"}}`

	// Write a temp JSON manifest file.
	tmpDir := t.TempDir()
	manifestFile := filepath.Join(tmpDir, "dashboard.json")
	if err := os.WriteFile(manifestFile, []byte(validJSON), 0o600); err != nil {
		t.Fatalf("failed to write temp file: %v", err)
	}

	// Write an invalid JSON file (starts with '{' so JSON branch is taken).
	badFile := filepath.Join(tmpDir, "bad.json")
	if err := os.WriteFile(badFile, []byte(`{bad json`), 0o600); err != nil {
		t.Fatalf("failed to write bad temp file: %v", err)
	}

	tests := []struct {
		name        string
		filename    string
		wantErr     bool
		errContains string
		wantName    string
	}{
		{
			name:     "reads from valid file path",
			filename: manifestFile,
			wantErr:  false,
			wantName: "file-dash",
		},
		{
			name:        "file not found",
			filename:    filepath.Join(tmpDir, "nonexistent.json"),
			wantErr:     true,
			errContains: "failed to open",
		},
		{
			name:        "invalid content in file",
			filename:    badFile,
			wantErr:     true,
			errContains: "failed to parse JSON manifest",
		},
		// Note: the filename == "-" (stdin) branch hard-codes os.Stdin and cannot
		// be injected without OS-level fd manipulation. The content-parsing logic
		// exercised by that branch is fully covered by TestDecodeManifest above.
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			obj, err := dashboards.ReadManifestForTest(tt.filename)

			if tt.wantErr {
				if err == nil {
					t.Fatalf("readManifest() expected error, got nil (obj=%v)", obj)
				}
				if tt.errContains != "" && !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("readManifest() error = %q, want it to contain %q", err.Error(), tt.errContains)
				}
				return
			}

			if err != nil {
				t.Fatalf("readManifest() unexpected error: %v", err)
			}
			if obj == nil {
				t.Fatal("readManifest() returned nil object, want non-nil")
			}
			if tt.wantName != "" && obj.GetName() != tt.wantName {
				t.Errorf("readManifest() GetName() = %q, want %q", obj.GetName(), tt.wantName)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// wrapUpdateError tests
// ---------------------------------------------------------------------------

func TestWrapUpdateError(t *testing.T) {
	gr := schema.GroupResource{Group: "dashboard.grafana.app", Resource: "dashboards"}
	conflictErr := apierrors.NewConflict(gr, "my-dash", errors.New("resource version mismatch"))
	// dynamic.NamespacedClient wraps server errors with ParseStatusError before
	// they reach the command layer, so verify the helper still recognises a
	// conflict when it has been routed through that wrapper.
	wrappedConflict := dynamic.ParseStatusError(conflictErr)

	tests := []struct {
		name        string
		inputErr    error
		want        error  // exact error to return when not conflict; nil for conflict path
		wantHint    string // substring expected in conflict-wrapped message
		wantUnwraps error  // expected base error returned by errors.Unwrap; nil when not applicable
	}{
		{
			name:     "nil error → nil",
			inputErr: nil,
		},
		{
			name:     "non-conflict error returned unchanged",
			inputErr: errors.New("network unreachable"),
		},
		{
			name:        "raw apierrors conflict → wrapped with hint",
			inputErr:    conflictErr,
			wantHint:    "gcx dashboards get my-dash",
			wantUnwraps: conflictErr,
		},
		{
			name:        "ParseStatusError-wrapped conflict → wrapped with hint",
			inputErr:    wrappedConflict,
			wantHint:    "gcx dashboards get my-dash",
			wantUnwraps: wrappedConflict,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := dashboards.WrapUpdateErrorForTest("my-dash", tt.inputErr)

			if tt.inputErr == nil {
				if got != nil {
					t.Fatalf("wrapUpdateError(nil) = %v, want nil", got)
				}
				return
			}

			if tt.wantHint == "" {
				// Non-conflict path: must return the same error untouched.
				if got != tt.inputErr { //nolint:errorlint // identity check intentional
					t.Fatalf("wrapUpdateError(non-conflict) = %v, want identity %v", got, tt.inputErr)
				}
				return
			}

			if got == nil {
				t.Fatal("wrapUpdateError(conflict) returned nil, want wrapped error")
			}
			if !apierrors.IsConflict(got) {
				t.Errorf("wrapUpdateError(conflict) lost the conflict reason: %v", got)
			}
			if !strings.Contains(got.Error(), tt.wantHint) {
				t.Errorf("wrapUpdateError(conflict) message %q missing hint %q", got.Error(), tt.wantHint)
			}
			if !strings.Contains(got.Error(), "metadata.resourceVersion") {
				t.Errorf("wrapUpdateError(conflict) message %q must reference metadata.resourceVersion", got.Error())
			}
			if !errors.Is(got, tt.wantUnwraps) {
				t.Errorf("wrapUpdateError(conflict) does not unwrap to original conflict (errors.Is=false)")
			}
		})
	}
}

// ---------------------------------------------------------------------------
// ConfirmDestructive tests (via providers.ConfirmDestructive)
// ---------------------------------------------------------------------------

func TestConfirmDestructive(t *testing.T) {
	testutils.SetAgentMode(t, false)
	tests := []struct {
		name    string
		input   string // text the "user" types
		addNL   bool   // whether to append "\n" (simulate line-terminated input)
		want    bool
		wantErr bool
	}{
		{name: "y lowercase", input: "y", addNL: true, want: true},
		{name: "yes lowercase", input: "yes", addNL: true, want: true},
		{name: "Y uppercase", input: "Y", addNL: true, want: true},
		{name: "YES all-caps", input: "YES", addNL: true, want: true},
		{name: "leading and trailing spaces", input: " y ", addNL: true, want: true},
		{name: "n lowercase", input: "n", addNL: true, want: false},
		{name: "no lowercase", input: "no", addNL: true, want: false},
		{name: "N uppercase", input: "N", addNL: true, want: false},
		{name: "empty string", input: "", addNL: true, want: false},
		// EOF: reader has no bytes at all — ReadString returns io.EOF error.
		{name: "EOF no input", input: "", addNL: false, want: false, wantErr: true},
		// --force bypasses prompt entirely.
		{name: "force flag", input: "", addNL: false, want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rawInput := tt.input
			if tt.addNL {
				rawInput += "\n"
			}
			r := strings.NewReader(rawInput)

			var w bytes.Buffer
			force := tt.name == "force flag"
			got, err := providers.ConfirmDestructive(r, &w, force, `Delete dashboard "my-dashboard"?`)

			if tt.wantErr {
				if err == nil {
					t.Errorf("ConfirmDestructive(%q) expected error, got nil", tt.input)
				}
				return
			}
			if err != nil {
				t.Fatalf("ConfirmDestructive(%q) unexpected error: %v", tt.input, err)
			}

			if got != tt.want {
				t.Errorf("ConfirmDestructive(%q) = %v, want %v", tt.input, got, tt.want)
			}

			// Prompt must be written for non-force cases.
			if !force && !strings.Contains(w.String(), "my-dashboard") {
				t.Errorf("ConfirmDestructive() output missing dashboard name: %q", w.String())
			}
		})
	}
}
