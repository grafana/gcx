package conversations_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/providers/agento11y/conversations"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTableCodec_TitleTruncation(t *testing.T) {
	convs := []conversations.Conversation{
		{ID: "c1", Title: strings.Repeat("A", 50), GenerationCount: 1},
	}

	codec := conversations.Table().Codec("table")
	var buf bytes.Buffer
	require.NoError(t, codec.Encode(&buf, convs))
	assert.Contains(t, buf.String(), "...")
	assert.NotContains(t, buf.String(), strings.Repeat("A", 50))
}

func TestCommands_HasAnnotationCommands(t *testing.T) {
	cmd := conversations.Commands(nil)

	for _, sub := range []string{"list-annotations", "annotate"} {
		c, _, err := cmd.Find([]string{sub})
		require.NoError(t, err)
		assert.Equal(t, sub, c.Name())
	}
}

func TestAnnotateCommand_RequiresBody(t *testing.T) {
	cmd := conversations.Commands(nil)
	cmd.SetArgs([]string{"annotate", "conv-1"})
	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--body is required")
}

func TestAnnotateCommand_RejectsInvalidTag(t *testing.T) {
	cmd := conversations.Commands(nil)
	cmd.SetArgs([]string{"annotate", "conv-1", "--body", "note", "--tag", "not-a-tag"})
	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid --tag")
}

func TestAnnotateCommand_RejectsInvalidMetadataJSON(t *testing.T) {
	cmd := conversations.Commands(nil)
	cmd.SetArgs([]string{"annotate", "conv-1", "--body", "note", "--metadata-json", "[]"})
	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid --metadata-json")
}
