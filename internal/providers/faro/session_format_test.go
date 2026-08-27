package faro //nolint:testpackage // Tests unexported dump formatters.

import (
	"bytes"
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/query/loki"
	querysql "github.com/grafana/gcx/internal/query/sql"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFormatPinotTSV(t *testing.T) {
	t.Parallel()

	got := formatPinotTSV(&querysql.QueryResponse{
		Columns: []querysql.Column{{Name: "kind"}, {Name: "count"}},
		Rows: [][]any{
			{"event", float64(2)},
			{"exception", nil},
		},
	})
	assert.Equal(t, "kind\tcount\nevent\t2\nexception\t\n", got)
}

func TestFormatPinotTSVEmpty(t *testing.T) {
	t.Parallel()
	assert.Empty(t, formatPinotTSV(nil))
	assert.Equal(t, "kind\n", formatPinotTSV(&querysql.QueryResponse{
		Columns: []querysql.Column{{Name: "kind"}},
	}))
}

func TestFormatPinotTSVEscapesControlChars(t *testing.T) {
	t.Parallel()

	got := formatPinotTSV(&querysql.QueryResponse{
		Columns: []querysql.Column{{Name: "kind"}, {Name: "message"}},
		Rows: [][]any{
			{"exception", "line1\tline2\nline3"},
		},
	})
	assert.Equal(t, "kind\tmessage\nexception\tline1\\tline2\\nline3\n", got)

	lines := strings.Split(strings.TrimRight(got, "\n"), "\n")
	require.Len(t, lines, 2)
	assert.Equal(t, 1, strings.Count(lines[1], "\t"))
}

func TestFormatSessionDump(t *testing.T) {
	t.Parallel()
	got := formatSessionDump("meta-row", "e1\ne2")
	require.Contains(t, got, "=== session metadata ===\nmeta-row\n")
	require.Contains(t, got, "=== events ===\ne1\ne2\n")
}

func TestWritePinotTables(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	err := writePinotTables(&buf, &querysql.QueryResponse{
		Columns: []querysql.Column{{Name: "kind"}, {Name: "count"}},
		Rows:    [][]any{{"event", float64(2)}},
	})
	require.NoError(t, err)
	out := buf.String()
	assert.Contains(t, out, "KIND")
	assert.Contains(t, out, "COUNT")
	assert.Contains(t, out, "event")
}

func TestPinotSessionWriteTables(t *testing.T) {
	t.Parallel()
	result := &pinotSessionResult{
		eventsMeta: &querysql.QueryResponse{
			Columns: []querysql.Column{{Name: "browser_name"}},
			Rows:    [][]any{{"Chrome"}},
		},
		userMeta: &querysql.QueryResponse{
			Columns: []querysql.Column{{Name: "sdk_name"}},
			Rows:    [][]any{{"faro-web"}},
		},
		journey: &querysql.QueryResponse{
			Columns: []querysql.Column{{Name: "kind"}},
			Rows:    [][]any{{"event"}},
		},
	}
	var buf bytes.Buffer
	require.NoError(t, result.writeTables(&buf))
	out := buf.String()
	assert.Contains(t, out, "=== session metadata ===")
	assert.Contains(t, out, "=== events ===")
	assert.Contains(t, out, "BROWSER_NAME")
	assert.Contains(t, out, "SDK_NAME")
	assert.Contains(t, out, "Chrome")
	assert.Contains(t, out, "faro-web")
	assert.Contains(t, out, "kind\nevent\n")
}

func TestLokiSessionWriteTables(t *testing.T) {
	t.Parallel()
	result := &lokiSessionResult{
		metadata: "sdk_name=faro-web\nos_name=\"Mac OS\"\n",
		events: &loki.QueryResponse{
			Data: loki.QueryResultData{
				Result: []loki.StreamEntry{{
					Values: []loki.LogEntry{
						{Timestamp: "1", Line: "kind=event name=click"},
						{Timestamp: "2", Line: "kind=event name=nav"},
					},
				}},
			},
		},
	}
	var buf bytes.Buffer
	require.NoError(t, result.writeTables(&buf))
	out := buf.String()
	assert.Contains(t, out, "=== session metadata ===")
	assert.Contains(t, out, "=== events ===")
	assert.Contains(t, out, "1\tkind=event name=click\n\n")
	assert.Contains(t, out, "2\tkind=event name=nav")
	assert.NotContains(t, out, "MESSAGE")
	assert.NotContains(t, out, "STREAM")
}

func TestFormatLokiLines(t *testing.T) {
	t.Parallel()
	got := formatLokiLines(&loki.QueryResponse{
		Data: loki.QueryResultData{
			Result: []loki.StreamEntry{{
				Values: []loki.LogEntry{
					{Timestamp: "1", Line: "a=1"},
					{Timestamp: "2", Line: "a=2"},
				},
			}},
		},
	})
	assert.Equal(t, "1\ta=1\n\n2\ta=2\n\n", got)

	merged := formatLokiLines(&loki.QueryResponse{
		Data: loki.QueryResultData{
			Result: []loki.StreamEntry{
				{Values: []loki.LogEntry{
					{Timestamp: "300", Line: "kind=event name=later"},
					{Timestamp: "100", Line: "kind=event name=first"},
				}},
				{Values: []loki.LogEntry{
					{Timestamp: "200", Line: "kind=measurement type=fcp"},
					{Timestamp: "1000", Line: "kind=exception"},
					{Timestamp: "999", Line: "kind=log"},
				}},
			},
		},
	})
	assert.Equal(t, "100\tkind=event name=first\n\n200\tkind=measurement type=fcp\n\n300\tkind=event name=later\n\n999\tkind=log\n\n1000\tkind=exception\n\n", merged)

	stripped := formatLokiLines(&loki.QueryResponse{
		Data: loki.QueryResultData{
			Result: []loki.StreamEntry{{
				Values: []loki.LogEntry{{
					Timestamp: "3",
					Line:      `timestamp=2026-08-27T17:47:31.165Z kind=event event_name=faro.tracing.fetch page_url=https://ops.grafana-ops.net/ sdk_name=faro-web sdk_version=2.8.2 os_name="Mac OS" browser_name=Brave browser_brand_0_brand=Brave app_name=grafana-frontend user_id=u@x session_id=7TiMbCCvby session_attr_previousSession=abc event_data_session.id=7TiMbCCvby event_data_user.id=u@x event_data_url=https://ops.grafana-ops.net/`,
				}},
			}},
		},
	})
	assert.Contains(t, stripped, "kind=event")
	assert.Contains(t, stripped, "event_name=faro.tracing.fetch")
	assert.Contains(t, stripped, "page_url=")
	assert.Contains(t, stripped, "event_data_url=")
	assert.NotContains(t, stripped, "session_id=")
	assert.NotContains(t, stripped, "sdk_name=")
	assert.NotContains(t, stripped, "browser_name=")
	assert.NotContains(t, stripped, "browser_brand_0_brand=")
	assert.NotContains(t, stripped, "app_name=")
	assert.NotContains(t, stripped, "user_id=")
	assert.NotContains(t, stripped, "os_name=")
	assert.NotContains(t, stripped, "session_attr_previousSession=")
	assert.NotContains(t, stripped, "event_data_session.id=")
	assert.NotContains(t, stripped, "event_data_user.id=")

	mobile := formatLokiLines(&loki.QueryResponse{
		Data: loki.QueryResultData{
			Result: []loki.StreamEntry{{
				Values: []loki.LogEntry{{
					Timestamp: "4",
					Line:      `kind=event event_name=view_changed view_name=Home device_brand=Apple device_model_name=iPhone app_installation_id=inst-1 event_data_fromView=Login`,
				}},
			}},
		},
	})
	assert.Contains(t, mobile, "kind=event")
	assert.Contains(t, mobile, "event_name=view_changed")
	assert.Contains(t, mobile, "view_name=Home")
	assert.Contains(t, mobile, "event_data_fromView=Login")
	assert.NotContains(t, mobile, "device_brand=")
	assert.NotContains(t, mobile, "device_model_name=")
	assert.NotContains(t, mobile, "app_installation_id=")
	assert.Equal(t, "a=1", firstLokiLine(&loki.QueryResponse{
		Data: loki.QueryResultData{Result: []loki.StreamEntry{{
			Values: []loki.LogEntry{{Line: ""}, {Line: "a=1"}},
		}}},
	}))
	assert.Equal(t, "faro-mobile-flutter", logfmtValue(
		`sdk_name=faro-mobile-flutter sdk_version=0.16.0 event_name=view_changed`,
		"sdk_name",
	))
	assert.Equal(t, "Mac OS", logfmtValue(`os_name="Mac OS" sdk_name=faro-web`, "os_name"))
}

func TestFormatLokiMetadata(t *testing.T) {
	t.Parallel()
	meta := &loki.QueryResponse{Data: loki.QueryResultData{Result: []loki.StreamEntry{{
		Values: []loki.LogEntry{{
			Timestamp: "100",
			Line:      `sdk_name=faro-mobile-flutter sdk_version=0.16.0 app_version=2.44.1 session_id=kwwAkkXwas device_brand=Apple session_attr_previousSession=prev event_data_session.id=kwwAkkXwas event_name=view_changed kind=event view_name=Home`,
		}},
	}}}}
	replay := &loki.QueryResponse{Data: loki.QueryResultData{Result: []loki.StreamEntry{{
		Values: []loki.LogEntry{{Timestamp: "150", Line: "event_name=faro.session_recording.started"}},
	}}}}
	got := formatLokiMetadata(meta, replay)
	assert.Contains(t, got, "sdk_name=faro-mobile-flutter\n")
	assert.Contains(t, got, "sdk_version=0.16.0\n")
	assert.Contains(t, got, "app_version=2.44.1\n")
	assert.Contains(t, got, "session_id=kwwAkkXwas\n")
	assert.Contains(t, got, "device_brand=Apple\n")
	assert.Contains(t, got, "session_attr_previousSession=prev\n")
	assert.Contains(t, got, "session_replay_start=150\n")
	assert.NotContains(t, got, "event_name=")
	assert.NotContains(t, got, "kind=")
	assert.NotContains(t, got, "view_name=")
	assert.NotContains(t, got, "event_data_session.id=")
	assert.NotContains(t, got, "100\t")
}

func TestIsSessionMetadataKey(t *testing.T) {
	t.Parallel()
	for _, key := range []string{
		"sdk_name", "app_installation_id", "user_email", "os_version",
		"geo_city", "browser_brand_0_brand", "device_battery_level",
		"session_attr_previousSession", "session_id", "event_data_session.id",
		"event_data_user.id", "faro_sdk_version",
	} {
		assert.True(t, isSessionMetadataKey(key), key)
	}
	for _, key := range []string{
		"kind", "event_name", "level", "timestamp", "traceID", "spanID",
		"event_data_url", "page_url", "page_attr_title", "view_name",
		"event_data_toView", "event_data_fromView",
	} {
		assert.False(t, isSessionMetadataKey(key), key)
	}
}
