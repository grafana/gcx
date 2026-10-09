package conversations_test

import (
	"encoding/json"
	"testing"

	"github.com/grafana/gcx/internal/providers/agento11y/conversations"
	"github.com/grafana/gcx/internal/testutils"
	"github.com/stretchr/testify/require"
)

func TestTableGolden(t *testing.T) {
	var row conversations.Conversation
	require.NoError(t, json.Unmarshal([]byte(`{"id": "conversation-1", "title": "Long unicode title \u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c", "generation_count": 3, "created_at": "2026-09-01T12:30:00Z", "last_generation_at": "2026-09-02T13:45:00Z"}`), &row))
	testutils.AssertTableGolden(t, conversations.Table(), row, "table", "wide")
}

func TestSearchTableGolden(t *testing.T) {
	var row conversations.SearchResult
	require.NoError(t, json.Unmarshal([]byte(`{"conversation_id": "conversation-1", "conversation_title": "search title \u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c", "generation_count": 5, "models": ["model-a", "model-b"], "agents": ["assistant", "reviewer"], "error_count": 2, "last_generation_at": "2026-09-02T13:45:00Z"}`), &row))
	testutils.AssertTableGolden(t, conversations.SearchTable(), row, "table", "wide")
}

func TestAnnotationsTableGolden(t *testing.T) {
	var row conversations.ConversationAnnotation
	require.NoError(t, json.Unmarshal([]byte(`{"annotation_id": "annotation-1", "annotation_type": "note", "body": "body \u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c", "tags": {"z": "last", "a": "first"}, "operator_name": "Reviewer", "operator_login": "reviewer", "operator_id": "user-1", "generation_id": "generation-1", "created_at": "2026-09-01T12:30:00Z"}`), &row))
	testutils.AssertTableGolden(t, conversations.AnnotationsTable(), row, "table", "wide")
}
