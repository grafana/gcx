package skills_test

import (
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"testing"
	"testing/fstest"

	claudeplugin "github.com/grafana/gcx/claude-plugin"
	"github.com/grafana/gcx/internal/skills"
	"github.com/stretchr/testify/require"
)

func TestLoadCatalog(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		data string
		err  string
	}{
		{name: "active", data: "skills: {alpha: {status: active}}"},
		{name: "deprecated with replacement", data: "skills: {alpha: {status: deprecated, replacement: old}, old: {status: retired}}"},
		{name: "retired with replacement", data: "skills: {alpha: {status: active}, old: {status: retired, replacement: alpha}}"},
		{name: "empty catalog", data: "skills: {}"},
		{name: "bad status", data: "skills: {alpha: {status: deleted}}", err: "invalid status"},
		{name: "missing status", data: "skills: {alpha: {}}", err: "invalid status"},
		{name: "invalid name", data: "skills: {alpha: {status: active}, '../old': {status: retired}}", err: "invalid skill name"},
		{name: "unknown field", data: "skills: {alpha: {status: active, typo: true}}", err: "field typo"},
		{name: "duplicate key", data: "skills: {alpha: {status: active}, alpha: {status: active}}", err: "already defined"},
		{name: "missing skills", data: "{}", err: "skills map"},
		{name: "multiple documents", data: "skills: {}\n---\nskills: {}", err: "exactly one"},
		{name: "invalid yaml", data: "[", err: "decode skills catalog"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := skills.LoadCatalog([]byte(tc.data))
			if tc.err != "" {
				require.ErrorContains(t, err, tc.err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestBundledCatalog(t *testing.T) {
	t.Parallel()
	catalog, err := skills.LoadCatalog(claudeplugin.SkillsCatalog())
	require.NoError(t, err)
	require.NotEmpty(t, catalog.Skills)
	source := claudeplugin.SkillsFS()
	bundled, err := fs.ReadDir(source, ".")
	require.NoError(t, err)
	for _, entry := range bundled {
		if entry.IsDir() {
			require.Contains(t, catalog.Skills, entry.Name(), "bundled skill must have a catalog entry")
		}
	}
	// Packaging invariants belong here, not on the uninstall recovery path.
	for name, entry := range catalog.Skills {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			switch entry.Status {
			case skills.Active, skills.Deprecated:
				info, err := fs.Stat(source, path.Join(name, "SKILL.md"))
				require.NoError(t, err, "active/deprecated skill must have bundled content")
				require.True(t, info.Mode().IsRegular(), "SKILL.md must be a regular file")
			case skills.Retired:
				_, err := fs.Stat(source, name)
				require.ErrorIs(t, err, fs.ErrNotExist, "retired skill must not have bundled content")
			}
			if entry.Replacement != "" {
				require.Contains(t, catalog.Skills, entry.Replacement, "replacement must exist in the catalog")
			}
		})
	}
}

func TestReconcile(t *testing.T) {
	t.Parallel()
	source := fstest.MapFS{
		"alpha/SKILL.md": {Data: []byte("alpha description")},
		"beta/SKILL.md":  {Data: []byte("beta description")},
	}
	catalog := []byte(`skills:
  alpha: {status: active}
  beta: {status: deprecated, replacement: alpha}
  old: {status: retired, replacement: alpha}
`)
	for _, tc := range []struct {
		name       string
		local      []string
		incomplete bool
	}{
		{name: "missing root"},
		{name: "installed", local: []string{"alpha", "beta", "old", "external"}},
		{name: "partial installation", local: []string{"old"}, incomplete: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			root := filepath.Join(t.TempDir(), ".agents")
			for _, name := range tc.local {
				dir := filepath.Join(root, "skills", name)
				require.NoError(t, os.MkdirAll(dir, 0o755))
				if !tc.incomplete {
					require.NoError(t, os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("local"), 0o600))
				}
			}
			states, err := skills.Reconcile(source, catalog, root)
			require.NoError(t, err)
			byName := make(map[string]skills.SkillState)
			for _, state := range states {
				byName[state.Name] = state
			}
			for _, name := range []string{"alpha", "beta", "old"} {
				require.True(t, byName[name].Known)
			}
			require.Equal(t, skills.Active, byName["alpha"].Status)
			require.Equal(t, "alpha description", byName["alpha"].ShortDescription)
			require.Equal(t, skills.Deprecated, byName["beta"].Status)
			require.Equal(t, skills.Retired, byName["old"].Status)
			require.Equal(t, "alpha", byName["old"].Replacement)
			for _, name := range tc.local {
				require.True(t, byName[name].Present)
				require.Equal(t, !tc.incomplete, byName[name].Installed)
			}
			switch tc.name {
			case "installed":
				require.False(t, byName["external"].Known)
				require.Empty(t, byName["external"].Status)
			case "missing root":
				require.Len(t, states, 3)
				require.False(t, byName["alpha"].Present)
				require.False(t, byName["old"].Installed)
				require.NoDirExists(t, root)
			}
		})
	}
}

func TestReconcile_StatErrorsDoNotBlockOtherSkills(t *testing.T) {
	t.Parallel()
	for _, failure := range []string{"unreadable directory", "symlink loop"} {
		t.Run(failure, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			source := fstest.MapFS{"healthy/SKILL.md": {Data: []byte("healthy")}}
			catalog := []byte("skills: {healthy: {status: active}, broken: {status: active}}")
			healthy := filepath.Join(root, "skills", "healthy")
			require.NoError(t, os.MkdirAll(healthy, 0o755))
			require.NoError(t, os.WriteFile(filepath.Join(healthy, "SKILL.md"), []byte("healthy"), 0o600))
			for _, name := range []string{"broken", "external"} {
				dir := filepath.Join(root, "skills", name)
				if failure == "symlink loop" {
					require.NoError(t, os.Symlink(name, dir))
				} else {
					require.NoError(t, os.MkdirAll(dir, 0o755))
					require.NoError(t, os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("hidden"), 0o600))
					t.Cleanup(func() { require.NoError(t, os.Chmod(dir, 0o755)) })
					require.NoError(t, os.Chmod(dir, 0))
				}
				_, err := os.Stat(filepath.Join(dir, "SKILL.md"))
				if failure == "unreadable directory" && err == nil {
					t.Skip("directory permissions do not restrict this test process")
				}
				require.Error(t, err)
				require.NotErrorIs(t, err, os.ErrNotExist)
			}

			states, err := skills.Reconcile(source, catalog, root)
			require.NoError(t, err)
			require.Len(t, states, 3)
			for _, state := range states {
				require.True(t, state.Present)
				require.Equal(t, state.Name == "healthy", state.Installed)
				require.Equal(t, state.Name != "external", state.Known)
				if !state.Known {
					require.Empty(t, state.Status)
				}
			}
			listed, err := skills.List(source, catalog, root)
			require.NoError(t, err)
			require.Len(t, listed.Skills, 2)
			require.Equal(t, "broken", listed.Skills[0].Name)
			require.False(t, listed.Skills[0].Installed)
			_, err = skills.Install(source, catalog, root, map[string]struct{}{"healthy": {}}, false, true)
			require.NoError(t, err)
			_, err = skills.Update(source, catalog, root, nil, true)
			require.NoError(t, err)
			removed, err := skills.Uninstall(source, catalog, root, []string{"healthy"}, false, false)
			require.NoError(t, err)
			require.Equal(t, []string{"healthy"}, removed.Removed)
			require.NoDirExists(t, healthy)
			if failure == "symlink loop" {
				removed, err = skills.Uninstall(source, catalog, root, []string{"broken"}, false, false)
				require.NoError(t, err)
				require.Equal(t, []string{"broken"}, removed.Removed)
			}
		})
	}
}

func TestValidateSkillName(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"", " ", ".", "..", "../alpha", "a/b", `a\b`, " alpha", "alpha ", "a\x00b"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			require.ErrorContains(t, skills.ValidateSkillName(name), "invalid skill name")
		})
	}
}
