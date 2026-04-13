package skills //nolint:testpackage // Tests exercise unexported installer helpers directly to cover conflict and dry-run behavior.

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/require"
)

func testSkillsFS() fs.FS {
	return fstest.MapFS{
		"alpha/SKILL.md":                     {Data: []byte("alpha-skill")},
		"alpha/references/guide.md":          {Data: []byte("alpha-guide")},
		"beta/SKILL.md":                      {Data: []byte("beta-skill")},
		"beta/references/troubleshooting.md": {Data: []byte("beta-help")},
	}
}

func TestInstallSkills_WritesBundleIntoSkillsSubdir(t *testing.T) {
	t.Parallel()

	root := filepath.Join(t.TempDir(), ".agents")

	result, err := installSkills(testSkillsFS(), root, false, false)
	require.NoError(t, err)

	require.Equal(t, filepath.Clean(root), result.Root)
	require.Equal(t, filepath.Join(filepath.Clean(root), "skills"), result.SkillsDir)
	require.Equal(t, []string{"alpha", "beta"}, result.Skills)
	require.Equal(t, 2, result.SkillCount)
	require.Equal(t, 4, result.FileCount)
	require.Equal(t, 4, result.Written)
	require.Zero(t, result.Overwritten)
	require.Zero(t, result.Unchanged)

	data, err := os.ReadFile(filepath.Join(root, "skills", "alpha", "SKILL.md"))
	require.NoError(t, err)
	require.Equal(t, []byte("alpha-skill"), data)

	data, err = os.ReadFile(filepath.Join(root, "skills", "beta", "references", "troubleshooting.md"))
	require.NoError(t, err)
	require.Equal(t, []byte("beta-help"), data)
}

func TestInstallSkills_DryRunDoesNotWriteFiles(t *testing.T) {
	t.Parallel()

	root := filepath.Join(t.TempDir(), ".agents")

	result, err := installSkills(testSkillsFS(), root, false, true)
	require.NoError(t, err)
	require.True(t, result.DryRun)
	require.Equal(t, 4, result.Written)

	_, err = os.Stat(filepath.Join(root, "skills", "alpha", "SKILL.md"))
	require.Error(t, err)
	require.True(t, os.IsNotExist(err))
}

func TestInstallSkills_ConflictingFileRequiresForce(t *testing.T) {
	t.Parallel()

	root := filepath.Join(t.TempDir(), ".agents")
	target := filepath.Join(root, "skills", "alpha")
	require.NoError(t, os.MkdirAll(target, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(target, "SKILL.md"), []byte("local-change"), 0o600))

	_, err := installSkills(testSkillsFS(), root, false, false)
	require.Error(t, err)
	require.ErrorContains(t, err, "use --force to overwrite")
}

func TestInstallSkills_ForceOverwritesDifferingFiles(t *testing.T) {
	t.Parallel()

	root := filepath.Join(t.TempDir(), ".agents")
	target := filepath.Join(root, "skills", "alpha")
	require.NoError(t, os.MkdirAll(target, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(target, "SKILL.md"), []byte("local-change"), 0o600))

	result, err := installSkills(testSkillsFS(), root, true, false)
	require.NoError(t, err)
	require.Equal(t, 3, result.Written)
	require.Equal(t, 1, result.Overwritten)

	data, err := os.ReadFile(filepath.Join(target, "SKILL.md"))
	require.NoError(t, err)
	require.Equal(t, []byte("alpha-skill"), data)
}

func TestInstallCommand_UsesDefaultAgentsRootFromHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	cmd := newInstallCommand(testSkillsFS())
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)
	cmd.SetArgs(nil)

	err := cmd.Execute()
	require.NoError(t, err)

	data, err := os.ReadFile(filepath.Join(home, ".agents", "skills", "alpha", "SKILL.md"))
	require.NoError(t, err)
	require.Equal(t, []byte("alpha-skill"), data)
	require.Contains(t, stdout.String(), "Installed 2 skill(s)")
}
