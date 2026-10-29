package root_test

import (
	"encoding/json"
	"os"
	"sort"
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/agent"
	"github.com/spf13/cobra"
)

// Canonical operations taken from docs/design/command-naming.md. These are the recommended, standard operations.
//
//nolint:gochecknoglobals // constant-like lookup table for test validation
var canonicalOperations = map[string]bool{
	"list":   true,
	"get":    true,
	"create": true,
	"update": true,
	"delete": true,
	"upsert": true,
	"push":   true,
	"pull":   true,
	"query":  true,
	"search": true,
}

// These operations are only valid when seen in shorthandAreas
//
//nolint:gochecknoglobals // constant-like lookup table for test validation
var shorthandOperations = map[string]bool{
	"labels":   true,
	"series":   true,
	"metrics":  true,
	"metadata": true,
}

//nolint:gochecknoglobals // constant-like lookup table for test validation
var shorthandAreas = map[string]bool{
	"datasources": true,
	"logs":        true,
	"metrics":     true,
	"profiles":    true,
	"traces":      true,
}

//nolint:gochecknoglobals // constant-like skip list for test validation
var toolingAreas = map[string]bool{
	"completion": true,
	"config":     true,
	"dev":        true,
	"help":       true,
}

// canonicalOperation reports whether op is a canonical operation.
func canonicalOperation(area, op string) bool {
	if canonicalOperations[op] {
		return true
	}
	if shorthandAreas[area] && shorthandOperations[op] {
		return true
	}
	if verb, _, isCompound := strings.Cut(op, "-"); isCompound {
		return canonicalOperations[verb]
	}
	return false
}

// Check that no new command operations are added.
func TestConsistency_NoNewCommandOperationsAdded(t *testing.T) {
	// read a list of all the existing commands that do not have canonical operations.
	raw, err := os.ReadFile("testdata/non_canonical_command_operations.json")
	if err != nil {
		t.Fatalf("reading command verb fixture: %v", err)
	}
	var entries []string
	if err := json.Unmarshal(raw, &entries); err != nil {
		t.Fatalf("parsing command verb fixture: %v", err)
	}
	grandfathered := make(map[string]bool, len(entries))
	for _, path := range entries {
		grandfathered[path] = true
	}

	judged := map[string]bool{}
	var uncanonical []string
	agent.WalkCommands(buildRootCmd(), func(cmd *cobra.Command) {
		if !isLeaf(cmd) || cmd.Hidden {
			return
		}
		fields := strings.Fields(cmd.CommandPath())
		// do not judge top level commands as they have no operation
		if len(fields) < 3 {
			return
		}
		area := fields[1]
		if toolingAreas[area] {
			return
		}
		if canonicalOperation(area, fields[len(fields)-1]) {
			return
		}
		path := cmd.CommandPath()
		judged[path] = true
		if !grandfathered[path] {
			uncanonical = append(uncanonical, path)
		}
	})

	sort.Strings(uncanonical)
	for _, path := range uncanonical {
		t.Errorf("leaf command %q does not end in a canonical operation. Prefer using these canonical operation names - see docs/design/command-naming.md. If you need to add a new operation, add the full command path to non_canonical_command_operations.json to make this pass.", path)
	}

	var stale []string
	for _, path := range entries {
		if !judged[path] {
			stale = append(stale, path)
		}
	}
	sort.Strings(stale)
	for _, path := range stale {
		t.Errorf("the non_canonical_command_operations.json entry %q does not exist in the gcx command tree any more, or it has been made into a canonical operation. Please remove it from non_canonical_command_operations.json", path)
	}
}

// bareTopLevelCommands is the closed list of single-token commands in
// DESIGN.md § CLI Grammar: bootstrapping that comes before any area, and
// commands about the binary itself.
//
//nolint:gochecknoglobals // constant-like lookup table for test validation
var bareTopLevelCommands = map[string]bool{
	"signup":     true,
	"login":      true,
	"setup":      true,
	"version":    true,
	"help":       true,
	"completion": true,
}

// grandfatheredBareTopLevelCommands shipped as bare top-level commands before
// the closed list was enforced. They stay for v1 compatibility; remove an
// entry when its command moves under an area.
//
//nolint:gochecknoglobals // constant-like lookup table for test validation
var grandfatheredBareTopLevelCommands = map[string]bool{
	"api":       true,
	"commands":  true,
	"help-tree": true,
}

// TestConsistency_BareTopLevelCommandsAreClosed fails when a new top-level
// command runs on its own instead of grouping subcommands under an area, and
// when a grandfathered entry no longer exists as a bare command.
func TestConsistency_BareTopLevelCommandsAreClosed(t *testing.T) {
	rootCmd := buildRootCmd()
	rootCmd.InitDefaultHelpCmd()
	rootCmd.InitDefaultCompletionCmd()

	seen := map[string]bool{}
	for _, cmd := range rootCmd.Commands() {
		if cmd.Hidden || !isLeaf(cmd) || cmd.HasSubCommands() {
			continue
		}
		name := cmd.Name()
		seen[name] = true
		if bareTopLevelCommands[name] || grandfatheredBareTopLevelCommands[name] {
			continue
		}
		t.Errorf("top-level command %q is a bare verb. New top-level commands group subcommands under an area; the bare list is closed (DESIGN.md § CLI Grammar).", name)
	}

	var stale []string
	for name := range grandfatheredBareTopLevelCommands {
		if !seen[name] {
			stale = append(stale, name)
		}
	}
	sort.Strings(stale)
	for _, name := range stale {
		t.Errorf("grandfathered bare command %q no longer exists as a bare top-level command; remove it from grandfatheredBareTopLevelCommands", name)
	}
}
