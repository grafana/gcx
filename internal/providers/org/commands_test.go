package org_test

import (
	"testing"

	"github.com/grafana/gcx/internal/providers/org"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func findSubcommand(t *testing.T, root *cobra.Command, path ...string) *cobra.Command {
	t.Helper()
	cur := root
	for _, name := range path {
		var next *cobra.Command
		for _, sub := range cur.Commands() {
			if sub.Name() == name {
				next = sub
				break
			}
		}
		require.NotNilf(t, next, "expected subcommand %q under %q", name, cur.CommandPath())
		cur = next
	}
	return cur
}

// silence suppresses errors/usage on the entire command tree so failing
// Execute calls don't spam test output.
func silence(cmd *cobra.Command) {
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	for _, sub := range cmd.Commands() {
		silence(sub)
	}
}

// orgRoot returns the provider's root "org" command with output silenced.
func orgRoot(t *testing.T) *cobra.Command {
	t.Helper()
	cmds := (&org.OrgProvider{}).Commands()
	require.Len(t, cmds, 1)
	silence(cmds[0])
	return cmds[0]
}

func TestOrgProvider_CommandShape(t *testing.T) {
	p := &org.OrgProvider{}
	assert.Equal(t, "org", p.Name())
	assert.NotEmpty(t, p.ShortDesc())
	require.NoError(t, p.Validate(nil))
	assert.Nil(t, p.ConfigKeys())

	orgCmd := orgRoot(t)
	assert.Equal(t, "org", orgCmd.Name())

	users := findSubcommand(t, orgCmd, "users")
	subs := map[string]*cobra.Command{}
	for _, s := range users.Commands() {
		subs[s.Name()] = s
	}
	for _, name := range []string{"list", "get", "add", "update-role", "remove"} {
		assert.Contains(t, subs, name)
	}
}

func TestOrgUsersAdd_RequiresLogin(t *testing.T) {
	orgCmd := orgRoot(t)
	orgCmd.SetArgs([]string{"users", "add", "--role", "Editor"})

	err := orgCmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "login")
}

func TestOrgUsersUpdateRole_ParsesIntArg(t *testing.T) {
	orgCmd := orgRoot(t)
	// Non-integer user ID must error during arg parsing, before any HTTP call.
	orgCmd.SetArgs([]string{"users", "update-role", "not-an-int", "--role", "Admin"})

	err := orgCmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not-an-int")
}
