package cloudwatch_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/query/cloudwatch"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"
)

func newTestClient(t *testing.T, handler http.Handler) *cloudwatch.Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	cfg := config.NamespacedRESTConfig{
		Config:    rest.Config{Host: srv.URL},
		Namespace: "default",
	}
	client, err := cloudwatch.NewClient(cfg)
	require.NoError(t, err)
	return client
}

// ---- typed test helpers (avoid errchkjson) ----

type testField struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

type testFrameData struct {
	Values []any `json:"values"`
}

type testFrame struct {
	Schema struct {
		Fields []testField `json:"fields"`
	} `json:"schema"`
	Data testFrameData `json:"data"`
}

type testResultEntry struct {
	Frames []testFrame `json:"frames,omitempty"`
	Error  string      `json:"error,omitempty"`
	Status int         `json:"status,omitempty"`
}

type testQueryResult struct {
	Results map[string]testResultEntry `json:"results"`
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	require.NoError(t, err)
	return b
}

func queryResultBody(t *testing.T, entry testResultEntry) []byte {
	t.Helper()
	return mustJSON(t, testQueryResult{Results: map[string]testResultEntry{"A": entry}})
}

func simpleFrame(tsMs []float64, values []any) testFrame {
	var f testFrame
	f.Schema.Fields = []testField{
		{Name: "time", Type: "time"},
		{Name: "CPUUtilization", Type: "number"},
	}
	f.Data.Values = []any{tsMs, values}
	return f
}

func testQueryReq() cloudwatch.QueryRequest {
	return cloudwatch.QueryRequest{
		Namespace:  "AWS/EC2",
		MetricName: "CPUUtilization",
		Region:     "us-east-1",
		Statistic:  "Average",
		Period:     300,
		Start:      time.Date(2026, 5, 17, 0, 0, 0, 0, time.UTC),
		End:        time.Date(2026, 5, 17, 1, 0, 0, 0, time.UTC),
	}
}

// ---- tests ----

func TestClient_Query_ParsesTimeSeries(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(queryResultBody(t, testResultEntry{
			Frames: []testFrame{
				simpleFrame([]float64{1747000000000, 1747000060000}, []any{10.0, 20.0}),
			},
		}))
	}))

	resp, err := client.Query(context.Background(), "test-uid", testQueryReq())
	require.NoError(t, err)
	require.NotEmpty(t, resp.Frames)
	assert.Len(t, resp.Frames[0].Timestamps, 2)
	require.NotNil(t, resp.Frames[0].Values[0])
	assert.InDelta(t, 10.0, *resp.Frames[0].Values[0], 0.001)
}

func TestClient_Query_MultipleFrames(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(queryResultBody(t, testResultEntry{
			Frames: []testFrame{
				simpleFrame([]float64{1747000000000}, []any{1.0}),
				simpleFrame([]float64{1747000000000}, []any{2.0}),
			},
		}))
	}))

	resp, err := client.Query(context.Background(), "test-uid", testQueryReq())
	require.NoError(t, err)
	assert.Len(t, resp.Frames, 2)
}

func TestClient_Query_EmptyValues(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(queryResultBody(t, testResultEntry{
			Frames: []testFrame{simpleFrame([]float64{}, []any{})},
		}))
	}))

	resp, err := client.Query(context.Background(), "test-uid", testQueryReq())
	require.NoError(t, err)
	require.Len(t, resp.Frames, 1)
	assert.Empty(t, resp.Frames[0].Timestamps)
}

func TestClient_Query_ErrorEnvelope(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(queryResultBody(t, testResultEntry{Error: "metric not found", Status: 400}))
	}))

	_, err := client.Query(context.Background(), "test-uid", testQueryReq())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "metric not found")
}

func TestClient_Query_HTTP4xx(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"message":"unauthorized"}`, http.StatusUnauthorized)
	}))

	_, err := client.Query(context.Background(), "test-uid", testQueryReq())
	require.Error(t, err)
}

func TestClient_Query_HTTP5xx(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"message":"internal error"}`, http.StatusInternalServerError)
	}))

	_, err := client.Query(context.Background(), "test-uid", testQueryReq())
	require.Error(t, err)
}

func TestClient_Query_K8sFallback(t *testing.T) {
	var paths []string
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		if r.URL.Path != "/api/ds/query" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(queryResultBody(t, testResultEntry{
			Frames: []testFrame{simpleFrame([]float64{1747000000000}, []any{5.0})},
		}))
	}))

	resp, err := client.Query(context.Background(), "test-uid", testQueryReq())
	require.NoError(t, err)
	assert.NotEmpty(t, resp.Frames)
	assert.Contains(t, paths, "/api/ds/query")
}

func TestClient_Query_MalformedJSON(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{not valid json}`))
	}))

	_, err := client.Query(context.Background(), "test-uid", testQueryReq())
	require.Error(t, err)
}

// ---- resource list tests ----

type testResourceItem[T any] struct {
	Value T `json:"value"`
}

func TestClient_ListNamespaces(t *testing.T) {
	type item = testResourceItem[string]
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/datasources/uid/test-uid/resources/namespaces", r.URL.Path)
		_, _ = w.Write(mustJSON(t, []item{{"AWS/EC2"}, {"AWS/Lambda"}}))
	}))

	result, err := client.ListNamespaces(context.Background(), "test-uid", "us-east-1", "")
	require.NoError(t, err)
	assert.Equal(t, []string{"AWS/EC2", "AWS/Lambda"}, result)
}

func TestClient_ListMetrics(t *testing.T) {
	type metricVal struct {
		Name      string `json:"name"`
		Namespace string `json:"namespace"`
	}
	type item = testResourceItem[metricVal]

	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/datasources/uid/test-uid/resources/metrics", r.URL.Path)
		_, _ = w.Write(mustJSON(t, []item{{metricVal{"CPUUtilization", "AWS/EC2"}}}))
	}))

	result, err := client.ListMetrics(context.Background(), "test-uid", "us-east-1", "AWS/EC2", "")
	require.NoError(t, err)
	require.Len(t, result, 1)
	assert.Equal(t, "CPUUtilization", result[0].Name)
	assert.Equal(t, "AWS/EC2", result[0].Namespace)
}

func TestClient_ListDimensionKeys(t *testing.T) {
	type item = testResourceItem[string]
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(mustJSON(t, []item{{"InstanceId"}}))
	}))

	result, err := client.ListDimensionKeys(context.Background(), "test-uid", "us-east-1", "AWS/EC2", "CPUUtilization", "")
	require.NoError(t, err)
	assert.Equal(t, []string{"InstanceId"}, result)
}

func TestClient_ListRegions(t *testing.T) {
	type regionVal struct {
		Name string `json:"name"`
	}
	type item = testResourceItem[regionVal]
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(mustJSON(t, []item{{regionVal{"us-east-1"}}, {regionVal{"eu-west-1"}}}))
	}))

	result, err := client.ListRegions(context.Background(), "test-uid")
	require.NoError(t, err)
	assert.Equal(t, []string{"us-east-1", "eu-west-1"}, result)
}

func TestClient_ListAccounts(t *testing.T) {
	type accountVal struct {
		ID    string `json:"id"`
		Label string `json:"label"`
		ARN   string `json:"arn"`
	}
	type item = testResourceItem[accountVal]
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(mustJSON(t, []item{{accountVal{"123456", "Prod", "arn:aws:iam::123456:root"}}}))
	}))

	result, err := client.ListAccounts(context.Background(), "test-uid", "us-east-1")
	require.NoError(t, err)
	require.Len(t, result, 1)
	assert.Equal(t, "123456", result[0].ID)
	assert.Equal(t, "Prod", result[0].Label)
}

func TestClient_ListAccounts_404_CleanError(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))

	_, err := client.ListAccounts(context.Background(), "test-uid", "us-east-1")
	require.Error(t, err) // must not panic
}
