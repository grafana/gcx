package root_test

import (
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/agent"
	"github.com/spf13/cobra"
)

// selfHostedAreas are top-level areas that work against self-hosted Grafana
// (OSS and Enterprise) as well as Grafana Cloud. Together with
// agent.CloudOnlyPaths this classifies every top-level area: a new area must
// be added to one or the other.
//
//nolint:gochecknoglobals // constant-like lookup table for test validation
var selfHostedAreas = map[string]bool{
	"gcx agent":       true,
	"gcx alert":       true,
	"gcx api":         true,
	"gcx commands":    true,
	"gcx completion":  true,
	"gcx config":      true,
	"gcx dashboards":  true,
	"gcx datasources": true,
	"gcx dev":         true,
	"gcx docs":        true,
	"gcx help":        true,
	"gcx help-tree":   true,
	"gcx login":       true,
	"gcx providers":   true,
	"gcx resources":   true,
	"gcx version":     true,
}

// areasWithoutAvailability shipped before availability was classified for
// every area. This list only shrinks: classify the area, then remove it.
//
//nolint:gochecknoglobals // constant-like lookup table for test validation
var areasWithoutAvailability = map[string]bool{
	"gcx dbo11y": true,
}

// areasWithoutSkillDecision shipped before every area declared its related
// skills. This list only shrinks: add the area to agent.commandSkills, with an
// empty list when no skill applies, then remove it here.
//
//nolint:gochecknoglobals // constant-like lookup table for test validation
var areasWithoutSkillDecision = map[string]bool{
	"gcx agent":           true,
	"gcx api":             true,
	"gcx appo11y":         true,
	"gcx assistant":       true,
	"gcx cloud":           true,
	"gcx commands":        true,
	"gcx completion":      true,
	"gcx config":          true,
	"gcx dbo11y":          true,
	"gcx dev":             true,
	"gcx docs":            true,
	"gcx fleet":           true,
	"gcx frontend":        true,
	"gcx help":            true,
	"gcx help-tree":       true,
	"gcx instrumentation": true,
	"gcx k6":              true,
	"gcx providers":       true,
	"gcx version":         true,
}

// topLevelAreas returns the command paths of the visible top-level commands.
func topLevelAreas() []*cobra.Command {
	root := buildRootCmd()
	root.InitDefaultHelpCmd()
	root.InitDefaultCompletionCmd()
	var out []*cobra.Command
	for _, cmd := range root.Commands() {
		if !cmd.Hidden {
			out = append(out, cmd)
		}
	}
	return out
}

// TestConsistency_EveryAreaHasAvailability fails when a top-level area is
// neither Grafana Cloud-only (wholly or in part) nor listed as self-hosted.
// Without a classification, agents and the help tree cannot tell users
// whether the area works on their Grafana.
func TestConsistency_EveryAreaHasAvailability(t *testing.T) {
	cloud := agent.CloudOnlyPaths()
	seen := map[string]bool{}
	for _, cmd := range topLevelAreas() {
		path := cmd.CommandPath()
		seen[path] = true
		partlyCloud := slices.ContainsFunc(cloud, func(p string) bool { return strings.HasPrefix(p, path+" ") })
		classified := agent.IsCloudOnlyPath(path) || partlyCloud || selfHostedAreas[path]
		switch {
		case classified && areasWithoutAvailability[path]:
			t.Errorf("%q is now classified; remove it from areasWithoutAvailability", path)
		case !classified && !areasWithoutAvailability[path]:
			t.Errorf("%q has no availability: add it to the cloud-only registry in internal/agent/availability.go or to selfHostedAreas", path)
		case agent.IsCloudOnlyPath(path) && selfHostedAreas[path]:
			t.Errorf("%q is both cloud-only and listed in selfHostedAreas", path)
		}
	}
	reportStale(t, seen, selfHostedAreas, "selfHostedAreas entry %q is not a top-level command any more; remove it")
	reportStale(t, seen, areasWithoutAvailability, "areasWithoutAvailability entry %q is not a top-level command any more; remove it")
}

// TestConsistency_EveryAreaHasSkillDecision fails when a top-level area has no
// entry in the command-to-skill registry. An empty entry is a valid decision:
// no bundled skill applies.
func TestConsistency_EveryAreaHasSkillDecision(t *testing.T) {
	mapped := agent.CommandSkillPaths()
	seen := map[string]bool{}
	for _, cmd := range topLevelAreas() {
		path := cmd.CommandPath()
		seen[path] = true
		decided := slices.Contains(mapped, path)
		switch {
		case decided && areasWithoutSkillDecision[path]:
			t.Errorf("%q now has a skill entry; remove it from areasWithoutSkillDecision", path)
		case !decided && !areasWithoutSkillDecision[path]:
			t.Errorf("%q has no entry in internal/agent/command_skills.go; add one, with an empty list when no skill applies", path)
		}
	}
	reportStale(t, seen, areasWithoutSkillDecision, "areasWithoutSkillDecision entry %q is not a top-level command any more; remove it")
}

// reportStale fails, with format applied to the entry, for every allowlist
// entry the test did not see. It keeps the allowlists shrink-only.
func reportStale[V any](t *testing.T, seen map[string]bool, list map[string]V, format string) {
	t.Helper()
	var stale []string
	for entry := range list {
		if !seen[entry] {
			stale = append(stale, entry)
		}
	}
	sort.Strings(stale)
	for _, entry := range stale {
		t.Errorf(format, entry)
	}
}
