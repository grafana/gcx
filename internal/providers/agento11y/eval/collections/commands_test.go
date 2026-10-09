package collections_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/providers/agento11y/eval/collections"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCommands_HasMembershipCompounds(t *testing.T) {
	cmd := collections.Commands(nil)

	for _, sub := range []string{"list-conversations", "add-conversations", "remove-conversation"} {
		c, _, err := cmd.Find([]string{sub})
		require.NoError(t, err, "subcommand %q must exist", sub)
		require.NotNil(t, c)
		require.Equal(t, sub, c.Name())
	}

	// The nested `conversations` noun group dissolved into the compounds
	// above; it must not resolve anymore.
	for _, c := range cmd.Commands() {
		require.NotEqual(t, "conversations", c.Name())
	}
}

func TestCreateCommand_RejectsConflictingFlags(t *testing.T) {
	cmd := collections.Commands(nil)
	cmd.SetArgs([]string{"create", "-f", "x.yaml", "--name", "x"})

	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "mutually exclusive")
}

func TestCreateCommand_RequiresInput(t *testing.T) {
	cmd := collections.Commands(nil)
	cmd.SetArgs([]string{"create"})

	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--filename/-f or --name is required")
}

func TestUpdateCommand_RequiresAtLeastOneFlag(t *testing.T) {
	cmd := collections.Commands(nil)
	cmd.SetArgs([]string{"update", "c-1"})

	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "at least one of --name or --description")
}

func TestAddConversationsCommand_NeedsTwoArgs(t *testing.T) {
	cmd := collections.Commands(nil)
	cmd.SetArgs([]string{"add-conversations", "c-1"})

	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "requires at least 2 arg")
}

func TestDeleteCommand_AbortsWithoutForce(t *testing.T) {
	cmd := collections.Commands(nil)
	cmd.SetArgs([]string{"delete", "c-1"})

	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetIn(strings.NewReader("n\n"))

	err := cmd.Execute()
	if err != nil {
		assert.Contains(t, err.Error(), "use --force")
	} else {
		assert.Contains(t, stderr.String(), "Aborted")
	}
}
