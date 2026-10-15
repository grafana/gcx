package watcher_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/grafana/gcx/internal/assistant/assistanthttp"
	"github.com/grafana/gcx/internal/assistant/watcher"
	"github.com/grafana/gcx/internal/assistant/watchers"
	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/retry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"
)

func TestDiffDefinitionsPreservesJSONDistinctions(t *testing.T) {
	from := json.RawMessage(`{"z":null,"queries":[{"id":"one","enabled":false}],"n":9007199254740992,"a/b~c":{"value":"old"},"a":1,"~":false}`)
	to := json.RawMessage(`{"new":null,"queries":[{"id":"one","enabled":true}],"n":9007199254740993,"a/b~c":{"value":"new"},"a":1,"~":true}`)
	changes, err := watcher.DiffDefinitions(from, to)
	require.NoError(t, err)
	require.Len(t, changes, 6)
	paths := make([]string, 0, len(changes))
	for _, change := range changes {
		paths = append(paths, change.Path)
	}
	assert.Equal(t, []string{"/a~1b~0c/value", "/n", "/new", "/queries", "/z", "/~0"}, paths)
	assert.Equal(t, "replace", changes[0].Operation)
	assert.Equal(t, `9007199254740992`, string(*changes[1].Before))
	assert.Equal(t, `9007199254740993`, string(*changes[1].After))
	assert.Equal(t, "add", changes[2].Operation)
	assert.Nil(t, changes[2].Before)
	assert.Equal(t, "null", string(*changes[2].After))
	assert.JSONEq(t, `[{"enabled":false,"id":"one"}]`, string(*changes[3].Before))
	assert.Equal(t, "remove", changes[4].Operation)
	assert.Equal(t, "null", string(*changes[4].Before))
	assert.Nil(t, changes[4].After)
	encoded, err := json.Marshal(changes)
	require.NoError(t, err)
	assert.Contains(t, string(encoded), `"operation":"add","after":null`)
	assert.Contains(t, string(encoded), `"operation":"remove","before":null`)
}

func TestDiffDefinitionsOrderAndAtomicValues(t *testing.T) {
	for _, tt := range []struct {
		name, from, to string
		count          int
		path           string
	}{
		{name: "object order ignored inside arrays", from: `{"queries":[{"a":1,"b":2}],"name":"same"}`, to: `{"name":"same","queries":[{"b":2,"a":1}]}`},
		{name: "empty objects preserved", from: `{"a":{}}`, to: `{"a":{}}`},
		{name: "root replacement", from: `null`, to: `{}`, count: 1},
		{name: "array order matters", from: `{"queries":[1,2]}`, to: `{"queries":[2,1]}`, count: 1, path: "/queries"},
		{name: "number notation preserved", from: `{"n":1}`, to: `{"n":1.0}`, count: 1, path: "/n"},
		{name: "empty object differs from null", from: `{"a":{}}`, to: `{"a":null}`, count: 1, path: "/a"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			changes, err := watcher.DiffDefinitions(json.RawMessage(tt.from), json.RawMessage(tt.to))
			require.NoError(t, err)
			require.NotNil(t, changes)
			require.Len(t, changes, tt.count)
			if tt.count > 0 {
				assert.Equal(t, tt.path, changes[0].Path)
			}
		})
	}
}

func TestDiffDefinitionsRejectsInvalidJSON(t *testing.T) {
	for _, raw := range []string{"", "{", "{} {}", "{} trailing"} {
		_, err := watcher.DiffDefinitions(json.RawMessage(raw), json.RawMessage(`{}`))
		require.Error(t, err)
		_, err = watcher.DiffDefinitions(json.RawMessage(`{}`), json.RawMessage(raw))
		require.Error(t, err)
	}
}

const versionDomainPath = "/api/plugins/grafana-assistant-app/resources/api/v1/watcher-agents/example"

func versionDomainClient(t *testing.T, handler http.HandlerFunc) *watchers.Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	base, err := assistanthttp.NewClient(config.NamespacedRESTConfig{Config: rest.Config{
		Host: server.URL,
		WrapTransport: func(base http.RoundTripper) http.RoundTripper {
			return &retry.Transport{Base: base, MaxRetries: 2, MinBackoff: time.Millisecond, MaxBackoff: time.Millisecond}
		},
	}})
	require.NoError(t, err)
	return watchers.NewClient(base)
}

func TestRestoreVersionRefusesBeforeIO(t *testing.T) {
	archived := time.Now()
	for _, tt := range []struct {
		name     string
		current  *watchers.Watcher
		version  int64
		expected int64
		summary  string
	}{
		{name: "no identity", current: &watchers.Watcher{Status: "paused", DefinitionVersion: 2}, version: 1, expected: 2},
		{name: "nil current", version: 1, expected: 2},
		{name: "archived", current: &watchers.Watcher{ID: "example", Status: "paused", DefinitionVersion: 2, ArchivedAt: &archived}, version: 1, expected: 2},
		{name: "stale guard", current: &watchers.Watcher{ID: "example", Status: "paused", DefinitionVersion: 3}, version: 1, expected: 2},
		{name: "missing guard", current: &watchers.Watcher{ID: "example", Status: "paused", DefinitionVersion: 2}, version: 1},
		{name: "invalid version", current: &watchers.Watcher{ID: "example", Status: "paused", DefinitionVersion: 2}, version: -1, expected: 2},
		{name: "summary too long", current: &watchers.Watcher{ID: "example", Status: "paused", DefinitionVersion: 2}, version: 1, expected: 2, summary: strings.Repeat("\u754c", 1001)},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var calls atomic.Int32
			client := versionDomainClient(t, func(w http.ResponseWriter, _ *http.Request) {
				calls.Add(1)
				w.WriteHeader(http.StatusInternalServerError)
			})
			outcome, err := watcher.RestoreVersion(t.Context(), client, tt.current, tt.version, tt.expected, tt.summary)
			require.Error(t, err)
			assert.Nil(t, outcome)
			assert.Zero(t, calls.Load())
		})
	}
	for _, status := range []string{"running", "draft", "ready", "calibrating", "", "future-state"} {
		t.Run("lifecycle "+status, func(t *testing.T) {
			var calls atomic.Int32
			client := versionDomainClient(t, func(w http.ResponseWriter, _ *http.Request) {
				calls.Add(1)
				w.WriteHeader(http.StatusInternalServerError)
			})
			outcome, err := watcher.RestoreVersion(t.Context(), client, &watchers.Watcher{ID: "example", Status: status, DefinitionVersion: 2}, 1, 2, "")
			require.ErrorContains(t, err, "explicitly paused")
			assert.Nil(t, outcome)
			assert.Zero(t, calls.Load())
		})
	}
}

func TestRestoreVersionRaceIsRejectedWithoutRetry(t *testing.T) {
	var posts atomic.Int32
	client := versionDomainClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, versionDomainPath+"/revisions/1/restore", r.URL.Path)
		posts.Add(1)
		var request struct {
			ExpectedDefinitionVersion int64 `json:"expectedDefinitionVersion"`
		}
		assert.NoError(t, json.NewDecoder(r.Body).Decode(&request))
		assert.Equal(t, int64(2), request.ExpectedDefinitionVersion)
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"message":"definition version changed"}`))
	})
	result, err := watcher.RestoreVersion(t.Context(), client, &watchers.Watcher{ID: "example", Status: "paused", DefinitionVersion: 2}, 1, 2, "")
	require.Error(t, err)
	require.ErrorIs(t, err, watchers.ErrConflict)
	assert.Nil(t, result)
	assert.Equal(t, int32(1), posts.Load())
}

func TestRestoreVersionAcceptanceAndLaterObservation(t *testing.T) {
	archived := time.Now()
	for _, tt := range []struct {
		name, status string
		version      int64
		archived     *time.Time
		readFailure  bool
		wrongID      bool
		accepted     bool
	}{
		{name: "paused becomes draft", status: "draft", version: 3, accepted: true},
		{name: "paused remains paused", status: "paused", version: 3, accepted: true},
		{name: "inactive ready", status: "ready", version: 3, accepted: true},
		{name: "concurrent start", status: "running", version: 3},
		{name: "concurrent version", status: "paused", version: 4},
		{name: "archive", status: "paused", version: 3, archived: &archived},
		{name: "unknown lifecycle", status: "future-state", version: 3},
		{name: "read denied", readFailure: true},
		{name: "wrong identity", status: "paused", version: 3, wrongID: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var posts, gets atomic.Int32
			client := versionDomainClient(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodPost {
					posts.Add(1)
					assert.Equal(t, versionDomainPath+"/revisions/1/restore", r.URL.Path)
					_, _ = w.Write([]byte(`{"data":{"version":3,"rollbackFromVersion":1,"definition":{"prompt":"Earlier definition"}}}`))
					return
				}
				assert.Equal(t, http.MethodGet, r.Method)
				assert.Equal(t, versionDomainPath, r.URL.Path)
				gets.Add(1)
				if tt.readFailure {
					w.WriteHeader(http.StatusForbidden)
					_, _ = w.Write([]byte(`{"message":"private server detail"}`))
					return
				}
				id := "example"
				if tt.wrongID {
					id = "different"
				}
				assert.NoError(t, json.NewEncoder(w).Encode(map[string]any{"data": watchers.Watcher{ID: id, Name: "Example", Status: tt.status, DefinitionVersion: tt.version, ArchivedAt: tt.archived}}))
			})
			result, err := watcher.RestoreVersion(t.Context(), client, &watchers.Watcher{ID: "example", Status: "paused", DefinitionVersion: 2}, 1, 2, "Restore earlier prompt")
			require.NotNil(t, result)
			require.NotNil(t, result.Accepted)
			assert.Equal(t, int64(3), result.Accepted.Version)
			require.NotNil(t, result.Changed)
			assert.True(t, *result.Changed)
			if tt.accepted {
				require.NoError(t, err)
				assert.Equal(t, "accepted", result.Outcome)
			} else {
				require.Error(t, err)
				assert.NotContains(t, err.Error(), "private server detail")
				assert.Equal(t, "saved-unverified", result.Outcome)
				assert.Contains(t, result.Guidance, "do not repeat")
			}
			if !tt.readFailure && !tt.wrongID {
				require.NotNil(t, result.Observed)
				assert.Equal(t, tt.version, result.Observed.CurrentVersion)
				assert.Equal(t, tt.status, result.Observed.Lifecycle)
			}
			assert.Equal(t, int32(1), posts.Load())
			assert.Equal(t, int32(1), gets.Load())
		})
	}
}

func TestRestoreVersionUnknownResponseRetainsRequestEvidence(t *testing.T) {
	for _, body := range []string{`{"data":`, `{"data":null}`, `{"data":{"version":3,"rollbackFromVersion":99,"definition":{}}}`} {
		t.Run(body, func(t *testing.T) {
			var calls atomic.Int32
			client := versionDomainClient(t, func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodPost, r.Method)
				calls.Add(1)
				_, _ = w.Write([]byte(body))
			})
			result, err := watcher.RestoreVersion(t.Context(), client, &watchers.Watcher{ID: "example", Status: "paused", DefinitionVersion: 2}, 1, 2, "")
			require.Error(t, err)
			var uncertain *watchers.UnknownRestoreError
			require.ErrorAs(t, err, &uncertain)
			require.NotNil(t, result)
			assert.Equal(t, "unknown", result.Outcome)
			assert.Nil(t, result.Accepted)
			assert.Nil(t, result.Changed)
			assert.Nil(t, result.Observed)
			encoded, encodeErr := json.Marshal(result)
			require.NoError(t, encodeErr)
			assert.NotContains(t, string(encoded), `"changed"`)
			assert.NotContains(t, string(encoded), `"accepted"`)
			assert.Equal(t, int32(1), calls.Load())
		})
	}
}
