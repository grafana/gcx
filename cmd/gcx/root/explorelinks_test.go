package root_test

import (
	"sort"
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/agent"
	"github.com/spf13/cobra"
)

// queryCommandsMissingExploreLink shipped without an Explore link. This list
// only shrinks: add the link, then remove the entry.
//
//nolint:gochecknoglobals // constant-like lookup table for test validation
var queryCommandsMissingExploreLink = map[string]bool{
	"gcx datasources infinity query": true,
	"gcx datasources influxdb query": true,
	"gcx datasources mysql query":    true,
	"gcx datasources postgres query": true,
}

// TestConsistency_QueryCommandsHaveExploreLinks fails when a datasource query
// command has no --share-link or --open flag. DESIGN.md § Output Model
// requires every query command to link to Grafana Explore, so a user can see
// the same query in Grafana.
func TestConsistency_QueryCommandsHaveExploreLinks(t *testing.T) {
	seen := map[string]bool{}
	agent.WalkCommands(buildRootCmd(), func(cmd *cobra.Command) {
		path := cmd.CommandPath()
		// The auto-detecting "datasources query" dispatches to typed commands.
		if !isLeaf(cmd) || cmd.Hidden || cmd.Name() != "query" ||
			!strings.HasPrefix(path, "gcx datasources ") || path == "gcx datasources query" {
			return
		}
		seen[path] = true
		hasLink := cmd.Flags().Lookup("share-link") != nil && cmd.Flags().Lookup("open") != nil
		switch {
		case hasLink && queryCommandsMissingExploreLink[path]:
			t.Errorf("%q now has --share-link and --open; remove it from queryCommandsMissingExploreLink", path)
		case !hasLink && !queryCommandsMissingExploreLink[path]:
			t.Errorf("%q has no --share-link/--open Explore link (DESIGN.md § Output Model); see .agents/skills/contribute/references/datasource-implementation.md", path)
		}
	})

	if len(seen) == 0 {
		t.Fatal("found no datasource query commands; the walk is broken")
	}
	var stale []string
	for path := range queryCommandsMissingExploreLink {
		if !seen[path] {
			stale = append(stale, path)
		}
	}
	sort.Strings(stale)
	for _, path := range stale {
		t.Errorf("allowlisted query command %q no longer exists; remove its entry", path)
	}
}
