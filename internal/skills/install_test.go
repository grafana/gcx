package skills_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"

	"github.com/grafana/gcx/internal/skills"
	"github.com/stretchr/testify/require"
)

func TestInstall(t *testing.T) {
	t.Parallel()
	source := fstest.MapFS{
		"alpha/SKILL.md":            {Data: []byte("alpha")},
		"alpha/references/guide.md": {Data: []byte("guide")},
		"beta/SKILL.md":             {Data: []byte("beta")},
	}
	catalog := []byte("skills: {alpha: {status: active}, beta: {status: active}, old: {status: retired}}")
	for _, tc := range []struct {
		name        string
		filter      map[string]struct{}
		initial     string
		force       bool
		dryRun      bool
		written     int
		overwritten int
		unchanged   int
		err         string
	}{
		{name: "all", written: 3},
		{name: "selected", filter: map[string]struct{}{"alpha": {}}, written: 2},
		{name: "empty selection", filter: map[string]struct{}{}},
		{name: "dry run", dryRun: true, written: 3},
		{name: "conflict", initial: "local-change", err: "use --force to overwrite"},
		{name: "force", initial: "local-change", force: true, written: 2, overwritten: 1},
		{name: "force dry run", initial: "local-change", force: true, dryRun: true, written: 2, overwritten: 1},
		{name: "unchanged", initial: "alpha", written: 2, unchanged: 1},
		{name: "unknown", filter: map[string]struct{}{"unknown": {}}, err: "unknown skill"},
		{name: "retired", filter: map[string]struct{}{"old": {}}, err: "retired"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			root := filepath.Join(t.TempDir(), ".agents")
			file := filepath.Join(root, "skills", "alpha", "SKILL.md")
			if tc.initial != "" {
				require.NoError(t, os.MkdirAll(filepath.Dir(file), 0o755))
				require.NoError(t, os.WriteFile(file, []byte(tc.initial), 0o600))
			}
			result, err := skills.Install(source, catalog, root, tc.filter, tc.force, tc.dryRun)
			if tc.err != "" {
				require.ErrorContains(t, err, tc.err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.written, result.Written)
			require.Equal(t, tc.overwritten, result.Overwritten)
			require.Equal(t, tc.unchanged, result.Unchanged)
			require.Equal(t, tc.written+tc.overwritten+tc.unchanged, result.FileCount)
			if tc.dryRun {
				if tc.initial == "" {
					require.NoDirExists(t, root)
				} else {
					data, err := os.ReadFile(file)
					require.NoError(t, err)
					require.Equal(t, tc.initial, string(data))
				}
			} else {
				for _, name := range result.Skills {
					data, err := os.ReadFile(filepath.Join(root, "skills", name, "SKILL.md"))
					require.NoError(t, err)
					expected, err := fs.ReadFile(source, name+"/SKILL.md")
					require.NoError(t, err)
					require.Equal(t, expected, data)
				}
			}
		})
	}
}

func TestRetiredInstallationWithoutSkillDocument(t *testing.T) {
	t.Parallel()
	for _, dryRun := range []bool{true, false} {
		t.Run(map[bool]string{true: "preview", false: "remove"}[dryRun], func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			dir := filepath.Join(root, "skills", "old")
			require.NoError(t, os.MkdirAll(dir, 0o755))
			require.NoError(t, os.WriteFile(filepath.Join(dir, "notes.md"), []byte("local edits"), 0o600))
			catalog := []byte("skills: {old: {status: retired}}")
			result, err := skills.Update(fstest.MapFS{}, catalog, root, nil, false)
			require.NoError(t, err)
			require.Len(t, result.Notices, 1)
			require.Equal(t, skills.Retired, result.Notices[0].Status)
			require.FileExists(t, filepath.Join(dir, "notes.md"))
			removed, err := skills.Uninstall(fstest.MapFS{}, catalog, root, []string{"old"}, false, dryRun)
			require.NoError(t, err)
			require.Equal(t, []string{"old"}, removed.Removed)
			if dryRun {
				require.DirExists(t, dir)
			} else {
				require.NoDirExists(t, dir)
			}
		})
	}
}

func TestUninstallSymlink(t *testing.T) {
	t.Parallel()
	root, target := t.TempDir(), t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(target, "SKILL.md"), []byte("external"), 0o600))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "skills"), 0o755))
	require.NoError(t, os.Symlink(target, filepath.Join(root, "skills", "old")))
	result, err := skills.Uninstall(fstest.MapFS{}, []byte("skills: {old: {status: retired}}"), root, nil, true, false)
	require.NoError(t, err)
	require.Equal(t, []string{"old"}, result.Removed)
	require.FileExists(t, filepath.Join(target, "SKILL.md"))
	_, err = os.Lstat(filepath.Join(root, "skills", "old"))
	require.ErrorIs(t, err, os.ErrNotExist)
}
