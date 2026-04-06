package sigilhttp_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/providers/sigil/sigilhttp"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"
)

func newTestClient(t *testing.T, handler http.Handler) *sigilhttp.Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	cfg := config.NamespacedRESTConfig{
		Config:    rest.Config{Host: srv.URL},
		Namespace: "default",
	}
	client, err := sigilhttp.NewClient(cfg)
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
		assert.Equal(t, "/api/plugins/grafana-sigil-app/resources/query/test", r.URL.Path)
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

	items, err := sigilhttp.ListAll[testItem](context.Background(), client, "/query/items", nil)
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

	items, err := sigilhttp.ListAll[testItem](context.Background(), client, "/query/items", nil)
	require.NoError(t, err)
	assert.Len(t, items, 2)
	assert.Equal(t, 2, callCount)
}

func TestListAll_EmptyResponse(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		writeJSON(w, map[string]any{"items": []any{}})
	}))

	items, err := sigilhttp.ListAll[testItem](context.Background(), client, "/query/items", nil)
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

	items, err := sigilhttp.ListAll[testItem](context.Background(), client, "/query/items", nil, 2)
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

	items, err := sigilhttp.ListAll[testItem](context.Background(), client, "/query/items", nil, 1)
	require.NoError(t, err)
	assert.Len(t, items, 1)
	assert.Equal(t, 1, callCount, "should not fetch second page when limit already reached")
}

func TestListAll_ErrorResponse(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "forbidden", http.StatusForbidden)
	}))

	_, err := sigilhttp.ListAll[testItem](context.Background(), client, "/query/items", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "403")
}

func TestShouldDefaultDetailToYAML(t *testing.T) {
	tests := []struct {
		name      string
		agentMode bool
		args      []string
		want      bool
	}{
		{
			name:      "defaults to yaml outside agent mode",
			agentMode: false,
			want:      true,
		},
		{
			name:      "keeps json default in agent mode",
			agentMode: true,
			want:      false,
		},
		{
			name:      "explicit output disables yaml override",
			agentMode: false,
			args:      []string{"--output=yaml"},
			want:      false,
		},
		{
			name:      "explicit json field selection disables yaml override",
			agentMode: false,
			args:      []string{"--json=field"},
			want:      false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			agent.SetFlag(tc.agentMode)
			t.Cleanup(func() { agent.SetFlag(false) })

			cmd := &cobra.Command{Use: "show"}
			flags := cmd.Flags()
			flags.StringP("output", "o", "table", "")
			flags.String("json", "", "")
			require.NoError(t, flags.Parse(tc.args))

			assert.Equal(t, tc.want, sigilhttp.ShouldDefaultDetailToYAML(cmd))
		})
	}
}
