package savedconversations_test

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/grafana/gcx/internal/providers/aio11y/eval/savedconversations"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTableCodec_Encode(t *testing.T) {
	items := []savedconversations.SavedConversation{
		{
			SavedID: "saved-1", Name: "Regression seed", ConversationID: "conv-1",
			Source: "telemetry", GenerationCount: 4, SavedBy: "alice",
			CreatedAt: time.Date(2026, 4, 1, 10, 0, 0, 0, time.UTC),
		},
		{SavedID: "saved-2", Name: "Manual", ConversationID: "conv-manual-2", Source: "manual"},
	}

	tests := []struct {
		name string
		wide bool
		want []string
	}{
		{
			name: "table format",
			wide: false,
			want: []string{"SAVED ID", "NAME", "CONVERSATION", "SOURCE", "GENS", "saved-1", "Regression seed", "telemetry"},
		},
		{
			name: "wide adds saved-by and created-at",
			wide: true,
			want: []string{"SAVED BY", "CREATED AT", "alice", "2026-04-01 10:00"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			codec := &savedconversations.TableCodec{Wide: tc.wide}
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
	codec := &savedconversations.TableCodec{}
	var buf bytes.Buffer
	err := codec.Encode(&buf, "not-a-slice")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "expected []SavedConversation")
}

func TestTableCodec_Format(t *testing.T) {
	assert.Equal(t, "table", string((&savedconversations.TableCodec{}).Format()))
	assert.Equal(t, "wide", string((&savedconversations.TableCodec{Wide: true}).Format()))
}

func TestCollectionsTableCodec_Encode(t *testing.T) {
	items := []savedconversations.CollectionRef{
		{CollectionID: "c-1", Name: "Regression suite", MemberCount: 3, Description: "Used for nightly regression"},
	}
	codec := &savedconversations.CollectionsTableCodec{Wide: true}
	var buf bytes.Buffer
	require.NoError(t, codec.Encode(&buf, items))

	out := buf.String()
	for _, s := range []string{"COLLECTION ID", "NAME", "MEMBERS", "DESCRIPTION", "c-1", "Regression suite", "3", "Used for nightly regression"} {
		assert.Contains(t, out, s)
	}
}

func TestCollectionsTableCodec_WrongType(t *testing.T) {
	codec := &savedconversations.CollectionsTableCodec{}
	var buf bytes.Buffer
	err := codec.Encode(&buf, []string{"x"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "expected []CollectionRef")
}

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
