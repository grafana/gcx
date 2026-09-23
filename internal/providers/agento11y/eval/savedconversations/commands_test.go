package savedconversations_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/providers/agento11y/eval/savedconversations"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSaveCommand_RequiresName(t *testing.T) {
	cmd := savedconversations.Commands(nil)
	cmd.SetArgs([]string{"save", "conv-1"})

	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--name is required")
}

func TestSaveCommand_ParseTags_Invalid(t *testing.T) {
	cmd := savedconversations.Commands(nil)
	cmd.SetArgs([]string{"save", "conv-1", "--name", "x", "--tag", "no-equals"})

	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid --tag")
}

func TestDeleteCommand_AbortsWithoutForce(t *testing.T) {
	// Without --force, with stdin piped (not a terminal) it should error out.
	cmd := savedconversations.Commands(nil)
	cmd.SetArgs([]string{"delete", "saved-1"})

	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetIn(strings.NewReader("n\n"))

	err := cmd.Execute()
	// Either a piped-stdin error OR aborted: both prove the prompt was consulted.
	if err != nil {
		// Piped-stdin path.
		assert.Contains(t, err.Error(), "use --force")
	} else {
		assert.Contains(t, stderr.String(), "Aborted")
	}
}

func TestListCommand_RegistersSourceFlag(t *testing.T) {
	cmd := savedconversations.Commands(nil)
	listCmd, _, err := cmd.Find([]string{"list"})
	require.NoError(t, err)
	require.NotNil(t, listCmd.Flag("source"))
	require.NotNil(t, listCmd.Flag("limit"))
}

func TestSaveCommand_DefaultsSavedID(t *testing.T) {
	// We can't run the full save (no server), but we can confirm flags exist
	// and that the default saved-id value is empty (derived at runtime).
	cmd := savedconversations.Commands(nil)
	saveCmd, _, err := cmd.Find([]string{"save"})
	require.NoError(t, err)

	savedIDFlag := saveCmd.Flag("saved-id")
	require.NotNil(t, savedIDFlag)
	assert.Empty(t, savedIDFlag.DefValue, "--saved-id default must be empty; runtime derives saved-<conversation-id>")
}
