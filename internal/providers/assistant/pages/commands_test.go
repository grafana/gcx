package pages //nolint:testpackage // Exercises push end to end without a configured stack.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/assistant/assistanthttp"
	clientpages "github.com/grafana/gcx/internal/assistant/pages"
	"github.com/grafana/gcx/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"
)

const api = "/api/plugins/grafana-assistant-app/resources/api/v1"

// fakeAssistant serves the Pages endpoints push calls and records them.
type fakeAssistant struct {
	t            *testing.T
	version      int
	buildError   string
	coldStarts   int
	calls        []string
	pushedSource clientpages.Source
}

func (f *fakeAssistant) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, api)
	f.calls = append(f.calls, r.Method+" "+path)
	reply := func(data any) {
		w.Header().Set("Content-Type", "application/json")
		assert.NoError(f.t, json.NewEncoder(w).Encode(map[string]any{"status": "success", "data": data}))
	}
	switch {
	case r.Method == http.MethodGet && path == "/page-catalog/page-1":
		reply(map[string]any{"page": map[string]any{"id": "page-1", "title": "Radar", "category": "overview", "tags": []string{}, "unlisted": true, "linkSharing": true, "version": f.version, "revisionId": "rel-old"}})
	case r.Method == http.MethodPost && path == "/chats":
		reply(map[string]any{"id": "chat-1"})
	case r.Method == http.MethodPost && path == "/page-catalog/page-1/edit":
		var body struct {
			Source clientpages.Source `json:"source"`
		}
		assert.NoError(f.t, json.NewDecoder(r.Body).Decode(&body))
		f.pushedSource = body.Source
		reply(map[string]any{"page": map[string]any{"id": "copy-1", "chatId": "chat-1", "rebuildPending": true}})
	case r.Method == http.MethodPost && path == "/chats/chat-1/pages/copy-1/rebuild":
		if f.coldStarts > 0 {
			f.coldStarts--
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		page := map[string]any{"id": "copy-1", "chatId": "chat-1"}
		if f.buildError != "" {
			page["error"] = f.buildError
		} else {
			page["candidateId"] = "build-1"
		}
		reply(map[string]any{"page": page})
	case r.Method == http.MethodPost && path == "/chats/chat-1/pages/copy-1/accept":
		reply(map[string]any{"page": map[string]any{"id": "copy-1", "chatId": "chat-1", "releaseId": "build-1"}})
	case r.Method == http.MethodPost && path == "/page-catalog":
		reply(map[string]any{"page": map[string]any{"id": "page-1", "version": f.version + 1, "revisionId": "build-1"}})
	default:
		f.t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		w.WriteHeader(http.StatusNotFound)
	}
}

func pulledDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	_, err := clientpages.WriteProject(dir, &clientpages.Entry{ID: "page-1", Title: "Radar", Version: 4, RevisionID: "rel-old"},
		&clientpages.Export{Source: clientpages.Source{Version: 1, Files: map[string][]byte{"src/App.tsx": []byte("export const v = 1")}}}, false)
	require.NoError(t, err)
	return dir
}

func testSession(t *testing.T, fake *fakeAssistant) *session {
	t.Helper()
	server := httptest.NewServer(fake)
	t.Cleanup(server.Close)
	base, err := assistanthttp.NewClient(config.NamespacedRESTConfig{Config: rest.Config{Host: server.URL}, Namespace: "default"})
	require.NoError(t, err)
	return &session{client: clientpages.NewClient(base), grafanaURL: "https://stack.example"}
}

func TestPushBuildsAndPublishesTheNextVersion(t *testing.T) {
	fake := &fakeAssistant{t: t, version: 4, coldStarts: 1}
	dir := pulledDir(t)

	result, err := runPush(context.Background(), testSession(t, fake), &pushOpts{Dir: dir})
	require.NoError(t, err)

	assert.Equal(t, &pushResult{ID: "page-1", Title: "Radar", ChatID: "chat-1", PageID: "copy-1", BuildID: "build-1", Published: true, Version: 5, URL: "https://stack.example/a/grafana-assistant-app/pages/page-1"}, result)
	assert.Equal(t, map[string][]byte{"src/App.tsx": []byte("export const v = 1")}, fake.pushedSource.Files)
	meta, err := clientpages.ReadMeta(dir)
	require.NoError(t, err)
	assert.Equal(t, 5, meta.Version, "a second push starts from the version just published")
	assert.Equal(t, "build-1", meta.RevisionID)
}

func TestPushDryRunBuildsWithoutPublishing(t *testing.T) {
	fake := &fakeAssistant{t: t, version: 4}
	result, err := runPush(context.Background(), testSession(t, fake), &pushOpts{Dir: pulledDir(t), DryRun: true})
	require.NoError(t, err)
	assert.False(t, result.Published)
	assert.NotContains(t, fake.calls, "POST /page-catalog")
	assert.NotContains(t, fake.calls, "POST /chats/chat-1/pages/copy-1/accept")
}

func TestPushReportsTheCompileError(t *testing.T) {
	fake := &fakeAssistant{t: t, version: 4, buildError: "src/App.tsx(3,1): error TS2304: Cannot find name 'x'."}
	_, err := runPush(context.Background(), testSession(t, fake), &pushOpts{Dir: pulledDir(t)})
	require.ErrorContains(t, err, "TS2304")
	assert.NotContains(t, fake.calls, "POST /page-catalog")
}

func TestPushRefusesAPageThatChangedSinceThePull(t *testing.T) {
	fake := &fakeAssistant{t: t, version: 6}
	_, err := runPush(context.Background(), testSession(t, fake), &pushOpts{Dir: pulledDir(t)})
	require.ErrorContains(t, err, "pulled at version 4")
	assert.Equal(t, []string{"GET /page-catalog/page-1"}, fake.calls, "nothing is created for a stale pull")
}

func TestPageIDAcceptsIDsAndURLs(t *testing.T) {
	const id = "3f2b8c1e-7a4d-4e9b-9c6a-5d1e2f3a4b5c"
	for _, ref := range []string{
		id,
		"https://stack.example/a/grafana-assistant-app/pages/" + id,
		"https://stack.example/a/grafana-assistant-app/pages/service-map-" + id + "?page-view=flow",
	} {
		assert.Equal(t, id, pageID(ref), ref)
	}
}
