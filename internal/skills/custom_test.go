package skills //nolint:testpackage // Tests exercise filesystem ownership and private command constructors.

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/require"
)

func customFixture(t *testing.T, name string) string {
	t.Helper()
	directory := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(directory, "references"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(directory, "SKILL.md"), []byte("---\nname: "+name+"\ndescription: Investigate service latency\nmetadata:\n  tags: logs, latency\n---\n# Workflow\nRead the reference, query and verify.\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(directory, "references", "queries.md"), []byte("query-reference-original"), 0o600))
	return directory
}

func TestCustomLifecycle(t *testing.T) {
	t.Parallel()
	for _, dryRun := range []bool{true, false} {
		t.Run(map[bool]string{true: "dry run", false: "apply"}[dryRun], func(t *testing.T) {
			t.Parallel()
			root := filepath.Join(t.TempDir(), "new-root")
			directory := customFixture(t, "service-debug")
			result, err := InstallCustom(root, directory, "", dryRun)
			require.NoError(t, err)
			require.Equal(t, "custom/service-debug", result.ID)
			if dryRun {
				require.NoDirExists(t, root)
				return
			}
			_, err = InstallCustom(root, directory, "", false)
			require.ErrorContains(t, err, "already exists")
			require.NoError(t, os.WriteFile(filepath.Join(directory, "references", "queries.md"), []byte("new-query-reference"), 0o600))
			read, err := GetCustom(root, result.ID, "references/queries.md")
			require.NoError(t, err)
			require.Equal(t, "query-reference-original", read.Body, "imports are copies, not links to the authored source")
			for _, preview := range []bool{true, false} {
				_, err = InstallCustom(root, directory, result.ID, preview)
				require.NoError(t, err)
				read, err = GetCustom(root, result.ID, "references/queries.md")
				require.NoError(t, err)
				if preview {
					require.Equal(t, "query-reference-original", read.Body)
				} else {
					require.Equal(t, "new-query-reference", read.Body)
				}
			}
			unchanged, err := InstallCustom(root, directory, result.ID, false)
			require.NoError(t, err)
			require.False(t, unchanged.Changed)
			require.NoError(t, os.Remove(filepath.Join(directory, "references", "queries.md")))
			_, err = InstallCustom(root, directory, result.ID, false)
			require.NoError(t, err)
			_, err = GetCustom(root, result.ID, "references/queries.md")
			require.ErrorContains(t, err, "not found", "replacement removes references omitted from the new snapshot")
			for _, reference := range []string{"../SKILL.md", "references/../../outside", "/etc/passwd", `references\queries.md`} {
				_, err = GetCustom(root, result.ID, reference)
				require.Error(t, err)
			}
			_, err = UninstallCustom(root, result.ID, true)
			require.NoError(t, err)
			require.DirExists(t, result.Path)
			_, err = UninstallCustom(root, result.ID, false)
			require.NoError(t, err)
			require.NoDirExists(t, result.Path)
			require.FileExists(t, filepath.Join(directory, "SKILL.md"))
		})
	}
}

func TestCustomImportValidation(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		body string
	}{
		{"no metadata", "# Just a document"},
		{"empty description", "---\nname: okay\ndescription: ''\n---\nInstructions"},
		{"unsafe name", "---\nname: ../outside\ndescription: test\n---\nInstructions"},
		{"empty body", "---\nname: okay\ndescription: test\n---\n"},
		{"duplicate field", "---\nname: okay\nname: other\ndescription: test\n---\nInstructions"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			directory := customFixture(t, "okay")
			require.NoError(t, os.WriteFile(filepath.Join(directory, "SKILL.md"), []byte(tc.body), 0o600))
			root := filepath.Join(t.TempDir(), "not-created")
			_, err := InstallCustom(root, directory, "", false)
			require.Error(t, err)
			require.NoDirExists(t, root)
		})
	}
}

func TestCustomDirectoryBoundaries(t *testing.T) {
	t.Parallel()
	for _, boundary := range []string{"source symlink", "reference symlink", "store symlink", "target symlink", "unmanaged target", "oversized file", "name mismatch"} {
		t.Run(boundary, func(t *testing.T) {
			t.Parallel()
			if runtime.GOOS == "windows" && strings.Contains(boundary, "symlink") {
				t.Skip("symlink creation requires Windows developer privileges")
			}
			root := t.TempDir()
			directory := customFixture(t, "debug")
			outside := customFixture(t, "outside")
			var err error
			switch boundary {
			case "source symlink":
				link := filepath.Join(root, "linked")
				require.NoError(t, os.Symlink(directory, link))
				_, err = InstallCustom(root, link, "", false)
			case "reference symlink":
				require.NoError(t, os.Symlink(filepath.Join(outside, "SKILL.md"), filepath.Join(directory, "references", "outside.md")))
				_, err = InstallCustom(root, directory, "", false)
			case "store symlink":
				require.NoError(t, os.Symlink(outside, filepath.Join(root, customDirectory)))
				_, err = InstallCustom(root, directory, "", false)
			case "target symlink":
				require.NoError(t, os.Mkdir(filepath.Join(root, customDirectory), 0o755))
				require.NoError(t, os.Symlink(outside, filepath.Join(root, customDirectory, "debug")))
				_, err = UninstallCustom(root, "custom/debug", false)
			case "unmanaged target":
				require.NoError(t, os.MkdirAll(filepath.Join(root, customDirectory, "debug"), 0o755))
				_, err = InstallCustom(root, directory, "custom/debug", false)
			case "oversized file":
				require.NoError(t, os.WriteFile(filepath.Join(directory, "big.txt"), make([]byte, (2<<20)+1), 0o600))
				_, err = InstallCustom(root, directory, "", false)
			case "name mismatch":
				_, err = InstallCustom(root, directory, "custom/other", false)
			}
			require.Error(t, err)
			require.FileExists(t, filepath.Join(outside, "SKILL.md"))
		})
	}
}

func TestSearchCustomAndBundledIsolation(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	source := fstest.MapFS{"debug/SKILL.md": {Data: []byte("---\ndescription: service latency\n---\nOfficial instructions")}}
	catalog := []byte("skills: {debug: {status: active}}")
	for _, name := range []string{"debug", "other"} {
		_, err := InstallCustom(root, customFixture(t, name), "", false)
		require.NoError(t, err)
	}
	for _, tc := range []struct {
		name      string
		selection string
		tags      []string
		limit     int
		total     int
		returned  int
	}{
		{"all limited", "all", nil, 1, 3, 1},
		{"all unlimited", "all", nil, 0, 3, 3},
		{"custom tags", "custom", []string{"LOGS", "latency"}, 0, 2, 2},
		{"all tags exclude untagged bundle", "all", []string{"logs"}, 0, 2, 2},
		{"bundled", "bundled", nil, 5, 1, 1},
		{"no matching tags", "custom", []string{"unmatched"}, 5, 0, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			result, err := Search(source, catalog, root, tc.selection, "SERVICE LATENCY", tc.tags, tc.limit)
			require.NoError(t, err)
			require.Equal(t, tc.total, result.Total)
			require.Equal(t, tc.returned, result.Returned)
			require.Equal(t, tc.total > tc.returned, result.Truncated)
			require.NotNil(t, result.Skills)
		})
	}
	_, err := Install(source, catalog, root, nil, false, false)
	require.NoError(t, err)
	_, err = Update(source, catalog, root, nil, false)
	require.NoError(t, err)
	_, err = Uninstall(source, catalog, root, nil, true, false)
	require.NoError(t, err)
	custom, err := GetCustom(root, "custom/debug", "")
	require.NoError(t, err)
	require.Contains(t, custom.Body, "Read the reference")
	_, err = Get(source, catalog, "debug", "")
	require.NoError(t, err)
	legacy, err := List(source, catalog, root)
	require.NoError(t, err)
	require.Len(t, legacy.Skills, 1)
	require.Empty(t, legacy.Skills[0].Source)
}

func TestCustomMutationLocks(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	directory := customFixture(t, "debug")
	_, err := InstallCustom(root, directory, "", false)
	require.NoError(t, err)
	store, err := customStore(root, false)
	require.NoError(t, err)
	defer store.Close()
	unlock, err := acquireCustomLock(store, "debug")
	require.NoError(t, err)
	defer unlock()
	_, err = InstallCustom(root, directory, "custom/debug", false)
	require.ErrorContains(t, err, "already being modified")
	_, err = UninstallCustom(root, "custom/debug", false)
	require.ErrorContains(t, err, "already being modified")
	_, err = GetCustom(root, "custom/debug", "")
	require.NoError(t, err)
}
