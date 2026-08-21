package sql_test

import (
	"encoding/json"
	"testing"
	"time"

	querysql "github.com/grafana/gcx/internal/query/sql"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildRawQueryBody(t *testing.T) {
	t.Run("wire shape", func(t *testing.T) {
		body, err := querysql.BuildRawQueryBody("mysql", "test-uid", querysql.RawQueryRequest{
			RawSQL: "SELECT 1",
			Start:  time.Date(2026, 7, 14, 0, 0, 0, 0, time.UTC),
			End:    time.Date(2026, 7, 14, 1, 0, 0, 0, time.UTC),
		})
		require.NoError(t, err)

		var decoded map[string]any
		require.NoError(t, json.Unmarshal(body, &decoded))

		queries, ok := decoded["queries"].([]any)
		require.True(t, ok)
		require.Len(t, queries, 1)
		q, ok := queries[0].(map[string]any)
		require.True(t, ok)

		assert.Equal(t, "A", q["refId"])
		assert.Equal(t, "SELECT 1", q["rawSql"])
		assert.Equal(t, "table", q["format"])
		assert.Equal(t, map[string]any{"type": "mysql", "uid": "test-uid"}, q["datasource"])
		assert.InDelta(t, 60000, q["intervalMs"], 0)
	})

	t.Run("zero Start/End defaults the range to the last hour", func(t *testing.T) {
		body, err := querysql.BuildRawQueryBody("mysql", "test-uid", querysql.RawQueryRequest{RawSQL: "SELECT 1"})
		require.NoError(t, err)

		var decoded map[string]any
		require.NoError(t, json.Unmarshal(body, &decoded))

		from, ok := decoded["from"].(string)
		require.True(t, ok)
		to, ok := decoded["to"].(string)
		require.True(t, ok)
		assert.NotEqual(t, "0", from)
		assert.NotEqual(t, "0", to)
	})

	t.Run("nonzero IntervalMs is preserved", func(t *testing.T) {
		body, err := querysql.BuildRawQueryBody("mysql", "test-uid", querysql.RawQueryRequest{
			RawSQL:     "SELECT 1",
			IntervalMs: 5000,
		})
		require.NoError(t, err)

		var decoded map[string]any
		require.NoError(t, json.Unmarshal(body, &decoded))
		queries, ok := decoded["queries"].([]any)
		require.True(t, ok)
		q, ok := queries[0].(map[string]any)
		require.True(t, ok)
		assert.InDelta(t, 5000, q["intervalMs"], 0)
	})
}
