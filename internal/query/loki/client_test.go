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

func TestTimeFlagSendsOneMinuteWindow(t *testing.T) {
	ts := time.Date(2026, 1, 15, 10, 30, 0, 0, time.UTC)
	wantFrom := strconv.FormatInt(ts.Add(-time.Minute).UnixMilli(), 10)
	wantTo := strconv.FormatInt(ts.UnixMilli(), 10)

	tests := []struct {
		name string
		call func(*loki.Client)
	}{
		{
			name: "Query",
			call: func(c *loki.Client) {
				_, _ = c.Query(context.Background(), "loki-uid", loki.QueryRequest{
					Query: `{job="varlogs"}`,
					Start: ts,
					// End is zero — signals --time (instant at timestamp)
				})
			},
		},
		{
			name: "MetricQuery",
			call: func(c *loki.Client) {
				_, _ = c.MetricQuery(context.Background(), "loki-uid", loki.QueryRequest{
					Query: `rate({job="varlogs"}[5m])`,
					Start: ts,
					// End is zero — signals --time (instant at timestamp)
				})
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var captured map[string]any
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, _ := io.ReadAll(r.Body)
				_ = json.Unmarshal(body, &captured)
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{"results":{"A":{}}}`))
			}))
			defer server.Close()

			cfg := config.NamespacedRESTConfig{
				Config:    rest.Config{Host: server.URL},
				Namespace: "default",
			}
			c, err := loki.NewClient(cfg)
			require.NoError(t, err)

			tt.call(c)

			require.NotNil(t, captured)
			assert.Equal(t, wantFrom, captured["from"])
			assert.Equal(t, wantTo, captured["to"])

			queries, _ := captured["queries"].([]any)
			require.Len(t, queries, 1)
			query, _ := queries[0].(map[string]any)
			assert.Equal(t, true, query["instant"])
		})
	}
}
