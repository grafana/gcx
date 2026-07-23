package agent_test

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/grafana/gcx/cmd/gcx/agent"
	iagent "github.com/grafana/gcx/internal/agent"
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

// runPrune executes `gcx agent prune` with the given extra args against a
// temp dir holding nOldFiles expired spill files, returning stdout and the
// execution error. agentMode must be set before the command is constructed —
// the agent-mode default format is resolved when flags are bound.
func runPrune(t *testing.T, agentMode bool, nOldFiles int, args ...string) (string, error) {
	t.Helper()

	dir := t.TempDir()
	t.Setenv("TMPDIR", dir)

	for i := range nOldFiles {
		f := filepath.Join(dir, "gcx-results-contract-"+string(rune('a'+i))+".json")
		require.NoError(t, os.WriteFile(f, []byte(`{"test":true}`), 0o600))
		oldTime := time.Now().Add(-31 * time.Minute)
		require.NoError(t, os.Chtimes(f, oldTime, oldTime))
	}

	iagent.SetFlag(agentMode)
	t.Cleanup(func() { iagent.SetFlag(false) })

	cmd := agent.Command()
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs(append([]string{"prune"}, args...))
	err := cmd.Execute()
	return stdout.String(), err
}

// decodeSingleJSONValue asserts that raw holds exactly one JSON value
// followed by EOF, and returns it decoded.
func decodeSingleJSONValue(t *testing.T, raw string) any {
	t.Helper()
	dec := json.NewDecoder(strings.NewReader(raw))
	var first any
	require.NoError(t, dec.Decode(&first), "stdout is not valid JSON:\n%s", raw)
	var second any
	err := dec.Decode(&second)
	require.ErrorIs(t, err, io.EOF, "stdout must contain exactly one JSON value\n%s", raw)
	return first
}

// TestPruneCommand_OutputContract pins the agent output contract for
// `gcx agent prune`: the human default stdout stays byte-identical to the
// pre-codec prose, agent mode emits exactly one JSON BatchMutation document,
// and an explicit -o always wins.
func TestPruneCommand_OutputContract(t *testing.T) {
	tests := []struct {
		name       string
		agentMode  bool
		nOldFiles  int
		args       []string
		wantStdout string // exact stdout; empty = use check
		check      func(t *testing.T, stdout string)
	}{
		{
			name:       "human default no files is byte-identical prose",
			nOldFiles:  0,
			wantStdout: "no spill files found older than 30 minutes\n",
		},
		{
			name:       "human default with removals is byte-identical prose",
			nOldFiles:  2,
			wantStdout: "removed 2 spill file(s)\n",
		},
		{
			name:      "agent mode emits exactly one JSON BatchMutation",
			agentMode: true,
			nOldFiles: 1,
			check: func(t *testing.T, stdout string) {
				t.Helper()
				doc, ok := decodeSingleJSONValue(t, stdout).(map[string]any)
				require.True(t, ok, "document must be a JSON object")
				assert.Equal(t, "gcx.mutation_batch", doc["type"])
				assert.Equal(t, "1", doc["schema_version"])
				assert.Equal(t, "pruned", doc["action"])
				summary, ok := doc["summary"].(map[string]any)
				require.True(t, ok, "summary must be an object")
				assert.EqualValues(t, 1, summary["succeeded"])
				failures, ok := doc["failures"].([]any)
				require.True(t, ok, "failures must serialize as []")
				assert.Empty(t, failures)
			},
		},
		{
			name:      "agent mode with nothing to remove still emits one JSON doc",
			agentMode: true,
			nOldFiles: 0,
			check: func(t *testing.T, stdout string) {
				t.Helper()
				doc, ok := decodeSingleJSONValue(t, stdout).(map[string]any)
				require.True(t, ok, "document must be a JSON object")
				assert.Equal(t, "gcx.mutation_batch", doc["type"])
				summary, ok := doc["summary"].(map[string]any)
				require.True(t, ok, "summary must be an object")
				assert.EqualValues(t, 0, summary["succeeded"])
			},
		},
		{
			name:      "explicit -o json wins in human mode",
			nOldFiles: 1,
			args:      []string{"-o", "json"},
			check: func(t *testing.T, stdout string) {
				t.Helper()
				doc, ok := decodeSingleJSONValue(t, stdout).(map[string]any)
				require.True(t, ok, "document must be a JSON object")
				assert.Equal(t, "gcx.mutation_batch", doc["type"])
			},
		},
		{
			name:      "explicit -o yaml wins in agent mode",
			agentMode: true,
			nOldFiles: 1,
			args:      []string{"-o", "yaml"},
			check: func(t *testing.T, stdout string) {
				t.Helper()
				assert.Contains(t, stdout, "action: pruned")
				assert.Contains(t, stdout, "type: gcx.mutation_batch")
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			stdout, err := runPrune(t, tc.agentMode, tc.nOldFiles, tc.args...)
			require.NoError(t, err)
			if tc.wantStdout != "" {
				assert.Equal(t, tc.wantStdout, stdout)
				return
			}
			tc.check(t, stdout)
		})
	}
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
