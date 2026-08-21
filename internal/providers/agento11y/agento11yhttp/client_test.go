package agento11yhttp_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/providers/agento11y/agento11yhttp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"
)

func newTestClient(t *testing.T, handler http.Handler) *agento11yhttp.Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	cfg := config.NamespacedRESTConfig{
		Config:    rest.Config{Host: srv.URL},
		Namespace: "default",
	}
	client, err := agento11yhttp.NewClient(cfg)
	require.NoError(t, err)
	return client
}

func writeJSON(w http.ResponseWriter, v any) {
	if err := json.NewEncoder(w).Encode(v); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

type testItem struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func TestClient_DoRequest(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/plugins/grafana-agento11y-app/resources/query/test", r.URL.Path)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))

	resp, err := client.DoRequest(context.Background(), http.MethodGet, "/query/test", nil)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestListAll_SinglePage(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		writeJSON(w, map[string]any{
			"items": []testItem{
				{ID: "1", Name: "first"},
				{ID: "2", Name: "second"},
			},
		})
	}))

	items, err := agento11yhttp.ListAll[testItem](context.Background(), client, "/query/items", nil)
	require.NoError(t, err)
	assert.Len(t, items, 2)
	assert.Equal(t, "first", items[0].Name)
}

func TestListAll_MultiplePages(t *testing.T) {
	callCount := 0
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")

		if callCount == 1 {
			assert.Empty(t, r.URL.Query().Get("cursor"))
			writeJSON(w, map[string]any{
				"items":       []testItem{{ID: "1", Name: "first"}},
				"next_cursor": "page2",
			})
			return
		}

		assert.Equal(t, "page2", r.URL.Query().Get("cursor"))
		writeJSON(w, map[string]any{
			"items": []testItem{{ID: "2", Name: "second"}},
		})
	}))

	items, err := agento11yhttp.ListAll[testItem](context.Background(), client, "/query/items", nil)
	require.NoError(t, err)
	assert.Len(t, items, 2)
	assert.Equal(t, 2, callCount)
}

func TestListAll_EmptyResponse(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		writeJSON(w, map[string]any{"items": []any{}})
	}))

	items, err := agento11yhttp.ListAll[testItem](context.Background(), client, "/query/items", nil)
	require.NoError(t, err)
	assert.Empty(t, items)
}

func TestListAll_LimitSlicesMidPage(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		writeJSON(w, map[string]any{
			"items": []testItem{
				{ID: "1", Name: "first"},
				{ID: "2", Name: "second"},
				{ID: "3", Name: "third"},
			},
			"next_cursor": "page2",
		})
	}))

	items, err := agento11yhttp.ListAll[testItem](context.Background(), client, "/query/items", nil, 2)
	require.NoError(t, err)
	assert.Len(t, items, 2)
	assert.Equal(t, "first", items[0].Name)
	assert.Equal(t, "second", items[1].Name)
}

func TestListAll_LimitStopsPagination(t *testing.T) {
	callCount := 0
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")
		writeJSON(w, map[string]any{
			"items":       []testItem{{ID: "1"}},
			"next_cursor": "more",
		})
	}))

	items, err := agento11yhttp.ListAll[testItem](context.Background(), client, "/query/items", nil, 1)
	require.NoError(t, err)
	assert.Len(t, items, 1)
	assert.Equal(t, 1, callCount, "should not fetch second page when limit already reached")
}

func TestListAllWithHasMore_CompleteAtLimit(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		writeJSON(w, map[string]any{
			"items": []testItem{
				{ID: "1", Name: "first"},
				{ID: "2", Name: "second"},
			},
		})
	}))

	items, hasMore, err := agento11yhttp.ListAllWithHasMore[testItem](context.Background(), client, "/query/items", nil, 2)
	require.NoError(t, err)
	assert.Len(t, items, 2)
	assert.False(t, hasMore)
}

func TestListAllWithHasMore_TruncatedMidPage(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		writeJSON(w, map[string]any{
			"items": []testItem{
				{ID: "1", Name: "first"},
				{ID: "2", Name: "second"},
				{ID: "3", Name: "third"},
			},
			"next_cursor": "page2",
		})
	}))

	items, hasMore, err := agento11yhttp.ListAllWithHasMore[testItem](context.Background(), client, "/query/items", nil, 2)
	require.NoError(t, err)
	assert.Len(t, items, 2)
	assert.True(t, hasMore)
}

func TestListAllWithHasMore_TruncatedWithNextPage(t *testing.T) {
	callCount := 0
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")
		writeJSON(w, map[string]any{
			"items":       []testItem{{ID: "1"}},
			"next_cursor": "more",
		})
	}))

	items, hasMore, err := agento11yhttp.ListAllWithHasMore[testItem](context.Background(), client, "/query/items", nil, 1)
	require.NoError(t, err)
	assert.Len(t, items, 1)
	assert.True(t, hasMore)
	assert.Equal(t, 1, callCount)
}

func TestListAll_ErrorResponse(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "forbidden", http.StatusForbidden)
	}))

	_, err := agento11yhttp.ListAll[testItem](context.Background(), client, "/query/items", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "403")
}
