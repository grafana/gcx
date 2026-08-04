package agent //nolint:testpackage // drift test inspects the unexported commandSkills registry directly

import (
	"io/fs"
	"testing"

	claudeplugin "github.com/grafana/gcx/claude-plugin"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

func TestSkillsForCommand_NearestAncestor(t *testing.T) {
	t.Parallel()

	root := &cobra.Command{Use: "gcx"}
	dashboards := &cobra.Command{Use: "dashboards"}
	get := &cobra.Command{Use: "get"}
	dashboards.AddCommand(get)
	root.AddCommand(dashboards)

	unmapped := &cobra.Command{Use: "providers"}
	list := &cobra.Command{Use: "list"}
	unmapped.AddCommand(list)
	root.AddCommand(unmapped)

	profiles := &cobra.Command{Use: "profiles"}
	profileQuery := &cobra.Command{Use: "query"}
	profiles.AddCommand(profileQuery)
	root.AddCommand(profiles)

	datasources := &cobra.Command{Use: "datasources"}
	pyroscope := &cobra.Command{Use: "pyroscope"}
	pyroscopeQuery := &cobra.Command{Use: "query"}
	pyroscope.AddCommand(pyroscopeQuery)
	datasources.AddCommand(pyroscope)
	root.AddCommand(datasources)

	cases := []struct {
		name string
		cmd  *cobra.Command
		want []string
	}{
		{name: "mapped area node", cmd: dashboards, want: []string{"create-dashboard", "manage-dashboards"}},
		{name: "leaf inherits from area ancestor", cmd: get, want: []string{"create-dashboard", "manage-dashboards"}},
		{name: "profiles retains general debugging alongside profiling RCA", cmd: profileQuery, want: []string{"performance-rca", "debug-with-grafana"}},
		{name: "typed Pyroscope overrides general datasource routing", cmd: pyroscopeQuery, want: []string{"performance-rca"}},
		{name: "other datasources retain general debugging", cmd: datasources, want: []string{"debug-with-grafana"}},
		{name: "unmapped area", cmd: unmapped, want: nil},
		{name: "unmapped leaf", cmd: list, want: nil},
		{name: "root", cmd: root, want: nil},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, SkillsForCommand(tc.cmd))
		})
	}
}

func TestCommandSkills_AllSkillsExistInBundle(t *testing.T) {
	t.Parallel()

	source := claudeplugin.SkillsFS()
	for area, skills := range commandSkills {
		for _, skill := range skills {
			t.Run(area+"/"+skill, func(t *testing.T) {
				t.Parallel()
				_, err := fs.Stat(source, skill+"/SKILL.md")
				require.NoErrorf(t, err, "mapped skill %q for %q must exist in the bundle", skill, area)
			})
		}
	}
}
