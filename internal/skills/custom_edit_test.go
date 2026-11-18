package skills //nolint:testpackage // Exercises snapshot recovery and cross-process lock ownership.

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestCustomExportEditAndConflict(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	source := customFixture(t, "debug")
	require.NoError(t, os.Mkdir(filepath.Join(source, "assets"), 0o700))
	binary := []byte{0, 1, 255, 42}
	require.NoError(t, os.WriteFile(filepath.Join(source, "assets", "sample.bin"), binary, 0o600))
	installed, err := InstallCustom(root, source, "", false)
	require.NoError(t, err)
	work := filepath.Join(t.TempDir(), "edit")
	preview, err := ExportCustom(root, installed.ID, work, true)
	require.NoError(t, err)
	require.Equal(t, installed.Revision, preview.Revision)
	require.NoDirExists(t, work)
	_, err = ExportCustom(root, installed.ID, work, false)
	require.NoError(t, err)
	data, err := os.ReadFile(filepath.Join(work, "assets", "sample.bin"))
	require.NoError(t, err)
	require.Equal(t, binary, data)
	_, err = ExportCustom(root, installed.ID, work, false)
	require.ErrorContains(t, err, "already exists")
	require.NoError(t, os.WriteFile(filepath.Join(work, "references", "queries.md"), []byte("edited query\n"), 0o600))
	changes, err := InstallCustom(root, work, installed.ID, true)
	require.NoError(t, err)
	require.Len(t, changes.Changes, 1, "export retains assets and all unedited references")
	require.Equal(t, "references/queries.md", changes.Changes[0].Path)
	require.Equal(t, "modified", changes.Changes[0].Action)
	require.Contains(t, changes.Changes[0].Diff, "+edited query")
	saved, err := GetCustom(root, installed.ID, "references/queries.md")
	require.NoError(t, err)
	require.Equal(t, "query-reference-original", saved.Body)
	other := filepath.Join(t.TempDir(), "other")
	_, err = ExportCustom(root, installed.ID, other, false)
	require.NoError(t, err)
	_, err = InstallCustom(root, work, installed.ID, false)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(other, "references", "queries.md"), []byte("stale edit"), 0o600))
	for _, dryRun := range []bool{true, false} {
		_, err = InstallCustom(root, other, installed.ID, dryRun)
		require.ErrorContains(t, err, "changed since export")
	}
	saved, err = GetCustom(root, installed.ID, "references/queries.md")
	require.NoError(t, err)
	require.Equal(t, "edited query\n", saved.Body)
	require.NoFileExists(t, filepath.Join(installed.Path, customEditFile))
}

func TestCustomPreviewFileChanges(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	source := customFixture(t, "debug")
	_, err := InstallCustom(root, source, "", false)
	require.NoError(t, err)
	require.NoError(t, os.Remove(filepath.Join(source, "references", "queries.md")))
	require.NoError(t, os.WriteFile(filepath.Join(source, "large.bin"), make([]byte, 33<<10), 0o600))
	result, err := InstallCustom(root, source, "custom/debug", true)
	require.NoError(t, err)
	require.Len(t, result.Changes, 2)
	require.Equal(t, "added", result.Changes[0].Action)
	require.True(t, result.Changes[0].DiffOmitted)
	require.NotEmpty(t, result.Changes[0].AfterHash)
	require.Equal(t, "removed", result.Changes[1].Action)
	require.Contains(t, result.Changes[1].Diff, "-query-reference-original")
}

func TestCustomInterruptedPublication(t *testing.T) {
	t.Parallel()
	for _, published := range []bool{false, true} {
		t.Run(map[bool]string{false: "rollback", true: "finish"}[published], func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			source := customFixture(t, "debug")
			_, err := InstallCustom(root, source, "", false)
			require.NoError(t, err)
			previous := filepath.Join(root, customDirectory, ".previous-debug")
			require.NoError(t, os.Rename(filepath.Join(root, customDirectory, "debug"), previous))
			if published {
				// A process died after publishing the new directory but before
				// deleting its backup. Use a separate real snapshot as the new one.
				otherRoot := t.TempDir()
				newSnapshot, err := InstallCustom(otherRoot, source, "", false)
				require.NoError(t, err)
				require.NoError(t, os.Rename(newSnapshot.Path, filepath.Join(root, customDirectory, "debug")))
			}
			_, err = InstallCustom(root, source, "custom/debug", true)
			require.ErrorContains(t, err, "interrupted update")
			require.DirExists(t, previous, "dry run must not recover by writing")
			_, err = listCustom(root)
			require.ErrorContains(t, err, "in progress or interrupted", "discovery must not silently omit the interrupted skill")
			_, err = InstallCustom(root, source, "custom/debug", false)
			require.NoError(t, err)
			_, err = GetCustom(root, "custom/debug", "references/queries.md")
			require.NoError(t, err)
			require.NoDirExists(t, previous)
		})
	}
}

func TestCustomExportBoundaries(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	source := customFixture(t, "debug")
	_, err := InstallCustom(root, source, "", false)
	require.NoError(t, err)
	_, err = ExportCustom(root, "custom/../outside", filepath.Join(t.TempDir(), "new"), false)
	require.ErrorContains(t, err, "invalid custom skill identity")
	_, err = ExportCustom(root, "custom/debug", filepath.Join(root, customDirectory, "debug", "edit"), false)
	require.ErrorContains(t, err, "outside the managed")
	work := filepath.Join(t.TempDir(), "edit")
	_, err = ExportCustom(root, "custom/debug", work, false)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(work, customEditFile), []byte(`{"id":"custom/other","revision":"wrong"}`), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(work, "references", "queries.md"), []byte("edit"), 0o600))
	_, err = InstallCustom(root, work, "custom/debug", false)
	require.ErrorContains(t, err, "another identity")
}

func TestCustomLockProcess(t *testing.T) {
	if root := os.Getenv("GCX_TEST_LOCK_ROOT"); root != "" {
		store, err := customStore(root, false)
		require.NoError(t, err)
		defer store.Close()
		unlock, err := acquireCustomLock(store, "debug")
		require.NoError(t, err)
		defer unlock()
		require.NoError(t, store.WriteFile("ready", nil, 0o600))
		time.Sleep(time.Minute) // Parent kills this process while it owns the lock.
		return
	}
	t.Parallel()
	root := t.TempDir()
	source := customFixture(t, "debug")
	_, err := InstallCustom(root, source, "", false)
	require.NoError(t, err)
	process := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestCustomLockProcess$") //nolint:gosec // Runs only this test binary.
	process.Env = append(os.Environ(), "GCX_TEST_LOCK_ROOT="+root)
	require.NoError(t, process.Start())
	t.Cleanup(func() { _ = process.Process.Kill() })
	require.Eventually(t, func() bool {
		_, err := os.Stat(filepath.Join(root, customDirectory, "ready"))
		return err == nil
	}, 10*time.Second, 20*time.Millisecond)
	_, err = InstallCustom(root, source, "custom/debug", false)
	require.ErrorContains(t, err, "already being modified")
	require.NoError(t, process.Process.Kill())
	require.Error(t, process.Wait())
	_, err = InstallCustom(root, source, "custom/debug", false)
	require.NoError(t, err, "a killed writer must not leave a stale lock")
}
