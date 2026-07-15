package cloudmonitoring_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/query/cloudmonitoring"
	"github.com/grafana/gcx/internal/queryerror"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"
)

func newTestClient(t *testing.T, handler http.Handler) *cloudmonitoring.Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	cfg := config.NamespacedRESTConfig{
		Config:    rest.Config{Host: srv.URL},
		Namespace: "default",
	}
	client, err := cloudmonitoring.NewClient(cfg)
	require.NoError(t, err)
	return client
}

func testQueryReq() cloudmonitoring.QueryRequest {
	return cloudmonitoring.QueryRequest{
		Project:    "my-project",
		MetricType: "compute.googleapis.com/instance/cpu/utilization",
		Reducer:    "REDUCE_MEAN",
		Aligner:    "ALIGN_MEAN",
		GroupBys:   []string{"resource.label.instance_name"},
		Filters:    map[string]string{"resource.label.zone": "us-east1-b"},
		Start:      time.Date(2026, 7, 14, 0, 0, 0, 0, time.UTC),
		End:        time.Date(2026, 7, 14, 1, 0, 0, 0, time.UTC),
	}
}

func TestQuery(t *testing.T) {
	t.Run("parses frames with labels and display name", func(t *testing.T) {
		client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"results":{"A":{"frames":[{"schema":{"fields":[{"name":"Time","type":"time"},{"name":"value","type":"number","labels":{"resource.type":"gce_instance"},"config":{"displayNameFromDS":"cpu/utilization"}}]},"data":{"values":[[1752451200000,1752451260000],[0.5,null]]}}]}}}`))
		}))

		resp, err := client.Query(context.Background(), "test-uid", testQueryReq())
		require.NoError(t, err)
		require.Len(t, resp.Frames, 1)
		frame := resp.Frames[0]
		assert.Equal(t, "cpu/utilization", frame.Name)
		assert.Equal(t, "gce_instance", frame.Labels["resource.type"])
		require.Len(t, frame.Values, 2)
		assert.InDelta(t, 0.5, *frame.Values[0], 0.001)
		assert.Nil(t, frame.Values[1], "null datapoints stay nil (gaps)")
	})

	// Pins the wire shape: filter triplets joined with AND, query-level
	// queryType timeSeriesList, and the auto alignment period default.
	t.Run("request body shape", func(t *testing.T) {
		var (
			captured   map[string]any
			decodedErr error
		)
		client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			decodedErr = json.NewDecoder(r.Body).Decode(&captured)
			_, _ = w.Write([]byte(`{"results":{"A":{"frames":[]}}}`))
		}))

		_, err := client.Query(context.Background(), "test-uid", testQueryReq())
		require.NoError(t, err)
		require.NoError(t, decodedErr)

		queries, ok := captured["queries"].([]any)
		require.True(t, ok)
		require.Len(t, queries, 1)
		q, ok := queries[0].(map[string]any)
		require.True(t, ok)

		assert.Equal(t, "timeSeriesList", q["queryType"])
		ds, ok := q["datasource"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "stackdriver", ds["type"])

		tsl, ok := q["timeSeriesList"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "my-project", tsl["projectName"])
		assert.Equal(t, "REDUCE_MEAN", tsl["crossSeriesReducer"])
		assert.Equal(t, "ALIGN_MEAN", tsl["perSeriesAligner"])
		assert.Equal(t, "cloud-monitoring-auto", tsl["alignmentPeriod"])
		assert.Equal(t, []any{"resource.label.instance_name"}, tsl["groupBys"])
		assert.Equal(t, []any{
			"metric.type", "=", "compute.googleapis.com/instance/cpu/utilization",
			"AND", "resource.label.zone", "=", "us-east1-b",
		}, tsl["filters"])
	})

	// Captured live: the plugin prefixes GCP's JSON error envelope with
	// "query failed: " — simplification must find the envelope mid-string.
	t.Run("GCP error envelope simplified", func(t *testing.T) {
		raw := "query failed: {\n  \"error\": {\n    \"code\": 404,\n    \"message\": \"Cannot find metric(s) that match type\",\n    \"status\": \"NOT_FOUND\"\n  }\n}"
		client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			body, _ := json.Marshal(map[string]any{"results": map[string]any{"A": map[string]any{"error": raw, "status": 400}}})
			_, _ = w.Write(body)
		}))

		_, err := client.Query(context.Background(), "test-uid", testQueryReq())
		require.Error(t, err)

		var apiErr *queryerror.APIError
		require.ErrorAs(t, err, &apiErr)
		assert.Equal(t, "cloudmonitoring", apiErr.Datasource)
		assert.Equal(t, "NOT_FOUND: Cannot find metric(s) that match type", apiErr.Message)
	})

	t.Run("plain error passes through unchanged", func(t *testing.T) {
		client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"results":{"A":{"error":"context deadline exceeded","status":500}}}`))
		}))

		_, err := client.Query(context.Background(), "test-uid", testQueryReq())
		require.Error(t, err)

		var apiErr *queryerror.APIError
		require.ErrorAs(t, err, &apiErr)
		assert.Equal(t, "context deadline exceeded", apiErr.Message)
	})
}

func TestListProjects(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/datasources/uid/test-uid/resources/projects", r.URL.Path)
		_, _ = w.Write([]byte(`[{"label":"My Project","value":"my-project"}]`))
	}))

	projects, err := client.ListProjects(context.Background(), "test-uid")
	require.NoError(t, err)
	require.Len(t, projects, 1)
	assert.Equal(t, cloudmonitoring.Project{ID: "my-project", Name: "My Project"}, projects[0])
}

func TestListMetricDescriptors(t *testing.T) {
	// Pins the resource path shape: the GCP API path rides after the
	// metricDescriptors route prefix, with a starts_with filter.
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/datasources/uid/test-uid/resources/metricDescriptors/v3/projects/my-project/metricDescriptors", r.URL.Path)
		assert.Equal(t, `metric.type = starts_with("compute.googleapis.com")`, r.URL.Query().Get("filter"))
		_, _ = w.Write([]byte(`[{"type":"compute.googleapis.com/instance/cpu/utilization","displayName":"CPU utilization","metricKind":"GAUGE","valueType":"DOUBLE","unit":"10^2.%","service":"compute.googleapis.com"}]`))
	}))

	descriptors, err := client.ListMetricDescriptors(context.Background(), "test-uid", "my-project", "compute.googleapis.com")
	require.NoError(t, err)
	require.Len(t, descriptors, 1)
	assert.Equal(t, "compute.googleapis.com/instance/cpu/utilization", descriptors[0].Type)
	assert.Equal(t, "GAUGE", descriptors[0].MetricKind)
}
