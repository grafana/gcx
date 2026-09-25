package faro

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/spf13/pflag"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWritePrivateReplayFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix file permission bits are not represented on Windows")
	}

	t.Run("creates a private file", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "events.json")
		require.NoError(t, writePrivateReplayFile(path, []byte("new replay data")))
		info, err := os.Stat(path)
		require.NoError(t, err)
		require.Equal(t, os.FileMode(0o600), info.Mode().Perm())
		contents, err := os.ReadFile(path)
		require.NoError(t, err)
		require.Equal(t, "new replay data", string(contents))
	})

	t.Run("replaces an existing permissive file privately", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "events.json")
		require.NoError(t, os.WriteFile(path, []byte("old data"), 0o600))
		require.NoError(t, os.Chmod(path, 0o644))
		require.NoError(t, writePrivateReplayFile(path, []byte("new replay data")))
		info, err := os.Stat(path)
		require.NoError(t, err)
		require.Equal(t, os.FileMode(0o600), info.Mode().Perm())
		contents, err := os.ReadFile(path)
		require.NoError(t, err)
		require.Equal(t, "new replay data", string(contents))
	})
}

func TestParseReplaySegmentID(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    int64
		wantErr bool
	}{
		{name: "zero", input: "0", want: 0},
		{name: "positive", input: "12", want: 12},
		{name: "whitespace", input: " 3 ", want: 3},
		{name: "negative", input: "-1", wantErr: true},
		{name: "not a number", input: "segment", wantErr: true},
		{name: "overflow", input: "9223372036854775808", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseReplaySegmentID(tt.input)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestListReplaySessionsOptsAcceptsPrometheusDurations(t *testing.T) {
	opts := &listReplaySessionsOpts{}
	opts.setup(pflag.NewFlagSet("test", pflag.ContinueOnError))
	opts.Since = "7d"
	opts.Limit = 10
	require.NoError(t, opts.Validate())
}

func TestListReplayRecordingsReturnsPartialResultsAndManifestIDs(t *testing.T) {
	server := newReplayInspectionTestServer(t)
	defer server.Close()
	loader := &fakeConfigLoader{grafanaURL: server.URL}
	cmd := newListReplayRecordingsCommand(loader)
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{"my-app-42", "sess-1", "--output", "json"})

	require.NoError(t, cmd.Execute())
	var result ReplayRecordingList
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &result))
	require.Len(t, result.Items, 2)
	require.Equal(t, int64(3), result.TotalItems)
	require.True(t, result.HasMore)
	require.True(t, result.Items[0].ManifestAvailable)
	require.Equal(t, []int64{0, 3}, result.Items[0].SegmentIDs)
	require.False(t, result.Items[1].ManifestAvailable)
	assert.Contains(t, stderr.String(), "Manifest unavailable for recording rec-2")
}

func TestInspectReplaySegmentSaveEmitsJSONReceipt(t *testing.T) {
	server := newReplayInspectionTestServer(t)
	defer server.Close()
	loader := &fakeConfigLoader{grafanaURL: server.URL}
	path := filepath.Join(t.TempDir(), "events.json")
	cmd := newInspectReplaySegmentCommand(loader)
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{"my-app-42", "sess-1", "3", "--recording-id", "rec-1", "--save", path, "--output", "json"})

	require.NoError(t, cmd.Execute())
	doc := decodeSingleJSONValue(t, stdout.String())
	assert.Equal(t, path, doc["path"])
	assert.InDelta(t, float64(1), doc["event_count"], 0)
	replay, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(replay), `"type": 4`)
}

func newReplayInspectionTestServer(t *testing.T) *httptest.Server {
	t.Helper()

	const base = "/api/plugin-proxy/grafana-sessionreplay-app/faro-api-proxy/api/v1/sessions/"
	mux := http.NewServeMux()
	mux.HandleFunc(base+"sess-1/recordings", func(w http.ResponseWriter, r *http.Request) {
		writeReplayTestJSON(w, SessionRecordingsListResponse{
			SessionID: "sess-1",
			Items: []RecordingListItem{
				{ID: "rec-1", Status: "complete", StartTS: time.Date(2026, 9, 25, 9, 0, 0, 0, time.UTC), EndTS: time.Date(2026, 9, 25, 9, 1, 0, 0, time.UTC)},
				{ID: "rec-2", Status: "in_progress"},
			},
			Page: SessionPage{HasNext: true, Limit: 2, TotalItems: 3},
		})
	})
	mux.HandleFunc(base+"sess-1/recordings/rec-1/manifest", func(w http.ResponseWriter, _ *http.Request) {
		writeReplayTestJSON(w, RecordingManifestResponse{
			ID:        "rec-1",
			SessionID: "sess-1",
			Segments: []ManifestSegment{
				{ID: 0, StartOffsetMs: 0, EndOffsetMs: 1000},
				{ID: 3, StartOffsetMs: 1000, EndOffsetMs: 2000},
			},
		})
	})
	mux.HandleFunc(base+"sess-1/recordings/rec-2/manifest", func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	})
	mux.HandleFunc(base+"sess-1/recordings/rec-1/segments/3", func(w http.ResponseWriter, _ *http.Request) {
		writeReplayTestJSON(w, RecordingSegmentResponse{
			ID:          "3",
			RecordingID: "rec-1",
			Events:      []RRWebEvent{{Type: 4, Timestamp: 1790326800000, Data: json.RawMessage(`{"source":"test"}`)}},
		})
	})
	return httptest.NewServer(mux)
}

func writeReplayTestJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(value); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
