package faro

import (
	"testing"

	querysql "github.com/grafana/gcx/internal/query/sql"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPinotReplayStartsQueryUsesSessionFetcherTable(t *testing.T) {
	t.Parallel()
	query, err := pinotReplayStartsQuery("66", "https://ops.grafana-ops.net", 25)
	require.NoError(t, err)
	assert.Contains(t, query, "FROM faro_pinot_events_v2")
	assert.Contains(t, query, "appId = 66")
	assert.Contains(t, query, "eventName = 'faro.session_recording.started'")
	assert.Contains(t, query, "LIMIT 25")

	query, err = pinotReplayStartsQuery("66", "https://example.grafana.net", 25)
	require.NoError(t, err)
	assert.Contains(t, query, "FROM faro_pinot_events_v1")
}

func TestPinotReplayStartsQueryRejectsNonNumericAppID(t *testing.T) {
	t.Parallel()
	_, err := pinotReplayStartsQuery("66; DROP TABLE events", "https://ops.grafana-ops.net", 10)
	require.ErrorContains(t, err, "invalid app id")
}

func TestExtractPinotReplaySessionRows(t *testing.T) {
	t.Parallel()
	response := &querysql.QueryResponse{
		Columns: []querysql.Column{
			{Name: "session_id"}, {Name: "last_seen"}, {Name: "browser_name"},
			{Name: "browser_version"}, {Name: "app_name"},
		},
		Rows: [][]any{
			{"sess-1", float64(1790340861602), "Chrome", "153", "web"},
			{"sess-1", float64(1790340850000), "Chrome", "153", "web"},
			{"sess-2", float64(1790340840000), "Firefox", "", "web"},
		},
	}
	rows, err := extractPinotReplaySessionRows(response)
	require.NoError(t, err)
	require.Len(t, rows, 2)
	assert.Equal(t, "sess-1", rows[0].SessionID)
	assert.Equal(t, "Chrome 153", rows[0].Browser)
	assert.Equal(t, "2026-09-25T12:54:21Z", rows[0].LastSeen)
	assert.Equal(t, "Firefox", rows[1].Browser)
}

func TestExtractPinotReplaySessionRowsRejectsMalformedResult(t *testing.T) {
	t.Parallel()
	_, err := extractPinotReplaySessionRows(&querysql.QueryResponse{
		Columns: []querysql.Column{{Name: "session_id"}},
		Rows:    [][]any{{"sess-1"}},
	})
	require.ErrorContains(t, err, "missing last_seen column")
}
