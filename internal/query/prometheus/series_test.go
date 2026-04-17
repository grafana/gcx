package prometheus_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/query/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"
)

func newTestClient(t *testing.T, srvURL string) *prometheus.Client {
	t.Helper()
	cfg := config.NamespacedRESTConfig{
		Config:    rest.Config{Host: srvURL, BearerToken: "test-token"},
		Namespace: "default",
	}
	client, err := prometheus.NewClient(cfg)
	require.NoError(t, err)
	return client
}

func TestClient_Series(t *testing.T) {
	var (
		capturedPath  string
		capturedQuery url.Values
		capturedAuth  string
	)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		capturedQuery = r.URL.Query()
		capturedAuth = r.Header.Get("Authorization")

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(prometheus.SeriesResponse{
			Status: "success",
			Data: []map[string]string{
				{"__name__": "up", "job": "prometheus"},
				{"__name__": "up", "job": "node"},
			},
		})
	}))
	defer srv.Close()

	client := newTestClient(t, srv.URL)

	start := time.Unix(1700000000, 0)
	end := time.Unix(1700003600, 0)
	resp, err := client.Series(
		context.Background(),
		"grafanacloud-usage",
		[]string{`{__name__="up"}`, `{job="node"}`},
		start, end,
	)
	require.NoError(t, err)

	assert.Equal(t, "/api/datasources/uid/grafanacloud-usage/resources/api/v1/series", capturedPath)
	assert.Equal(t, []string{`{__name__="up"}`, `{job="node"}`}, capturedQuery["match[]"])
	assert.Equal(t, "1700000000", capturedQuery.Get("start"))
	assert.Equal(t, "1700003600", capturedQuery.Get("end"))
	assert.Equal(t, "Bearer test-token", capturedAuth)

	require.NotNil(t, resp)
	assert.Equal(t, "success", resp.Status)
	require.Len(t, resp.Data, 2)
	assert.Equal(t, "up", resp.Data[0]["__name__"])
	assert.Equal(t, "prometheus", resp.Data[0]["job"])
}

func TestClient_Series_NoTimeRange(t *testing.T) {
	var capturedQuery url.Values

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedQuery = r.URL.Query()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(prometheus.SeriesResponse{Status: "success"})
	}))
	defer srv.Close()

	client := newTestClient(t, srv.URL)

	_, err := client.Series(
		context.Background(),
		"test",
		[]string{`{a="b"}`},
		time.Time{}, time.Time{},
	)
	require.NoError(t, err)

	assert.Empty(t, capturedQuery.Get("start"))
	assert.Empty(t, capturedQuery.Get("end"))
	assert.Equal(t, []string{`{a="b"}`}, capturedQuery["match[]"])
}

func TestClient_Series_NonOK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":"forbidden"}`))
	}))
	defer srv.Close()

	client := newTestClient(t, srv.URL)

	_, err := client.Series(
		context.Background(),
		"test",
		[]string{`{a="b"}`},
		time.Time{}, time.Time{},
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "403")
}

func TestClient_BuildSeriesPathEscapesUID(t *testing.T) {
	c := &prometheus.Client{}
	path := c.BuildSeriesPath("uid/../admin")
	assert.Contains(t, path, "uid%2F..%2Fadmin")
	assert.NotContains(t, path, "uid/../admin")
}
