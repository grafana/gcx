package permissions_test

import (
	"bytes"
	"os"
	"path/filepath"
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

func TestSetCommand_DeclineConfirmationSkipsOverwrite(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "acl.json")
	require.NoError(t, os.WriteFile(file, []byte(`[{"permission":"Edit","userId":1}]`), 0o600))

	p := &permissions.PermissionsProvider{}
	root := p.Commands()[0]
	root.SetArgs([]string{"set", "dashboards", "d1", "-f", file})
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetIn(strings.NewReader("n\n"))

	err := root.Execute()
	require.NoError(t, err)
	assert.Contains(t, out.String(), "[y/N]")
	assert.NotContains(t, out.String(), "updated permissions")
}

// TestSetCommand_RejectsEmptyOrUnrecognizedPermissionsPayload covers the
// data-loss bug where parsing silently produced an empty permission list for
// JSON shapes that don't match the documented bare-array or
// {"permissions":[...]} envelope. Client.Set would then POST
// {"permissions": null}, wiping every managed permission on the resource.
// --force is passed so parsing/validation failures surface before any
// confirmation prompt or network call.
func TestSetCommand_RejectsEmptyOrUnrecognizedPermissionsPayload(t *testing.T) {
	tests := []struct {
		name          string
		payload       string
		wantErrSubstr string
	}{
		{"empty array", `[]`, "permissions payload is empty"},
		{"null", `null`, "permissions payload is empty"},
		{"empty envelope", `{}`, "permissions payload is empty"},
		// A natural mistake given the sibling `grant` command's flags: this
		// looks like a permission assignment but isn't the documented
		// envelope shape.
		{"unrecognized envelope shape", `{"userId":1,"permission":"Edit"}`, "failed to parse permissions"},
		{"wrong envelope key", `{"perms":[{"permission":"Edit","userId":1}]}`, "failed to parse permissions"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			file := filepath.Join(dir, "acl.json")
			require.NoError(t, os.WriteFile(file, []byte(tt.payload), 0o600))

			p := &permissions.PermissionsProvider{}
			root := p.Commands()[0]
			root.SetArgs([]string{"set", "dashboards", "d1", "-f", file, "--force"})
			var out bytes.Buffer
			root.SetOut(&out)
			root.SetErr(&out)

			err := root.Execute()
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErrSubstr)
		})
	}
}

func TestSetCommand_AcceptsRecognizedPermissionsPayloadShapes(t *testing.T) {
	tests := []struct {
		name    string
		payload string
	}{
		{"bare array", `[{"permission":"Edit","userId":1}]`},
		{"envelope", `{"permissions":[{"permission":"Admin","teamId":2}]}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			file := filepath.Join(dir, "acl.json")
			require.NoError(t, os.WriteFile(file, []byte(tt.payload), 0o600))

			p := &permissions.PermissionsProvider{}
			root := p.Commands()[0]
			root.SetArgs([]string{"set", "dashboards", "d1", "-f", file, "--force"})
			var out bytes.Buffer
			root.SetOut(&out)
			root.SetErr(&out)

			// A recognized payload parses successfully; the command goes on
			// to fail at client/config loading in this unconfigured test
			// environment, but that failure must not be a parsing error.
			err := root.Execute()
			if err != nil {
				assert.NotContains(t, err.Error(), "permissions payload is empty")
				assert.NotContains(t, err.Error(), "failed to parse permissions")
			}
		})
	}
}

func TestSetCommand_ForceSkipsConfirmationPrompt(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "acl.json")
	require.NoError(t, os.WriteFile(file, []byte(`[{"permission":"Edit","userId":1}]`), 0o600))

	p := &permissions.PermissionsProvider{}
	root := p.Commands()[0]
	root.SetArgs([]string{"set", "dashboards", "d1", "-f", file, "--force"})
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)

	// --force bypasses the prompt; the command then goes on to load a
	// Grafana client from the (unconfigured) test context and fails there —
	// what matters here is only that no confirmation prompt was shown.
	_ = root.Execute()
	assert.NotContains(t, out.String(), "[y/N]")
	assert.NotContains(t, out.String(), "Aborted.")
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
