package prometheus_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/query/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"
)

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
	client, err := prometheus.NewClient(cfg)
	require.NoError(t, err)

	_, err = client.Query(context.Background(), "prom-uid", prometheus.QueryRequest{Query: "up"})
	require.NoError(t, err)

	require.Len(t, paths, 2)
	assert.Contains(t, paths[0], "/apis/query.grafana.app/v0alpha1/namespaces/default/query")
	assert.Equal(t, "/api/ds/query", paths[1])
}

func TestClient_Labels_Match(t *testing.T) {
	var (
		capturedPath  string
		capturedQuery url.Values
	)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		capturedQuery = r.URL.Query()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"success","data":["__name__","instance","job"]}`))
	}))
	defer srv.Close()

	client := newTestClient(t, srv.URL)

	resp, err := client.Labels(context.Background(), "prom-uid", []string{"http_requests_total", `{job="api"}`})
	require.NoError(t, err)

	assert.Equal(t, "/api/datasources/uid/prom-uid/resources/api/v1/labels", capturedPath)
	assert.Equal(t, []string{"http_requests_total", `{job="api"}`}, capturedQuery["match[]"])
	require.NotNil(t, resp)
	assert.Equal(t, []string{"__name__", "instance", "job"}, resp.Data)
}

func TestClient_Labels_NoMatch(t *testing.T) {
	var capturedRawQuery string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedRawQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"success","data":[]}`))
	}))
	defer srv.Close()

	client := newTestClient(t, srv.URL)

	_, err := client.Labels(context.Background(), "prom-uid", nil)
	require.NoError(t, err)

	assert.Empty(t, capturedRawQuery)
}

func TestClient_LabelValues_Match(t *testing.T) {
	var (
		capturedPath  string
		capturedQuery url.Values
	)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		capturedQuery = r.URL.Query()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"success","data":["api","worker"]}`))
	}))
	defer srv.Close()

	client := newTestClient(t, srv.URL)

	resp, err := client.LabelValues(context.Background(), "prom-uid", "job", []string{"http_requests_total"}, 0)
	require.NoError(t, err)

	assert.Equal(t, "/api/datasources/uid/prom-uid/resources/api/v1/label/job/values", capturedPath)
	assert.Equal(t, []string{"http_requests_total"}, capturedQuery["match[]"])
	assert.NotContains(t, capturedQuery, "limit", "limit <= 0 must not send a limit param")
	require.NotNil(t, resp)
	assert.Equal(t, []string{"api", "worker"}, resp.Data)
}

func TestClient_LabelValues_Limit(t *testing.T) {
	var capturedQuery url.Values

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedQuery = r.URL.Query()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"success","data":["api"]}`))
	}))
	defer srv.Close()

	client := newTestClient(t, srv.URL)

	_, err := client.LabelValues(context.Background(), "prom-uid", "job", nil, 5)
	require.NoError(t, err)

	assert.Equal(t, []string{"5"}, capturedQuery["limit"])
}

func TestClient_LabelValues_NoMatch(t *testing.T) {
	var capturedRawQuery string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedRawQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"success","data":[]}`))
	}))
	defer srv.Close()

	client := newTestClient(t, srv.URL)

	_, err := client.LabelValues(context.Background(), "prom-uid", "job", nil, 0)
	require.NoError(t, err)

	assert.Empty(t, capturedRawQuery)
}

func TestBuildPathsEscapeDatasourceUID(t *testing.T) {
	c := &prometheus.Client{}
	uid := "uid/../admin"
	escapedUID := url.PathEscape(uid)

	tests := []struct {
		name string
		path string
	}{
		{"labels", c.BuildLabelsPath(uid)},
		{"labelValues", c.BuildLabelValuesPath(uid, "job")},
		{"metadata", c.BuildMetadataPath(uid)},
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
