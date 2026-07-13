package azuremonitor_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/query/azuremonitor"
	"github.com/grafana/gcx/internal/queryerror"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"
)

func newTestClient(t *testing.T, handler http.Handler) *azuremonitor.Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	cfg := config.NamespacedRESTConfig{
		Config:    rest.Config{Host: srv.URL},
		Namespace: "default",
	}
	client, err := azuremonitor.NewClient(cfg)
	require.NoError(t, err)
	return client
}

// ---- typed test helpers (avoid errchkjson) ----

type testFieldConfig struct {
	DisplayNameFromDS string `json:"displayNameFromDS,omitempty"`
	Unit              string `json:"unit,omitempty"`
}

type testField struct {
	Name   string            `json:"name"`
	Type   string            `json:"type"`
	Labels map[string]string `json:"labels,omitempty"`
	Config *testFieldConfig  `json:"config,omitempty"`
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

func metricFrame(tsMs []float64, values []any, labels map[string]string, cfg *testFieldConfig) testFrame {
	var f testFrame
	f.Schema.Fields = []testField{
		{Name: "Time", Type: "time"},
		{Name: "Transactions", Type: "number", Labels: labels, Config: cfg},
	}
	f.Data.Values = []any{tsMs, values}
	return f
}

func testQueryReq() azuremonitor.QueryRequest {
	return azuremonitor.QueryRequest{
		Subscription:    "sub-1",
		ResourceGroup:   "my-rg",
		ResourceName:    "mystorage",
		MetricNamespace: "Microsoft.Storage/storageAccounts",
		MetricName:      "Transactions",
		Aggregation:     "Total",
		TimeGrain:       "auto",
		Start:           time.Date(2026, 5, 17, 0, 0, 0, 0, time.UTC),
		End:             time.Date(2026, 5, 17, 1, 0, 0, 0, time.UTC),
	}
}

func TestClient_Query(t *testing.T) {
	t.Run("parses time series with unit and labels", func(t *testing.T) {
		client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Contains(t, r.URL.Path, "/apis/query.grafana.app/v0alpha1/namespaces/default/query")
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(queryResultBody(t, testResultEntry{
				Frames: []testFrame{
					metricFrame([]float64{1747000000000, 1747000060000}, []any{10.0, 20.0},
						map[string]string{"apiname": "GetBlob"},
						&testFieldConfig{DisplayNameFromDS: "Transactions {GetBlob}", Unit: "short"}),
				},
			}))
		}))

		resp, err := client.Query(context.Background(), "test-uid", testQueryReq())
		require.NoError(t, err)
		require.Len(t, resp.Frames, 1)
		frame := resp.Frames[0]
		assert.Equal(t, "Transactions {GetBlob}", frame.Name)
		assert.Equal(t, "short", frame.Unit)
		assert.Equal(t, "GetBlob", frame.Labels["apiname"])
		require.Len(t, frame.Timestamps, 2)
		require.NotNil(t, frame.Values[0])
		assert.InDelta(t, 10.0, *frame.Values[0], 0.001)
	})

	t.Run("falls back to field name when displayNameFromDS is absent", func(t *testing.T) {
		client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(queryResultBody(t, testResultEntry{
				Frames: []testFrame{metricFrame([]float64{1747000000000}, []any{1.0}, nil, nil)},
			}))
		}))

		resp, err := client.Query(context.Background(), "test-uid", testQueryReq())
		require.NoError(t, err)
		require.Len(t, resp.Frames, 1)
		assert.Equal(t, "Transactions", resp.Frames[0].Name)
	})

	t.Run("null values preserved as gaps", func(t *testing.T) {
		client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(queryResultBody(t, testResultEntry{
				Frames: []testFrame{metricFrame([]float64{1747000000000, 1747000060000}, []any{nil, 2.0}, nil, nil)},
			}))
		}))

		resp, err := client.Query(context.Background(), "test-uid", testQueryReq())
		require.NoError(t, err)
		require.Len(t, resp.Frames, 1)
		require.Len(t, resp.Frames[0].Values, 2)
		assert.Nil(t, resp.Frames[0].Values[0])
		require.NotNil(t, resp.Frames[0].Values[1])
	})

	t.Run("empty placeholder frame is dropped", func(t *testing.T) {
		client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(queryResultBody(t, testResultEntry{
				Frames: []testFrame{metricFrame([]float64{}, []any{}, nil, nil)},
			}))
		}))

		resp, err := client.Query(context.Background(), "test-uid", testQueryReq())
		require.NoError(t, err)
		assert.Empty(t, resp.Frames)
	})

	t.Run("error envelope", func(t *testing.T) {
		client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(queryResultBody(t, testResultEntry{Error: "InvalidSubscriptionId", Status: 400}))
		}))

		_, err := client.Query(context.Background(), "test-uid", testQueryReq())
		require.Error(t, err)

		var apiErr *queryerror.APIError
		require.ErrorAs(t, err, &apiErr)
		assert.Equal(t, "azuremonitor", apiErr.Datasource)
		assert.Equal(t, 400, apiErr.StatusCode)
		assert.Contains(t, apiErr.Message, "InvalidSubscriptionId")
	})

	t.Run("malformed JSON", func(t *testing.T) {
		client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{not valid json}`))
		}))

		_, err := client.Query(context.Background(), "test-uid", testQueryReq())
		require.Error(t, err)
	})

	t.Run("missing refId result yields empty response", func(t *testing.T) {
		client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(mustJSON(t, testQueryResult{Results: map[string]testResultEntry{}}))
		}))

		resp, err := client.Query(context.Background(), "test-uid", testQueryReq())
		require.NoError(t, err)
		assert.Empty(t, resp.Frames)
	})

	t.Run("schema/data mismatch drops the frame", func(t *testing.T) {
		var f testFrame
		f.Schema.Fields = []testField{{Name: "Time", Type: "time"}, {Name: "Transactions", Type: "number"}}
		f.Data.Values = []any{[]float64{1747000000000}} // one column of data for two schema fields
		client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(queryResultBody(t, testResultEntry{Frames: []testFrame{f}}))
		}))

		resp, err := client.Query(context.Background(), "test-uid", testQueryReq())
		require.NoError(t, err)
		assert.Empty(t, resp.Frames)
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
				Frames: []testFrame{metricFrame([]float64{1747000000000}, []any{5.0}, nil, nil)},
			}))
		}))

		resp, err := client.Query(context.Background(), "test-uid", testQueryReq())
		require.NoError(t, err)
		assert.NotEmpty(t, resp.Frames)
		assert.Contains(t, paths, "/api/ds/query")
	})

	// Pins the wire shape of the query payload. The Azure Monitor plugin
	// backend builds the ARM URL from the query-level subscription field —
	// placing it only in resources[] produces a malformed ARM request
	// (InvalidSubscriptionId). Do not loosen these assertions without
	// re-verifying against the plugin.
	t.Run("request body shape", func(t *testing.T) {
		req := testQueryReq()
		req.DimensionFilters = map[string]string{"ApiName": "*"}
		req.Top = "10"
		q := captureQuery(t, req)

		assert.Equal(t, "Azure Monitor", q["queryType"])
		// subscription must be at the query level, not inside resources[].
		assert.Equal(t, "sub-1", q["subscription"])

		azm, ok := q["azureMonitor"].(map[string]any)
		require.True(t, ok, "azureMonitor must be a JSON object, got %T", q["azureMonitor"])
		assert.Equal(t, "Transactions", azm["metricName"])
		assert.Equal(t, "Microsoft.Storage/storageAccounts", azm["metricNamespace"])
		assert.Equal(t, "Total", azm["aggregation"])
		assert.Equal(t, "auto", azm["timeGrain"])
		assert.Equal(t, "10", azm["top"])

		resources, ok := azm["resources"].([]any)
		require.True(t, ok, "resources must be a JSON array, got %T", azm["resources"])
		require.Len(t, resources, 1)
		resource, ok := resources[0].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "my-rg", resource["resourceGroup"])
		assert.Equal(t, "mystorage", resource["resourceName"])
		_, hasSub := resource["subscription"]
		assert.False(t, hasSub, "subscription must not be inside resources[]")

		filters, ok := azm["dimensionFilters"].([]any)
		require.True(t, ok, "dimensionFilters must be a JSON array, got %T", azm["dimensionFilters"])
		require.Len(t, filters, 1)
		filter, ok := filters[0].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "ApiName", filter["dimension"])
		assert.Equal(t, "eq", filter["operator"])
		assert.Equal(t, []any{"*"}, filter["filters"])

		// intervalMs: number, derived from the time range with a 60s floor.
		intervalMs, ok := q["intervalMs"].(float64)
		require.True(t, ok, "intervalMs must be a JSON number, got %T", q["intervalMs"])
		assert.InDelta(t, 60_000, intervalMs, 0.5)
	})

	t.Run("optional fields omitted when empty", func(t *testing.T) {
		q := captureQuery(t, testQueryReq())

		azm, ok := q["azureMonitor"].(map[string]any)
		require.True(t, ok)
		_, hasTop := azm["top"]
		assert.False(t, hasTop, "top must be omitted when empty")
		_, hasRegion := azm["region"]
		assert.False(t, hasRegion, "region must be omitted when empty")

		filters, ok := azm["dimensionFilters"].([]any)
		require.True(t, ok, "dimensionFilters must be an empty array, got %T", azm["dimensionFilters"])
		assert.Empty(t, filters)
	})

	t.Run("interval scales with the time range", func(t *testing.T) {
		req := testQueryReq()
		req.Start = time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
		req.End = time.Date(2026, 5, 31, 0, 0, 0, 0, time.UTC) // 30 days
		q := captureQuery(t, req)

		intervalMs, ok := q["intervalMs"].(float64)
		require.True(t, ok)
		// 30 days / 1000 points = 2592s per point.
		assert.InDelta(t, 2_592_000, intervalMs, 0.5)
	})
}

func TestClient_LogsQuery(t *testing.T) {
	logsReq := azuremonitor.LogsQueryRequest{
		Subscription:  "sub-1",
		ResourceGroup: "my-rg",
		Workspace:     "my-ws",
		Query:         "AppRequests | take 5",
		Start:         time.Date(2026, 5, 17, 0, 0, 0, 0, time.UTC),
		End:           time.Date(2026, 5, 17, 1, 0, 0, 0, time.UTC),
	}

	t.Run("parses table result with time conversion", func(t *testing.T) {
		var f testFrame
		f.Schema.Fields = []testField{
			{Name: "TimeGenerated", Type: "time"},
			{Name: "Name", Type: "string"},
			{Name: "DurationMs", Type: "number"},
		}
		f.Data.Values = []any{
			[]any{1747000000000.0},
			[]any{"timer"},
			[]any{146.89},
		}
		client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(queryResultBody(t, testResultEntry{Frames: []testFrame{f}}))
		}))

		resp, err := client.LogsQuery(context.Background(), "test-uid", logsReq)
		require.NoError(t, err)
		require.Len(t, resp.Columns, 3)
		assert.Equal(t, azuremonitor.Column{Name: "TimeGenerated", Type: "time"}, resp.Columns[0])
		require.Len(t, resp.Rows, 1)
		assert.Equal(t, "2025-05-11T21:46:40Z", resp.Rows[0][0])
		assert.Equal(t, "timer", resp.Rows[0][1])
	})

	// Pins the wire shape: the workspace is addressed as a full ARM resource
	// URI inside azureLogAnalytics.resources.
	t.Run("request body shape", func(t *testing.T) {
		var (
			captured   map[string]any
			decodedErr error
		)
		client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			decodedErr = json.NewDecoder(r.Body).Decode(&captured)
			_, _ = w.Write(queryResultBody(t, testResultEntry{Frames: []testFrame{}}))
		}))

		_, err := client.LogsQuery(context.Background(), "test-uid", logsReq)
		require.NoError(t, err)
		require.NoError(t, decodedErr)

		queries, ok := captured["queries"].([]any)
		require.True(t, ok)
		require.Len(t, queries, 1)
		q, ok := queries[0].(map[string]any)
		require.True(t, ok)

		assert.Equal(t, "Azure Log Analytics", q["queryType"])
		la, ok := q["azureLogAnalytics"].(map[string]any)
		require.True(t, ok, "azureLogAnalytics must be a JSON object, got %T", q["azureLogAnalytics"])
		assert.Equal(t, "AppRequests | take 5", la["query"])
		assert.Equal(t, "table", la["resultFormat"])
		resources, ok := la["resources"].([]any)
		require.True(t, ok)
		require.Len(t, resources, 1)
		assert.Equal(t, "/subscriptions/sub-1/resourceGroups/my-rg/providers/Microsoft.OperationalInsights/workspaces/my-ws", resources[0])
	})

	t.Run("error envelope", func(t *testing.T) {
		client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(queryResultBody(t, testResultEntry{Error: "invalid KQL", Status: 400}))
		}))

		_, err := client.LogsQuery(context.Background(), "test-uid", logsReq)
		require.Error(t, err)

		var apiErr *queryerror.APIError
		require.ErrorAs(t, err, &apiErr)
		assert.Equal(t, "azuremonitor", apiErr.Datasource)
		assert.Equal(t, "logs", apiErr.Operation)
	})

	// The plugin returns HTTP 400 with the Azure error envelope embedded in
	// results.A.error; the wrapped body must be reduced to the deepest inner
	// message on this transport-error path too, not only inside the parser.
	t.Run("HTTP 400 with wrapped Azure error is simplified", func(t *testing.T) {
		wrapped := `request failed, status: 400 Bad Request, body: {"error":{"message":"The request had some invalid properties","code":"BadArgumentError","innererror":{"code":"SemanticError","message":"A semantic error occurred.","innererror":{"code":"SEM0100","message":"bad table"}}}}`
		client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write(queryResultBody(t, testResultEntry{Error: wrapped, Status: 400}))
		}))

		_, err := client.LogsQuery(context.Background(), "test-uid", logsReq)
		require.Error(t, err)

		var apiErr *queryerror.APIError
		require.ErrorAs(t, err, &apiErr)
		assert.Equal(t, "SEM0100: bad table", apiErr.Message)
	})
}

func TestClient_ResourceGraphQuery(t *testing.T) {
	argReq := azuremonitor.ResourceGraphRequest{
		Subscriptions: []string{"sub-1", "sub-2"},
		Query:         "Resources | project name",
		Start:         time.Date(2026, 5, 17, 0, 0, 0, 0, time.UTC),
		End:           time.Date(2026, 5, 17, 1, 0, 0, 0, time.UTC),
	}

	t.Run("parses table result", func(t *testing.T) {
		var f testFrame
		f.Schema.Fields = []testField{{Name: "name", Type: "string"}}
		f.Data.Values = []any{[]any{"vm-a", "vm-b"}}
		client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(queryResultBody(t, testResultEntry{Frames: []testFrame{f}}))
		}))

		resp, err := client.ResourceGraphQuery(context.Background(), "test-uid", argReq)
		require.NoError(t, err)
		require.Len(t, resp.Rows, 2)
		assert.Equal(t, "vm-a", resp.Rows[0][0])
	})

	// Pins the wire shape: Resource Graph reads a query-level "subscriptions"
	// array — unlike metrics, which uses a singular "subscription" field.
	t.Run("request body shape", func(t *testing.T) {
		var (
			captured   map[string]any
			decodedErr error
		)
		client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			decodedErr = json.NewDecoder(r.Body).Decode(&captured)
			_, _ = w.Write(queryResultBody(t, testResultEntry{Frames: []testFrame{}}))
		}))

		_, err := client.ResourceGraphQuery(context.Background(), "test-uid", argReq)
		require.NoError(t, err)
		require.NoError(t, decodedErr)

		queries, ok := captured["queries"].([]any)
		require.True(t, ok)
		require.Len(t, queries, 1)
		q, ok := queries[0].(map[string]any)
		require.True(t, ok)

		assert.Equal(t, "Azure Resource Graph", q["queryType"])
		assert.Equal(t, []any{"sub-1", "sub-2"}, q["subscriptions"])
		_, hasSingular := q["subscription"]
		assert.False(t, hasSingular, "resource graph must not set the singular subscription field")

		arg, ok := q["azureResourceGraph"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "Resources | project name", arg["query"])
		assert.Equal(t, "table", arg["resultFormat"])
	})
}

func captureQuery(t *testing.T, req azuremonitor.QueryRequest) map[string]any {
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

// ---- ARM discovery tests ----

type armPage struct {
	Value    []map[string]any `json:"value"`
	NextLink string           `json:"nextLink,omitempty"`
}

func TestClient_ListSubscriptions(t *testing.T) {
	t.Run("parses subscription list", func(t *testing.T) {
		client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "/api/datasources/uid/test-uid/resources/azuremonitor/subscriptions", r.URL.Path)
			assert.Equal(t, "2020-01-01", r.URL.Query().Get("api-version"))
			_, _ = w.Write(mustJSON(t, armPage{Value: []map[string]any{
				{"subscriptionId": "sub-1", "displayName": "Dev"},
				{"subscriptionId": "sub-2", "displayName": "Prod"},
			}}))
		}))

		result, err := client.ListSubscriptions(context.Background(), "test-uid")
		require.NoError(t, err)
		assert.Equal(t, []azuremonitor.Subscription{
			{ID: "sub-1", Name: "Dev"},
			{ID: "sub-2", Name: "Prod"},
		}, result)
	})

	t.Run("follows nextLink pagination through the proxy", func(t *testing.T) {
		var srv *httptest.Server
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Query().Get("$skiptoken") == "" {
				_, _ = w.Write(mustJSON(t, armPage{
					Value:    []map[string]any{{"subscriptionId": "sub-1", "displayName": "Dev"}},
					NextLink: "https://management.azure.com/subscriptions?api-version=2020-01-01&$skiptoken=abc",
				}))
				return
			}
			assert.Equal(t, "/api/datasources/uid/test-uid/resources/azuremonitor/subscriptions", r.URL.Path)
			_, _ = w.Write(mustJSON(t, armPage{
				Value: []map[string]any{{"subscriptionId": "sub-2", "displayName": "Prod"}},
			}))
		})
		srv = httptest.NewServer(handler)
		t.Cleanup(srv.Close)

		cfg := config.NamespacedRESTConfig{Config: rest.Config{Host: srv.URL}, Namespace: "default"}
		client, err := azuremonitor.NewClient(cfg)
		require.NoError(t, err)

		result, err := client.ListSubscriptions(context.Background(), "test-uid")
		require.NoError(t, err)
		require.Len(t, result, 2)
		assert.Equal(t, "sub-2", result[1].ID)
	})

	t.Run("HTTP 500 returns typed error", func(t *testing.T) {
		client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, `{"error":"user is not authenticated with Azure AD"}`, http.StatusInternalServerError)
		}))

		_, err := client.ListSubscriptions(context.Background(), "test-uid")
		require.Error(t, err)

		var apiErr *queryerror.APIError
		require.ErrorAs(t, err, &apiErr)
		assert.Equal(t, "azuremonitor", apiErr.Datasource)
		assert.Equal(t, http.StatusInternalServerError, apiErr.StatusCode)
	})
}

func TestClient_ListResourceGroups(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/datasources/uid/test-uid/resources/azuremonitor/subscriptions/sub-1/resourceGroups", r.URL.Path)
		_, _ = w.Write(mustJSON(t, armPage{Value: []map[string]any{
			{"name": "my-rg", "location": "uksouth"},
		}}))
	}))

	result, err := client.ListResourceGroups(context.Background(), "test-uid", "sub-1")
	require.NoError(t, err)
	assert.Equal(t, []azuremonitor.ResourceGroup{{Name: "my-rg", Location: "uksouth"}}, result)
}

func TestClient_ListResources(t *testing.T) {
	t.Run("scoped to a resource group", func(t *testing.T) {
		client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "/api/datasources/uid/test-uid/resources/azuremonitor/subscriptions/sub-1/resourceGroups/my-rg/resources", r.URL.Path)
			assert.Equal(t, "2021-04-01", r.URL.Query().Get("api-version"))
			_, _ = w.Write(mustJSON(t, armPage{Value: []map[string]any{
				{"name": "mystorage", "type": "Microsoft.Storage/storageAccounts", "location": "uksouth"},
			}}))
		}))

		result, err := client.ListResources(context.Background(), "test-uid", "sub-1", "my-rg")
		require.NoError(t, err)
		require.Len(t, result, 1)
		assert.Equal(t, "Microsoft.Storage/storageAccounts", result[0].Type)
	})

	t.Run("whole subscription when the resource group is empty", func(t *testing.T) {
		client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "/api/datasources/uid/test-uid/resources/azuremonitor/subscriptions/sub-1/resources", r.URL.Path)
			_, _ = w.Write(mustJSON(t, armPage{}))
		}))

		result, err := client.ListResources(context.Background(), "test-uid", "sub-1", "")
		require.NoError(t, err)
		assert.Empty(t, result)
	})
}

func TestClient_ListMetricDefinitions(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t,
			"/api/datasources/uid/test-uid/resources/azuremonitor/subscriptions/sub-1/resourceGroups/my-rg/providers/Microsoft.Storage/storageAccounts/mystorage/providers/microsoft.insights/metricdefinitions",
			r.URL.Path)
		assert.Equal(t, "2018-01-01", r.URL.Query().Get("api-version"))
		_, _ = w.Write(mustJSON(t, armPage{Value: []map[string]any{
			{
				"name":                      map[string]any{"value": "Transactions", "localizedValue": "Transactions"},
				"primaryAggregationType":    "Total",
				"supportedAggregationTypes": []string{"Total", "Average"},
				"unit":                      "Count",
				"dimensions": []map[string]any{
					{"value": "ResponseType"},
					{"value": "ApiName"},
				},
			},
		}}))
	}))

	result, err := client.ListMetricDefinitions(context.Background(), "test-uid", "sub-1", "my-rg", "Microsoft.Storage/storageAccounts", "mystorage")
	require.NoError(t, err)
	require.Len(t, result, 1)
	assert.Equal(t, "Transactions", result[0].Name)
	assert.Equal(t, "Total", result[0].PrimaryAggregation)
	assert.Equal(t, []string{"Total", "Average"}, result[0].SupportedAggregations)
	assert.Equal(t, "Count", result[0].Unit)
	assert.Equal(t, []string{"ResponseType", "ApiName"}, result[0].Dimensions)
}
