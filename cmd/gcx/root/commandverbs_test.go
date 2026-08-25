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

// Canonical operations, from docs/design/command-naming.md. View verbs
// (`status`, `timeline`, `inspect`, `diff`) are absent on purpose: the guide
// allows one only for a derived view, which no mechanical check can judge, so
// they reach the tree through the fixture instead.
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

// Shorthand operations, canonical only in the signal and datasource families the
// guide scopes them to.
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

// Tooling acts on the project or the CLI, not on Grafana resources, so the
// product vocabulary does not apply. CONSTITUTION.md § CLI Grammar names `dev`
// and `config`; `help` and `completion` come from Cobra.
//
//nolint:gochecknoglobals // constant-like skip list for test validation
var toolingAreas = map[string]bool{
	"completion": true,
	"config":     true,
	"dev":        true,
	"help":       true,
}

// canonicalOperation reports whether op is a canonical operation in area. An
// `<operation>-<subject>` compound is canonical when its operation half is: the
// guide prescribes the compound for discovery facets and parent-scoped
// operations.
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

// TestConsistency_LeafOperationsAreCanonical requires every runnable leaf to end
// in a canonical operation, or to be listed in testdata/command_verbs.json.
//
// The fixture grandfathers the operations already in the tree, not because they
// are right but because CONSTITUTION.md § CLI Grammar makes the released surface
// a compatibility exception for every v1.x release. The gate is on new commands:
// nothing checks their verbs today, and a fixture line is the explicit review the
// guide asks for.
func TestConsistency_LeafOperationsAreCanonical(t *testing.T) {
	raw, err := os.ReadFile("testdata/command_verbs.json")
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

	// Recorded only for paths that needed a verdict, so a fixture entry whose
	// operation later becomes canonical is reported as stale.
	judged := map[string]bool{}
	var uncanonical []string
	agent.WalkCommands(buildRootCmd(), func(cmd *cobra.Command) {
		if !isLeaf(cmd) || cmd.Hidden {
			return
		}
		fields := strings.Fields(cmd.CommandPath())
		// Bare top-level verbs, a closed set per CONSTITUTION.md § CLI Grammar,
		// have no operation segment to judge.
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
		t.Errorf("leaf command %q does not end in a canonical operation — rename it to an operation from docs/design/command-naming.md, or, if the operation genuinely has no canonical spelling, define it in the pull request and add the path to cmd/gcx/root/testdata/command_verbs.json", path)
	}

	var stale []string
	for _, path := range entries {
		if !judged[path] {
			stale = append(stale, path)
		}
	}
	sort.Strings(stale)
	for _, path := range stale {
		t.Errorf("fixture entry %q does not correspond to a leaf command needing a verdict (renamed, removed, or now canonical?) — update cmd/gcx/root/testdata/command_verbs.json", path)
	}
}
