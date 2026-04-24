package skills //nolint:testpackage // Tests exercise unexported installer helpers directly to cover conflict and dry-run behavior.

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"

	"github.com/grafana/gcx/internal/agent"
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

	result, err := installSkills(testSkillsFS(), root, nil, false, false)
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

func TestInstallSkills_SingleSkill(t *testing.T) {
	t.Parallel()

	root := filepath.Join(t.TempDir(), ".agents")
	filter := map[string]struct{}{"alpha": {}}

	result, err := installSkills(testSkillsFS(), root, filter, false, false)
	require.NoError(t, err)

	require.Equal(t, []string{"alpha"}, result.Skills)
	require.Equal(t, 1, result.SkillCount)
	require.Equal(t, 2, result.FileCount)
	require.Equal(t, 2, result.Written)

	data, err := os.ReadFile(filepath.Join(root, "skills", "alpha", "SKILL.md"))
	require.NoError(t, err)
	require.Equal(t, []byte("alpha-skill"), data)

	_, err = os.Stat(filepath.Join(root, "skills", "beta"))
	require.True(t, os.IsNotExist(err))
}

func TestInstallSkills_UnknownSkillReturnsError(t *testing.T) {
	t.Parallel()

	root := filepath.Join(t.TempDir(), ".agents")
	filter := map[string]struct{}{"nonexistent": {}}

	_, err := installSkills(testSkillsFS(), root, filter, false, false)
	require.Error(t, err)
	require.ErrorContains(t, err, "unknown skill")
}

func TestInstallSkills_DryRunDoesNotWriteFiles(t *testing.T) {
	t.Parallel()

	root := filepath.Join(t.TempDir(), ".agents")

	result, err := installSkills(testSkillsFS(), root, nil, false, true)
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

	_, err := installSkills(testSkillsFS(), root, nil, false, false)
	require.Error(t, err)
	require.ErrorContains(t, err, "use --force to overwrite")
}

func TestInstallSkills_ForceOverwritesDifferingFiles(t *testing.T) {
	t.Parallel()

	root := filepath.Join(t.TempDir(), ".agents")
	target := filepath.Join(root, "skills", "alpha")
	require.NoError(t, os.MkdirAll(target, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(target, "SKILL.md"), []byte("local-change"), 0o600))

	result, err := installSkills(testSkillsFS(), root, nil, true, false)
	require.NoError(t, err)
	require.Equal(t, 3, result.Written)
	require.Equal(t, 1, result.Overwritten)

	data, err := os.ReadFile(filepath.Join(target, "SKILL.md"))
	require.NoError(t, err)
	require.Equal(t, []byte("alpha-skill"), data)
}

func TestInstallCommand_AllInstallsEverything(t *testing.T) {
	t.Setenv("GCX_AGENT_MODE", "false")
	agent.ResetForTesting()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	cmd := newInstallCommand(testSkillsFS())
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)
	cmd.SetArgs([]string{"--all"})

	err := cmd.Execute()
	require.NoError(t, err)

	data, err := os.ReadFile(filepath.Join(home, ".agents", "skills", "alpha", "SKILL.md"))
	require.NoError(t, err)
	require.Equal(t, []byte("alpha-skill"), data)
	require.Contains(t, stdout.String(), "Installed 2 skill(s)")
}

func TestInstallCommand_SingleSkillByName(t *testing.T) {
	t.Setenv("GCX_AGENT_MODE", "false")
	agent.ResetForTesting()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	cmd := newInstallCommand(testSkillsFS())
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)
	cmd.SetArgs([]string{"beta"})

	err := cmd.Execute()
	require.NoError(t, err)
	require.Contains(t, stdout.String(), "Installed 1 skill(s)")

	_, err = os.Stat(filepath.Join(home, ".agents", "skills", "alpha"))
	require.True(t, os.IsNotExist(err))

	data, err := os.ReadFile(filepath.Join(home, ".agents", "skills", "beta", "SKILL.md"))
	require.NoError(t, err)
	require.Equal(t, []byte("beta-skill"), data)
}

func TestInstallCommand_NoArgsNoAllReturnsError(t *testing.T) {
	cmd := newInstallCommand(testSkillsFS())
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)
	cmd.SetArgs(nil)

	err := cmd.Execute()
	require.Error(t, err)
	require.ErrorContains(t, err, "provide at least one skill name or use --all")
}

func TestInstalledBundledSkillNames_ReturnsOnlyInstalledBundled(t *testing.T) {
	t.Parallel()

	root := filepath.Join(t.TempDir(), ".agents")
	require.NoError(t, os.MkdirAll(filepath.Join(root, "skills", "alpha"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "skills", "external"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "skills", "alpha", "SKILL.md"), []byte("alpha-skill"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(root, "skills", "external", "SKILL.md"), []byte("external-skill"), 0o600))

	names, err := installedBundledSkillNames(testSkillsFS(), root)
	require.NoError(t, err)
	require.Equal(t, []string{"alpha"}, names)
}

func TestUpdateCommand_UpdatesOnlyInstalledSkills(t *testing.T) {
	t.Setenv("GCX_AGENT_MODE", "false")
	agent.ResetForTesting()
	root := filepath.Join(t.TempDir(), ".agents")

	require.NoError(t, os.MkdirAll(filepath.Join(root, "skills", "alpha"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "skills", "alpha", "SKILL.md"), []byte("local-change"), 0o600))

	cmd := newUpdateCommand(testSkillsFS())
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)
	cmd.SetArgs([]string{"--dir", root})

	err := cmd.Execute()
	require.NoError(t, err)
	require.Contains(t, stdout.String(), "Updated 1 skill(s)")

	data, err := os.ReadFile(filepath.Join(root, "skills", "alpha", "SKILL.md"))
	require.NoError(t, err)
	require.Equal(t, []byte("alpha-skill"), data)

	_, err = os.Stat(filepath.Join(root, "skills", "beta"))
	require.True(t, os.IsNotExist(err))
}

func TestUpdateCommand_NoInstalledSkillsNoOp(t *testing.T) {
	t.Setenv("GCX_AGENT_MODE", "false")
	agent.ResetForTesting()
	root := filepath.Join(t.TempDir(), ".agents")

	cmd := newUpdateCommand(testSkillsFS())
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)
	cmd.SetArgs([]string{"--dir", root})

	err := cmd.Execute()
	require.NoError(t, err)
	require.Contains(t, stdout.String(), "Updated 0 skill(s)")

	_, err = os.Stat(filepath.Join(root, "skills"))
	require.True(t, os.IsNotExist(err))
}

func TestUpdateCommand_ExplicitSkillMustAlreadyBeInstalled(t *testing.T) {
	t.Setenv("GCX_AGENT_MODE", "false")
	agent.ResetForTesting()
	root := filepath.Join(t.TempDir(), ".agents")

	cmd := newUpdateCommand(testSkillsFS())
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)
	cmd.SetArgs([]string{"--dir", root, "alpha"})

	err := cmd.Execute()
	require.Error(t, err)
	require.ErrorContains(t, err, `skill "alpha" is not installed`)

	_, err = os.Stat(filepath.Join(root, "skills", "alpha", "SKILL.md"))
	require.True(t, os.IsNotExist(err))
}

func TestListBundledSkills_ReturnsSourceSkills(t *testing.T) {
	t.Parallel()

	nonexistent := filepath.Join(t.TempDir(), "no-such-dir")
	result, err := listBundledSkills(testSkillsFS(), nonexistent)
	require.NoError(t, err)
	require.Equal(t, []skillInfo{
		{Name: "alpha", ShortDescription: "alpha-skill", Installed: false},
		{Name: "beta", ShortDescription: "beta-skill", Installed: false},
	}, result.Skills)
	require.Equal(t, 2, result.SkillCount)
}

func TestListBundledSkills_ShowsInstalledStatus(t *testing.T) {
	t.Parallel()

	root := filepath.Join(t.TempDir(), ".agents")
	require.NoError(t, os.MkdirAll(filepath.Join(root, "skills", "alpha"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "skills", "alpha", "SKILL.md"), []byte("alpha-skill"), 0o600))

	result, err := listBundledSkills(testSkillsFS(), root)
	require.NoError(t, err)
	require.Len(t, result.Skills, 2)

	require.Equal(t, "alpha", result.Skills[0].Name)
	require.True(t, result.Skills[0].Installed)

	require.Equal(t, "beta", result.Skills[1].Name)
	require.False(t, result.Skills[1].Installed)
}

func TestExtractSkillShortDescription_ParsesFrontMatter(t *testing.T) {
	t.Parallel()

	data := []byte(`---
name: sample
description: >
  Use this skill to do an example operation in a concise way.
---

# Sample
`)
	desc := extractSkillShortDescription(data)
	require.Equal(t, "Use this skill to do an example operation in a concise way.", desc)
}

func TestListCommand_JSONIncludesShortDescription(t *testing.T) {
	source := fstest.MapFS{
		"alpha/SKILL.md": {
			Data: []byte(`---
name: alpha
description: alpha skill description
---
`),
		},
	}

	cmd := newListCommand(source)
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)
	cmd.SetArgs([]string{"-o", "json"})

	err := cmd.Execute()
	require.NoError(t, err)
	require.Contains(t, stdout.String(), `"name": "alpha"`)
	require.Contains(t, stdout.String(), `"short_description": "alpha skill description"`)
}

func TestUninstallSkills_RemovesRequestedSkills(t *testing.T) {
	t.Parallel()

	root := filepath.Join(t.TempDir(), ".agents")
	require.NoError(t, os.MkdirAll(filepath.Join(root, "skills", "alpha"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "skills", "beta"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "skills", "alpha", "SKILL.md"), []byte("alpha"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(root, "skills", "beta", "SKILL.md"), []byte("beta"), 0o600))

	result, err := uninstallSkills(root, []string{"alpha", "missing"}, false)
	require.NoError(t, err)
	require.Equal(t, []string{"alpha", "missing"}, result.Requested)
	require.Equal(t, []string{"alpha"}, result.Removed)
	require.Equal(t, []string{"missing"}, result.Missing)
	require.Equal(t, 1, result.RemovedCount)
	require.Equal(t, 1, result.MissingCount)

	_, err = os.Stat(filepath.Join(root, "skills", "alpha"))
	require.Error(t, err)
	require.True(t, os.IsNotExist(err))
}

func TestUninstallSkills_DryRunDoesNotRemoveFiles(t *testing.T) {
	t.Parallel()

	root := filepath.Join(t.TempDir(), ".agents")
	require.NoError(t, os.MkdirAll(filepath.Join(root, "skills", "alpha"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "skills", "alpha", "SKILL.md"), []byte("alpha"), 0o600))

	result, err := uninstallSkills(root, []string{"alpha"}, true)
	require.NoError(t, err)
	require.Equal(t, []string{"alpha"}, result.Removed)
	require.True(t, result.DryRun)

	_, err = os.Stat(filepath.Join(root, "skills", "alpha", "SKILL.md"))
	require.NoError(t, err)
}

func TestUninstallSkills_InvalidName(t *testing.T) {
	t.Parallel()

	root := filepath.Join(t.TempDir(), ".agents")

	_, err := uninstallSkills(root, []string{"../alpha"}, false)
	require.Error(t, err)
	require.ErrorContains(t, err, "invalid skill name")
}

func TestUninstallCommand_AllRequiresApproval(t *testing.T) {
	t.Setenv("GCX_AUTO_APPROVE", "0")

	cmd := newUninstallCommand(testSkillsFS())
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)
	cmd.SetArgs([]string{"--all"})

	err := cmd.Execute()
	require.Error(t, err)
	require.ErrorContains(t, err, "refusing to uninstall all gcx skills without --yes")
}

func TestUninstallCommand_AllWithYesRemovesOnlyBundledSkills(t *testing.T) {
	t.Setenv("GCX_AGENT_MODE", "false")
	agent.ResetForTesting()
	root := filepath.Join(t.TempDir(), ".agents")

	// Install gcx-bundled skills (alpha, beta) and a non-gcx skill (external).
	require.NoError(t, os.MkdirAll(filepath.Join(root, "skills", "alpha"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "skills", "beta"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "skills", "external"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "skills", "alpha", "SKILL.md"), []byte("alpha"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(root, "skills", "beta", "SKILL.md"), []byte("beta"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(root, "skills", "external", "SKILL.md"), []byte("not from gcx"), 0o600))

	cmd := newUninstallCommand(testSkillsFS())
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)
	cmd.SetArgs([]string{"--dir", root, "--all", "--yes"})

	err := cmd.Execute()
	require.NoError(t, err)
	require.Contains(t, stdout.String(), "Uninstalled 2 skill(s)")

	// Bundled skills removed.
	_, err = os.Stat(filepath.Join(root, "skills", "alpha"))
	require.True(t, os.IsNotExist(err))
	_, err = os.Stat(filepath.Join(root, "skills", "beta"))
	require.True(t, os.IsNotExist(err))

	// Non-gcx skill is untouched.
	_, err = os.Stat(filepath.Join(root, "skills", "external", "SKILL.md"))
	require.NoError(t, err)
}

func TestUninstallCommand_RejectsNonBundledSkillName(t *testing.T) {
	cmd := newUninstallCommand(testSkillsFS())
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)
	cmd.SetArgs([]string{"external"})

	err := cmd.Execute()
	require.Error(t, err)
	require.ErrorContains(t, err, "unknown skill")
}
