package loki_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/query/loki"
	"github.com/grafana/gcx/internal/queryerror"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"
)

func TestBuildPathsEscapeDatasourceUID(t *testing.T) {
	c := &loki.Client{}
	uid := "uid/../admin"
	escapedUID := url.PathEscape(uid)

	tests := []struct {
		name string
		path string
	}{
		{"labels", c.BuildLabelsPath(uid)},
		{"labelValues", c.BuildLabelValuesPath(uid, "job")},
		{"series", c.BuildSeriesPath(uid)},
		{"patterns", c.BuildPatternsPath(uid)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if strings.Contains(tt.path, uid) && !strings.Contains(tt.path, escapedUID) {
				t.Errorf("path contains unescaped UID: %s", tt.path)
			}
			if !strings.Contains(tt.path, escapedUID) {
				t.Errorf("path missing escaped UID %q: %s", escapedUID, tt.path)
			}
		})
	}
}

func TestClient_Patterns(t *testing.T) {
	var capturedPath string
	var capturedQuery url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		capturedQuery = r.URL.Query()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"success","data":[{"pattern":"<_> level=error <_>","samples":[[1711839260,1],[1711839270,2]]}]}`))
	}))
	defer server.Close()

	cfg := config.NamespacedRESTConfig{Config: rest.Config{Host: server.URL}, Namespace: "default"}
	client, err := loki.NewClient(cfg)
	require.NoError(t, err)

	start := time.Unix(1711839260, 0)
	end := time.Unix(1711839280, 0)
	resp, err := client.Patterns(context.Background(), "loki-uid", `{job="varlogs"}`, start, end, "10s")
	require.NoError(t, err)

	assert.Equal(t, "/api/datasources/uid/loki-uid/resources/patterns", capturedPath)
	assert.Equal(t, `{job="varlogs"}`, capturedQuery.Get("query"))
	assert.Equal(t, strconv.FormatInt(start.UnixNano(), 10), capturedQuery.Get("start"))
	assert.Equal(t, strconv.FormatInt(end.UnixNano(), 10), capturedQuery.Get("end"))
	assert.Equal(t, "10s", capturedQuery.Get("step"))

	require.Len(t, resp.Data, 1)
	assert.Equal(t, "<_> level=error <_>", resp.Data[0].Pattern)
	assert.Equal(t, [][]int64{{1711839260, 1}, {1711839270, 2}}, resp.Data[0].Samples)
}

func TestClient_Patterns_OmitsStepWhenEmpty(t *testing.T) {
	var capturedRawQuery string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedRawQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"success","data":[]}`))
	}))
	defer server.Close()

	cfg := config.NamespacedRESTConfig{Config: rest.Config{Host: server.URL}, Namespace: "default"}
	client, err := loki.NewClient(cfg)
	require.NoError(t, err)

	_, err = client.Patterns(context.Background(), "loki-uid", `{job="varlogs"}`, time.Unix(1, 0), time.Unix(2, 0), "")
	require.NoError(t, err)

	assert.NotContains(t, capturedRawQuery, "step=")
}

func TestQuery_FallsBackOn403(t *testing.T) {
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		if r.URL.Path != "/api/ds/query" {
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"message":"forbidden"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"results":{"A":{"frames":[]}}}`))
	}))
	defer server.Close()

	cfg := config.NamespacedRESTConfig{
		Config:    rest.Config{Host: server.URL},
		Namespace: "default",
	}
	client, err := loki.NewClient(cfg)
	require.NoError(t, err)

	_, err = client.Query(context.Background(), "loki-uid", loki.QueryRequest{Query: `{job="grafana"}`})
	require.NoError(t, err)

	require.Len(t, paths, 2)
	assert.Contains(t, paths[0], "/apis/query.grafana.app/v0alpha1/namespaces/default/query")
	assert.Equal(t, "/api/ds/query", paths[1])
}

func TestQuery_ReturnsTypedAPIErrorForGrafanaEnvelope(t *testing.T) {
	// A 400 from the k8s query API now triggers the fallback, so both endpoints
	// return the same envelope; the typed error from the final response surfaces.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"results":{"A":{"error":"parse error at line 1, col 12: syntax error: unexpected IDENTIFIER, expecting STRING","errorSource":"downstream","status":400}}}`))
	}))
	defer server.Close()

	cfg := config.NamespacedRESTConfig{
		Config:    rest.Config{Host: server.URL},
		Namespace: "default",
	}
	client, err := loki.NewClient(cfg)
	require.NoError(t, err)

	_, err = client.Query(context.Background(), "loki-uid", loki.QueryRequest{Query: `{namespace=tempoprod10}`})
	require.Error(t, err)

	var apiErr *queryerror.APIError
	require.ErrorAs(t, err, &apiErr)
	assert.Equal(t, "loki", apiErr.Datasource)
	assert.Equal(t, "query", apiErr.Operation)
	assert.Equal(t, http.StatusBadRequest, apiErr.StatusCode)
	assert.Equal(t, "parse error at line 1, col 12: syntax error: unexpected IDENTIFIER, expecting STRING", apiErr.Message)
	assert.Equal(t, "downstream", apiErr.ErrorSource)
}

func TestQuery_SendsMaxLines(t *testing.T) {
	var gotBody []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"results":{"A":{"frames":[]}}}`))
	}))
	defer server.Close()

	cfg := config.NamespacedRESTConfig{
		Config:    rest.Config{Host: server.URL},
		Namespace: "default",
	}
	client, err := loki.NewClient(cfg)
	require.NoError(t, err)

	start := time.Unix(1, 0)
	end := time.Unix(2, 0)
	_, err = client.Query(context.Background(), "loki-uid", loki.QueryRequest{
		Query: `{job="grafana"}`,
		Start: start,
		End:   end,
		Limit: 1000,
	})
	require.NoError(t, err)
	require.NotEmpty(t, gotBody)

	var payload struct {
		Queries []map[string]any `json:"queries"`
	}
	require.NoError(t, json.Unmarshal(gotBody, &payload))
	require.Len(t, payload.Queries, 1)
	assert.InDelta(t, float64(1000), payload.Queries[0]["maxLines"], 0)
}
