package savedconversations_test

import (
	"bytes"
	"encoding/json"
	"testing"

	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers/agento11y/eval/savedconversations"
	"github.com/grafana/gcx/internal/testutils"
	"github.com/stretchr/testify/require"
)

func TestTableGolden(t *testing.T) {
	var row savedconversations.SavedConversation
	require.NoError(t, json.Unmarshal([]byte(`{"saved_id": "saved-1", "name": "name \u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c", "conversation_id": "conversation-1", "source": "manual", "generation_count": 4, "saved_by": "reviewer", "created_at": "2026-09-01T12:30:00Z"}`), &row))
	assertTableGolden(t, savedconversations.Table(), row, "table", "wide")
}

func TestCollectionsTableGolden(t *testing.T) {
	var row savedconversations.CollectionRef
	require.NoError(t, json.Unmarshal([]byte(`{"collection_id": "collection-1", "name": "Review cases", "member_count": 4, "description": "description \u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c", "created_by": "reviewer", "created_at": "2026-09-01T12:30:00Z"}`), &row))
	assertTableGolden(t, savedconversations.CollectionsTable(), row, "table", "wide")
}

func assertTableGolden[T any](t *testing.T, table cmdio.Table[T], row T, formats ...string) {
	t.Helper()
	var zero T
	for _, name := range formats {
		t.Run(name, func(t *testing.T) {
			for _, rows := range []struct {
				name  string
				items []T
			}{
				{"populated", []T{row, zero}},
				{"empty", []T{}},
				{"nil", nil},
			} {
				t.Run(rows.name, func(t *testing.T) {
					var buf bytes.Buffer
					require.NoError(t, table.Codec(name).Encode(&buf, rows.items))
					testutils.Golden(t, t.Name(), buf.String())
				})
			}
		})
	}
}
