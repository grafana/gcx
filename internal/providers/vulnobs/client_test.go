package vulnobs_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/providers/vulnobs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"
)

func newTestClient(t *testing.T, srv *httptest.Server) *vulnobs.Client {
	t.Helper()
	c, err := vulnobs.NewClient(config.NamespacedRESTConfig{
		Config:    rest.Config{Host: srv.URL},
		Namespace: "stack-123",
	})
	require.NoError(t, err)
	return c
}

// requestBody decodes the incoming POST body into a generic GraphQL request shape.
func requestBody(t *testing.T, r *http.Request) (string, string, map[string]any) {
	t.Helper()
	b, err := io.ReadAll(r.Body)
	require.NoError(t, err)
	var body struct {
		OperationName string         `json:"operationName"`
		Query         string         `json:"query"`
		Variables     map[string]any `json:"variables"`
	}
	require.NoError(t, json.Unmarshal(b, &body))
	return body.OperationName, body.Query, body.Variables
}

func writeData(t *testing.T, w http.ResponseWriter, data any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	require.NoError(t, json.NewEncoder(w).Encode(map[string]any{"data": data}))
}

func writeErrors(t *testing.T, w http.ResponseWriter, messages ...string) {
	t.Helper()
	errs := make([]map[string]any, 0, len(messages))
	for _, m := range messages {
		errs = append(errs, map[string]any{"message": m})
	}
	w.Header().Set("Content-Type", "application/json")
	require.NoError(t, json.NewEncoder(w).Encode(map[string]any{"errors": errs}))
}

func TestClient_Groups_PostsAndDecodes(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/api/plugin-proxy/grafana-vulnerabilityobs-app/api-proxy/graphql/query", r.URL.Path)
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
		op, query, _ := requestBody(t, r)
		assert.Equal(t, "Groups", op)
		assert.Contains(t, query, "groups")
		writeData(t, w, map[string]any{
			"groups": []map[string]any{
				{"id": 57, "name": "feO11y"},
				{"id": 16, "name": "o11y"},
			},
		})
	}))
	defer srv.Close()

	got, err := newTestClient(t, srv).Groups(context.Background())
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Equal(t, vulnobs.Group{ID: 57, Name: "feO11y"}, got[0])
}

func TestClient_GraphQLErrors_Surface(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeErrors(t, w, "introspection disabled")
	}))
	defer srv.Close()

	_, err := newTestClient(t, srv).Groups(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "introspection disabled")
}

func TestClient_HTTPErrors_Surface(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte("upstream unavailable"))
	}))
	defer srv.Close()

	_, err := newTestClient(t, srv).Issues(context.Background(), "10355")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "HTTP 502")
	assert.Contains(t, err.Error(), "upstream unavailable")
}

func TestClient_Projects_PassesFilters(t *testing.T) {
	var captured map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _, vars := requestBody(t, r)
		captured = vars
		writeData(t, w, map[string]any{
			"sources": map[string]any{
				"metadata": map[string]any{"totalCount": 1},
				"response": []map[string]any{
					{
						"id":   1064,
						"name": "grafana/faro-web-sdk",
						"versions": []map[string]any{
							{"id": 10354, "tag": "main", "totalCveCounts": map[string]any{"critical": 3}},
						},
					},
				},
			},
		})
	}))
	defer srv.Close()

	sources, total, err := newTestClient(t, srv).Projects(context.Background(), vulnobs.ProjectsOptions{
		GroupID:      "57",
		SortBy:       "CRITICALS_DESC",
		First:        30,
		HideK8s:      true,
		ShowArchived: false,
	})
	require.NoError(t, err)
	require.Equal(t, 1, total)
	require.Len(t, sources, 1)
	assert.Equal(t, "grafana/faro-web-sdk", sources[0].Name)
	assert.Equal(t, 3, sources[0].Versions[0].TotalCveCounts.Critical)

	// Variables shape sanity-check: top-level first/after, filters nested.
	assert.EqualValues(t, 30, captured["first"])
	filters, ok := captured["filters"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "57", filters["groupId"])
	assert.Equal(t, "CRITICALS_DESC", filters["sortBy"])
	vf, ok := filters["versionFilters"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, true, vf["hideK8s"])
	assert.Equal(t, false, vf["showArchived"])
}

func TestClient_Issues_RequiresVersionID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		t.Fatal("server should not be called when versionId is empty")
	}))
	defer srv.Close()

	_, err := newTestClient(t, srv).Issues(context.Background(), "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "versionID is required")
}

func TestClient_ResolveGroupID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeData(t, w, map[string]any{
			"groups": []map[string]any{
				{"id": 57, "name": "feO11y"},
				{"id": 16, "name": "o11y"},
			},
		})
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	ctx := context.Background()

	got, err := c.ResolveGroupID(ctx, "57")
	require.NoError(t, err)
	assert.Equal(t, "57", got, "numeric pass-through")

	got, err = c.ResolveGroupID(ctx, "feO11y")
	require.NoError(t, err)
	assert.Equal(t, "57", got, "name lookup")

	got, err = c.ResolveGroupID(ctx, "FEO11Y")
	require.NoError(t, err)
	assert.Equal(t, "57", got, "case-insensitive name lookup")

	_, err = c.ResolveGroupID(ctx, "no-such-group")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown group")
}

func TestClient_ResolveVersion(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		op, _, _ := requestBody(t, r)
		assert.Equal(t, "Projects", op)
		writeData(t, w, map[string]any{
			"sources": map[string]any{
				"metadata": map[string]any{"totalCount": 1},
				"response": []map[string]any{
					{
						"id":   1064,
						"name": "grafana/faro-web-sdk",
						"versions": []map[string]any{
							{"id": 10354, "tag": "main", "publishDate": "2026-05-15"},
							{"id": 6297262, "tag": "v2.6.3", "publishDate": "2026-05-11"},
						},
					},
				},
			},
		})
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	ctx := context.Background()

	got, err := c.ResolveVersion(ctx, "grafana/faro-web-sdk", "main")
	require.NoError(t, err)
	assert.Equal(t, "10354", got)

	got, err = c.ResolveVersion(ctx, "grafana/faro-web-sdk", "v2.6.3")
	require.NoError(t, err)
	assert.Equal(t, "6297262", got)

	got, err = c.ResolveVersion(ctx, "faro-web-sdk", "main")
	require.NoError(t, err, "suffix match should work")
	assert.Equal(t, "10354", got)
}
