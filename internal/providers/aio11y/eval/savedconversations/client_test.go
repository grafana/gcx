package savedconversations_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/providers/aio11y/aio11yhttp"
	"github.com/grafana/gcx/internal/providers/aio11y/eval/savedconversations"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"
)

func writeJSON(w http.ResponseWriter, v any) {
	if err := json.NewEncoder(w).Encode(v); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func newTestClient(t *testing.T, handler http.Handler) *savedconversations.Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	cfg := config.NamespacedRESTConfig{
		Config:    rest.Config{Host: srv.URL},
		Namespace: "default",
	}
	base, err := aio11yhttp.NewClient(cfg)
	require.NoError(t, err)
	return savedconversations.NewClient(base)
}

func TestClient_List(t *testing.T) {
	var capturedSource string
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Contains(t, r.URL.Path, "/eval/saved-conversations")
		capturedSource = r.URL.Query().Get("source")

		w.Header().Set("Content-Type", "application/json")
		writeJSON(w, map[string]any{
			"items": []savedconversations.SavedConversation{
				{SavedID: "saved-1", Name: "first", ConversationID: "conv-1", Source: "telemetry"},
				{SavedID: "saved-2", Name: "second", ConversationID: "conv-2", Source: "telemetry"},
			},
		})
	}))

	items, err := client.List(context.Background(), "telemetry", 10)
	require.NoError(t, err)
	require.Len(t, items, 2)
	assert.Equal(t, "saved-1", items[0].SavedID)
	assert.Equal(t, "telemetry", capturedSource)
}

func TestClient_List_NoSource(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Empty(t, r.URL.Query().Get("source"))
		w.Header().Set("Content-Type", "application/json")
		writeJSON(w, map[string]any{"items": []savedconversations.SavedConversation{}})
	}))

	items, err := client.List(context.Background(), "", 0)
	require.NoError(t, err)
	assert.Empty(t, items)
}

func TestClient_Get(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Contains(t, r.URL.Path, "/eval/saved-conversations/saved-1")

		w.Header().Set("Content-Type", "application/json")
		writeJSON(w, savedconversations.SavedConversation{
			SavedID:        "saved-1",
			ConversationID: "conv-1",
			Name:           "Regression seed",
			Source:         "telemetry",
		})
	}))

	sc, err := client.Get(context.Background(), "saved-1")
	require.NoError(t, err)
	assert.Equal(t, "saved-1", sc.SavedID)
	assert.Equal(t, "conv-1", sc.ConversationID)
}

func TestClient_Get_NotFound(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	}))

	_, err := client.Get(context.Background(), "missing")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "404")
}

func TestClient_Save(t *testing.T) {
	var captured savedconversations.SaveRequest
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Contains(t, r.URL.Path, "/eval/saved-conversations")
		body, _ := io.ReadAll(r.Body)
		assert.NoError(t, json.Unmarshal(body, &captured))

		w.WriteHeader(http.StatusOK)
		writeJSON(w, savedconversations.SavedConversation{
			SavedID:        captured.SavedID,
			ConversationID: captured.ConversationID,
			Name:           captured.Name,
			Tags:           captured.Tags,
			Source:         "telemetry",
			SavedBy:        "user",
			CreatedAt:      time.Now().UTC(),
		})
	}))

	sc, err := client.Save(context.Background(), &savedconversations.SaveRequest{
		SavedID:        "saved-conv-123",
		ConversationID: "conv-123",
		Name:           "Regression seed",
		Tags:           map[string]string{"suite": "checkout", "priority": "high"},
	})
	require.NoError(t, err)
	assert.Equal(t, "saved-conv-123", sc.SavedID)
	assert.Equal(t, "conv-123", captured.ConversationID)
	assert.Equal(t, "Regression seed", captured.Name)
	assert.Equal(t, "checkout", captured.Tags["suite"])
	assert.Equal(t, "high", captured.Tags["priority"])
}

func TestClient_Delete(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodDelete, r.Method)
		assert.Contains(t, r.URL.Path, "/eval/saved-conversations/saved-1")
		w.WriteHeader(http.StatusNoContent)
	}))

	require.NoError(t, client.Delete(context.Background(), "saved-1"))
}

func TestClient_Delete_NotFound(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	}))

	err := client.Delete(context.Background(), "missing")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "404")
}

func TestClient_ListCollections(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Contains(t, r.URL.Path, "/eval/saved-conversations/saved-1/collections")

		w.Header().Set("Content-Type", "application/json")
		writeJSON(w, map[string]any{
			"items": []savedconversations.CollectionRef{
				{CollectionID: "c-1", Name: "Regression suite", MemberCount: 3},
				{CollectionID: "c-2", Name: "Smoke", MemberCount: 1},
			},
		})
	}))

	cols, err := client.ListCollections(context.Background(), "saved-1")
	require.NoError(t, err)
	require.Len(t, cols, 2)
	assert.Equal(t, "c-1", cols[0].CollectionID)
	assert.Equal(t, "Regression suite", cols[0].Name)
}

func TestClient_ListCollections_NotFound(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	}))

	_, err := client.ListCollections(context.Background(), "missing")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "404")
}
