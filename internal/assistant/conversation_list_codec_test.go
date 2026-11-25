package assistant_test

import (
	"bytes"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/grafana/gcx/internal/assistant"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConversationListCodec_TruncatesTitleOnRuneBoundary(t *testing.T) {
	chats := []assistant.Chat{{ID: "c1", Name: strings.Repeat("あ", 30), Source: "cli"}}

	var buf bytes.Buffer
	require.NoError(t, (&assistant.ConversationListCodec{}).Encode(&buf, chats))

	out := buf.String()
	assert.True(t, utf8.ValidString(out), "table output must be valid UTF-8")
	assert.Contains(t, out, strings.Repeat("あ", 30), "a 30 character title fits and must not be cut")
}

func TestConversationListCodec_LongTitleStaysValidUTF8(t *testing.T) {
	chats := []assistant.Chat{{ID: "c1", Name: strings.Repeat("あ", 80), Source: "cli"}}

	var buf bytes.Buffer
	require.NoError(t, (&assistant.ConversationListCodec{}).Encode(&buf, chats))

	out := buf.String()
	assert.True(t, utf8.ValidString(out), "table output must be valid UTF-8")
	assert.Contains(t, out, strings.Repeat("あ", 50)+"...")
}
