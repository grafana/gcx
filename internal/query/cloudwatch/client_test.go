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
	"github.com/grafana/gcx/internal/queryerror"
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
	Name   string            `json:"name"`
	Type   string            `json:"type"`
	Labels map[string]string `json:"labels,omitempty"`
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
	return labeledFrame(tsMs, values, nil)
}

func labeledFrame(tsMs []float64, values []any, labels map[string]string) testFrame {
	var f testFrame
	f.Schema.Fields = []testField{
		{Name: "time", Type: "time"},
		{Name: "CPUUtilization", Type: "number", Labels: labels},
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
		Period:     "300",
		Start:      time.Date(2026, 5, 17, 0, 0, 0, 0, time.UTC),
		End:        time.Date(2026, 5, 17, 1, 0, 0, 0, time.UTC),
	}
}

func TestClient_Query(t *testing.T) {
	t.Run("parses time series", func(t *testing.T) {
		client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Contains(t, r.URL.Path, "/apis/query.grafana.app/v0alpha1/namespaces/default/query")
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
	})

	t.Run("multiple frames", func(t *testing.T) {
		client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(queryResultBody(t, testResultEntry{
				Frames: []testFrame{
					labeledFrame([]float64{1747000000000}, []any{1.0}, map[string]string{"InstanceId": "i-a"}),
					labeledFrame([]float64{1747000000000}, []any{2.0}, map[string]string{"InstanceId": "i-b"}),
				},
			}))
		}))

		resp, err := client.Query(context.Background(), "test-uid", testQueryReq())
		require.NoError(t, err)
		require.Len(t, resp.Frames, 2)
		assert.Equal(t, "i-a", resp.Frames[0].Labels["InstanceId"])
		assert.Equal(t, "i-b", resp.Frames[1].Labels["InstanceId"])
	})

	t.Run("empty values placeholder frame is dropped", func(t *testing.T) {
		client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(queryResultBody(t, testResultEntry{
				Frames: []testFrame{simpleFrame([]float64{}, []any{})},
			}))
		}))

		resp, err := client.Query(context.Background(), "test-uid", testQueryReq())
		require.NoError(t, err)
		assert.Empty(t, resp.Frames)
	})

	t.Run("error envelope", func(t *testing.T) {
		client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(queryResultBody(t, testResultEntry{Error: "metric not found", Status: 400}))
		}))

		_, err := client.Query(context.Background(), "test-uid", testQueryReq())
		require.Error(t, err)

		var apiErr *queryerror.APIError
		require.ErrorAs(t, err, &apiErr)
		assert.Equal(t, "cloudwatch", apiErr.Datasource)
		assert.Equal(t, 400, apiErr.StatusCode)
		assert.Contains(t, apiErr.Message, "metric not found")
	})

	t.Run("HTTP 4xx", func(t *testing.T) {
		client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, `{"message":"unauthorized"}`, http.StatusUnauthorized)
		}))

		_, err := client.Query(context.Background(), "test-uid", testQueryReq())
		require.Error(t, err)

		var apiErr *queryerror.APIError
		require.ErrorAs(t, err, &apiErr)
		assert.Equal(t, "cloudwatch", apiErr.Datasource)
		assert.Equal(t, "query", apiErr.Operation)
		assert.Equal(t, http.StatusUnauthorized, apiErr.StatusCode)
	})

	t.Run("HTTP 5xx", func(t *testing.T) {
		client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, `{"message":"internal error"}`, http.StatusInternalServerError)
		}))

		_, err := client.Query(context.Background(), "test-uid", testQueryReq())
		require.Error(t, err)

		var apiErr *queryerror.APIError
		require.ErrorAs(t, err, &apiErr)
		assert.Equal(t, "cloudwatch", apiErr.Datasource)
		assert.Equal(t, "query", apiErr.Operation)
		assert.Equal(t, http.StatusInternalServerError, apiErr.StatusCode)
	})

	t.Run("K8s fallback to legacy /api/ds/query", func(t *testing.T) {
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
	})

	t.Run("malformed JSON", func(t *testing.T) {
		client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{not valid json}`))
		}))

		_, err := client.Query(context.Background(), "test-uid", testQueryReq())
		require.Error(t, err)
	})

	// Pins the wire shape of the query payload. Period must be a JSON string,
	// intervalMs must be a JSON number, dimensions must be the multi-valued
	// map[string][]string. The Grafana CloudWatch plugin is sensitive to these
	// shapes; do not loosen these assertions without re-verifying against the
	// plugin.
	t.Run("request body shape", func(t *testing.T) {
		var (
			captured   map[string]any
			decodedErr error
		)
		client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			decodedErr = json.NewDecoder(r.Body).Decode(&captured)
			_, _ = w.Write(queryResultBody(t, testResultEntry{Frames: []testFrame{}}))
		}))

		req := testQueryReq()
		req.Dimensions = map[string][]string{"InstanceId": {"i-abc"}}
		_, err := client.Query(context.Background(), "test-uid", req)
		require.NoError(t, err)
		require.NoError(t, decodedErr)

		queries, ok := captured["queries"].([]any)
		require.True(t, ok, "queries should be an array")
		require.Len(t, queries, 1)
		q, ok := queries[0].(map[string]any)
		require.True(t, ok, "queries[0] must be a JSON object, got %T", queries[0])

		// Period: string (plugin requirement).
		period, ok := q["period"].(string)
		require.True(t, ok, "period must be a JSON string, got %T", q["period"])
		assert.Equal(t, "300", period)

		// intervalMs: number (decodes as float64 from JSON), defaults to Period*1000.
		intervalMs, ok := q["intervalMs"].(float64)
		require.True(t, ok, "intervalMs must be a JSON number, got %T", q["intervalMs"])
		assert.InDelta(t, 300_000, intervalMs, 0.5)

		// accountId absent when QueryRequest.AccountID is empty.
		_, hasAccount := q["accountId"]
		assert.False(t, hasAccount, "accountId must be omitted when AccountID is empty")

		// Dimensions: array-valued.
		dims, ok := q["dimensions"].(map[string]any)
		require.True(t, ok, "dimensions must be a JSON object, got %T", q["dimensions"])
		values, ok := dims["InstanceId"].([]any)
		require.True(t, ok, "dimension values must be a JSON array, got %T", dims["InstanceId"])
		require.Len(t, values, 1)
		assert.Equal(t, "i-abc", values[0])
	})

	// Pins the conditional branches in the query payload: accountId is added
	// only when non-empty, and intervalMs honors an explicit override.
	t.Run("conditional fields", func(t *testing.T) {
		t.Run("accountId='all' propagates", func(t *testing.T) {
			req := testQueryReq()
			req.AccountID = "all"
			q := captureQuery(t, req)
			assert.Equal(t, "all", q["accountId"])
		})

		t.Run("explicit IntervalMs overrides Period*1000 default", func(t *testing.T) {
			req := testQueryReq()
			req.IntervalMs = 60_000
			q := captureQuery(t, req)
			intervalMs, ok := q["intervalMs"].(float64)
			require.True(t, ok)
			assert.InDelta(t, 60_000, intervalMs, 0.5)
		})
	})
}

func captureQuery(t *testing.T, req cloudwatch.QueryRequest) map[string]any {
	t.Helper()
	var (
		captured   map[string]any
		decodedErr error
	)
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		decodedErr = json.NewDecoder(r.Body).Decode(&captured)
		_, _ = w.Write(queryResultBody(t, testResultEntry{Frames: []testFrame{}}))
	}))
	_, err := client.Query(context.Background(), "test-uid", req)
	require.NoError(t, err)
	require.NoError(t, decodedErr)
	queries, ok := captured["queries"].([]any)
	require.True(t, ok)
	require.Len(t, queries, 1)
	q, ok := queries[0].(map[string]any)
	require.True(t, ok)
	return q
}

func TestClient_Query_PeriodWireShape(t *testing.T) {
	t.Run("period='auto' forwarded literally", func(t *testing.T) {
		req := testQueryReq()
		req.Period = "auto"
		q := captureQuery(t, req)
		period, ok := q["period"].(string)
		require.True(t, ok, "period must be a JSON string, got %T", q["period"])
		assert.Equal(t, "auto", period)
	})

	t.Run("numeric period forwarded as string", func(t *testing.T) {
		req := testQueryReq()
		req.Period = "60"
		q := captureQuery(t, req)
		period, ok := q["period"].(string)
		require.True(t, ok)
		assert.Equal(t, "60", period)
		intervalMs, ok := q["intervalMs"].(float64)
		require.True(t, ok)
		assert.InDelta(t, 60_000, intervalMs, 0.5)
	})

	t.Run("period='auto' uses default intervalMs", func(t *testing.T) {
		req := testQueryReq()
		req.Period = "auto"
		q := captureQuery(t, req)
		intervalMs, ok := q["intervalMs"].(float64)
		require.True(t, ok, "intervalMs must be a JSON number, got %T", q["intervalMs"])
		assert.InDelta(t, 60_000, intervalMs, 0.5)
	})
}

// ---- resource list tests ----

type testResourceItem[T any] struct {
	Value T `json:"value"`
}

func TestClient_ListNamespaces(t *testing.T) {
	t.Run("parses namespace list", func(t *testing.T) {
		type item = testResourceItem[string]
		client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "/api/datasources/uid/test-uid/resources/namespaces", r.URL.Path)
			_, _ = w.Write(mustJSON(t, []item{{"AWS/EC2"}, {"AWS/Lambda"}}))
		}))

		result, err := client.ListNamespaces(context.Background(), "test-uid", "us-east-1", "")
		require.NoError(t, err)
		assert.Equal(t, []string{"AWS/EC2", "AWS/Lambda"}, result)
	})
}

func TestClient_ListMetrics(t *testing.T) {
	t.Run("parses metric list", func(t *testing.T) {
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
	})
}

func TestClient_ListDimensionKeys(t *testing.T) {
	t.Run("parses dimension key list", func(t *testing.T) {
		type item = testResourceItem[string]
		client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write(mustJSON(t, []item{{"InstanceId"}}))
		}))

		result, err := client.ListDimensionKeys(context.Background(), "test-uid", "us-east-1", "AWS/EC2", "CPUUtilization", "")
		require.NoError(t, err)
		assert.Equal(t, []string{"InstanceId"}, result)
	})
}

func TestClient_ListRegions(t *testing.T) {
	t.Run("parses region list", func(t *testing.T) {
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
	})
}

func TestClient_ListAccounts(t *testing.T) {
	t.Run("parses account list", func(t *testing.T) {
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
	})

	t.Run("404 returns clean error not panic", func(t *testing.T) {
		client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.NotFound(w, r)
		}))

		_, err := client.ListAccounts(context.Background(), "test-uid", "us-east-1")
		require.Error(t, err)
	})
}
