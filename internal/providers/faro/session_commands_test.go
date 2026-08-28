package faro //nolint:testpackage // Tests unexported opts, fetch, and command constructors.

import (
	"bytes"
	"context"
	"strconv"
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
		{name: "missing app", ds: "grafanacloud-logs", wantErr: "--app is required"},
		{name: "bad app type", app: "66", appType: "native", ds: "grafanacloud-logs", since: "1h", wantErr: "--app-type"},
		{name: "missing datasource", app: "66", since: "1h", wantErr: "--datasource is required"},
		{name: "missing time", app: "66", ds: "grafanacloud-logs", wantErr: "--since or --from/--to is required"},
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
		Datasource: "grafanacloud-pinot",
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
		Datasource: "  c-R8UWvVk  ",
		Save:       " /tmp/session.txt ",
		TimeRangeOpts: dsquery.TimeRangeOpts{
			Since: "7d",
		},
	}
	require.NoError(t, opts.Validate())
	assert.Equal(t, "my-app-66", opts.App)
	assert.Equal(t, appTypeMobile, opts.AppType)
	assert.Equal(t, "c-R8UWvVk", opts.Datasource)
	assert.Equal(t, "/tmp/session.txt", opts.Save)
	assert.Equal(t, "66", resolveAppID(opts.App))
}

func TestSessionsGetOptsValidateDoesNotLowercaseUID(t *testing.T) {
	t.Parallel()
	opts := sessionsGetOpts{
		App:        "66",
		Datasource: "  c-R8UWvVk  ",
		TimeRangeOpts: dsquery.TimeRangeOpts{
			Since: "7d",
		},
	}
	require.NoError(t, opts.Validate())
	assert.Equal(t, "c-R8UWvVk", opts.Datasource)
}

func TestSessionKindFromDatasourceType(t *testing.T) {
	t.Parallel()
	tests := []struct {
		pluginID string
		want     string
		wantErr  string
	}{
		{pluginID: "loki", want: datasourceLoki},
		{pluginID: "startree-pinot-datasource", want: datasourcePinot},
		{pluginID: "clickhouse", wantErr: "not loki or pinot"},
		{pluginID: "grafana-clickhouse-datasource", wantErr: "not loki or pinot"},
		{pluginID: "", wantErr: "not loki or pinot"},
	}
	for _, tt := range tests {
		t.Run(tt.pluginID, func(t *testing.T) {
			t.Parallel()
			got, err := sessionKindFromDatasourceType(tt.pluginID)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				assert.Empty(t, got)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestSessionsGetOptsValidateAgentRequiresSave(t *testing.T) {
	testutils.SetAgentMode(t, true)
	base := sessionsGetOpts{
		App:        "66",
		Datasource: "grafanacloud-logs",
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
	defer s.mu.Unlock()
	s.queries = append(s.queries, req.Query)
	s.dirs = append(s.dirs, req.Direction)
	s.limits = append(s.limits, req.Limit)
	if s.empty {
		return &loki.QueryResponse{}, nil
	}

	isJourney := strings.Contains(req.Query, `!~ "performanceEntry`)
	switch {
	case strings.Contains(req.Query, "faro.session_recording.started"):
		return lokiSingle("150", "event_name=faro.session_recording.started"), nil
	case isJourney && strings.Contains(req.Query, `kind="event"`):
		page := s.eventCalls
		s.eventCalls++
		n := 1
		if page < len(s.eventPages) {
			n = s.eventPages[page]
		}
		ts := strconv.FormatInt(time.Unix(9-int64(page), 0).UnixNano(), 10)
		values := make([]loki.LogEntry, n)
		for i := range values {
			values[i] = loki.LogEntry{Timestamp: ts, Line: "kind=event"}
		}
		return &loki.QueryResponse{Data: loki.QueryResultData{Result: []loki.StreamEntry{{
			Values: values,
		}}}}, nil
	case isJourney:
		line := "kind=log"
		switch {
		case strings.Contains(req.Query, `kind="exception"`):
			line = "kind=exception"
		case strings.Contains(req.Query, `kind="measurement"`):
			line = "kind=measurement"
		}
		return lokiSingle("200", line), nil
	default:
		line := "meta"
		if s.metaLine != "" {
			line = s.metaLine
		}
		return lokiSingle("100", line), nil
	}
}

func lokiSingle(ts, line string) *loki.QueryResponse {
	return &loki.QueryResponse{Data: loki.QueryResultData{Result: []loki.StreamEntry{{
		Values: []loki.LogEntry{{Timestamp: ts, Line: line}},
	}}}}
}

func lokiJourneyQueryFrom(queries []string) string {
	for _, q := range queries {
		if strings.Contains(q, `!~ "performanceEntry`) && strings.Contains(q, `kind="event"`) {
			return q
		}
	}
	return ""
}

func lokiMeasurementQueryFrom(queries []string) string {
	for _, q := range queries {
		if strings.Contains(q, `kind="measurement"`) && strings.Contains(q, `!~ "performanceEntry`) {
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
	require.Len(t, stub.queries, 6)
	joined := queryJoined(stub.queries)
	assert.NotContains(t, joined, "line_format")
	assert.Contains(t, joined, "faro.session_recording.started")
	assert.Contains(t, joined, "logfmt")
	events := lokiJourneyQueryFrom(stub.queries)
	require.NotEmpty(t, events)
	assert.Contains(t, events, `{app_id="66", kind="event"}`)
	assert.Contains(t, events, `| logfmt`)
	assert.NotContains(t, events, `| logfmt | session_id=`)
	assert.NotContains(t, events, "faro.tracing.fetch")
	assert.NotContains(t, events, "app_memory")
	for i, dir := range stub.dirs {
		q := stub.queries[i]
		if strings.Contains(q, `!~ "performanceEntry`) {
			assert.Empty(t, dir)
			continue
		}
		assert.Equal(t, lokiQueryDirectionForward, dir)
	}
}

func TestFetchLokiSessionInfersMobile(t *testing.T) {
	t.Parallel()
	stub := &stubLoki{metaLine: "sdk_name=@grafana/faro-react-native os_name=iOS"}
	p := sessionQueryParams{AppID: "96", SessionID: "sid"}
	_, err := fetchLokiSession(context.Background(), stub, "uid", p, time.Unix(0, 0), time.Unix(1, 0))
	require.NoError(t, err)
	require.Len(t, stub.queries, 6)
	assert.Contains(t, lokiMeasurementQueryFrom(stub.queries), `type!="app_memory"`)
	assert.Contains(t, lokiMeasurementQueryFrom(stub.queries), `type!="app_cpu_usage"`)
}

func TestFetchLokiSessionEmpty(t *testing.T) {
	t.Parallel()
	stub := &stubLoki{empty: true}
	p := sessionQueryParams{AppID: "66", SessionID: "missing"}
	_, err := fetchLokiSession(context.Background(), stub, "uid", p, time.Unix(0, 0), time.Unix(1, 0))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no telemetry for session missing")
}

type hangLoki struct{}

func (hangLoki) Query(ctx context.Context, _ string, _ loki.QueryRequest) (*loki.QueryResponse, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

func TestFetchLokiSessionTimeoutExits(t *testing.T) {
	orig := sessionLokiQueryTimeout
	sessionLokiQueryTimeout = 20 * time.Millisecond
	t.Cleanup(func() { sessionLokiQueryTimeout = orig })

	p := sessionQueryParams{AppID: "67", SessionID: "4JPV1T7Nyi"}
	_, err := fetchLokiSession(context.Background(), hangLoki{}, "uid", p, time.Unix(0, 0), time.Unix(1, 0))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no telemetry for session 4JPV1T7Nyi")
	assert.Contains(t, err.Error(), "timed out")
	assert.Contains(t, err.Error(), "try a Pinot datasource UID")
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
	otherKinds := 3 // exception, log, measurement each return 1 row
	assert.Equal(t, lokiEventsPageSize+4+otherKinds, lokiEntryCount(got.events))
	eventKindPages := 0
	for i, q := range stub.queries {
		if !strings.Contains(q, `!~ "performanceEntry`) || !strings.Contains(q, `kind="event"`) {
			continue
		}
		eventKindPages++
		assert.Equal(t, lokiEventsPageSize, stub.limits[i])
		assert.Empty(t, stub.dirs[i])
	}
	assert.Equal(t, 2, eventKindPages)
}

func TestSessionsGetCommandRequiresDatasource(t *testing.T) {
	t.Parallel()
	cmd := newSessionsGetCommand(&providers.ConfigLoader{})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"7TiMbCCvby", "--app", "66", "--since", "7d"})
	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--datasource is required")
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
