package org_test

import (
	"testing"

	"github.com/grafana/gcx/internal/providers/org"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// findSubcommand walks the command tree to find a subcommand by path segments.
func findSubcommand(t *testing.T, root *cobra.Command, path ...string) *cobra.Command {
	t.Helper()
	cur := root
	for _, name := range path {
		var next *cobra.Command
		for _, sub := range cur.Commands() {
			// Use first word of Use as match key.
			useName := sub.Name()
			if useName == name {
				next = sub
				break
			}
		}
		require.NotNilf(t, next, "expected subcommand %q under %q", name, cur.CommandPath())
		cur = next
	}
	return cur
}

func TestOrgProvider_CommandShape(t *testing.T) {
	p := &org.OrgProvider{}
	assert.Equal(t, "org", p.Name())
	assert.NotEmpty(t, p.ShortDesc())
	require.NoError(t, p.Validate(nil))
	assert.Nil(t, p.ConfigKeys())

	cmds := p.Commands()
	require.Len(t, cmds, 1)

	orgCmd := cmds[0]
	assert.Equal(t, "org", orgCmd.Name())

	users := findSubcommand(t, orgCmd, "users")
	subs := map[string]*cobra.Command{}
	for _, s := range users.Commands() {
		subs[s.Name()] = s
	}
	assert.Contains(t, subs, "list")
	assert.Contains(t, subs, "get")
	assert.Contains(t, subs, "add")
	assert.Contains(t, subs, "update-role")
	assert.Contains(t, subs, "remove")
}

// silence silences errors/usage on the entire command tree so failing
// Execute calls don't spam test output.
func silence(cmd *cobra.Command) {
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	for _, sub := range cmd.Commands() {
		silence(sub)
	}
}

func TestOrgUsersAdd_RequiresLogin(t *testing.T) {
	p := &org.OrgProvider{}
	orgCmd := p.Commands()[0]
	silence(orgCmd)

	// Invoke through the root so cobra routes to the add subcommand correctly.
	orgCmd.SetArgs([]string{"users", "add", "--role", "Editor"})

	err := orgCmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "login")
}

func TestOrgUsersUpdateRole_ParsesIntArg(t *testing.T) {
	p := &org.OrgProvider{}
	orgCmd := p.Commands()[0]
	silence(orgCmd)

	// Non-integer user ID must error during arg parsing, before any HTTP call.
	orgCmd.SetArgs([]string{"users", "update-role", "not-an-int", "--role", "Admin"})

	err := orgCmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not-an-int")
}
