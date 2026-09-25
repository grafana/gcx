package faro //nolint:testpackage // Exercises the unexported replay command and file writer.

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSessionsGetReplaySavesViewerDefaultAsOneFile(t *testing.T) {
	testutils.SetAgentMode(t, true)
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		assert.Equal(t, "42", r.URL.Query().Get("app_id"))
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/sess-1/recordings"):
			assert.Equal(t, "1", r.URL.Query().Get("limit"))
			_, _ = w.Write([]byte(`{"session_id":"sess-1","items":[{"id":"newest","status":"finished"}],"page":{"hasNext":true,"totalItems":2}}`))
		case strings.HasSuffix(r.URL.Path, "/newest/manifest"):
			_, _ = w.Write([]byte(`{"id":"newest","session_id":"sess-1","status":"finished","segments":[{"id":0},{"id":3}]}`))
		case strings.HasSuffix(r.URL.Path, "/newest/segments/0"):
			_, _ = w.Write([]byte(`{"id":"0","recording_id":"newest","events":[{"type":4,"timestamp":1000,"data":{"href":"/"}}]}`))
		case strings.HasSuffix(r.URL.Path, "/newest/segments/3"):
			_, _ = w.Write([]byte(`{"id":"3","recording_id":"newest","events":[{"type":3,"timestamp":2000,"data":{"source":2}}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	path := filepath.Join(t.TempDir(), "replay.json")
	cmd := newSessionsGetReplayCommand(&fakeConfigLoader{grafanaURL: server.URL})
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetArgs([]string{"sess-1", "--app", "my-app-42", "--save", path})
	require.NoError(t, cmd.Execute())

	var receipt replayArtifactReceipt
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &receipt))
	assert.Equal(t, 2, receipt.EventCount)
	assert.Equal(t, server.URL+"/a/grafana-sessionreplay-app/app/42/session/sess-1?recording_id=newest", receipt.ReplayURL)
	assert.Equal(t, path, receipt.Files[0].Path)
	assert.Len(t, paths, 4)
	for _, requestedPath := range paths {
		assert.NotContains(t, requestedPath, "older")
	}

	contents, err := os.ReadFile(path)
	require.NoError(t, err)
	var events []RRWebEvent
	require.NoError(t, json.Unmarshal(contents, &events))
	require.Len(t, events, 2)
	assert.Equal(t, int64(1000), events[0].Timestamp)
	assert.Equal(t, int64(2000), events[1].Timestamp)
	if runtime.GOOS != "windows" {
		info, statErr := os.Stat(path)
		require.NoError(t, statErr)
		assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	}
}

func TestSessionsGetReplaySegmentFailurePreservesDestination(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/sess-1/recordings"):
			_, _ = w.Write([]byte(`{"items":[{"id":"rec-1"}],"page":{}}`))
		case strings.HasSuffix(r.URL.Path, "/rec-1/manifest"):
			_, _ = w.Write([]byte(`{"id":"rec-1","session_id":"sess-1","segments":[{"id":0},{"id":1}]}`))
		case strings.HasSuffix(r.URL.Path, "/rec-1/segments/0"):
			_, _ = w.Write([]byte(`{"id":"0","recording_id":"rec-1","events":[{"type":4,"timestamp":1000,"data":{}}]}`))
		default:
			http.Error(w, "unavailable", http.StatusServiceUnavailable)
		}
	}))
	t.Cleanup(server.Close)

	path := filepath.Join(t.TempDir(), "replay.json")
	require.NoError(t, os.WriteFile(path, []byte("original"), 0o600))
	cmd := newSessionsGetReplayCommand(&fakeConfigLoader{grafanaURL: server.URL})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"sess-1", "--app", "42", "--save", path})
	require.ErrorContains(t, cmd.Execute(), "fetching replay segment 1")
	contents, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "original", string(contents))
}

func TestSessionsGetReplayValidatesFlagsBeforeIO(t *testing.T) {
	tests := []struct {
		args    []string
		wantErr string
	}{
		{args: []string{"sess-1", "--save", "replay.json"}, wantErr: "--app is required"},
		{args: []string{"sess-1", "--app", "42"}, wantErr: "--save is required"},
		{args: []string{"", "--app", "42", "--save", "replay.json"}, wantErr: "non-empty session ID"},
	}
	for _, tt := range tests {
		cmd := newSessionsGetReplayCommand(&fakeConfigLoader{})
		cmd.SetOut(&bytes.Buffer{})
		cmd.SetErr(&bytes.Buffer{})
		cmd.SetArgs(tt.args)
		require.ErrorContains(t, cmd.Execute(), tt.wantErr)
	}
}
