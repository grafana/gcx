package faro //nolint:testpackage // Exercises the unexported replay command and file writer.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/grafana/gcx/internal/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSessionsGetReplayBundlesAllRecordingsAsOneFile(t *testing.T) {
	testutils.SetAgentMode(t, true)
	var paths []string
	var pathsMu sync.Mutex
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		pathsMu.Lock()
		paths = append(paths, r.URL.Path)
		pathsMu.Unlock()
		assert.Equal(t, "42", r.URL.Query().Get("app_id"))
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/sess-1/recordings"):
			assert.Equal(t, "50", r.URL.Query().Get("limit"))
			if r.URL.Query().Get("page") == "next" {
				_, _ = w.Write([]byte(`{"session_id":"sess-1","items":[{"id":"older","status":"finished"}],"page":{"hasNext":false,"totalItems":2}}`))
			} else {
				_, _ = w.Write([]byte(`{"session_id":"sess-1","items":[{"id":"newest","status":"finished"}],"page":{"hasNext":true,"next":"next","totalItems":2}}`))
			}
		case strings.HasSuffix(r.URL.Path, "/newest/manifest"):
			_, _ = w.Write([]byte(`{"id":"newest","session_id":"sess-1","status":"finished","segments":[{"id":0},{"id":3}]}`))
		case strings.HasSuffix(r.URL.Path, "/older/manifest"):
			_, _ = w.Write([]byte(`{"id":"older","session_id":"sess-1","status":"finished","segments":[{"id":0}]}`))
		case strings.HasSuffix(r.URL.Path, "/newest/segments/0"):
			_, _ = w.Write([]byte(`{"id":"0","recording_id":"newest","events":[{"type":4,"timestamp":1000,"data":{"href":"/"}}]}`))
		case strings.HasSuffix(r.URL.Path, "/newest/segments/3"):
			_, _ = w.Write([]byte(`{"id":"3","recording_id":"newest","events":[{"type":3,"timestamp":2000,"data":{"source":2}}]}`))
		case strings.HasSuffix(r.URL.Path, "/older/segments/0"):
			_, _ = w.Write([]byte(`{"id":"0","recording_id":"older","events":[{"type":4,"timestamp":1500,"data":{"href":"/older"},"extra":"preserved"}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	path := filepath.Join(t.TempDir(), "replay.json")
	cmd := newSessionsGetReplayCommand(&fakeConfigLoader{grafanaURL: server.URL, deepLinkURL: "https://public.example.net"})
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetArgs([]string{"sess-1", "--app", "my-app-42", "--save", path})
	require.NoError(t, cmd.Execute())

	var receipt replayArtifactReceipt
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &receipt))
	assert.Equal(t, 3, receipt.EventCount)
	require.Len(t, receipt.Files, 1)
	assert.Equal(t, 2, receipt.Files[0].Count)
	assert.Equal(t, "https://public.example.net/a/grafana-sessionreplay-app/app/42/session/sess-1", receipt.ReplayURL)
	assert.Equal(t, path, receipt.Files[0].Path)
	assert.Len(t, paths, 7)

	contents, err := os.ReadFile(path)
	require.NoError(t, err)
	var bundle struct {
		AppID      string `json:"app_id"`
		SessionID  string `json:"session_id"`
		Recordings []struct {
			ID     string       `json:"id"`
			Events []RRWebEvent `json:"events"`
		} `json:"recordings"`
	}
	require.NoError(t, json.Unmarshal(contents, &bundle))
	assert.Equal(t, "42", bundle.AppID)
	assert.Equal(t, "sess-1", bundle.SessionID)
	require.Len(t, bundle.Recordings, 2)
	assert.Equal(t, "newest", bundle.Recordings[0].ID)
	assert.Equal(t, "older", bundle.Recordings[1].ID)
	assert.JSONEq(t, `{"type":4,"timestamp":1000,"data":{"href":"/"}}`, string(bundle.Recordings[0].Events[0]))
	assert.JSONEq(t, `{"type":3,"timestamp":2000,"data":{"source":2}}`, string(bundle.Recordings[0].Events[1]))
	assert.JSONEq(t, `{"type":4,"timestamp":1500,"data":{"href":"/older"},"extra":"preserved"}`, string(bundle.Recordings[1].Events[0]))
	assert.Contains(t, string(contents), `"extra":"preserved"`)
	if runtime.GOOS != "windows" {
		info, statErr := os.Stat(path)
		require.NoError(t, statErr)
		assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	}
}

func TestSessionsGetReplayFetchesBoundedSegmentsInManifestOrder(t *testing.T) {
	const segmentCount = 12
	var active, maxActive, started atomic.Int32
	barrier := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/sess-1/recordings"):
			_, _ = w.Write([]byte(`{"items":[{"id":"rec-1"}],"page":{}}`))
		case strings.HasSuffix(r.URL.Path, "/rec-1/manifest"):
			segments := make([]ManifestSegment, segmentCount)
			for i := range segments {
				segments[i].ID = int64(i)
			}
			_ = json.NewEncoder(w).Encode(RecordingManifestResponse{ID: "rec-1", SessionID: "sess-1", Segments: segments})
		case strings.Contains(r.URL.Path, "/rec-1/segments/"):
			current := active.Add(1)
			for {
				previous := maxActive.Load()
				if current <= previous || maxActive.CompareAndSwap(previous, current) {
					break
				}
			}
			defer active.Add(-1)
			if started.Add(1) == replayFetchConcurrency {
				close(barrier)
			}
			select {
			case <-barrier:
			case <-time.After(5 * time.Second):
				http.Error(w, "segments were not fetched concurrently", http.StatusInternalServerError)
				return
			}
			id, err := strconv.Atoi(r.URL.Path[strings.LastIndexByte(r.URL.Path, '/')+1:])
			if !assert.NoError(t, err) {
				return
			}
			_ = json.NewEncoder(w).Encode(RecordingSegmentResponse{RecordingID: "rec-1", Events: []RRWebEvent{json.RawMessage(fmt.Sprintf(`{"type":3,"timestamp":%d}`, id))}})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	path := filepath.Join(t.TempDir(), "replay.json")
	cmd := newSessionsGetReplayCommand(&fakeConfigLoader{grafanaURL: server.URL})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetArgs([]string{"sess-1", "--app", "42", "--save", path})
	require.NoError(t, cmd.Execute())
	assert.Equal(t, int32(replayFetchConcurrency), maxActive.Load())
	var bundle struct {
		Recordings []struct {
			Events []struct {
				Timestamp int `json:"timestamp"`
			} `json:"events"`
		} `json:"recordings"`
	}
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(data, &bundle))
	require.Len(t, bundle.Recordings, 1)
	require.Len(t, bundle.Recordings[0].Events, segmentCount)
	for i, event := range bundle.Recordings[0].Events {
		assert.Equal(t, i, event.Timestamp)
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

func TestSessionsGetReplayRejectsMismatchedResponseIdentity(t *testing.T) {
	for _, tt := range []struct {
		name       string
		manifestID string
		segmentID  string
		wantErr    string
	}{
		{name: "manifest", manifestID: "another-recording", segmentID: "rec-1", wantErr: "manifest identity"},
		{name: "segment", manifestID: "rec-1", segmentID: "another-recording", wantErr: "different recording"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch {
				case strings.HasSuffix(r.URL.Path, "/sess-1/recordings"):
					_, _ = w.Write([]byte(`{"items":[{"id":"rec-1"}],"page":{}}`))
				case strings.HasSuffix(r.URL.Path, "/rec-1/manifest"):
					_ = json.NewEncoder(w).Encode(RecordingManifestResponse{ID: tt.manifestID, SessionID: "sess-1", Segments: []ManifestSegment{{ID: 0}}})
				case strings.HasSuffix(r.URL.Path, "/rec-1/segments/0"):
					_ = json.NewEncoder(w).Encode(RecordingSegmentResponse{RecordingID: tt.segmentID, Events: []RRWebEvent{json.RawMessage(`{"type":4}`)}})
				default:
					http.NotFound(w, r)
				}
			}))
			t.Cleanup(server.Close)
			path := filepath.Join(t.TempDir(), "replay.json")
			require.NoError(t, os.WriteFile(path, []byte("original"), 0o600))
			cmd := newSessionsGetReplayCommand(&fakeConfigLoader{grafanaURL: server.URL})
			cmd.SetOut(&bytes.Buffer{})
			cmd.SetErr(&bytes.Buffer{})
			cmd.SetArgs([]string{"sess-1", "--app", "42", "--save", path})
			require.ErrorContains(t, cmd.Execute(), tt.wantErr)
			contents, err := os.ReadFile(path)
			require.NoError(t, err)
			assert.Equal(t, "original", string(contents))
		})
	}
}

func TestSessionsGetReplayValidatesFlagsBeforeIO(t *testing.T) {
	tests := []struct {
		args    []string
		wantErr string
	}{
		{args: []string{"sess-1", "--save", "replay.json"}, wantErr: "--app is required"},
		{args: []string{"sess-1", "--app", "42"}, wantErr: "--save is required"},
		{args: []string{"sess-1", "--app", "my-web-app", "--save", "replay.json"}, wantErr: "expected a numeric ID or slug-id"},
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
