package collections_test

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/grafana/gcx/internal/providers/aio11y/eval/collections"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTableCodec_Encode(t *testing.T) {
	items := []collections.Collection{
		{
			CollectionID: "c-1", Name: "Regression", Description: "Nightly", MemberCount: 4,
			CreatedBy: "alice", CreatedAt: time.Date(2026, 4, 1, 10, 0, 0, 0, time.UTC),
		},
		{CollectionID: "c-2", Name: "Smoke"},
	}

	tests := []struct {
		name string
		wide bool
		want []string
	}{
		{
			name: "table format",
			wide: false,
			want: []string{"ID", "NAME", "MEMBERS", "DESCRIPTION", "c-1", "Regression"},
		},
		{
			name: "wide adds CREATED BY",
			wide: true,
			want: []string{"CREATED BY", "alice", "2026-04-01 10:00"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			codec := &collections.TableCodec{Wide: tc.wide}
			var buf bytes.Buffer
			require.NoError(t, codec.Encode(&buf, items))
			out := buf.String()
			for _, s := range tc.want {
				assert.Contains(t, out, s)
			}
		})
	}
}

func TestTableCodec_WrongType(t *testing.T) {
	codec := &collections.TableCodec{}
	var buf bytes.Buffer
	err := codec.Encode(&buf, "not-a-slice")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "expected []Collection")
}

func TestTableCodec_Format(t *testing.T) {
	assert.Equal(t, "table", string((&collections.TableCodec{}).Format()))
	assert.Equal(t, "wide", string((&collections.TableCodec{Wide: true}).Format()))
}

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
