package dashboards_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/providers/dashboards"
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
// confirmDelete tests
// ---------------------------------------------------------------------------

func TestConfirmDelete(t *testing.T) {
	tests := []struct {
		name  string
		input string // text the "user" types; empty string simulates EOF
		addNL bool   // whether to append "\n" (simulate line-terminated input)
		want  bool
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
		// EOF: reader has no bytes at all — scanner returns false immediately.
		{name: "EOF no input", input: "", addNL: false, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rawInput := tt.input
			if tt.addNL {
				rawInput += "\n"
			}
			r := strings.NewReader(rawInput)

			var w bytes.Buffer
			got := dashboards.ConfirmDeleteForTest(&w, r, "my-dashboard")

			if got != tt.want {
				t.Errorf("confirmDelete(%q) = %v, want %v", tt.input, got, tt.want)
			}

			// Prompt must always be written regardless of the answer.
			if !strings.Contains(w.String(), "my-dashboard") {
				t.Errorf("confirmDelete() output missing dashboard name: %q", w.String())
			}
		})
	}
}
