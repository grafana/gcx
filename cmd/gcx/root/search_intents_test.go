package root_test

import (
	"io/fs"
	"strings"
	"testing"

	claudeplugin "github.com/grafana/gcx/claude-plugin"
	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/skills"
	"github.com/stretchr/testify/require"
)

func TestSearchIntentRegistryResolves(t *testing.T) {
	cmd := buildRootCmd()
	cmd.Use = "gcx"
	for path, terms := range agent.CommandSearchTerms() {
		leaf, args, err := cmd.Find(strings.Fields(path)[1:])
		require.NoError(t, err, path)
		require.Empty(t, args, path)
		require.Equal(t, path, leaf.CommandPath())
		require.True(t, leaf.Runnable(), path)
		require.NotEmpty(t, terms, path)
		for node := leaf; node != nil; node = node.Parent() {
			require.False(t, node.Hidden, path)
			require.Empty(t, node.Deprecated, path)
		}
	}
	catalog, err := skills.LoadCatalog(claudeplugin.SkillsCatalog())
	require.NoError(t, err)
	seen := map[string]bool{}
	for _, workflow := range agent.SearchWorkflows() {
		require.False(t, seen[workflow.Skill])
		seen[workflow.Skill] = true
		require.Equal(t, skills.Active, catalog.Skills[workflow.Skill].Status, workflow.Skill)
		_, err := fs.ReadFile(claudeplugin.SkillsFS(), workflow.Skill+"/SKILL.md")
		require.NoError(t, err)
		require.NotEmpty(t, workflow.Terms)
	}
}
