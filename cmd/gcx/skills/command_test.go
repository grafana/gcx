package skills //nolint:testpackage // Tests exercise command wiring and text codecs with a fixture bundle.

import (
	"bytes"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"

	"github.com/grafana/gcx/internal/agent"
	skillops "github.com/grafana/gcx/internal/skills"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

func testSkillsFS() fs.FS {
	return fstest.MapFS{
		"alpha/SKILL.md":                     {Data: []byte("---\nname: alpha\ndescription: alpha skill description\n---\nalpha-skill")},
		"alpha/references/guide.md":          {Data: []byte("alpha-guide")},
		"beta/SKILL.md":                      {Data: []byte("beta-skill")},
		"beta/references/troubleshooting.md": {Data: []byte("beta-help")},
	}
}

func testCatalog() []byte {
	return []byte(`skills:
  alpha: {status: active}
  beta: {status: deprecated, replacement: alpha, message: Use alpha instead}
  old: {status: retired, replacement: alpha}
  missing: {status: retired}
`)
}

func putSkill(t *testing.T, root, name, content string) {
	t.Helper()
	dir := filepath.Join(root, "skills", name)
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o600))
}

func executeCommand(t *testing.T, cmd *cobra.Command, args ...string) (string, string, error) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return stdout.String(), stderr.String(), err
}

func TestInstallCommand(t *testing.T) {
	t.Setenv("GCX_AGENT_MODE", "false")
	agent.ResetForTesting()
	for _, tc := range []struct {
		name    string
		args    []string
		want    []string
		err     string
		dryRun  bool
		warning bool
	}{
		{name: "all", args: []string{"--all"}, want: []string{"alpha", "beta"}, warning: true},
		{name: "single", args: []string{"alpha"}, want: []string{"alpha"}},
		{name: "deprecated", args: []string{"beta"}, want: []string{"beta"}, warning: true},
		{name: "dry run", args: []string{"--all", "--dry-run"}, want: []string{"alpha", "beta"}, warning: true, dryRun: true},
		{name: "retired", args: []string{"old"}, err: "retired"},
		{name: "unknown", args: []string{"external"}, err: "unknown skill"},
		{name: "no targets", err: "provide at least one skill name or use --all"},
		{name: "all and targets", args: []string{"--all", "alpha"}, err: "skill names cannot be provided"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			args := append([]string{"--dir", root, "-o", "json"}, tc.args...)
			out, diagnostics, err := executeCommand(t, newInstallCommand(testSkillsFS(), testCatalog()), args...)
			if tc.err != "" {
				require.ErrorContains(t, err, tc.err)
				require.NoDirExists(t, filepath.Join(root, "skills"))
				return
			}
			require.NoError(t, err)
			var result installResult
			require.NoError(t, json.Unmarshal([]byte(out), &result))
			require.Equal(t, tc.want, result.Skills)
			require.Equal(t, tc.dryRun, result.DryRun)
			if tc.warning {
				require.Contains(t, diagnostics, "deprecated")
				require.Len(t, result.Notices, 1)
				require.Equal(t, "alpha", result.Notices[0].Replacement)
			}
			for _, name := range tc.want {
				file := filepath.Join(root, "skills", name, "SKILL.md")
				if tc.dryRun {
					require.NoFileExists(t, file)
				} else {
					require.FileExists(t, file)
				}
			}
			require.NoDirExists(t, filepath.Join(root, "skills", "old"))
		})
	}
}

func TestInstallCommand_DefaultRoot(t *testing.T) {
	t.Setenv("GCX_AGENT_MODE", "false")
	agent.ResetForTesting()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	out, _, err := executeCommand(t, newInstallCommand(testSkillsFS(), testCatalog()), "alpha")
	require.NoError(t, err)
	require.Contains(t, out, "Installed 1 skill(s)")
	require.FileExists(t, filepath.Join(home, ".agents", "skills", "alpha", "SKILL.md"))
}

func TestUpdateCommand(t *testing.T) {
	t.Setenv("GCX_AGENT_MODE", "false")
	agent.ResetForTesting()
	for _, tc := range []struct {
		name      string
		installed []string
		args      []string
		updated   []string
		notices   int
		err       string
		dryRun    bool
	}{
		{name: "installed only", installed: []string{"alpha", "external"}, updated: []string{"alpha"}},
		{name: "no installed", updated: []string{}},
		{name: "missing explicit", args: []string{"alpha"}, err: "skill \"alpha\" is not installed; use 'gcx agent skills install alpha' to install it first"},
		{name: "missing deprecated", args: []string{"beta"}, err: "skill \"beta\" is not installed; use 'gcx agent skills install beta' to install it first"},
		{name: "missing retired", args: []string{"old"}, err: "skill \"old\" is retired and not installed; use 'gcx agent skills list' to see available skills"},
		{name: "unmanaged explicit", installed: []string{"external"}, args: []string{"external"}, err: "unknown skill"},
		{name: "retirement", installed: []string{"old", "external"}, updated: []string{}, notices: 1},
		{name: "explicit retirement", installed: []string{"old"}, args: []string{"old"}, updated: []string{}, notices: 1},
		{name: "mixed", installed: []string{"beta", "old"}, updated: []string{"beta"}, notices: 2},
		{name: "dry run", installed: []string{"alpha", "old"}, args: []string{"--dry-run"}, updated: []string{"alpha"}, notices: 1, dryRun: true},
		{name: "validate all before writes", installed: []string{"alpha"}, args: []string{"alpha", "unknown"}, err: "unknown skill"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			for _, name := range tc.installed {
				putSkill(t, root, name, "local-change")
				require.NoError(t, os.Chmod(filepath.Join(root, "skills", name, "SKILL.md"), 0o444))
			}
			out, diagnostics, err := executeCommand(t, newUpdateCommand(testSkillsFS(), testCatalog()), append([]string{"--dir", root, "-o", "json"}, tc.args...)...)
			if tc.err != "" {
				require.ErrorContains(t, err, tc.err)
			} else {
				require.NoError(t, err)
				var result installResult
				require.NoError(t, json.Unmarshal([]byte(out), &result))
				require.Equal(t, tc.updated, result.Skills)
				require.Len(t, result.Notices, tc.notices)
				if tc.notices > 0 {
					require.Contains(t, diagnostics, "replacement: alpha")
				}
			}
			for _, name := range tc.installed {
				file := filepath.Join(root, "skills", name, "SKILL.md")
				data, err := os.ReadFile(file)
				require.NoError(t, err)
				if tc.err != "" || tc.dryRun || name == "old" || name == "external" {
					require.Equal(t, "local-change", string(data))
				} else {
					expected, err := fs.ReadFile(testSkillsFS(), name+"/SKILL.md")
					require.NoError(t, err)
					require.Equal(t, expected, data)
					info, err := os.Stat(file)
					require.NoError(t, err)
					require.Equal(t, fs.FileMode(0o644), info.Mode().Perm())
				}
			}
			if len(tc.installed) == 0 {
				require.NoDirExists(t, filepath.Join(root, "skills"))
			}
			// A replacement is never installed just because beta/old names it.
			if tc.name == "mixed" || tc.name == "retirement" {
				require.NoDirExists(t, filepath.Join(root, "skills", "alpha"))
			}
		})
	}
}

func TestListCommand(t *testing.T) {
	t.Setenv("GCX_AGENT_MODE", "false")
	agent.ResetForTesting()
	for _, format := range []string{"json", "text"} {
		t.Run(format, func(t *testing.T) {
			root := t.TempDir()
			for _, name := range []string{"alpha", "old", "external"} {
				putSkill(t, root, name, "local")
			}
			out, _, err := executeCommand(t, newListCommand(testSkillsFS(), testCatalog()), "--dir", root, "-o", format)
			require.NoError(t, err)
			require.NotContains(t, out, "external")
			require.NotContains(t, out, "missing")
			require.Contains(t, out, "retired")
			require.Contains(t, out, "deprecated")
			if format == "json" {
				var result listResult
				require.NoError(t, json.Unmarshal([]byte(out), &result))
				require.Equal(t, 3, result.SkillCount)
				for _, skill := range result.Skills {
					require.True(t, skill.Known)
				}
				require.True(t, result.Skills[0].Installed)
				require.Equal(t, "alpha skill description", result.Skills[0].ShortDescription)
				require.False(t, result.Skills[1].Installed)
				require.Equal(t, skillops.Retired, result.Skills[2].Status)
				require.Equal(t, "alpha", result.Skills[2].Replacement)
			}
		})
	}
}

func TestGetCommand(t *testing.T) {
	t.Setenv("GCX_AGENT_MODE", "false")
	agent.ResetForTesting()
	for _, tc := range []struct {
		name string
		args []string
		want string
		err  string
	}{
		{name: "body", args: []string{"alpha"}, want: "alpha-skill"},
		{name: "reference", args: []string{"alpha", "references/guide.md"}, want: "alpha-guide"},
		{name: "json", args: []string{"alpha", "-o", "json"}, want: `"description": "alpha skill description"`},
		{name: "unknown", args: []string{"unknown"}, err: "unknown skill"},
		{name: "retired", args: []string{"old"}, err: "retired"},
		{name: "missing reference", args: []string{"alpha", "references/missing.md"}, err: "not found"},
		{name: "parent", args: []string{"alpha", "../beta/SKILL.md"}, err: "invalid reference path"},
		{name: "nested parent", args: []string{"alpha", "references/../../beta/SKILL.md"}, err: "invalid reference path"},
		{name: "absolute", args: []string{"alpha", "/etc/passwd"}, err: "invalid reference path"},
		{name: "bare parent", args: []string{"alpha", ".."}, err: "invalid reference path"},
		{name: "bad name", args: []string{"../alpha"}, err: "invalid skill name"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, _, err := executeCommand(t, newGetCommand(testSkillsFS(), testCatalog()), tc.args...)
			if tc.err != "" {
				require.ErrorContains(t, err, tc.err)
				return
			}
			require.NoError(t, err)
			require.Contains(t, out, tc.want)
			if tc.name == "json" {
				var result getResult
				require.NoError(t, json.Unmarshal([]byte(out), &result))
				require.Equal(t, []string{"references/guide.md"}, result.References)
				require.Equal(t, "SKILL.md", result.Path)
			}
		})
	}
}

func TestUninstallCommand(t *testing.T) {
	t.Setenv("GCX_AGENT_MODE", "false")
	t.Setenv("GCX_AUTO_APPROVE", "0")
	agent.ResetForTesting()
	for _, tc := range []struct {
		name    string
		args    []string
		removed []string
		missing []string
		dryRun  bool
		err     string
	}{
		{name: "current", args: []string{"alpha"}, removed: []string{"alpha"}, missing: []string{}},
		{name: "retired", args: []string{"old"}, removed: []string{"old"}, missing: []string{}},
		{name: "missing", args: []string{"missing"}, removed: []string{}, missing: []string{"missing"}},
		{name: "duplicates", args: []string{"old", "old"}, removed: []string{"old"}, missing: []string{}},
		{name: "dry run", args: []string{"old", "--dry-run"}, removed: []string{"old"}, missing: []string{}, dryRun: true},
		{name: "all", args: []string{"--all", "--yes"}, removed: []string{"alpha", "old"}, missing: []string{"beta"}},
		{name: "all dry run", args: []string{"--all", "--yes", "--dry-run"}, removed: []string{"alpha", "old"}, missing: []string{"beta"}, dryRun: true},
		{name: "requires approval", args: []string{"--all"}, err: "refusing to uninstall all gcx skills without --yes"},
		{name: "unmanaged", args: []string{"external"}, err: "unknown skill"},
		{name: "validate all first", args: []string{"alpha", "external"}, err: "unknown skill"},
		{name: "invalid name", args: []string{"../alpha"}, err: "invalid skill name"},
		{name: "no targets", err: "provide at least one skill name"},
		{name: "all and targets", args: []string{"--all", "--yes", "old"}, err: "skill names cannot be provided"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			for _, name := range []string{"alpha", "old", "external"} {
				putSkill(t, root, name, "local")
			}
			out, _, err := executeCommand(t, newUninstallCommand(testSkillsFS(), testCatalog()), append([]string{"--dir", root, "-o", "json"}, tc.args...)...)
			if tc.err != "" {
				require.ErrorContains(t, err, tc.err)
				for _, name := range []string{"alpha", "old"} {
					require.DirExists(t, filepath.Join(root, "skills", name))
				}
			} else {
				require.NoError(t, err)
				var result uninstallResult
				require.NoError(t, json.Unmarshal([]byte(out), &result))
				require.Equal(t, tc.removed, result.Removed)
				require.Equal(t, tc.missing, result.Missing)
				require.Equal(t, len(tc.removed), result.RemovedCount)
				for _, name := range tc.removed {
					if tc.dryRun {
						require.DirExists(t, filepath.Join(root, "skills", name))
					} else {
						require.NoDirExists(t, filepath.Join(root, "skills", name))
					}
				}
			}
			require.FileExists(t, filepath.Join(root, "skills", "external", "SKILL.md"))
		})
	}
}

func TestUninstallCommand_IgnoresBundleMismatch(t *testing.T) {
	t.Setenv("GCX_AGENT_MODE", "false")
	agent.ResetForTesting()
	for _, tc := range []struct {
		name    string
		source  fs.FS
		catalog string
	}{
		{
			name: "missing active content", source: fstest.MapFS{},
			catalog: "skills: {old: {status: active}}",
		},
		{
			name:    "uncataloged bundled content",
			source:  fstest.MapFS{"other/SKILL.md": {Data: []byte("other")}},
			catalog: "skills: {old: {status: retired}}",
		},
		{
			name:    "retired bundled content",
			source:  fstest.MapFS{"old/SKILL.md": {Data: []byte("old")}},
			catalog: "skills: {old: {status: retired}}",
		},
		{
			name: "unknown replacement", source: fstest.MapFS{},
			catalog: "skills: {old: {status: retired, replacement: missing}}",
		},
		{
			name: "cyclic replacement metadata", source: fstest.MapFS{},
			catalog: "skills: {old: {status: retired, replacement: other}, other: {status: retired, replacement: old}}",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, mode := range []string{"named", "all"} {
				t.Run(mode, func(t *testing.T) {
					root := t.TempDir()
					putSkill(t, root, "old", "installed content")
					putSkill(t, root, "external", "unmanaged content")
					args := []string{"--dir", root, "-o", "json"}
					if mode == "all" {
						args = append(args, "--all", "--yes")
					} else {
						args = append(args, "old")
					}
					out, _, err := executeCommand(t, newUninstallCommand(tc.source, []byte(tc.catalog)), args...)
					require.NoError(t, err)
					var result uninstallResult
					require.NoError(t, json.Unmarshal([]byte(out), &result))
					require.Equal(t, []string{"old"}, result.Removed)
					require.NoDirExists(t, filepath.Join(root, "skills", "old"))
					require.FileExists(t, filepath.Join(root, "skills", "external", "SKILL.md"))
				})
			}
		})
	}
}

func TestSkillCompletion(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name        string
		constructor func(fs.FS, []byte) *cobra.Command
		retired     bool
	}{
		{name: "install", constructor: newInstallCommand},
		{name: "get", constructor: newGetCommand},
		{name: "update", constructor: newUpdateCommand, retired: true},
		{name: "uninstall", constructor: newUninstallCommand, retired: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cmd := tc.constructor(testSkillsFS(), testCatalog())
			names, directive := cmd.ValidArgsFunction(cmd, nil, "")
			require.Equal(t, cobra.ShellCompDirectiveNoFileComp, directive)
			require.Contains(t, names, "alpha")
			require.Contains(t, names, "beta")
			if tc.retired {
				require.Contains(t, names, "old")
			} else {
				require.NotContains(t, names, "old")
			}
		})
	}
}
