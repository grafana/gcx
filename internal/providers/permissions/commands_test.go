package permissions_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/providers/permissions"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProvider_CommandShape(t *testing.T) {
	p := &permissions.PermissionsProvider{}
	cmds := p.Commands()
	require.Len(t, cmds, 1)
	root := cmds[0]
	assert.Equal(t, "permissions", root.Use)

	subNames := make([]string, 0, len(root.Commands()))
	for _, c := range root.Commands() {
		subNames = append(subNames, strings.Fields(c.Use)[0])
	}
	assert.ElementsMatch(t, []string{"get", "set", "grant", "levels"}, subNames)

	// Command-only provider: no adapter registrations.
	assert.Empty(t, p.TypedRegistrations())
}

func TestGetCommand_RejectsInvalidResource(t *testing.T) {
	p := &permissions.PermissionsProvider{}
	root := p.Commands()[0]
	root.SetArgs([]string{"get", "widgets", "some-id"})
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	err := root.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid resource")
}

func TestGrantCommand_RequiresExactlyOnePrincipal(t *testing.T) {
	p := &permissions.PermissionsProvider{}
	root := p.Commands()[0]
	root.SetArgs([]string{"grant", "dashboards", "d1", "--user", "1", "--team", "2", "--level", "Edit"})
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	err := root.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "exactly one")
}

func TestPermissionsTableCodec(t *testing.T) {
	codec := &permissions.PermissionsTableCodec{}
	var buf bytes.Buffer
	err := codec.Encode(&buf, []permissions.ResourcePermission{
		{BuiltInRole: "Editor", Permission: "Edit", IsManaged: true},
		{UserLogin: "alice", Permission: "Admin"},
	})
	require.NoError(t, err)
	out := buf.String()
	assert.Contains(t, out, "PERMISSION")
	assert.Contains(t, out, "BUILT-IN ROLE")
	assert.Contains(t, out, "Editor")
	assert.Contains(t, out, "alice")
}

func TestDescriptionTableCodec(t *testing.T) {
	codec := &permissions.DescriptionTableCodec{}
	var buf bytes.Buffer
	err := codec.Encode(&buf, &permissions.Description{
		Assignments: permissions.Assignments{Users: true, Teams: true},
		Permissions: []string{"View", "Edit", "Admin"},
	})
	require.NoError(t, err)
	out := buf.String()
	assert.Contains(t, out, "ASSIGNABLE LEVEL")
	assert.Contains(t, out, "View")
	assert.Contains(t, out, "users")
}
