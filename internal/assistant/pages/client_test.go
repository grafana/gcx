package pages_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/grafana/gcx/internal/assistant/assistanthttp"
	"github.com/grafana/gcx/internal/assistant/pages"
	"github.com/grafana/gcx/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"
)

func testClient(t *testing.T, handler http.HandlerFunc) *pages.Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	base, err := assistanthttp.NewClient(config.NamespacedRESTConfig{Config: rest.Config{Host: server.URL}, Namespace: "default"})
	require.NoError(t, err)
	return pages.NewClient(base)
}

func respond(t *testing.T, w http.ResponseWriter, data any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	assert.NoError(t, json.NewEncoder(w).Encode(map[string]any{"status": "success", "data": data}))
}

const base = "/api/plugins/grafana-assistant-app/resources/api/v1"

func TestEditFromSourceSendsTheSourceAsTheExportShape(t *testing.T) {
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, base+"/page-catalog/page-1/edit", r.URL.Path)
		var body map[string]json.RawMessage
		assert.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		assert.JSONEq(t, `"chat-1"`, string(body["chatId"]))
		// Files travel base64-encoded, as the export returns them.
		assert.JSONEq(t, `{"version":1,"files":{"src/App.tsx":"ZXhwb3J0IHt9"}}`, string(body["source"]))
		assert.JSONEq(t, `{"intro":"hi"}`, string(body["speakerNotes"]))
		respond(t, w, map[string]any{"page": map[string]any{"id": "copy-1", "chatId": "chat-1", "rebuildPending": true}})
	})
	page, err := client.EditFromSource(context.Background(), "page-1", "chat-1", pages.Export{
		Source:       pages.Source{Version: 1, Files: map[string][]byte{"src/App.tsx": []byte("export {}")}},
		SpeakerNotes: map[string]string{"intro": "hi"},
	})
	require.NoError(t, err)
	assert.Equal(t, "copy-1", page.ID)
}

func TestEditFromSourceRefusesAnAssistantThatIgnoredTheSource(t *testing.T) {
	// An older Assistant drops the unknown field and copies the published
	// revision; publishing that copy would lose the pushed changes.
	client := testClient(t, func(w http.ResponseWriter, _ *http.Request) {
		respond(t, w, map[string]any{"page": map[string]any{"id": "copy-1", "releaseId": "rel-9"}})
	})
	_, err := client.EditFromSource(context.Background(), "page-1", "chat-1", pages.Export{Source: pages.Source{Version: 1, Files: map[string][]byte{"a": []byte("b")}}})
	require.ErrorIs(t, err, pages.ErrSourceUnsupported)
}

func TestEditFromSourceRefusesAnAssistantThatRejectsTheField(t *testing.T) {
	client := testClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(`{"title":"Unprocessable Entity","errors":[{"message":"unexpected property","location":"body.source"}]}`))
	})
	_, err := client.EditFromSource(context.Background(), "page-1", "chat-1", pages.Export{Source: pages.Source{Version: 1, Files: map[string][]byte{"a": []byte("b")}}})
	require.ErrorIs(t, err, pages.ErrSourceUnsupported)
}

func TestPublishKeepsThePagesDetailsAndSharing(t *testing.T) {
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, base+"/page-catalog", r.URL.Path)
		var body map[string]any
		assert.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		assert.Equal(t, "page-1", body["id"])
		assert.InDelta(t, 4, body["version"], 0)
		assert.Equal(t, "copy-1", body["pageId"])
		assert.Equal(t, "rel-2", body["releaseId"])
		assert.Equal(t, map[string]any{"title": "Radar", "description": "d", "category": "overview", "tags": []any{"beyla"}, "folderUid": "", "unlisted": true}, body["metadata"])
		assert.Equal(t, map[string]any{"users": []any{map[string]any{"userId": float64(7), "login": "viewer"}}, "linkSharing": true}, body["sharing"])
		respond(t, w, map[string]any{"page": map[string]any{"id": "page-1", "version": 5, "revisionId": "rel-2"}})
	})
	entry := &pages.Entry{ID: "page-1", Title: "Radar", Description: "d", Category: "overview", Tags: []string{"beyla"}, Unlisted: true, LinkSharing: true, SharedWith: []pages.Share{{UserID: 7, Login: "viewer"}}, Version: 4}
	published, err := client.Publish(context.Background(), entry, "copy-1", "rel-2")
	require.NoError(t, err)
	assert.Equal(t, 5, published.Version)
}

func TestAStalePublishExplainsHowToRecover(t *testing.T) {
	client := testClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"name":"ga:pages/page-changed","message":"The page changed"}`))
	})
	_, err := client.Publish(context.Background(), &pages.Entry{ID: "page-1"}, "copy-1", "rel-2")
	require.ErrorContains(t, err, "pull it again")
}

func TestListReturnsAnEmptyArrayAndTheNextCursor(t *testing.T) {
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "mine", r.URL.Query().Get("section"))
		assert.Equal(t, "5", r.URL.Query().Get("limit"))
		respond(t, w, map[string]any{"pages": nil, "nextCursor": "c2"})
	})
	entries, next, err := client.List(context.Background(), pages.ListOptions{Section: "mine", Limit: 5})
	require.NoError(t, err)
	assert.Equal(t, []pages.Entry{}, entries)
	assert.Equal(t, "c2", next)
}
