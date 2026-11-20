package faro

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/grafana/gcx/internal/config"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/query/loki"
	"github.com/grafana/gcx/internal/query/pinot"
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

func TestLokiReplayScanUsesBoundedPages(t *testing.T) {
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
	for _, limit := range []int{5000, 30, 0} {
		_, _, err := queryLokiReplaySessions(t.Context(), cfg, "loki-uid", "42", time.Unix(1, 0), time.Unix(2, 0), limit)
		require.NoError(t, err)
	}
	assert.Equal(t, []float64{1000, 1000, 1000}, maxLines)
}

func TestLokiReplayPagesStopAfterSessionLimit(t *testing.T) {
	dataset := make([]loki.LogEntry, 0, lokiEventsPageSize+2)
	for i := 1; i <= lokiEventsPageSize+2; i++ {
		dataset = append(dataset, loki.LogEntry{
			Timestamp: lokiUnixNanoMS(int64(i) * 1000),
			Line:      fmt.Sprintf("session_id=sess-%d", i),
		})
	}
	query := `{kind="event"}`
	accumulator := replaySessionAccumulator{sessions: make(map[string]replaySessionInfo)}
	stop := func() bool {
		return len(accumulator.sessions) > 50
	}
	_, stopped, err := fetchLokiEventPagesUntil(t.Context(), &rangeLoki{dataset: dataset}, "uid", query, time.UnixMilli(0), time.UnixMilli(2000000), sessionLokiQueryTimeout, lokiEventsPageSize, stop, accumulator.addPage)
	require.NoError(t, err)
	assert.True(t, stopped)
	rows := accumulator.rows()
	assert.Greater(t, len(rows), 50)
	items, meta := cmdio.TruncatePagedList(rows, 50)
	assert.Len(t, items, 50)
	assert.Equal(t, 50, meta.Returned)
	assert.Zero(t, meta.Cap)
	assert.Equal(t, "sess-1002", items[0].SessionID)
}

type multiPageReplayLoki struct {
	queries   int
	dataPages int
}

func (c *multiPageReplayLoki) Query(_ context.Context, _ string, req loki.QueryRequest) (*loki.QueryResponse, error) {
	c.queries++
	// The pager re-queries the earliest timestamp to preserve ties. Return just
	// that boundary row. Return 101 full data pages, followed by a short final
	// page, to prove discovery drains past its former 100-page cap.
	if req.End.Sub(req.Start) <= time.Millisecond {
		return &loki.QueryResponse{Data: loki.QueryResultData{Result: []loki.StreamEntry{{Values: []loki.LogEntry{{
			Timestamp: lokiUnixNanoMS(req.Start.UnixMilli()), Line: "session_id=session-1",
		}}}}}}, nil
	}
	c.dataPages++
	if c.dataPages > 101 {
		return &loki.QueryResponse{Data: loki.QueryResultData{Result: []loki.StreamEntry{{Values: []loki.LogEntry{{
			Timestamp: lokiUnixNanoMS(req.Start.UnixMilli()), Line: "session_id=session-final",
		}}}}}}, nil
	}
	entries := make([]loki.LogEntry, lokiEventsPageSize)
	for i := range entries {
		millis := req.End.Add(-time.Duration(i+1) * time.Second).UnixMilli()
		entries[i] = loki.LogEntry{Timestamp: lokiUnixNanoMS(millis), Line: "session_id=session-1"}
	}
	return &loki.QueryResponse{Data: loki.QueryResultData{Result: []loki.StreamEntry{{Values: entries}}}}, nil
}

func TestLokiReplayDiscoveryDrainsPastFormerPageCap(t *testing.T) {
	client := &multiPageReplayLoki{}
	accumulator := replaySessionAccumulator{sessions: make(map[string]replaySessionInfo)}
	resp, stopped, err := fetchLokiEventPagesUntil(t.Context(), client, "uid", `{kind="event"}`, time.Unix(0, 0), time.Unix(1_000_000, 0), sessionLokiQueryTimeout, lokiEventsPageSize, func() bool { return false }, accumulator.addPage)
	require.NoError(t, err)
	assert.False(t, stopped)
	assert.Equal(t, 102, client.dataPages)
	assert.Equal(t, 203, client.queries) // 101 data pages plus boundary checks and one short final page
	assert.Nil(t, resp, "discovery reduces each page instead of retaining every event")
	rows := accumulator.rows()
	assert.Len(t, rows, 2)
	_, meta := cmdio.TruncateCompleteList(rows, 0)
	assert.Nil(t, meta, "a fully drained time range is a complete result")
}

func TestLokiReplayDiscoveryReportsObservedTotalAfterShortPage(t *testing.T) {
	dataset := []loki.LogEntry{
		{Timestamp: lokiUnixNanoMS(1_000), Line: "session_id=sess-a"},
		{Timestamp: lokiUnixNanoMS(2_000), Line: "session_id=sess-b"},
		{Timestamp: lokiUnixNanoMS(3_000), Line: "session_id=sess-c"},
	}
	accumulator := replaySessionAccumulator{sessions: make(map[string]replaySessionInfo)}
	stop := func() bool { return len(accumulator.sessions) > 2 }
	_, stopped, err := fetchLokiEventPagesUntil(t.Context(), &rangeLoki{dataset: dataset}, "uid", `{kind="event"}`, time.UnixMilli(0), time.UnixMilli(10_000), sessionLokiQueryTimeout, lokiEventsPageSize, stop, accumulator.addPage)
	require.NoError(t, err)
	assert.False(t, stopped, "a short page means the selected range was drained")
	rows, meta := cmdio.TruncateCompleteList(accumulator.rows(), 2)
	assert.Len(t, rows, 2)
	require.NotNil(t, meta)
	require.NotNil(t, meta.Total)
	assert.Equal(t, 3, *meta.Total)
	meta = cmdio.AttachListMeta(meta, []string{"gcx", "frontend", "apps", "list-replay-sessions", "42"})
	assert.Contains(t, meta.Continue, "--limit 0")
}

func TestLokiReplayDiscoveryQueryTimesOut(t *testing.T) {
	resp, stopped, err := fetchLokiEventPagesUntil(t.Context(), hangLoki{}, "uid", `{kind="event"}`, time.Unix(1, 0), time.Unix(2, 0), 20*time.Millisecond, lokiEventsPageSize, nil, nil)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Nil(t, resp)
	assert.False(t, stopped)
	wrapped := wrapLokiReplayDiscoveryErr(err)
	assert.Contains(t, wrapped.Error(), "scan did not finish; this is not an empty result")
	assert.Contains(t, wrapped.Error(), "narrower --from/--to")
}

type invalidTimestampLoki struct{}

func (invalidTimestampLoki) Query(_ context.Context, _ string, _ loki.QueryRequest) (*loki.QueryResponse, error) {
	entries := make([]loki.LogEntry, lokiEventsPageSize)
	for i := range entries {
		entries[i] = loki.LogEntry{Timestamp: "invalid", Line: fmt.Sprintf("session_id=sess-%d", i)}
	}
	return &loki.QueryResponse{Data: loki.QueryResultData{Result: []loki.StreamEntry{{Values: entries}}}}, nil
}

func TestLokiReplayPagerRejectsFullPageWithoutTimestamps(t *testing.T) {
	start, end := time.Unix(1, 0), time.Unix(2, 0)
	resp, stopped, err := fetchLokiEventPagesUntil(t.Context(), invalidTimestampLoki{}, "uid", `{kind="event"}`, start, end, time.Second, lokiEventsPageSize, func() bool { return false }, nil)
	require.ErrorContains(t, err, "no valid timestamp")
	assert.Nil(t, resp)
	assert.False(t, stopped)
}

type replayLokiLimitRecorder struct {
	rangeLoki

	limits []int
}

func (c *replayLokiLimitRecorder) Query(ctx context.Context, uid string, req loki.QueryRequest) (*loki.QueryResponse, error) {
	c.limits = append(c.limits, req.Limit)
	return c.rangeLoki.Query(ctx, uid, req)
}

func TestLokiReplayPagerKeepsFullTimestampBucketLimit(t *testing.T) {
	t.Parallel()
	timestamp := lokiUnixNanoMS(1000)
	client := &replayLokiLimitRecorder{rangeLoki: rangeLoki{dataset: []loki.LogEntry{
		{Timestamp: timestamp, Line: "event-1"},
		{Timestamp: timestamp, Line: "event-2"},
		{Timestamp: timestamp, Line: "event-3"},
	}}}

	got, _, err := fetchLokiEventPagesUntil(t.Context(), client, "uid", `{kind="event"}`, time.UnixMilli(0), time.UnixMilli(2000), sessionLokiQueryTimeout, 2, nil, nil)
	require.NoError(t, err)
	assert.Equal(t, []int{2, lokiEventsPageSize, 2}, client.limits)
	assert.Equal(t, 3, lokiEntryCount(got))
}

func TestPinotReplayStartsQueryUsesSessionFetcherTable(t *testing.T) {
	t.Parallel()
	query, err := pinotReplayStartsQuery("66", "https://ops.grafana-ops.net", 26, 0)
	require.NoError(t, err)
	assert.Contains(t, query, "FROM faro_pinot_events_v2")
	assert.Contains(t, query, "appId = 66")
	assert.Contains(t, query, "eventName = 'faro.session_recording.started'")
	assert.Contains(t, query, "GROUP BY sessionId")
	assert.Contains(t, query, "ORDER BY recording_started DESC, session_id ASC")
	assert.Contains(t, query, "LIMIT 26 OFFSET 0")

	query, err = pinotReplayStartsQuery("66", "https://example.grafana.net", 25, 100)
	require.NoError(t, err)
	assert.Contains(t, query, "FROM faro_pinot_events_v1")
	assert.Contains(t, query, "LIMIT 25 OFFSET 100")
}

func TestPinotReplayStartsQueryRejectsNonNumericAppID(t *testing.T) {
	t.Parallel()
	_, err := pinotReplayStartsQuery("66; DROP TABLE events", "https://ops.grafana-ops.net", 10, 0)
	require.ErrorContains(t, err, "invalid app id")
}

func TestExtractPinotReplaySessionRows(t *testing.T) {
	t.Parallel()
	response := &querysql.QueryResponse{
		Columns: []querysql.Column{
			{Name: "session_id"}, {Name: "recording_started"}, {Name: "browser_name"},
			{Name: "browser_version"}, {Name: "app_name"},
		},
		Rows: [][]any{
			{"sess-bad", "not-a-timestamp", "Unknown", "", "web"},
			{"sess-1", float64(1790340861602), "Chrome", "153", "web"},
			{"sess-1", float64(1790340850000), "Chrome", "153", "web"},
			{"sess-2", float64(1790340840000), "Firefox", "", "web"},
		},
	}
	rows, hasUsableRows, err := extractPinotReplaySessionRows(response, make(map[string]struct{}))
	require.NoError(t, err)
	assert.True(t, hasUsableRows)
	require.Len(t, rows, 2)
	assert.Equal(t, "sess-1", rows[0].SessionID)
	assert.Equal(t, "Chrome 153", rows[0].Browser)
	assert.Equal(t, "2026-09-25T12:54:21Z", rows[0].RecordingStarted)
	assert.Equal(t, "Firefox", rows[1].Browser)
}

type replayPinotPages struct {
	first  *querysql.QueryResponse
	second *querysql.QueryResponse
}

type replayPinotChangingPages struct {
	offsets []int
}

func (p *replayPinotChangingPages) Query(_ context.Context, _ string, req pinot.QueryRequest) (*querysql.QueryResponse, error) {
	page := len(p.offsets)
	expectedOffset := len(p.offsets) * pinotJourneyPageSize
	if !strings.Contains(req.RawSQL, fmt.Sprintf("OFFSET %d", expectedOffset)) {
		return nil, fmt.Errorf("unexpected Pinot page offset: want %d in %q", expectedOffset, req.RawSQL)
	}
	p.offsets = append(p.offsets, expectedOffset)
	if page == pinotJourneyMaxPages+1 {
		return replayPinotRowsFrom(expectedOffset, 1), nil
	}
	return replayPinotRowsFrom(expectedOffset, pinotJourneyPageSize), nil
}

func (p replayPinotPages) Query(_ context.Context, _ string, req pinot.QueryRequest) (*querysql.QueryResponse, error) {
	if strings.Contains(req.RawSQL, "OFFSET 0") {
		return p.first, nil
	}
	return p.second, nil
}

func replayPinotRows(count int) *querysql.QueryResponse {
	return replayPinotRowsFrom(0, count)
}

func replayPinotRowsFrom(first, count int) *querysql.QueryResponse {
	resp := &querysql.QueryResponse{Columns: []querysql.Column{
		{Name: "session_id"}, {Name: "recording_started"}, {Name: "browser_name"}, {Name: "browser_version"}, {Name: "app_name"},
	}}
	for i := range count {
		resp.Rows = append(resp.Rows, []any{fmt.Sprintf("sess-%d", first+i), float64(1790340861602), "Chrome", "153", "web"})
	}
	return resp
}

func TestPinotReplaySessionPagerDeduplicatesAcrossPages(t *testing.T) {
	first := replayPinotRows(pinotJourneyPageSize)
	second := replayPinotRows(2)
	second.Rows[1][0] = "new-session"
	rows, meta, err := fetchPinotReplaySessions(t.Context(), replayPinotPages{first: first, second: second}, "uid", "66", "https://example.grafana.net", time.Unix(1, 0), time.Unix(2, 0), 0)
	require.NoError(t, err)
	assert.Nil(t, meta)
	assert.Len(t, rows, pinotJourneyPageSize+1)
	assert.Equal(t, "new-session", rows[len(rows)-1].SessionID)
}

func TestPinotReplaySessionPagerReportsObservedTotalAfterShortPage(t *testing.T) {
	first := replayPinotRows(1000)
	second := replayPinotRowsFrom(1000, 600)
	rows, meta, err := fetchPinotReplaySessions(t.Context(), replayPinotPages{first: first, second: second}, "uid", "66", "https://example.grafana.net", time.Unix(1, 0), time.Unix(2, 0), 1500)
	require.NoError(t, err)
	assert.Len(t, rows, 1500)
	require.NotNil(t, meta)
	require.NotNil(t, meta.Total)
	assert.Equal(t, 1600, *meta.Total)
}

func TestPinotReplaySessionPagerRejectsRepeatedPage(t *testing.T) {
	page := replayPinotRows(pinotJourneyPageSize)
	_, _, err := fetchPinotReplaySessions(t.Context(), replayPinotPages{first: page, second: page}, "uid", "66", "https://example.grafana.net", time.Unix(1, 0), time.Unix(2, 0), 0)
	require.ErrorContains(t, err, "repeated without new sessions")
}

func TestPinotReplaySessionPagerReportsPageWithoutUsableSessions(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func([]any)
	}{
		{name: "empty session ID", mutate: func(row []any) { row[0] = "" }},
		{name: "invalid recording start", mutate: func(row []any) { row[1] = "invalid" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			page := replayPinotRows(pinotJourneyPageSize)
			for _, row := range page.Rows {
				tc.mutate(row)
			}
			_, _, err := fetchPinotReplaySessions(t.Context(), replayPinotPages{first: page}, "uid", "66", "https://example.grafana.net", time.Unix(1, 0), time.Unix(2, 0), 0)
			require.ErrorContains(t, err, "contained no usable sessions")
		})
	}
}

func TestPinotReplaySessionPagerDrainsPastFormerPageLimit(t *testing.T) {
	client := &replayPinotChangingPages{}
	rows, meta, err := fetchPinotReplaySessions(t.Context(), client, "uid", "66", "https://example.grafana.net", time.Unix(1, 0), time.Unix(2, 0), 0)
	require.NoError(t, err)
	assert.Len(t, rows, (pinotJourneyMaxPages+1)*pinotJourneyPageSize+1)
	assert.Nil(t, meta)
	require.Len(t, client.offsets, pinotJourneyMaxPages+2)
	for page, offset := range client.offsets {
		assert.Equal(t, page*pinotJourneyPageSize, offset)
	}
}

func TestExtractPinotReplaySessionRowsRejectsMalformedResult(t *testing.T) {
	t.Parallel()
	_, _, err := extractPinotReplaySessionRows(&querysql.QueryResponse{
		Columns: []querysql.Column{{Name: "session_id"}},
		Rows:    [][]any{{"sess-1"}},
	}, make(map[string]struct{}))
	require.ErrorContains(t, err, "missing recording_started column")
}
