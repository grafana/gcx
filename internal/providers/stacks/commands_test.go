package stacks_test

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/providers/stacks"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// runCmd builds a parent command with SilenceUsage/SilenceErrors, adds the
// given child, sets args and optional stdin, then executes.
func runCmd(t *testing.T, child *cobra.Command, args []string, stdin string) (string, error) {
	t.Helper()

	parent := &cobra.Command{
		Use:           "test",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	parent.AddCommand(child)

	var outBuf bytes.Buffer
	parent.SetOut(&outBuf)
	parent.SetErr(&outBuf)
	parent.SetIn(strings.NewReader(stdin))
	parent.SetArgs(args)

	err := parent.Execute()
	return outBuf.String(), err
}

// parseDryRunOutput splits dry-run output into the header line and the JSON
// body payload. Returns the header (e.g. "Dry run: POST /api/instances") and
// the decoded JSON as a map.
func parseDryRunOutput(t *testing.T, out string) (string, map[string]any) {
	t.Helper()
	parts := strings.SplitN(out, "\n\n", 2)
	require.Len(t, parts, 2, "dry-run output should have header + blank line + JSON body")

	header := strings.TrimSpace(parts[0])
	var body map[string]any
	err := json.Unmarshal([]byte(parts[1]), &body)
	require.NoError(t, err, "dry-run body should be valid JSON")
	return header, body
}

// ---------------------------------------------------------------------------
// list
// ---------------------------------------------------------------------------

func TestListCommand_OrgRequired(t *testing.T) {
	_, err := runCmd(t, stacks.NewTestListCommand(), []string{"list"}, "")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "org")
}

// ---------------------------------------------------------------------------
// create — validation
// ---------------------------------------------------------------------------

func TestCreateCommand_NameAndSlugRequired(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		wantErr string
	}{
		{"missing both", []string{"create"}, "required flag"},
		{"missing slug", []string{"create", "--name", "foo"}, "required flag"},
		{"missing name", []string{"create", "--slug", "foo"}, "required flag"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := runCmd(t, stacks.NewTestCreateCommand(), tt.args, "")
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

// ---------------------------------------------------------------------------
// create — dry-run
// ---------------------------------------------------------------------------

func TestCreateCommand_DryRun(t *testing.T) {
	out, err := runCmd(t, stacks.NewTestCreateCommand(), []string{
		"create", "--name", "My Stack", "--slug", "mystack", "--region", "us",
		"--dry-run", "-o", "table",
	}, "")

	require.NoError(t, err)

	header, body := parseDryRunOutput(t, out)
	assert.Equal(t, "Dry run: POST /api/instances", header)
	assert.Equal(t, "My Stack", body["name"])
	assert.Equal(t, "mystack", body["slug"])
	assert.Equal(t, "us", body["region"])
}

func TestCreateCommand_DryRun_WithLabels(t *testing.T) {
	out, err := runCmd(t, stacks.NewTestCreateCommand(), []string{
		"create", "--name", "My Stack", "--slug", "mystack",
		"--labels", "env=prod", "--labels", "team=platform",
		"--dry-run", "-o", "table",
	}, "")

	require.NoError(t, err)

	_, body := parseDryRunOutput(t, out)
	labels, ok := body["labels"].(map[string]any)
	require.True(t, ok, "labels should be a JSON object")
	assert.Equal(t, "prod", labels["env"])
	assert.Equal(t, "platform", labels["team"])
}

func TestCreateCommand_DryRun_DeleteProtectionFlag(t *testing.T) {
	out, err := runCmd(t, stacks.NewTestCreateCommand(), []string{
		"create", "--name", "My Stack", "--slug", "mystack",
		"--delete-protection",
		"--dry-run", "-o", "table",
	}, "")

	require.NoError(t, err)

	_, body := parseDryRunOutput(t, out)
	assert.Equal(t, true, body["deleteProtection"], "deleteProtection should be true in the request body")
}

func TestCreateCommand_DryRun_InvalidLabels(t *testing.T) {
	_, err := runCmd(t, stacks.NewTestCreateCommand(), []string{
		"create", "--name", "My Stack", "--slug", "mystack",
		"--labels", "noequalssign",
		"--dry-run", "-o", "table",
	}, "")

	require.Error(t, err)
	assert.Contains(t, err.Error(), `invalid label "noequalssign"`)
}

func TestCreateCommand_DryRun_DoesNotCallAPI(t *testing.T) {
	// Dry-run should return before reaching the config loader. If it tried
	// to load config, it would error because there's no config context set up.
	_, err := runCmd(t, stacks.NewTestCreateCommand(), []string{
		"create", "--name", "X", "--slug", "x", "--dry-run", "-o", "table",
	}, "")
	require.NoError(t, err)
}

// ---------------------------------------------------------------------------
// update — validation
// ---------------------------------------------------------------------------

func TestUpdateCommand_MutualExclusion(t *testing.T) {
	_, err := runCmd(t, stacks.NewTestUpdateCommand(), []string{
		"update", "mystack",
		"--delete-protection", "--no-delete-protection",
		"--dry-run", "-o", "table",
	}, "")

	require.Error(t, err)
	assert.Equal(t, "--delete-protection and --no-delete-protection are mutually exclusive", err.Error())
}

func TestUpdateCommand_RequiresArg(t *testing.T) {
	_, err := runCmd(t, stacks.NewTestUpdateCommand(), []string{"update"}, "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "accepts 1 arg")
}

// ---------------------------------------------------------------------------
// update — dry-run
// ---------------------------------------------------------------------------

func TestUpdateCommand_DryRun(t *testing.T) {
	out, err := runCmd(t, stacks.NewTestUpdateCommand(), []string{
		"update", "mystack", "--name", "New Name", "--dry-run", "-o", "table",
	}, "")

	require.NoError(t, err)

	header, body := parseDryRunOutput(t, out)
	assert.Equal(t, "Dry run: POST /api/instances/mystack", header)
	assert.Equal(t, "New Name", body["name"])
}

func TestUpdateCommand_DryRun_EnableDeleteProtection(t *testing.T) {
	out, err := runCmd(t, stacks.NewTestUpdateCommand(), []string{
		"update", "mystack", "--delete-protection", "--dry-run", "-o", "table",
	}, "")

	require.NoError(t, err)

	_, body := parseDryRunOutput(t, out)
	assert.Equal(t, true, body["deleteProtection"])
}

func TestUpdateCommand_DryRun_DisableDeleteProtection(t *testing.T) {
	out, err := runCmd(t, stacks.NewTestUpdateCommand(), []string{
		"update", "mystack", "--no-delete-protection", "--dry-run", "-o", "table",
	}, "")

	require.NoError(t, err)

	_, body := parseDryRunOutput(t, out)
	assert.Equal(t, false, body["deleteProtection"])
}

func TestUpdateCommand_DryRun_DoesNotCallAPI(t *testing.T) {
	_, err := runCmd(t, stacks.NewTestUpdateCommand(), []string{
		"update", "mystack", "--name", "X", "--dry-run", "-o", "table",
	}, "")
	require.NoError(t, err)
}

// ---------------------------------------------------------------------------
// delete — dry-run
// ---------------------------------------------------------------------------

func TestDeleteCommand_DryRun(t *testing.T) {
	out, err := runCmd(t, stacks.NewTestDeleteCommand(), []string{
		"delete", "mystack", "--dry-run",
	}, "")

	require.NoError(t, err)

	lines := strings.Split(strings.TrimSpace(out), "\n")
	require.Len(t, lines, 3, "dry-run delete should produce 3 lines (header, blank, summary)")
	assert.Equal(t, "Dry run: DELETE /api/instances/mystack", lines[0])
	assert.Empty(t, lines[1])
	assert.Equal(t, `Stack "mystack" would be permanently deleted. No changes were made.`, lines[2])
}

func TestDeleteCommand_DryRun_DoesNotCallAPI(t *testing.T) {
	_, err := runCmd(t, stacks.NewTestDeleteCommand(), []string{
		"delete", "mystack", "--dry-run",
	}, "")
	require.NoError(t, err)
}

// ---------------------------------------------------------------------------
// delete — confirmation
// ---------------------------------------------------------------------------

func TestDeleteCommand_ConfirmationMismatch(t *testing.T) {
	_, err := runCmd(t, stacks.NewTestDeleteCommand(), []string{"delete", "mystack"}, "wrong-slug\n")

	require.Error(t, err)
	assert.Equal(t, `confirmation did not match: expected "mystack", got "wrong-slug"`, err.Error())
}

func TestDeleteCommand_ConfirmationPrompt(t *testing.T) {
	stdout, _ := runCmd(t, stacks.NewTestDeleteCommand(), []string{"delete", "mystack"}, "wrong\n")

	assert.Contains(t, stdout, "WARNING")
	assert.Contains(t, stdout, `permanently delete stack "mystack"`)
	assert.Contains(t, stdout, "Type the stack slug to confirm")
}

func TestDeleteCommand_RequiresArg(t *testing.T) {
	_, err := runCmd(t, stacks.NewTestDeleteCommand(), []string{"delete"}, "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "accepts 1 arg")
}

// ---------------------------------------------------------------------------
// get — validation
// ---------------------------------------------------------------------------

func TestGetCommand_RequiresArg(t *testing.T) {
	_, err := runCmd(t, stacks.NewTestGetCommand(), []string{"get"}, "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "accepts 1 arg")
}

// ---------------------------------------------------------------------------
// provider registration
// ---------------------------------------------------------------------------

func TestStacksProvider_Commands(t *testing.T) {
	p := &stacks.StacksProvider{}

	assert.Equal(t, "stacks", p.Name())
	assert.NotEmpty(t, p.ShortDesc())

	cmds := p.Commands()
	require.Len(t, cmds, 1, "should return one top-level 'stacks' command")

	stacksCmd := cmds[0]
	assert.Equal(t, "stacks", stacksCmd.Use)

	subNames := make([]string, 0, len(stacksCmd.Commands()))
	for _, sub := range stacksCmd.Commands() {
		subNames = append(subNames, sub.Name())
	}
	assert.ElementsMatch(t, []string{"list", "get", "create", "update", "delete", "regions"}, subNames)
}
