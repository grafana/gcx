package root_test

import (
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/agent"
	"github.com/spf13/cobra"
)

// unlinkedTimeRangedCommands are datasource commands that take a time range
// (--from) but have no Explore link. Each entry says why: a query command
// missing its link is debt; a metadata command runs no user query; an
// undecided entry still needs that call. This list only shrinks: add the link
// or settle the call, then remove the entry.
//
//nolint:gochecknoglobals // constant-like lookup table for test validation
var unlinkedTimeRangedCommands = map[string]string{
	"gcx datasources infinity query":                 "query, missing link",
	"gcx datasources influxdb query":                 "query, missing link",
	"gcx datasources mysql query":                    "query, missing link",
	"gcx datasources postgres query":                 "query, missing link",
	"gcx datasources loki query-patterns":            "query, missing link",
	"gcx datasources prometheus search-label-names":  "metadata",
	"gcx datasources prometheus search-label-values": "metadata",
	"gcx datasources prometheus search-metric-names": "metadata",
	"gcx datasources pyroscope labels":               "metadata",
	"gcx datasources pyroscope list-profile-types":   "metadata",
	"gcx datasources pyroscope series":               "metadata",
	"gcx datasources pyroscope exemplars profile":    "undecided",
	"gcx datasources pyroscope exemplars span":       "undecided",
	"gcx datasources pyroscope metrics":              "undecided",
	"gcx datasources pyroscope query-anomalies":      "undecided",
	"gcx datasources tempo baseline":                 "undecided",
	"gcx datasources tempo diff":                     "undecided",
}

// TestConsistency_QueryCommandsHaveExploreLinks fails when a time-ranged
// datasource command has no --share-link/--open Explore link and is not
// listed in unlinkedTimeRangedCommands. DESIGN.md § Output Model requires
// every query command to link to Grafana Explore, so a user can see the same
// query in Grafana. The check keys off the time range rather than the command
// name, so a query command named anything but "query" is still covered.
func TestConsistency_QueryCommandsHaveExploreLinks(t *testing.T) {
	seen := map[string]bool{}
	agent.WalkCommands(buildRootCmd(), func(cmd *cobra.Command) {
		path := cmd.CommandPath()
		// The auto-detecting "datasources query" dispatches to typed commands.
		if !isLeaf(cmd) || cmd.Hidden || !strings.HasPrefix(path, "gcx datasources ") ||
			path == "gcx datasources query" || cmd.Flags().Lookup("from") == nil {
			return
		}
		seen[path] = true
		hasLink := cmd.Flags().Lookup("share-link") != nil && cmd.Flags().Lookup("open") != nil
		_, listed := unlinkedTimeRangedCommands[path]
		switch {
		case hasLink && listed:
			t.Errorf("%q now has --share-link and --open; remove it from unlinkedTimeRangedCommands", path)
		case !hasLink && !listed:
			t.Errorf("%q takes a time range but has no --share-link/--open Explore link (DESIGN.md § Output Model; see .agents/skills/contribute/references/datasource-implementation.md). If it runs no user query, list it in unlinkedTimeRangedCommands as metadata", path)
		}
	})

	if len(seen) == 0 {
		t.Fatal("found no time-ranged datasource commands; the walk is broken")
	}
	reportStale(t, seen, unlinkedTimeRangedCommands, "unlinkedTimeRangedCommands entry %q no longer names a time-ranged datasource command; remove it")
}
