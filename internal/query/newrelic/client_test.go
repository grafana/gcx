package newrelic_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/query/newrelic"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"
)

func testClient(t *testing.T, handler http.Handler) *newrelic.Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	client, err := newrelic.NewClient(config.NamespacedRESTConfig{Config: rest.Config{Host: srv.URL}, Namespace: "org-1"})
	require.NoError(t, err)
	return client
}

func testRequest() newrelic.QueryRequest {
	return newrelic.QueryRequest{
		QueryText: "SELECT count(*) FROM Transaction",
		Start:     time.Date(2026, 5, 17, 0, 0, 0, 0, time.UTC),
		End:       time.Date(2026, 5, 17, 1, 0, 0, 0, time.UTC),
	}
}

func respondWithResult(t *testing.T, w http.ResponseWriter, r *http.Request, result string) {
	t.Helper()
	var payload struct {
		Queries []struct {
			RefID string `json:"refId"`
		} `json:"queries"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		t.Errorf("decode query: %v", err)
		http.Error(w, "invalid query", http.StatusBadRequest)
		return
	}
	if !assert.Len(t, payload.Queries, 1) {
		http.Error(w, "invalid query", http.StatusBadRequest)
		return
	}
	if !assert.NotEmpty(t, payload.Queries[0].RefID) {
		http.Error(w, "missing refId", http.StatusBadRequest)
		return
	}
	_, _ = fmt.Fprintf(w, `{"results":{%q:%s}}`, payload.Queries[0].RefID, result)
}

func TestQueryRequest(t *testing.T) {
	client := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		var payload struct {
			From    string           `json:"from"`
			To      string           `json:"to"`
			Queries []map[string]any `json:"queries"`
		}
		if !assert.NoError(t, json.NewDecoder(r.Body).Decode(&payload)) || !assert.Len(t, payload.Queries, 1) {
			return
		}
		assert.Equal(t, "1778976000000", payload.From)
		assert.Equal(t, "1778979600000", payload.To)
		q := payload.Queries[0]
		assert.Equal(t, "SELECT count(*) FROM Transaction", q["queryText"])
		assert.InDelta(t, 123, q["accountID"], 0)
		assert.InDelta(t, 30, q["timeoutSeconds"], 0)
		assert.Equal(t, map[string]any{"type": "nrgrafanaplugin-newrelic-datasource", "uid": "nr-uid"}, q["datasource"])
		_, _ = fmt.Fprintf(w, `{"results":{%q:{"frames":[]}}}`, q["refId"])
	}))
	req := testRequest()
	req.AccountID = 123
	req.TimeoutSeconds = 30
	_, err := client.Query(context.Background(), "nr-uid", req)
	require.NoError(t, err)
}

func TestQueryTimeoutBoundaries(t *testing.T) {
	for _, timeout := range []int{5, 120} {
		t.Run(strconv.Itoa(timeout), func(t *testing.T) {
			client := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var payload struct {
					Queries []map[string]any `json:"queries"`
				}
				if !assert.NoError(t, json.NewDecoder(r.Body).Decode(&payload)) || !assert.Len(t, payload.Queries, 1) {
					return
				}
				assert.InDelta(t, timeout, payload.Queries[0]["timeoutSeconds"], 0)
				_, _ = fmt.Fprintf(w, `{"results":{%q:{"frames":[]}}}`, payload.Queries[0]["refId"])
			}))
			req := testRequest()
			req.TimeoutSeconds = timeout
			_, err := client.Query(context.Background(), "nr-uid", req)
			require.NoError(t, err)
		})
	}
}

func TestQueryFrames(t *testing.T) {
	tests := []struct {
		name   string
		body   string
		values [][][]any
	}{
		{"count", `{"frames":[{"schema":{"name":"counts","fields":[{"name":"count","type":"number"}]},"data":{"values":[[42]]}}]}`, [][][]any{{{float64(42)}}}},
		{"facets and logs", `{"frames":[{"schema":{"name":"facets","fields":[{"name":"facet","type":"string"},{"name":"count","type":"number"}]},"data":{"values":[["a"],[2]]}},{"schema":{"name":"logs","fields":[{"name":"time","type":"time"},{"name":"message","type":"string"}]},"data":{"values":[[1778976000000],["hello"]]}}]}`, [][][]any{{{"a"}, {float64(2)}}, {{float64(1778976000000)}, {"hello"}}}},
		{"empty", `{"frames":[]}`, nil},
		{"empty response without frames", `{"status":200}`, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				respondWithResult(t, w, r, tt.body)
			}))
			resp, err := client.Query(context.Background(), "nr-uid", testRequest())
			require.NoError(t, err)
			require.Len(t, resp.Frames, len(tt.values))
			for i, frame := range resp.Frames {
				assert.Equal(t, tt.values[i], frame.Data.Values)
			}
		})
	}
}

func TestQueryErrors(t *testing.T) {
	tests := []struct {
		name   string
		result string
		status int
		want   string
	}{
		{"embedded", `{"error":"NRQL syntax error","status":400}`, 200, "NRQL syntax error"},
		{"embedded status boundary", `{"status":400,"frames":[]}`, 200, "400"},
		{"missing result", "", 200, "missing results"},
		{"malformed", `{"frames":"wrong"}`, 200, "response"},
		{"upstream", `{"message":"upstream unavailable"}`, 503, "upstream unavailable"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tt.status != http.StatusOK {
					w.WriteHeader(tt.status)
					_, _ = w.Write([]byte(tt.result))
					return
				}
				if tt.result == "" {
					_, _ = w.Write([]byte(`{"results":{}}`))
					return
				}
				respondWithResult(t, w, r, tt.result)
			}))
			_, err := client.Query(context.Background(), "nr-uid", testRequest())
			require.ErrorContains(t, err, tt.want)
		})
	}
}
