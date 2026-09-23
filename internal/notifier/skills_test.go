package notifier //nolint:testpackage // Tests fixture the notifier against an in-memory release bundle.

import (
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"

	skillops "github.com/grafana/gcx/internal/skills"
	"github.com/stretchr/testify/require"
)

func TestSkillsUpdateMessage(t *testing.T) {
	t.Parallel()
	const updateMessage = "Installed gcx skills can be updated to match this gcx version.\nRun: gcx agent skills update"
	const retiredMessage = "Retired gcx skills are still present locally.\nRun: gcx agent skills uninstall old"
	catalog := []byte("skills: {alpha: {status: active}, beta: {status: active}, old: {status: retired, replacement: alpha}, old-two: {status: retired}}")
	for _, tc := range []struct {
		name             string
		installed        map[string]string
		missingReference bool
		message          string
	}{
		{name: "no installed skills"},
		{name: "matches bundle", installed: map[string]string{"alpha": "alpha-skill"}},
		{name: "differs from bundle", installed: map[string]string{"alpha": "local-change"}, message: updateMessage},
		{name: "retired installation", installed: map[string]string{"old": "old-skill"}, message: retiredMessage},
		{name: "unmanaged installation", installed: map[string]string{"external": "external-skill"}},
		{name: "retired and matching content", installed: map[string]string{"old": "old-skill", "alpha": "alpha-skill"}, message: retiredMessage},
		{name: "retired and changed content", installed: map[string]string{"old": "old-skill", "alpha": "local-change"}, message: updateMessage + "\n\n" + retiredMessage},
		{name: "retired and missing bundled file", installed: map[string]string{"old": "old-skill", "alpha": "alpha-skill"}, missingReference: true, message: updateMessage + "\n\n" + retiredMessage},
		{name: "multiple retired skills", installed: map[string]string{"old": "old-skill", "old-two": "another-old-skill"}, message: retiredMessage + " old-two"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			source := fstest.MapFS{
				"alpha/SKILL.md": {Data: []byte("alpha-skill")},
				"beta/SKILL.md":  {Data: []byte("beta-skill")},
			}
			if tc.missingReference {
				source["alpha/references/guide.md"] = &fstest.MapFile{Data: []byte("new guide")}
			}
			root := t.TempDir()
			for name, content := range tc.installed {
				dir := filepath.Join(root, "skills", name)
				require.NoError(t, os.MkdirAll(dir, 0o755))
				require.NoError(t, os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o600))
			}
			message, err := SkillsUpdateMessage(source, catalog, root)
			require.NoError(t, err)
			require.Equal(t, tc.message, message)
			for name, content := range tc.installed {
				data, err := os.ReadFile(filepath.Join(root, "skills", name, "SKILL.md"))
				require.NoError(t, err)
				require.Equal(t, content, string(data))
			}
			require.NoFileExists(t, filepath.Join(root, "skills", "alpha", "references", "guide.md"))
		})
	}
}

func TestSkillsUpdateMessage_ActionsClearOnlyTheirOwnNotice(t *testing.T) {
	t.Parallel()
	source := fstest.MapFS{"alpha/SKILL.md": {Data: []byte("alpha-skill")}}
	catalog := []byte("skills: {alpha: {status: active}, old: {status: retired}}")
	for _, firstAction := range []string{"update", "uninstall"} {
		t.Run(firstAction+" first", func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			for _, name := range []string{"alpha", "old"} {
				dir := filepath.Join(root, "skills", name)
				require.NoError(t, os.MkdirAll(dir, 0o755))
				require.NoError(t, os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("local-content"), 0o600))
			}
			actions := map[string]func(){
				"update": func() {
					_, err := skillops.Update(source, catalog, root, nil, false)
					require.NoError(t, err)
				},
				"uninstall": func() {
					_, err := skillops.Uninstall(source, catalog, root, []string{"old"}, false, false)
					require.NoError(t, err)
				},
			}
			actions[firstAction]()
			message, err := SkillsUpdateMessage(source, catalog, root)
			require.NoError(t, err)
			if firstAction == "update" {
				require.NotContains(t, message, "Run: gcx agent skills update")
				require.Contains(t, message, "Run: gcx agent skills uninstall old")
				require.FileExists(t, filepath.Join(root, "skills", "old", "SKILL.md"))
				actions["uninstall"]()
			} else {
				require.Contains(t, message, "Run: gcx agent skills update")
				require.NotContains(t, message, "Run: gcx agent skills uninstall")
				actions["update"]()
			}
			message, err = SkillsUpdateMessage(source, catalog, root)
			require.NoError(t, err)
			require.Empty(t, message)
		})
	}
}
