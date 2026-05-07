package agent_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/grafana/gcx/cmd/gcx/agent"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPrune_DeletesOldSpillFiles(t *testing.T) {
	dir := t.TempDir()

	old := filepath.Join(dir, "gcx-results-old.json")
	require.NoError(t, os.WriteFile(old, []byte(`{"test":true}`), 0o600))
	oldTime := time.Now().Add(-31 * time.Minute)
	require.NoError(t, os.Chtimes(old, oldTime, oldTime))

	deleted, err := agent.PruneSpillFiles(dir, 30*time.Minute)
	require.NoError(t, err)
	assert.Equal(t, 1, deleted)
	_, statErr := os.Stat(old)
	assert.True(t, os.IsNotExist(statErr), "old spill file must be deleted")
}

func TestPrune_KeepsRecentSpillFiles(t *testing.T) {
	dir := t.TempDir()

	recent := filepath.Join(dir, "gcx-results-recent.json")
	require.NoError(t, os.WriteFile(recent, []byte(`{"test":true}`), 0o600))

	deleted, err := agent.PruneSpillFiles(dir, 30*time.Minute)
	require.NoError(t, err)
	assert.Equal(t, 0, deleted)
	_, statErr := os.Stat(recent)
	require.NoError(t, statErr, "recent spill file must not be deleted")
}

func TestPrune_IgnoresNonSpillFiles(t *testing.T) {
	dir := t.TempDir()

	other := filepath.Join(dir, "not-a-spill.json")
	require.NoError(t, os.WriteFile(other, []byte(`{}`), 0o600))
	oldTime := time.Now().Add(-60 * time.Minute)
	require.NoError(t, os.Chtimes(other, oldTime, oldTime))

	deleted, err := agent.PruneSpillFiles(dir, 30*time.Minute)
	require.NoError(t, err)
	assert.Equal(t, 0, deleted, "non-spill files must not be deleted")
	_, statErr := os.Stat(other)
	require.NoError(t, statErr, "non-spill file must still exist")
}

func TestPrune_NoFiles_ReturnsZero(t *testing.T) {
	dir := t.TempDir()
	deleted, err := agent.PruneSpillFiles(dir, 30*time.Minute)
	require.NoError(t, err)
	assert.Equal(t, 0, deleted)
}

func TestPruneCommand_DeletesOldFiles_ReportsCount(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("TMPDIR", dir)

	old := filepath.Join(dir, "gcx-results-cmd.json")
	require.NoError(t, os.WriteFile(old, []byte(`{"test":true}`), 0o600))
	oldTime := time.Now().Add(-31 * time.Minute)
	require.NoError(t, os.Chtimes(old, oldTime, oldTime))

	cmd := agent.Command()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetArgs([]string{"prune"})
	require.NoError(t, cmd.Execute())
	assert.Contains(t, buf.String(), "1")
}

func TestPruneCommand_Structure(t *testing.T) {
	cmd := agent.Command()

	var pruneCmd *cobra.Command
	for _, sub := range cmd.Commands() {
		if sub.Name() == "prune" {
			pruneCmd = sub
			break
		}
	}
	require.NotNil(t, pruneCmd, "agent command must have a prune subcommand")
	assert.NotEmpty(t, pruneCmd.Short)
}
