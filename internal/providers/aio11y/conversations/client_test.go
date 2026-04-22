package conversations_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/providers/aio11y/aio11yhttp"
	"github.com/grafana/gcx/internal/providers/aio11y/conversations"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"
)

func writeJSON(w http.ResponseWriter, v any) {
	if err := json.NewEncoder(w).Encode(v); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func newTestClient(t *testing.T, handler http.Handler) *conversations.Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	cfg := config.NamespacedRESTConfig{
		Config:    rest.Config{Host: srv.URL},
		Namespace: "default",
	}
	base, err := aio11yhttp.NewClient(cfg)
	require.NoError(t, err)
	return conversations.NewClient(base)
}

func TestClient_List(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Contains(t, r.URL.Path, "/query/conversations")

		w.Header().Set("Content-Type", "application/json")
		writeJSON(w, map[string]any{
			"items": []conversations.Conversation{
				{
					ID:               "conv-1",
					Title:            "Test conversation",
					GenerationCount:  5,
					LastGenerationAt: now,
				},
			},
		})
	}))

	items, err := client.List(context.Background())
	require.NoError(t, err)
	require.Len(t, items, 1)
	assert.Equal(t, "conv-1", items[0].ID)
	assert.Equal(t, "Test conversation", items[0].Title)
	assert.Equal(t, 5, items[0].GenerationCount)
}

func TestClient_Get(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Contains(t, r.URL.Path, "/query/conversations/conv-123")

		w.Header().Set("Content-Type", "application/json")
		writeJSON(w, map[string]any{
			"conversation_id": "conv-123",
			"generations": []map[string]any{
				{"generation_id": "gen-1", "model": map[string]string{"name": "gpt-4", "provider": "openai"}},
				{"generation_id": "gen-2", "model": map[string]string{"name": "claude-3", "provider": "anthropic"}},
			},
		})
	}))

	detail, err := client.Get(context.Background(), "conv-123")
	require.NoError(t, err)
	assert.Equal(t, "conv-123", (*detail)["conversation_id"])
	gens, ok := (*detail)["generations"].([]any)
	require.True(t, ok)
	assert.Len(t, gens, 2)
}

func TestClient_Search(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Contains(t, r.URL.Path, "/query/conversations/search")
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))

		var req conversations.SearchRequest
		if !assert.NoError(t, json.NewDecoder(r.Body).Decode(&req)) {
			http.Error(w, "bad request body", http.StatusBadRequest)
			return
		}
		assert.Equal(t, "error", req.Filters)
		assert.Equal(t, 10, req.PageSize)

		w.Header().Set("Content-Type", "application/json")
		writeJSON(w, conversations.SearchResponse{
			Conversations: []conversations.SearchResult{
				{ConversationID: "conv-err", HasErrors: true, ErrorCount: 3},
			},
			NextCursor: "page2",
			HasMore:    true,
		})
	}))

	resp, err := client.Search(context.Background(), conversations.SearchRequest{
		Filters:  "error",
		PageSize: 10,
	})
	require.NoError(t, err)
	require.Len(t, resp.Conversations, 1)
	assert.Equal(t, "conv-err", resp.Conversations[0].ConversationID)
	assert.True(t, resp.HasMore)
	assert.Equal(t, "page2", resp.NextCursor)
}

func TestClient_Get_NotFound(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	}))

	_, err := client.Get(context.Background(), "nonexistent")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "404")
}
