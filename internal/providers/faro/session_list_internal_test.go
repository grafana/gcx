package faro

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/grafana/gcx/internal/config"
	querysql "github.com/grafana/gcx/internal/query/sql"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"
)

func TestLokiReplayDiscoveryQueryUsesIndexedEventFilter(t *testing.T) {
	t.Parallel()
	appID := `42"\`
	query := lokiReplayDiscoveryQuery(appID)
	assert.Contains(t, query, `kind="event"`)
	assert.Contains(t, query, `|= "faro.session_recording.started"`)
	assert.Contains(t, query, `app_id="`+escapeLogQLString(appID)+`"`)
}

func TestLokiReplayScanHonorsEffectiveCap(t *testing.T) {
	var maxLines []float64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Decode only maxLines; the other query fields are intentionally ignored.
		var body struct {
			Queries []struct {
				MaxLines float64 `json:"maxLines"`
			} `json:"queries"`
		}
		if assert.NoError(t, json.NewDecoder(r.Body).Decode(&body)) && assert.Len(t, body.Queries, 1) {
			maxLines = append(maxLines, body.Queries[0].MaxLines)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"results":{"A":{"frames":[]}}}`))
	}))
	t.Cleanup(server.Close)
	cfg := config.NamespacedRESTConfig{Config: rest.Config{Host: server.URL}, Namespace: "default"}
	for _, limit := range []int{5000, 30} {
		_, _, err := queryLokiReplaySessions(t.Context(), cfg, "loki-uid", "42", time.Unix(1, 0), time.Unix(2, 0), limit)
		require.NoError(t, err)
	}
	assert.Equal(t, []float64{1000, 30}, maxLines)
}

func TestReplaySessionListMetaKeepsEventAndSessionCountsSeparate(t *testing.T) {
	argv := []string{"gcx", "frontend", "apps", "list-replay-sessions", "42", "--limit", "600"}
	meta := replaySessionListMeta(12, true, true, 600, argv)
	require.NotNil(t, meta)
	assert.Equal(t, 12, meta.Returned)
	assert.Zero(t, meta.Cap, "the event cap is not a cap on returned sessions")
	assert.Contains(t, meta.Continue, "--limit 1000")

	meta = replaySessionListMeta(12, true, true, 1000, argv)
	require.NotNil(t, meta)
	assert.True(t, meta.Truncated)
	assert.Equal(t, 12, meta.Returned)
	assert.Zero(t, meta.Cap)
	assert.Empty(t, meta.Continue, "increasing the Loki scan limit cannot help past the cap")
	assert.Nil(t, replaySessionListMeta(12, false, true, 1000, argv))
}

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
