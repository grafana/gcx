package faro_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	faro "github.com/grafana/gcx/internal/providers/faro"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestPlayerServer(t *testing.T, eventsJSON json.RawMessage) *faro.SessionPlayerServer {
	t.Helper()
	srv, err := faro.NewSessionPlayerServer(eventsJSON, "2.0.0-alpha.17")
	require.NoError(t, err)
	return srv
}

func TestValidateEventStream_Valid(t *testing.T) {
	events := []faro.RRWebEvent{
		{Type: 4, Timestamp: 1000, Data: json.RawMessage(`{}`)},
		{Type: 2, Timestamp: 1001, Data: json.RawMessage(`{}`)},
		{Type: 3, Timestamp: 1002, Data: json.RawMessage(`{}`)},
	}
	var buf bytes.Buffer
	faro.ValidateEventStream(events, &buf)
	assert.Empty(t, buf.String())
}

func TestValidateEventStream_MissingMeta(t *testing.T) {
	events := []faro.RRWebEvent{
		{Type: 2, Timestamp: 1000, Data: json.RawMessage(`{}`)},
	}
	var buf bytes.Buffer
	faro.ValidateEventStream(events, &buf)
	assert.Contains(t, buf.String(), "Meta")
}

func TestValidateEventStream_MissingFullSnapshot(t *testing.T) {
	events := []faro.RRWebEvent{
		{Type: 4, Timestamp: 1000, Data: json.RawMessage(`{}`)},
		{Type: 3, Timestamp: 1001, Data: json.RawMessage(`{}`)},
	}
	var buf bytes.Buffer
	faro.ValidateEventStream(events, &buf)
	assert.Contains(t, buf.String(), "FullSnapshot")
}

func TestValidateEventStream_Empty(t *testing.T) {
	var buf bytes.Buffer
	faro.ValidateEventStream(nil, &buf)
	assert.Contains(t, buf.String(), "empty")
}

func TestSessionPlayerServer_Events(t *testing.T) {
	events := []faro.RRWebEvent{{Type: 4, Timestamp: 1000, Data: json.RawMessage(`{}`)}}
	eventsJSON, err := json.Marshal(events)
	require.NoError(t, err)

	srv := newTestPlayerServer(t, eventsJSON)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, ts.URL+"/api/events", nil)
	require.NoError(t, err)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "application/json", resp.Header.Get("Content-Type"))

	var got []faro.RRWebEvent
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&got))
	assert.Len(t, got, 1)
	assert.Equal(t, 4, got[0].Type)
}

func TestSessionPlayerServer_State(t *testing.T) {
	srv := newTestPlayerServer(t, json.RawMessage(`[]`))
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, ts.URL+"/api/state", nil)
	require.NoError(t, err)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var state map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&state))
	assert.Contains(t, state, "timestamp")
}

func TestSessionPlayerServer_Control(t *testing.T) {
	srv := newTestPlayerServer(t, json.RawMessage(`[]`))
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws"
	ws, wsResp, err := websocket.DefaultDialer.Dial(wsURL, nil)
	require.NoError(t, err)
	defer ws.Close()
	if wsResp != nil {
		wsResp.Body.Close()
	}

	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, ts.URL+"/api/play", nil)
	require.NoError(t, err)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	require.NoError(t, ws.SetReadDeadline(time.Now().Add(2*time.Second)))
	_, msg, err := ws.ReadMessage()
	require.NoError(t, err)

	var cmd map[string]any
	require.NoError(t, json.Unmarshal(msg, &cmd))
	assert.Equal(t, "play", cmd["action"])
}

func TestPlaySessionCommandRegistered(t *testing.T) {
	p := &faro.FaroProvider{}
	cmds := p.Commands()
	require.Len(t, cmds, 1)

	frontendCmd := cmds[0]
	appsCmd, _, err := frontendCmd.Find([]string{"apps"})
	require.NoError(t, err)

	found := false
	for _, sub := range appsCmd.Commands() {
		if sub.Name() == "play-session" {
			found = true
			break
		}
	}
	assert.True(t, found, "play-session command should be registered under apps")
}
