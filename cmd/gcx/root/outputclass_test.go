package root_test

import (
	"encoding/json"
	"os"
	"sort"
	"testing"

	"github.com/grafana/gcx/internal/agent"
	"github.com/spf13/cobra"
)

// Output protocol classes. The class declares what a command's stdout is in
// agent mode when agent mode supplies the default (no explicit -o/--json/--jq):
//
//	finite      — exactly one JSON value (the result or a fused error)
//	artifact    — files on disk are the real output; stdout carries exactly
//	              one JSON receipt (paths/format/counts/failures)
//	stream      — typed, versioned JSONL with a terminal success/error event
//	interactive — drives a prompt/editor/wizard; exempt from the JSON contract
//	server      — long-running listener; exempt
//	shell       — shell completion source; exempt
//	prose       — the data itself is prose (help text); exempt
//	raw         — byte passthrough from a backend; exempt
//
// Contract reference: docs/design/agent-mode.md.
//
//nolint:gochecknoglobals // constant-like lookup table for test validation
var validOutputClasses = map[string]bool{
	"finite":      true,
	"artifact":    true,
	"stream":      true,
	"interactive": true,
	"server":      true,
	"shell":       true,
	"prose":       true,
	"raw":         true,
}

// TestConsistency_AllLeafCommandsHaveOutputClass is the classification gate
// for the agent output contract: every runnable leaf command must be
// declared in testdata/output_classes.json with its protocol class, and
// every fixture entry must correspond to a real command. Adding a new
// command without classifying its output protocol fails this test — that is
// the point: unclassified commands are how prose leaks back onto agent-mode
// stdout.
func TestConsistency_AllLeafCommandsHaveOutputClass(t *testing.T) {
	raw, err := os.ReadFile("testdata/output_classes.json")
	if err != nil {
		t.Fatalf("reading output class fixture: %v", err)
	}
	classes := map[string]string{}
	if err := json.Unmarshal(raw, &classes); err != nil {
		t.Fatalf("parsing output class fixture: %v", err)
	}

	for cmd, class := range classes {
		if !validOutputClasses[class] {
			t.Errorf("fixture entry %q has unknown class %q", cmd, class)
		}
	}

	rootCmd := buildRootCmd()
	rootCmd.InitDefaultHelpCmd()
	rootCmd.InitDefaultCompletionCmd()

	seen := map[string]bool{}
	var unclassified []string
	agent.WalkCommands(rootCmd, func(cmd *cobra.Command) {
		if !isLeaf(cmd) || cmd.Hidden {
			return
		}
		path := cmd.CommandPath()
		seen[path] = true
		if _, ok := classes[path]; !ok {
			unclassified = append(unclassified, path)
		}
	})

	sort.Strings(unclassified)
	for _, path := range unclassified {
		t.Errorf("leaf command %q has no output class — classify it in cmd/gcx/root/testdata/output_classes.json (finite commands must emit exactly one JSON value in agent mode; see the class table in this test file)", path)
	}

	var stale []string
	for cmd := range classes {
		if !seen[cmd] {
			stale = append(stale, cmd)
		}
	}
	sort.Strings(stale)
	for _, cmd := range stale {
		t.Errorf("fixture entry %q does not correspond to a runnable leaf command (renamed or removed?) — update cmd/gcx/root/testdata/output_classes.json", cmd)
	}
}
