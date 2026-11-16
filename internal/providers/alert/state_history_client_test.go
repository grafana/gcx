package alert_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/grafana/gcx/internal/providers/alert"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stateHistoryFrameJSON is a minimal but faithful reproduction of the bare
// Grafana data frame returned by GET /api/v1/rules/history: three columns
// (time in epoch ms, the JSON "line", and the JSON stream "labels"). Row 0 is
// older than row 1 so ordering (newest-first) is observable.
const stateHistoryFrameJSON = `{
  "schema": {
    "name": "states",
    "fields": [
      {"name": "time", "type": "time"},
      {"name": "line", "type": "other"},
      {"name": "labels", "type": "other"}
    ]
  },
  "data": {
    "values": [
      [1727189670000, 1727189680000],
      [
        {"schemaVersion":1,"previous":"Alerting","current":"Normal","ruleUID":"uid-1","ruleTitle":"CPU Usage","fingerprint":"abc123","dashboardUID":"dash-1","panelID":3,"labels":{"alertname":"CPU Usage","severity":"critical"}},
        {"schemaVersion":1,"previous":"Normal","current":"Alerting","ruleUID":"uid-1","ruleTitle":"CPU Usage","values":{"B":42},"labels":{"alertname":"CPU Usage","severity":"critical"}}
      ],
      [
        {"alertname":"CPU Usage"},
        {"alertname":"CPU Usage"}
      ]
    ]
  }
}`

func TestClient_QueryStateHistory(t *testing.T) {
	var gotQuery url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/api/v1/rules/history", r.URL.Path)
		gotQuery = r.URL.Query()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(stateHistoryFrameJSON))
	}))
	defer srv.Close()

	client := newTestClient(t, srv)
	transitions, err := client.QueryStateHistory(context.Background(), alert.StateHistoryOptions{
		RuleUID: "uid-1",
		From:    time.Unix(1727189000, 0),
		To:      time.Unix(1727190000, 0),
		Limit:   50,
		Labels:  map[string]string{"severity": "critical"},
	})
	require.NoError(t, err)

	// Query parameters are translated to the alerting API vocabulary.
	assert.Equal(t, "uid-1", gotQuery.Get("ruleUID"))
	assert.Equal(t, "1727189000", gotQuery.Get("from"))
	assert.Equal(t, "1727190000", gotQuery.Get("to"))
	assert.Equal(t, "50", gotQuery.Get("limit"))
	assert.Equal(t, "critical", gotQuery.Get("labels_severity"))

	// Records are flattened and ordered newest-first.
	require.Len(t, transitions, 2)
	assert.True(t, transitions[0].Time.After(transitions[1].Time), "transitions must be newest-first")

	newest := transitions[0] // row 1 (1727189680000)
	assert.Equal(t, "uid-1", newest.RuleUID)
	assert.Equal(t, "CPU Usage", newest.RuleTitle)
	assert.Equal(t, "Normal", newest.Previous)
	assert.Equal(t, "Alerting", newest.Current)
	assert.Equal(t, map[string]string{"alertname": "CPU Usage", "severity": "critical"}, newest.Labels)
	assert.Equal(t, int64(1727189680000), newest.Time.UnixMilli())

	oldest := transitions[1] // row 0 (1727189670000)
	assert.Equal(t, "Normal", oldest.Current)
	assert.Equal(t, "dash-1", oldest.DashboardUID)
	assert.Equal(t, int64(3), oldest.PanelID)
	assert.Equal(t, "abc123", oldest.Fingerprint)
}

func TestClient_QueryStateHistory_OmitsUnsetParams(t *testing.T) {
	var gotQuery url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"schema":{"fields":[]},"data":{"values":[]}}`))
	}))
	defer srv.Close()

	client := newTestClient(t, srv)
	transitions, err := client.QueryStateHistory(context.Background(), alert.StateHistoryOptions{})
	require.NoError(t, err)
	assert.Empty(t, transitions)

	// A fully-zero query sends no parameters at all.
	assert.Empty(t, gotQuery)
}

func TestClient_QueryStateHistory_Error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"message":"ruleUID is required to query annotations"}`))
	}))
	defer srv.Close()

	client := newTestClient(t, srv)
	_, err := client.QueryStateHistory(context.Background(), alert.StateHistoryOptions{})
	require.Error(t, err)
}

// annotationsStateHistoryFrameJSON reproduces the frame that the annotations
// state history backend returns: time, free-form text, prev, next, and the
// annotation data as a JSON string, with the rule UID in the field labels.
const annotationsStateHistoryFrameJSON = `{
  "schema": {
    "name": "states",
    "fields": [
      {"name": "time", "type": "time", "labels": {"from": "state-history", "ruleUID": "uid-1"}},
      {"name": "text", "type": "string", "labels": {"from": "state-history", "ruleUID": "uid-1"}},
      {"name": "prev", "type": "string", "labels": {"from": "state-history", "ruleUID": "uid-1"}},
      {"name": "next", "type": "string", "labels": {"from": "state-history", "ruleUID": "uid-1"}},
      {"name": "data", "type": "string", "labels": {"from": "state-history", "ruleUID": "uid-1"}}
    ]
  },
  "data": {
    "values": [
      [1792582450000, 1792583050000, 1792582750000],
      [
        "MyAlertRule {alertname=MyAlertRule, severity=warning} - A=1.000000",
        "MyAlertRule {alertname=MyAlertRule, severity=warning} - A=-1.000000",
        "MyAlertRule {alertname=MyAlertRule, severity=warning} - Error"
      ],
      ["Normal", "Alerting", "Pending"],
      ["Pending", "Normal (NoData)", "Error"],
      ["{\"values\":{\"A\":1}}", "{\"values\":{\"A\":-1}}", "{\"error\":\"query failed\"}"]
    ]
  }
}`

func TestClient_QueryStateHistory_AnnotationsBackend(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(annotationsStateHistoryFrameJSON))
	}))
	defer srv.Close()

	client := newTestClient(t, srv)
	transitions, err := client.QueryStateHistory(context.Background(), alert.StateHistoryOptions{RuleUID: "uid-1"})
	require.NoError(t, err)
	require.Len(t, transitions, 3)

	tests := []struct {
		name     string
		got      alert.StateTransition
		timeMs   int64
		previous string
		current  string
		values   map[string]any
		errMsg   string
	}{
		{"newest", transitions[0], 1792583050000, "Alerting", "Normal (NoData)", map[string]any{"A": float64(-1)}, ""},
		{"error", transitions[1], 1792582750000, "Pending", "Error", nil, "query failed"},
		{"oldest", transitions[2], 1792582450000, "Normal", "Pending", map[string]any{"A": float64(1)}, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.timeMs, tc.got.Time.UnixMilli())
			assert.Equal(t, "uid-1", tc.got.RuleUID)
			assert.Equal(t, tc.previous, tc.got.Previous)
			assert.Equal(t, tc.current, tc.got.Current)
			assert.Equal(t, tc.values, tc.got.Values)
			assert.Equal(t, tc.errMsg, tc.got.Error)
		})
	}
}

func TestClient_QueryStateHistory_AnnotationsBackendNullData(t *testing.T) {
	const frame = `{
  "schema": {"fields": [
    {"name": "time", "type": "time"},
    {"name": "text", "type": "string"},
    {"name": "prev", "type": "string"},
    {"name": "next", "type": "string"},
    {"name": "data", "type": "string"}
  ]},
  "data": {"values": [[1792583050000], ["MyAlertRule {} - No data"], ["Normal"], ["NoData"], ["null"]]}
}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(frame))
	}))
	defer srv.Close()

	client := newTestClient(t, srv)
	transitions, err := client.QueryStateHistory(context.Background(), alert.StateHistoryOptions{RuleUID: "uid-1"})
	require.NoError(t, err)
	require.Len(t, transitions, 1)
	assert.Equal(t, "Normal", transitions[0].Previous)
	assert.Equal(t, "NoData", transitions[0].Current)
	assert.Empty(t, transitions[0].RuleUID)
	assert.Nil(t, transitions[0].Values)
}
