package agent

import (
	"github.com/spf13/cobra"
)

// commandSkills maps command-area paths to the bundled Agent Skills that are
// most relevant when working in that area. The mapping is intentionally kept at
// area granularity (not per-leaf) to keep the table small and avoid per-command
// churn; the same static-registry pattern as commandAnnotations and
// KnownResources. A drift test verifies every mapped skill name exists in the
// bundle and every key resolves to a real command.
//
//nolint:gochecknoglobals // centralized skill registry, accessed via SkillsForCommand
var commandSkills = map[string][]string{
	"gcx dashboards":           {"create-dashboard", "manage-dashboards"},
	"gcx resources":            {"generate-resource-stubs", "import-dashboards", "scaffold-project"},
	"gcx slo":                  {"slo-manage", "slo-investigate", "slo-optimize", "slo-check-status"},
	"gcx synthetic-monitoring": {"synth-manage-checks", "synth-investigate-check", "synth-check-status"},
	"gcx alert":                {"investigate-alert"},
	"gcx irm":                  {"oncall-triage"},
	"gcx logs":                 {"debug-with-grafana"},
	"gcx metrics":              {"debug-with-grafana"},
	"gcx traces":               {"debug-with-grafana"},
	"gcx profiles":             {"debug-with-grafana"},
	"gcx datasources":          {"explore-datasources"},
	"gcx kg":                   {"diagnose-entity-graph"},
	"gcx aio11y":               {"aio11y"},
	"gcx setup":                {"setup-gcx"},
	"gcx login":                {"setup-gcx"},
}

// SkillsForCommand returns the bundled skill names mapped to the nearest mapped
// ancestor of cmd (including cmd itself). It returns nil when no ancestor is
// mapped.
func SkillsForCommand(cmd *cobra.Command) []string {
	for c := cmd; c != nil; c = c.Parent() {
		if skills, ok := commandSkills[c.CommandPath()]; ok {
			return skills
		}
	}
	return nil
}

// CommandSkillPaths returns all command-area paths in the skill registry. Used
// by consistency tests to detect entries that don't resolve to a real command.
func CommandSkillPaths() []string {
	paths := make([]string, 0, len(commandSkills))
	for p := range commandSkills {
		paths = append(paths, p)
	}
	return paths
}
