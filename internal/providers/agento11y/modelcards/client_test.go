package modelcards_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/providers/agento11y/agento11yhttp"
	"github.com/grafana/gcx/internal/providers/agento11y/modelcards"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"
)

const pluginPrefix = "/api/plugins/grafana-agento11y-app/resources"

func writeJSON(w http.ResponseWriter, v any) {
	if err := json.NewEncoder(w).Encode(v); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func newTestClient(t *testing.T, handler http.Handler) *modelcards.Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	base, err := agento11yhttp.NewClient(config.NamespacedRESTConfig{Config: rest.Config{Host: srv.URL}, Namespace: "default"})
	require.NoError(t, err)
	return modelcards.NewClient(base)
}

func TestClient_Lookup(t *testing.T) {
	tests := []struct {
		name  string
		query modelcards.LookupQuery
		want  url.Values
	}{
		{
			name:  "model key",
			query: modelcards.LookupQuery{ModelKey: "openai/gpt-4o-mini"},
			want:  url.Values{"model_key": {"openai/gpt-4o-mini"}},
		},
		{
			name:  "source pair",
			query: modelcards.LookupQuery{Source: "openrouter", SourceModelID: "openai/gpt-4o-mini"},
			want:  url.Values{"source": {"openrouter"}, "source_model_id": {"openai/gpt-4o-mini"}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodGet, r.Method)
				assert.Equal(t, pluginPrefix+"/query/model-cards/lookup", r.URL.Path)
				assert.Equal(t, tt.want, r.URL.Query())
				writeJSON(w, map[string]any{
					"data":      map[string]any{"model_key": "openai/gpt-4o-mini", "future_dimension": "retained"},
					"freshness": map[string]any{"stale": false},
				})
			}))

			result, err := client.Lookup(context.Background(), tt.query)
			require.NoError(t, err)
			data, ok := result["data"].(map[string]any)
			require.True(t, ok)
			assert.Equal(t, "retained", data["future_dimension"])
			assert.Contains(t, result, "freshness")
		})
	}
}

func TestClient_List_SendsFilters(t *testing.T) {
	free := true
	minContext := 100000
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, pluginPrefix+"/query/model-cards", r.URL.Path)
		assert.Equal(t, url.Values{
			"q":                  {"gpt"},
			"source":             {"openrouter"},
			"provider":           {"openai"},
			"regex":              {"^openai/"},
			"sort":               {"prompt_price"},
			"order":              {"desc"},
			"free_only":          {"true"},
			"min_context_length": {"100000"},
			"limit":              {"10"},
		}, r.URL.Query())
		writeJSON(w, map[string]any{"data": []any{}, "next_cursor": ""})
	}))

	result, err := client.List(context.Background(), modelcards.ListQuery{
		Q: "gpt", Source: "openrouter", Provider: "openai", Regex: "^openai/",
		Sort: "prompt_price", Order: "desc", FreeOnly: &free, MinContextLength: &minContext,
	}, 10)
	require.NoError(t, err)
	assert.Empty(t, result.Cards)
	assert.False(t, result.HasMore)
}

func TestClient_List_Paging(t *testing.T) {
	tests := []struct {
		name        string
		limit       int
		wantLimit   string
		wantCards   int
		wantMore    bool
		wantStale   bool
		wantCursors []string
	}{
		{name: "follows cursors until exhausted", limit: 0, wantLimit: "200", wantCards: 3, wantMore: false, wantStale: true, wantCursors: []string{"", "c1"}},
		{name: "truncates at limit and reports more", limit: 2, wantLimit: "2", wantCards: 2, wantMore: true, wantStale: true, wantCursors: []string{""}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var cursors []string
			client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, tt.wantLimit, r.URL.Query().Get("limit"))
				cursor := r.URL.Query().Get("cursor")
				cursors = append(cursors, cursor)
				if cursor == "" {
					writeJSON(w, map[string]any{
						"data":        []any{map[string]any{"model_key": "a"}, map[string]any{"model_key": "b"}},
						"next_cursor": "c1",
						"freshness":   map[string]any{"stale": true, "catalog_last_refreshed_at": "2026-01-02T03:04:05Z"},
					})
					return
				}
				writeJSON(w, map[string]any{"data": []any{map[string]any{"model_key": "c"}}, "next_cursor": ""})
			}))

			result, err := client.List(context.Background(), modelcards.ListQuery{}, tt.limit)
			require.NoError(t, err)
			assert.Len(t, result.Cards, tt.wantCards)
			assert.Equal(t, tt.wantMore, result.HasMore)
			// Page 2 omits freshness, which must not clear page 1's report.
			assert.Equal(t, tt.wantStale, result.Freshness.Stale)
			assert.Equal(t, "2026-01-02T03:04:05Z", result.Freshness.CatalogLastRefreshedAt)
			assert.Equal(t, tt.wantCursors, cursors)
		})
	}
}

func TestClient_List_ErrorStatus(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "invalid regex", http.StatusBadRequest)
	}))
	_, err := client.List(context.Background(), modelcards.ListQuery{Regex: "["}, 10)
	require.ErrorContains(t, err, "invalid regex")
}

func TestTable(t *testing.T) {
	cards := []modelcards.Card{
		{
			"model_key": "openai/gpt-4o-mini", "provider": "openai", "name": "GPT-4o mini", "context_length": float64(128000),
			"pricing": map[string]any{"prompt_usd_per_token": 0.00000015, "completion_usd_per_token": 0.0000006},
		},
		{"model_key": "x/unpriced", "is_free": true},
	}
	table := modelcards.Table()
	require.GreaterOrEqual(t, len(table.Columns), 6)

	cell := func(card modelcards.Card, header string) string {
		for _, col := range table.Columns {
			if col.Header == header {
				return col.Content(card)
			}
		}
		t.Fatalf("no column %q", header)
		return ""
	}
	assert.Equal(t, "128000", cell(cards[0], "CONTEXT"))
	assert.Equal(t, "0.15", cell(cards[0], "INPUT /1M"))
	assert.Equal(t, "0.6", cell(cards[0], "OUTPUT /1M"))
	assert.Equal(t, "-", cell(cards[1], "CONTEXT"))
	assert.Equal(t, "-", cell(cards[1], "INPUT /1M"))
	assert.Equal(t, "yes", cell(cards[1], "FREE"))
}
