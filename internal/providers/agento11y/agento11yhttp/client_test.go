package agento11yhttp_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/gcxerrors"
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

func TestHandleErrorResponsePluginUnavailable(t *testing.T) {
	for _, tc := range []struct {
		name        string
		status      int
		body        string
		unavailable bool
	}{
		{"plugin absent JSON", http.StatusNotFound, `{"message":"Plugin not found"}`, true},
		{"plugin absent text", http.StatusNotFound, "Plugin not found, no installed plugin with id grafana-agento11y-app", true},
		{"plugin wording suffix", http.StatusNotFound, `{"message":"Plugin not found with id: grafana-agento11y-app"}`, true},
		{"resource absent", http.StatusNotFound, `{"message":"experiment not found"}`, false},
		{"unknown route", http.StatusNotFound, "404 page not found", false},
		{"unrelated status", http.StatusForbidden, `{"message":"Plugin not found"}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resp := &http.Response{StatusCode: tc.status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(tc.body))}
			err := agento11yhttp.HandleErrorResponse(resp)
			require.Error(t, err)
			var statusErr *gcxerrors.HTTPStatusError
			require.ErrorAs(t, err, &statusErr)
			assert.Equal(t, tc.status, statusErr.HTTPStatusCode())
			var pluginErr *agento11yhttp.PluginUnavailableError
			assert.Equal(t, tc.unavailable, errors.As(err, &pluginErr))
			if tc.unavailable {
				require.NotNil(t, pluginErr)
				assert.Contains(t, err.Error(), "grafana-agento11y-app")
				assert.Contains(t, err.Error(), "Agent Observability")
				assert.NotContains(t, err.Error(), "\n")
				assert.NotContains(t, err.Error(), "\x1b")
			}
		})
	}
}

func TestNotFoundHelpersPreservePluginUnavailable(t *testing.T) {
	for _, operation := range []string{"get", "delete"} {
		for _, pluginMissing := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/plugin-missing-%t", operation, pluginMissing), func(t *testing.T) {
				client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					assert.Equal(t, "/api/plugins/grafana-agento11y-app/resources/eval/experiments/example", r.URL.Path)
					if operation == "get" {
						assert.Equal(t, http.MethodGet, r.Method)
					} else {
						assert.Equal(t, http.MethodDelete, r.Method)
					}
					w.WriteHeader(http.StatusNotFound)
					if pluginMissing {
						writeJSON(w, map[string]string{"message": "Plugin not found"})
					} else {
						writeJSON(w, map[string]string{"message": "experiment not found"})
					}
				}))
				sentinel := errors.New("example: not found")
				var err error
				if operation == "get" {
					_, err = agento11yhttp.DoJSONNotFound[any, testItem](context.Background(), client, http.MethodGet, "/eval/experiments/example", nil, sentinel, http.StatusOK)
				} else {
					err = agento11yhttp.DoStatusNotFound[any](context.Background(), client, http.MethodDelete, "/eval/experiments/example", nil, sentinel, http.StatusNoContent)
				}
				require.Error(t, err)
				if pluginMissing {
					var pluginErr *agento11yhttp.PluginUnavailableError
					require.ErrorAs(t, err, &pluginErr)
					assert.NotErrorIs(t, err, sentinel)
				} else {
					assert.ErrorIs(t, err, sentinel)
				}
			})
		}
	}
}
