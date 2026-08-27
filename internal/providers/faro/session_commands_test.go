package faro //nolint:testpackage // Tests unexported opts, fetch, and command constructors.

import (
	"bytes"
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	dsquery "github.com/grafana/gcx/internal/datasources/query"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/query/loki"
	"github.com/grafana/gcx/internal/query/pinot"
	querysql "github.com/grafana/gcx/internal/query/sql"
	"github.com/grafana/gcx/internal/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSessionsGetOptsValidate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		app     string
		appType string
		ds      string
		since   string
		wantErr string
	}{
		{name: "missing app", wantErr: "--app is required"},
		{name: "bad app type", app: "66", appType: "native", since: "1h", wantErr: "--app-type"},
		{name: "bad datasource", app: "66", ds: "clickhouse", since: "1h", wantErr: "--datasource"},
		{name: "missing time", app: "66", wantErr: "--since or --from/--to is required"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			opts := sessionsGetOpts{
				App:        tt.app,
				AppType:    tt.appType,
				Datasource: tt.ds,
				TimeRangeOpts: dsquery.TimeRangeOpts{
					Since: tt.since,
				},
			}
			if opts.Datasource == "" {
				opts.Datasource = datasourceLoki
			}
			err := opts.Validate()
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestSessionsGetOptsValidateOK(t *testing.T) {
	t.Parallel()
	opts := sessionsGetOpts{
		App:        "66",
		Datasource: datasourcePinot,
		TimeRangeOpts: dsquery.TimeRangeOpts{
			Since: "7d",
		},
	}
	require.NoError(t, opts.Validate())
}

func TestSessionsGetOptsValidateTrimsInputs(t *testing.T) {
	t.Parallel()
	opts := sessionsGetOpts{
		App:        "  my-app-66  ",
		AppType:    " Mobile ",
		Datasource: " Pinot ",
		Save:       " /tmp/session.txt ",
		TimeRangeOpts: dsquery.TimeRangeOpts{
			Since: "7d",
		},
	}
	require.NoError(t, opts.Validate())
	assert.Equal(t, "my-app-66", opts.App)
	assert.Equal(t, appTypeMobile, opts.AppType)
	assert.Equal(t, datasourcePinot, opts.Datasource)
	assert.Equal(t, "/tmp/session.txt", opts.Save)
	assert.Equal(t, "66", resolveAppID(opts.App))
}

func TestSessionsGetOptsValidateAgentRequiresSave(t *testing.T) {
	testutils.SetAgentMode(t, true)
	base := sessionsGetOpts{
		App:        "66",
		Datasource: datasourceLoki,
		TimeRangeOpts: dsquery.TimeRangeOpts{
			Since: "1h",
		},
	}

	missing := base
	err := missing.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--save is required")

	ok := base
	ok.Save = "/tmp/session.txt"
	require.NoError(t, ok.Validate())
}

type stubPinot struct {
	mu      sync.Mutex
	sqls    []string
	sdkName string
	osName  string
	empty   bool
}

func (s *stubPinot) Query(_ context.Context, _ string, req pinot.QueryRequest) (*querysql.QueryResponse, error) {
	s.mu.Lock()
	s.sqls = append(s.sqls, req.RawSQL)
	s.mu.Unlock()
	if s.empty {
		return &querysql.QueryResponse{
			Columns: []querysql.Column{{Name: "c"}},
			Rows:    [][]any{{""}},
		}, nil
	}
	if strings.Contains(req.RawSQL, "UNION ALL") {
		return &querysql.QueryResponse{
			Columns: []querysql.Column{{Name: "c"}},
			Rows:    [][]any{{"event"}},
		}, nil
	}
	return &querysql.QueryResponse{
		Columns: []querysql.Column{{Name: "sdk_name"}, {Name: "os_name"}},
		Rows:    [][]any{{s.sdkName, s.osName}},
	}, nil
}

func queryJoined(sqls []string) string {
	return strings.Join(sqls, "\n")
}

func TestFetchPinotSession(t *testing.T) {
	t.Parallel()
	stub := &stubPinot{}
	p := sessionQueryParams{AppID: "66", SessionID: "sid", AppType: appTypeWeb}
	got, err := fetchPinotSession(context.Background(), stub, "uid", p, time.Unix(0, 0), time.Unix(1, 0))
	require.NoError(t, err)
	dump := got.dump()
	assert.Contains(t, dump, "=== session metadata ===")
	assert.Contains(t, dump, "=== events ===")
	require.Len(t, stub.sqls, 3)
	joined := queryJoined(stub.sqls)
	assert.Contains(t, joined, "FIRSTWITHTIME(appName")
	assert.Contains(t, joined, "userId")
	assert.Contains(t, joined, "UNION ALL")
	assert.NotContains(t, joined, "app_memory")
	assert.NotContains(t, joined, "LIMIT")
}

func TestFetchPinotSessionMobile(t *testing.T) {
	t.Parallel()
	stub := &stubPinot{}
	p := sessionQueryParams{AppID: "96", SessionID: "sid", AppType: appTypeMobile}
	_, err := fetchPinotSession(context.Background(), stub, "uid", p, time.Unix(0, 0), time.Unix(1, 0))
	require.NoError(t, err)
	require.Len(t, stub.sqls, 3)
	joined := queryJoined(stub.sqls)
	assert.Contains(t, joined, "userId")
	assert.Contains(t, joined, "device_model_name")
	assert.Contains(t, joined, "UNION ALL")
	assert.Contains(t, joined, "app_memory")
}

func TestFetchPinotSessionInfersMobile(t *testing.T) {
	t.Parallel()
	stub := &stubPinot{sdkName: "@grafana/faro-react-native"}
	p := sessionQueryParams{AppID: "96", SessionID: "sid"}
	_, err := fetchPinotSession(context.Background(), stub, "uid", p, time.Unix(0, 0), time.Unix(1, 0))
	require.NoError(t, err)
	require.Len(t, stub.sqls, 3)
	assert.Contains(t, queryJoined(stub.sqls), "app_memory")
}

func TestFetchPinotSessionEmpty(t *testing.T) {
	t.Parallel()
	stub := &stubPinot{empty: true}
	p := sessionQueryParams{AppID: "66", SessionID: "missing"}
	_, err := fetchPinotSession(context.Background(), stub, "uid", p, time.Unix(0, 0), time.Unix(1, 0))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no telemetry for session missing")
}

func TestFetchPinotSessionRejectsNonIntegerAppID(t *testing.T) {
	t.Parallel()
	stub := &stubPinot{}
	p := sessionQueryParams{AppID: "66; DROP TABLE events", SessionID: "sid"}
	_, err := fetchPinotSession(context.Background(), stub, "uid", p, time.Unix(0, 0), time.Unix(1, 0))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "must be an integer")
	assert.Empty(t, stub.sqls)
}

type stubLoki struct {
	mu         sync.Mutex
	queries    []string
	dirs       []string
	limits     []int
	metaLine   string
	empty      bool
	eventPages []int
	eventCalls int
}

func (s *stubLoki) Query(_ context.Context, _ string, req loki.QueryRequest) (*loki.QueryResponse, error) {
	s.mu.Lock()
	s.queries = append(s.queries, req.Query)
	s.dirs = append(s.dirs, req.Direction)
	s.limits = append(s.limits, req.Limit)
	s.mu.Unlock()
	if s.empty {
		return &loki.QueryResponse{}, nil
	}
	line := "kind=event"
	ts := "200"
	switch {
	case strings.Contains(req.Query, "faro.session_recording.started"):
		line = "event_name=faro.session_recording.started"
		ts = "150"
	case strings.Contains(req.Query, `kind="event"`):
		line = "meta"
		if s.metaLine != "" {
			line = s.metaLine
		}
		ts = "100"
	default:
		n := 1
		if s.eventCalls < len(s.eventPages) {
			n = s.eventPages[s.eventCalls]
		}
		s.eventCalls++
		values := make([]loki.LogEntry, n)
		for i := range values {
			values[i] = loki.LogEntry{Timestamp: ts, Line: line}
		}
		return &loki.QueryResponse{Data: loki.QueryResultData{Result: []loki.StreamEntry{{
			Values: values,
		}}}}, nil
	}
	return &loki.QueryResponse{Data: loki.QueryResultData{Result: []loki.StreamEntry{{
		Values: []loki.LogEntry{{Timestamp: ts, Line: line}},
	}}}}, nil
}

func lokiEventsQueryFrom(queries []string) string {
	for _, q := range queries {
		if !strings.Contains(q, `kind="event"`) {
			return q
		}
	}
	return ""
}

func TestFetchLokiSession(t *testing.T) {
	t.Parallel()
	stub := &stubLoki{metaLine: `sdk_name=faro-web os_name="Mac OS" browser_name=Chrome`}
	p := sessionQueryParams{AppID: "66", SessionID: "sid"}
	got, err := fetchLokiSession(context.Background(), stub, "uid", p, time.Unix(0, 0), time.Unix(1, 0))
	require.NoError(t, err)
	dump := got.dump()
	assert.Contains(t, dump, "sdk_name=faro-web")
	assert.Contains(t, dump, `os_name="Mac OS"`)
	assert.Contains(t, dump, "browser_name=Chrome")
	assert.Contains(t, dump, "session_replay_start=150")
	assert.NotContains(t, dump, "100\tsdk_name=")
	assert.NotContains(t, dump, "150\tevent_name=faro.session_recording.started")
	assert.Contains(t, dump, "kind=event")
	require.Len(t, stub.queries, 3)
	joined := queryJoined(stub.queries)
	assert.NotContains(t, joined, "line_format")
	assert.Contains(t, joined, "faro.session_recording.started")
	assert.Contains(t, joined, "logfmt")
	events := lokiEventsQueryFrom(stub.queries)
	require.NotEmpty(t, events)
	assert.Contains(t, events, `| logfmt | session_id="sid"`)
	assert.NotContains(t, events, "faro.tracing.fetch")
	assert.NotContains(t, events, "app_memory")
	for _, dir := range stub.dirs {
		assert.Equal(t, "forward", dir)
	}
}

func TestFetchLokiSessionInfersMobile(t *testing.T) {
	t.Parallel()
	stub := &stubLoki{metaLine: "sdk_name=@grafana/faro-react-native os_name=iOS"}
	p := sessionQueryParams{AppID: "96", SessionID: "sid"}
	_, err := fetchLokiSession(context.Background(), stub, "uid", p, time.Unix(0, 0), time.Unix(1, 0))
	require.NoError(t, err)
	require.Len(t, stub.queries, 3)
	assert.Contains(t, lokiEventsQueryFrom(stub.queries), `kind!="measurement"`)
}

func TestFetchLokiSessionEmpty(t *testing.T) {
	t.Parallel()
	stub := &stubLoki{empty: true}
	p := sessionQueryParams{AppID: "66", SessionID: "missing"}
	_, err := fetchLokiSession(context.Background(), stub, "uid", p, time.Unix(0, 0), time.Unix(1, 0))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no telemetry for session missing")
}

func TestFetchLokiSessionPagesUntilComplete(t *testing.T) {
	t.Parallel()
	stub := &stubLoki{
		metaLine:   "sdk_name=faro-web",
		eventPages: []int{lokiEventsPageSize, 4},
	}
	p := sessionQueryParams{AppID: "66", SessionID: "sid"}
	got, err := fetchLokiSession(context.Background(), stub, "uid", p, time.Unix(0, 0), time.Unix(10, 0))
	require.NoError(t, err)
	assert.Equal(t, lokiEventsPageSize+4, lokiEntryCount(got.events))
	eventQueries := 0
	for i, q := range stub.queries {
		if strings.Contains(q, `kind="event"`) {
			continue
		}
		eventQueries++
		assert.Equal(t, lokiEventsPageSize, stub.limits[i])
	}
	assert.Equal(t, 2, eventQueries)
}

func TestSessionsGetCommandArgs(t *testing.T) {
	t.Parallel()
	cmd := newSessionsGetCommand(&providers.ConfigLoader{})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{})
	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "accepts 1 arg")
}

func TestSessionsGetCommandBlankSessionID(t *testing.T) {
	t.Parallel()
	cmd := newSessionsGetCommand(&providers.ConfigLoader{})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"   "})
	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "session-id is required")
}
