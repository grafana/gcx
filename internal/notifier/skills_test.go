package notifier //nolint:testpackage // Tests fixture the notifier against an in-memory release bundle.

import (
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/require"
)

func TestSkillsUpdateMessage(t *testing.T) {
	t.Parallel()
	source := fstest.MapFS{
		"alpha/SKILL.md": {Data: []byte("alpha-skill")},
		"beta/SKILL.md":  {Data: []byte("beta-skill")},
	}
	catalog := []byte("skills: {alpha: {status: active}, beta: {status: active}, old: {status: retired, replacement: alpha}}")
	for _, tc := range []struct {
		name    string
		skill   string
		content string
		message string
	}{
		{name: "no installed skills"},
		{name: "matches bundle", skill: "alpha", content: "alpha-skill"},
		{name: "differs from bundle", skill: "alpha", content: "local-change", message: "can be updated"},
		{name: "retired installation", skill: "old", content: "old-skill", message: "Retired gcx skills"},
		{name: "unmanaged installation", skill: "external", content: "external-skill"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			if tc.skill != "" {
				dir := filepath.Join(root, "skills", tc.skill)
				require.NoError(t, os.MkdirAll(dir, 0o755))
				require.NoError(t, os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(tc.content), 0o600))
			}
			message, err := SkillsUpdateMessage(source, catalog, root)
			require.NoError(t, err)
			if tc.message == "" {
				require.Empty(t, message)
			} else {
				require.Contains(t, message, tc.message)
				require.Contains(t, message, "Run: gcx agent skills update")
			}
			if tc.skill != "" {
				data, err := os.ReadFile(filepath.Join(root, "skills", tc.skill, "SKILL.md"))
				require.NoError(t, err)
				require.Equal(t, tc.content, string(data))
			}
		})
	}
}
