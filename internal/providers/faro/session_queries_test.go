package faro //nolint:testpackage // Tests unexported SQL/LogQL builders.

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPinotJourneyQuery(t *testing.T) {
	t.Parallel()

	p := sessionQueryParams{AppID: "66", SessionID: "7TiMbCCvby", AppType: appTypeWeb}
	sql, err := pinotJourneyQuery(p)
	require.NoError(t, err)

	assert.Contains(t, sql, "appId = 66")
	assert.Contains(t, sql, "sessionId = '7TiMbCCvby'")
	assert.Contains(t, sql, "$__timeFilter(\"timestamp\")")
	assert.Contains(t, sql, "SET useMultistageEngine = true;")
	assert.Contains(t, sql, "faro_pinot_measurements_v1")
	assert.Contains(t, sql, "faro_pinot_exceptions_v1")
	assert.Contains(t, sql, "faro_pinot_events_v2")
	assert.NotContains(t, sql, "{{APP_ID}}")
	assert.NotContains(t, sql, "{{SESSION_ID}}")
	assert.NotContains(t, sql, "{{MEASUREMENT_FILTER}}")
	assert.NotContains(t, sql, "{{MEASUREMENT_FILTER}}")
	assert.NotContains(t, sql, "measurementType NOT IN")
	assert.NotRegexp(t, `(?i)\bLIMIT\b`, sql)
	assert.NotContains(t, sql, "sdk_name")
	assert.NotContains(t, sql, "app_name")
	assert.NotContains(t, sql, "app_environment")
}

func TestPinotJourneyQueryMobileFilter(t *testing.T) {
	t.Parallel()

	p := sessionQueryParams{AppID: "96", SessionID: "kwwAkkXwas", AppType: appTypeMobile}
	sql, err := pinotJourneyQuery(p)
	require.NoError(t, err)

	assert.Contains(t, sql, "AND sessionId = 'kwwAkkXwas' AND measurementType NOT IN ('app_memory', 'app_cpu_usage')")
	assert.Contains(t, sql, "appId = 96")
}

func TestPinotQueryEscapesSessionID(t *testing.T) {
	t.Parallel()

	p := sessionQueryParams{AppID: "1", SessionID: "O'Brien", AppType: appTypeWeb}
	sql, err := pinotEventsMetadataQuery(p)
	require.NoError(t, err)
	assert.Contains(t, sql, "sessionId = 'O''Brien'")
}

func TestPinotQueryRejectsNonIntegerAppID(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		appID string
	}{
		{name: "statement injection", appID: "66; DROP TABLE events"},
		{name: "or-clause", appID: "66 OR 1=1"},
		{name: "quoted payload", appID: "1' OR '1'='1"},
		{name: "empty", appID: ""},
		{name: "letters", appID: "not-a-number"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			p := sessionQueryParams{AppID: tt.appID, SessionID: "sid", AppType: appTypeWeb}
			sql, err := pinotEventsMetadataQuery(p)
			require.Error(t, err)
			assert.Empty(t, sql)
			assert.Contains(t, err.Error(), "must be an integer")
			assert.NotContains(t, sql, "DROP")
			assert.NotContains(t, sql, "OR 1=1")
		})
	}
}

func TestPinotMetadataQueries(t *testing.T) {
	t.Parallel()

	p := sessionQueryParams{AppID: "66", SessionID: "abc", AppType: appTypeWeb}
	eventsSQL, err := pinotEventsMetadataQuery(p)
	require.NoError(t, err)
	userSQL, err := pinotUserMetadataQuery(p)
	require.NoError(t, err)
	assert.Contains(t, eventsSQL, "session_replay_start")
	assert.Contains(t, userSQL, "faro_pinot_measurements_v1")
	assert.Contains(t, userSQL, "userEmail")
	assert.Contains(t, userSQL, "user_email")
	assert.Contains(t, userSQL, "sdkName")
	assert.Contains(t, userSQL, "device_model_name")
	assert.NotContains(t, eventsSQL, "appNamespace")
}

func TestLokiQueries(t *testing.T) {
	t.Parallel()

	web := sessionQueryParams{AppID: "66", SessionID: "7TiMbCCvby", AppType: appTypeWeb}
	meta := lokiMetadataQuery(web)
	require.Contains(t, meta, `{app_id="66", kind="event"}`)
	require.Contains(t, meta, `|= "session_id=7TiMbCCvby"`)
	require.Contains(t, meta, `| logfmt | session_id="7TiMbCCvby"`)
	require.NotContains(t, meta, "line_format")
	require.NotContains(t, meta, "|~|")

	eventQ := lokiEventsQueryForKind(web, lokiKindEvent)
	assert.Contains(t, eventQ, `{app_id="66", kind="event"}`)
	assert.Contains(t, eventQ, `|= "session_id=7TiMbCCvby"`)
	assert.Contains(t, eventQ, `| logfmt`)
	assert.NotContains(t, eventQ, `| logfmt | session_id=`)
	assert.NotContains(t, eventQ, "app_memory")
	assert.Equal(t, lokiSessionEventKinds, []string{lokiKindEvent, lokiKindException, lokiKindLog, lokiKindMeasurement})
	for _, kind := range lokiSessionEventKinds {
		q := lokiEventsQueryForKind(web, kind)
		assert.Contains(t, q, fmt.Sprintf(`kind="%s"`, kind))
		assert.NotContains(t, q, `{app_id="66"} |=`)
	}
	assert.NotContains(t, lokiEventsQueryForKind(web, lokiKindMeasurement), "app_memory")

	mobile := sessionQueryParams{AppID: "96", SessionID: `id"x`, AppType: appTypeMobile}
	mobileMeas := lokiEventsQueryForKind(mobile, lokiKindMeasurement)
	assert.Contains(t, mobileMeas, `app_id="96"`)
	assert.Contains(t, mobileMeas, `kind="measurement"`)
	assert.Contains(t, mobileMeas, `session_id=id\"x`)
	assert.Contains(t, mobileMeas, `type!="app_memory"`)
	assert.Contains(t, mobileMeas, `type!="app_cpu_usage"`)
	assert.NotContains(t, lokiEventsQueryForKind(mobile, lokiKindEvent), "app_memory")
	replay := lokiReplayStartQuery(web)
	assert.Contains(t, replay, "faro.session_recording.started")
	assert.Contains(t, replay, `| session_id="7TiMbCCvby"`)
}

func TestInferAppType(t *testing.T) {
	t.Parallel()
	assert.Equal(t, appTypeWeb, inferAppType("faro-web", "Mac OS"))
	assert.Equal(t, appTypeMobile, inferAppType("@grafana/faro-react-native", ""))
	assert.Equal(t, appTypeMobile, inferAppType("faro-flutter", ""))
	assert.Equal(t, appTypeMobile, inferAppType("", "iOS"))
	assert.Equal(t, appTypeMobile, inferAppType("", "Android"))
	assert.Equal(t, appTypeWeb, inferAppType("", ""))
}
