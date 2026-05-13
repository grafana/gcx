package loki_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
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

func TestPatterns_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Contains(t, r.URL.Path, "/api/datasources/uid/loki-uid/resources/patterns")
		assert.Equal(t, `{job="varlogs"}`, r.URL.Query().Get("query"))
		assert.NotEmpty(t, r.URL.Query().Get("start"))
		assert.NotEmpty(t, r.URL.Query().Get("end"))

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"success","data":[{"pattern":"<_> level=info <_>","samples":[[1711839260,105],[1711839270,222]]}]}`))
	}))
	defer server.Close()

	cfg := config.NamespacedRESTConfig{
		Config:    rest.Config{Host: server.URL},
		Namespace: "default",
	}
	client, err := loki.NewClient(cfg)
	require.NoError(t, err)

	now := time.Now()
	resp, err := client.Patterns(context.Background(), "loki-uid", loki.PatternsRequest{
		Query: `{job="varlogs"}`,
		Start: now.Add(-1 * time.Hour),
		End:   now,
	})
	require.NoError(t, err)
	assert.Equal(t, "success", resp.Status)
	require.Len(t, resp.Data, 1)
	assert.Equal(t, "<_> level=info <_>", resp.Data[0].Pattern)
	assert.Len(t, resp.Data[0].Samples, 2)
}

func TestPatterns_HTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"status":"error","error":"parse error"}`))
	}))
	defer server.Close()

	cfg := config.NamespacedRESTConfig{
		Config:    rest.Config{Host: server.URL},
		Namespace: "default",
	}
	client, err := loki.NewClient(cfg)
	require.NoError(t, err)

	now := time.Now()
	_, err = client.Patterns(context.Background(), "loki-uid", loki.PatternsRequest{
		Query: `{job="bad"}`,
		Start: now.Add(-1 * time.Hour),
		End:   now,
	})
	require.Error(t, err)
}

func TestQuery_ReturnsTypedAPIErrorForGrafanaEnvelope(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Contains(t, r.URL.Path, "/apis/query.grafana.app/v0alpha1/namespaces/default/query")
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
