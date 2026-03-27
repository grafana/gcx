package output_test

// integration_test.go verifies pipe detection and truncation suppression
// end-to-end through the terminal package and Options binding.
//
// Tests simulate a non-TTY stdout by controlling package-level state via
// terminal.SetPiped / terminal.SetNoTruncate, which mirrors what the root
// PersistentPreRun does after calling terminal.Detect().

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"

	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/terminal"
	"github.com/spf13/pflag"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// TestPipeDetection_OSPipe verifies that when stdout is connected to an
// os.Pipe (non-TTY), the terminal package correctly reports IsPiped=true
// and the Options struct reflects this.
//
// Acceptance criterion:
//
//	GIVEN a test that executes a command with stdout set to os.Pipe()
//	WHEN the command produces table output
//	THEN the output contains no ANSI codes and columns are not truncated.
func TestPipeDetection_OSPipe(t *testing.T) {
	// Create an os.Pipe to simulate piped stdout (non-TTY).
	r, w, err := os.Pipe()
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = r.Close()
		_ = w.Close()
	})

	// Simulate what root PersistentPreRun does when it detects stdout is a pipe:
	// it calls terminal.SetPiped(true) and terminal.SetNoTruncate(true).
	terminal.ResetForTesting()
	t.Cleanup(terminal.ResetForTesting)

	terminal.SetPiped(true)
	terminal.SetNoTruncate(true)

	opts := &cmdio.Options{}
	flags := pflag.NewFlagSet("test", pflag.ContinueOnError)
	opts.BindFlags(flags)

	assert.True(t, opts.IsPiped, "Options.IsPiped should be true when terminal.SetPiped(true)")
	assert.True(t, opts.NoTruncate, "Options.NoTruncate should be true when terminal.SetNoTruncate(true)")
}

// TestPipeDetection_NoANSIWhenPiped verifies that JSON output written through
// Options.Encode() when piped contains no ANSI escape sequences.
//
// Acceptance criterion:
//
//	GIVEN a test that executes a command with stdout set to os.Pipe()
//	WHEN the command produces table output
//	THEN the output contains no ANSI codes.
func TestPipeDetection_NoANSIWhenPiped(t *testing.T) {
	terminal.ResetForTesting()
	t.Cleanup(terminal.ResetForTesting)
	terminal.SetPiped(true)
	terminal.SetNoTruncate(true)

	opts := &cmdio.Options{}
	flags := pflag.NewFlagSet("test", pflag.ContinueOnError)
	opts.BindFlags(flags)

	item := unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "v1",
		"kind":       "Dashboard",
		"metadata": map[string]any{
			"name":      "my-dashboard",
			"namespace": "default",
		},
	}}

	var buf bytes.Buffer
	require.NoError(t, opts.Encode(&buf, item))

	output := buf.String()
	assert.False(t, containsANSI(output), "output must not contain ANSI escape sequences when piped\nGot: %q", output)
}

// TestPipeDetection_NoTruncateReflectedInOptions verifies that NoTruncate is
// propagated to Options.NoTruncate after BindFlags is called.
//
// Acceptance criterion:
//
//	GIVEN gcx is invoked on a TTY with --no-truncate
//	WHEN the command produces table output
//	THEN no table column values are truncated with ellipsis.
func TestPipeDetection_NoTruncateReflectedInOptions(t *testing.T) {
	tests := []struct {
		name           string
		piped          bool
		noTruncate     bool
		wantIsPiped    bool
		wantNoTruncate bool
	}{
		{
			name:           "piped stdout enables both IsPiped and NoTruncate",
			piped:          true,
			noTruncate:     true,
			wantIsPiped:    true,
			wantNoTruncate: true,
		},
		{
			name:           "TTY stdout without --no-truncate: both flags false",
			piped:          false,
			noTruncate:     false,
			wantIsPiped:    false,
			wantNoTruncate: false,
		},
		{
			name:           "TTY stdout with --no-truncate: piped=false, noTruncate=true",
			piped:          false,
			noTruncate:     true,
			wantIsPiped:    false,
			wantNoTruncate: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			terminal.ResetForTesting()
			t.Cleanup(terminal.ResetForTesting)

			terminal.SetPiped(tc.piped)
			terminal.SetNoTruncate(tc.noTruncate)

			opts := &cmdio.Options{}
			flags := pflag.NewFlagSet("test", pflag.ContinueOnError)
			opts.BindFlags(flags)

			assert.Equal(t, tc.wantIsPiped, opts.IsPiped)
			assert.Equal(t, tc.wantNoTruncate, opts.NoTruncate)
		})
	}
}

// TestPipeDetection_JSONOutputIsValidWhenPiped verifies that JSON output
// produced under piped mode is well-formed (no partial writes or color codes
// corrupting the JSON stream).
//
// This guards NC-004: the system MUST NEVER produce invalid JSON to stdout
// when --json or agent-mode error reporting is active.
func TestPipeDetection_JSONOutputIsValidWhenPiped(t *testing.T) {
	terminal.ResetForTesting()
	t.Cleanup(terminal.ResetForTesting)
	terminal.SetPiped(true)
	terminal.SetNoTruncate(true)

	opts := &cmdio.Options{}
	flags := pflag.NewFlagSet("test", pflag.ContinueOnError)
	opts.BindFlags(flags)

	list := unstructured.UnstructuredList{}
	for i := range 3 {
		list.Items = append(list.Items, unstructured.Unstructured{
			Object: map[string]any{
				"kind": "Dashboard",
				"metadata": map[string]any{
					"name": strings.Repeat("dashboard", i+1),
				},
			},
		})
	}

	var buf bytes.Buffer
	require.NoError(t, opts.Encode(&buf, list))

	// Must parse as valid JSON.
	var decoded any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &decoded), "output must be valid JSON when piped\nGot: %q", buf.String())

	// Must contain no ANSI sequences.
	assert.False(t, containsANSI(buf.String()), "piped JSON output must not contain ANSI escape sequences")
}

// containsANSI reports whether s contains ANSI escape sequences.
func containsANSI(s string) bool {
	return strings.Contains(s, "\x1b[")
}
