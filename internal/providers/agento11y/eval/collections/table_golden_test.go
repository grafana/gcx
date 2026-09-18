package collections_test

import (
	"encoding/json"
	"testing"

	"github.com/grafana/gcx/internal/providers/agento11y/eval/collections"
	"github.com/grafana/gcx/internal/testutils"
	"github.com/stretchr/testify/require"
)

func TestTableGolden(t *testing.T) {
	var row collections.Collection
	require.NoError(t, json.Unmarshal([]byte(`{"collection_id": "collection-1", "name": "Review cases", "member_count": 4, "description": "description \u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c", "created_by": "reviewer", "created_at": "2026-09-01T12:30:00Z", "updated_at": "2026-09-02T13:45:00Z"}`), &row))
	testutils.AssertTableGolden(t, collections.Table(), row, "table", "wide")
}
