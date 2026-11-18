package skills //nolint:testpackage // Tests exercise filesystem ownership and private command constructors.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/grafana/gcx/internal/agent"
	skillops "github.com/grafana/gcx/internal/skills"
	"github.com/stretchr/testify/require"
)

func writeCustomSource(t *testing.T) string {
	t.Helper()
	directory := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(directory, "references"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(directory, "SKILL.md"), []byte("---\nname: service-debug\ndescription: Investigate service latency\nmetadata:\n  tags: logs, latency\n---\n# Workflow\nRead references/queries.md and verify the result.\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(directory, "references", "queries.md"), []byte("original query guidance"), 0o600))
	return directory
}

func TestCustomPublicCommandWorkflow(t *testing.T) {
	for _, mode := range []string{"json", "yaml", "text", "agents"} {
		t.Run(mode, func(t *testing.T) {
			t.Setenv("GCX_AGENT_MODE", "false")
			t.Setenv("GCX_AUTO_APPROVE", "0")
			agent.ResetForTesting()
			root := t.TempDir()
			directory := writeCustomSource(t)
			base := []string{"--dir", root, "-o", mode}
			out, _, err := executeCommand(t, newInstallCommand(testSkillsFS(), testCatalog()), append(base, directory)...)
			require.NoError(t, err)
			require.Contains(t, out, "custom/service-debug")
			out, _, err = executeCommand(t, newGetCommand(testSkillsFS(), testCatalog()), append(base, "custom/service-debug", "references/queries.md")...)
			require.NoError(t, err)
			require.Contains(t, out, "original query guidance")
			if mode == "json" {
				var content map[string]any
				require.NoError(t, json.Unmarshal([]byte(out), &content))
				require.Equal(t, "custom", content["source"])
				require.NotContains(t, content, "status", "custom workflows have no bundled release lifecycle")
			}
			out, _, err = executeCommand(t, newListCommand(testSkillsFS(), testCatalog()), append(base, "--source", "custom")...)
			require.NoError(t, err)
			require.Contains(t, out, "service-debug")
			require.NotContains(t, out, "alpha skill description")
			edit := filepath.Join(t.TempDir(), "edit")
			out, _, err = executeCommand(t, newExportCommand(), append(base, "custom/service-debug", edit)...)
			require.NoError(t, err)
			require.Contains(t, out, "revision")
			require.FileExists(t, filepath.Join(edit, "references", "queries.md"))
			require.NoError(t, os.WriteFile(filepath.Join(edit, "references", "queries.md"), []byte("previewed edit"), 0o600))
			out, _, err = executeCommand(t, newUpdateCommand(testSkillsFS(), testCatalog()), append(base, "custom/service-debug", "--from", edit, "--dry-run")...)
			require.NoError(t, err)
			require.Contains(t, out, "modified")
			require.Contains(t, out, "+previewed edit")
			require.NoError(t, os.WriteFile(filepath.Join(directory, "references", "queries.md"), []byte("updated query guidance"), 0o600))
			_, _, err = executeCommand(t, newUpdateCommand(testSkillsFS(), testCatalog()), append(base, "custom/service-debug", "--from", directory)...)
			require.NoError(t, err)
			out, _, err = executeCommand(t, newGetCommand(testSkillsFS(), testCatalog()), append(base, "custom/service-debug", "references/queries.md")...)
			require.NoError(t, err)
			require.Contains(t, out, "updated query guidance")
			_, _, err = executeCommand(t, newUninstallCommand(testSkillsFS(), testCatalog()), append(base, "custom/service-debug")...)
			require.ErrorContains(t, err, "requires --yes")
			_, _, err = executeCommand(t, newUninstallCommand(testSkillsFS(), testCatalog()), append(base, "custom/service-debug", "--dry-run")...)
			require.NoError(t, err)
			require.DirExists(t, filepath.Join(root, "custom-skills", "service-debug"))
			_, _, err = executeCommand(t, newUninstallCommand(testSkillsFS(), testCatalog()), append(base, "custom/service-debug", "--yes")...)
			require.NoError(t, err)
			require.NoDirExists(t, filepath.Join(root, "custom-skills", "service-debug"))
		})
	}
}

func TestSearchCommand(t *testing.T) {
	t.Setenv("GCX_AGENT_MODE", "false")
	agent.ResetForTesting()
	root := t.TempDir()
	_, err := skillops.InstallCustom(root, writeCustomSource(t), "", false)
	require.NoError(t, err)
	for _, tc := range []struct {
		name string
		args []string
		err  string
	}{
		{"case insensitive", []string{"LATENCY", "--tag", "LOGS", "--source", "custom", "--limit", "1"}, ""},
		{"no matches", []string{"missing"}, ""},
		{"negative limit", []string{"latency", "--limit=-1"}, "nonnegative"},
		{"empty query", []string{""}, "nonempty"},
		{"empty tag", []string{"latency", "--tag="}, "must not be empty"},
		{"invalid source", []string{"latency", "--source="}, "invalid skill source"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, _, err := executeCommand(t, newSearchCommand(testSkillsFS(), testCatalog()), append([]string{"--dir", root, "-o", "json"}, tc.args...)...)
			if tc.err != "" {
				require.ErrorContains(t, err, tc.err)
				return
			}
			require.NoError(t, err)
			var result skillops.SearchResult
			require.NoError(t, json.Unmarshal([]byte(out), &result))
			require.NotContains(t, out, "original query guidance")
			require.NotContains(t, out, "# Workflow")
			require.NotNil(t, result.Skills)
			if tc.name == "no matches" {
				require.Zero(t, result.Total)
			} else {
				require.Equal(t, 1, result.Total)
				require.Equal(t, "custom/service-debug", result.Skills[0].ID)
			}
		})
	}
}
