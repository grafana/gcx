package commands_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/grafana/gcx/cmd/gcx/commands"
	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/gcxerrors"
	"github.com/spf13/pflag"
)

// These tests pin the atomic-stdout contract for `gcx commands`.
//
// Before the fix, `gcx commands --validate` Encoded the ValidationResult and
// then returned a plain fmt.Errorf when uncovered types existed — in agent
// mode reportError appended a SECOND JSON error document to stdout, and the
// process exited 1 instead of the partial-failure code 4. The emit tail now
// writes exactly one document and returns EmittedError with ExitPartialFailure.

// uncoveredReport is the byte-exact human validation report for the sample
// result below, pinned against the pre-migration writeValidationReport output.
const uncoveredReport = "Resource type catalog validation\n" +
	"================================\n\n" +
	"Live types discovered:  3\n" +
	"Catalog coverage:       2/3\n" +
	"Uncovered (live only):  1\n" +
	"Stale (catalog only):   0\n\n" +
	"Uncovered types (add to well-known or adapter registry):\n" +
	"  Playlist                            playlist.grafana.app/v1\n\n"

// coveredReport is the byte-exact human validation report when everything is
// covered.
const coveredReport = "Resource type catalog validation\n" +
	"================================\n\n" +
	"Live types discovered:  2\n" +
	"Catalog coverage:       2/2\n" +
	"Uncovered (live only):  0\n" +
	"Stale (catalog only):   0\n\n" +
	"All catalog entries verified against live instance.\n"

func uncoveredResult() *agent.ValidationResult {
	return &agent.ValidationResult{
		Total:   3,
		Covered: 2,
		Uncovered: []agent.UncoveredType{
			{Kind: "Playlist", Group: "playlist.grafana.app", Version: "v1", Plural: "playlists"},
		},
	}
}

func coveredResult() *agent.ValidationResult {
	return &agent.ValidationResult{Total: 2, Covered: 2}
}

// assertSingleJSONValue asserts raw holds exactly one JSON value then EOF.
func assertSingleJSONValue(t *testing.T, raw string) map[string]any {
	t.Helper()
	dec := json.NewDecoder(strings.NewReader(raw))
	var first any
	if err := dec.Decode(&first); err != nil {
		t.Fatalf("stdout is not valid JSON: %v\n%s", err, raw)
	}
	var second any
	if err := dec.Decode(&second); !errors.Is(err, io.EOF) {
		t.Fatalf("stdout must contain exactly one JSON value, second decode = %v\n%s", err, raw)
	}
	doc, ok := first.(map[string]any)
	if !ok {
		t.Fatalf("document is %T, want object", first)
	}
	return doc
}

func TestEmitValidationResult_OutputContract(t *testing.T) {
	tests := []struct {
		name       string
		agentMode  bool
		output     string // explicit -o value; empty = default
		result     *agent.ValidationResult
		wantErr    bool   // expect EmittedError with ExitPartialFailure
		wantStdout string // exact stdout; empty = JSON/YAML check below
		wantYAML   bool
	}{
		{
			name:    "human default json with uncovered types",
			result:  uncoveredResult(),
			wantErr: true,
		},
		{
			name:       "human -o text with uncovered types is byte-identical",
			output:     "text",
			result:     uncoveredResult(),
			wantErr:    true,
			wantStdout: uncoveredReport,
		},
		{
			name:      "agent mode with uncovered types emits one fused doc",
			agentMode: true,
			result:    uncoveredResult(),
			wantErr:   true,
		},
		{
			name:   "human default json all covered",
			result: coveredResult(),
		},
		{
			name:       "human -o text all covered is byte-identical",
			output:     "text",
			result:     coveredResult(),
			wantStdout: coveredReport,
		},
		{
			name:      "explicit -o yaml wins in agent mode",
			agentMode: true,
			output:    "yaml",
			result:    uncoveredResult(),
			wantErr:   true,
			wantYAML:  true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			agent.SetFlag(tc.agentMode)
			t.Cleanup(func() { agent.SetFlag(false) })

			flags := pflag.NewFlagSet("commands", pflag.ContinueOnError)
			opts := commands.NewCommandsOptsForTest(flags)
			if tc.output != "" {
				if err := flags.Set("output", tc.output); err != nil {
					t.Fatalf("set -o %s: %v", tc.output, err)
				}
			}
			if err := opts.Validate(); err != nil {
				t.Fatalf("Validate() = %v", err)
			}

			var stdout, stderr bytes.Buffer
			opts.IO.ErrWriter = &stderr

			err := commands.EmitValidationResultForTest(&stdout, &opts.IO, tc.result)

			if tc.wantErr {
				var emitted *gcxerrors.EmittedError
				if !errors.As(err, &emitted) {
					t.Fatalf("error = %T (%v), want *gcxerrors.EmittedError", err, err)
				}
				if emitted.Code != gcxerrors.ExitPartialFailure {
					t.Fatalf("EmittedError.Code = %d, want %d", emitted.Code, gcxerrors.ExitPartialFailure)
				}
			} else if err != nil {
				t.Fatalf("emitValidationResult() = %v, want nil", err)
			}

			switch {
			case tc.wantStdout != "":
				if stdout.String() != tc.wantStdout {
					t.Fatalf("stdout not byte-identical:\ngot:  %q\nwant: %q", stdout.String(), tc.wantStdout)
				}
			case tc.wantYAML:
				if !strings.Contains(stdout.String(), "covered: 2") || !strings.Contains(stdout.String(), "total: 3") {
					t.Fatalf("yaml output missing expected fields:\n%s", stdout.String())
				}
			default:
				doc := assertSingleJSONValue(t, stdout.String())
				if _, ok := doc["uncovered"]; !ok {
					t.Fatalf("validation document missing uncovered field:\n%s", stdout.String())
				}
			}
		})
	}
}

// TestCommandsAgentMode_SingleJSONDocument pins the catalog path: in agent
// mode with no explicit -o the whole command emits exactly one JSON value.
func TestCommandsAgentMode_SingleJSONDocument(t *testing.T) {
	agent.SetFlag(true)
	t.Cleanup(func() { agent.SetFlag(false) })

	root := buildTestTree()
	cmd := commands.NewTestCommand(root)

	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute failed: %v", err)
	}

	doc := assertSingleJSONValue(t, stdout.String())
	if _, ok := doc["commands"]; !ok {
		t.Fatal("catalog document missing commands field")
	}
	if _, ok := doc["resource_types"]; !ok {
		t.Fatal("catalog document missing resource_types field")
	}
}
