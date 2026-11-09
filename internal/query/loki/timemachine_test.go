package loki_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"
	"time"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/query/loki"
	"github.com/grafana/gcx/internal/queryerror"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"
)

func newDroppedLogsTestClient(t *testing.T, status int, body string, capture func(*http.Request)) *loki.Client {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if capture != nil {
			capture(r)
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)

	client, err := loki.NewClient(config.NamespacedRESTConfig{
		Config:    rest.Config{Host: server.URL},
		Namespace: "default",
	})
	require.NoError(t, err)
	return client
}

func TestQueryDroppedLogs_Request(t *testing.T) {
	start := time.Date(2026, 1, 2, 15, 0, 0, 0, time.UTC)
	end := start.Add(15 * time.Minute)

	tests := []struct {
		name      string
		limit     int
		wantLimit string
	}{
		{name: "limit set", limit: 25, wantLimit: "25"},
		{name: "limit zero is omitted", limit: 0, wantLimit: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var method, path string
			var query url.Values
			client := newDroppedLogsTestClient(t, http.StatusOK, `{"status":"success","data":{"resultType":"streams","result":[]}}`, func(r *http.Request) {
				method, path, query = r.Method, r.URL.Path, r.URL.Query()
			})

			_, err := client.QueryDroppedLogs(context.Background(), "logs-uid", loki.QueryRequest{
				Query: `{job="app"}`, Start: start, End: end, Limit: tt.limit,
			})
			require.NoError(t, err)

			assert.Equal(t, http.MethodGet, method)
			assert.Equal(t, "/api/datasources/proxy/uid/logs-uid/time-machine/loki/api/v1/query_range", path)
			assert.Equal(t, `{job="app"}`, query.Get("query"))
			assert.Equal(t, strconv.FormatInt(start.UnixNano(), 10), query.Get("start"))
			assert.Equal(t, strconv.FormatInt(end.UnixNano(), 10), query.Get("end"))
			assert.Equal(t, "backward", query.Get("direction"))
			assert.Equal(t, tt.wantLimit, query.Get("limit"))
			assert.Equal(t, tt.wantLimit != "", query.Has("limit"))
		})
	}
}

func TestQueryDroppedLogs_InstantQueryUsesTrailingMinute(t *testing.T) {
	var query url.Values
	client := newDroppedLogsTestClient(t, http.StatusOK, `{"status":"success","data":{"resultType":"streams","result":[]}}`, func(r *http.Request) {
		query = r.URL.Query()
	})

	_, err := client.QueryDroppedLogs(context.Background(), "logs-uid", loki.QueryRequest{Query: `{job="app"}`})
	require.NoError(t, err)

	startNs, err := strconv.ParseInt(query.Get("start"), 10, 64)
	require.NoError(t, err)
	endNs, err := strconv.ParseInt(query.Get("end"), 10, 64)
	require.NoError(t, err)
	assert.Equal(t, time.Minute.Nanoseconds(), endNs-startNs)
}

func TestQueryDroppedLogs_Decode(t *testing.T) {
	body := `{
		"status": "success",
		"warnings": ["results are truncated to the time machine retention floor: no entries before 2026-01-02T14:00:00Z are available"],
		"data": {
			"resultType": "streams",
			"result": [
				{
					"stream": {"job": "app"},
					"values": [
						["1767366000000000002", "with metadata", {"structuredMetadata": {"__time_machine__": "q-1", "stream": "stderr"}, "parsed": {"level": "info"}}],
						["1767366000000000001", "bare"]
					]
				}
			],
			"stats": {"summary": {"totalBytesProcessed": 42}}
		}
	}`
	client := newDroppedLogsTestClient(t, http.StatusOK, body, nil)

	resp, err := client.QueryDroppedLogs(context.Background(), "logs-uid", loki.QueryRequest{Query: `{job="app"}`})
	require.NoError(t, err)

	assert.Equal(t, "success", resp.Status)
	assert.Equal(t, "streams", resp.Data.ResultType)
	require.Len(t, resp.Data.Result, 1)
	assert.Equal(t, map[string]string{"job": "app"}, resp.Data.Result[0].Stream)
	assert.Equal(t, []loki.LogEntry{
		{
			Timestamp:          "1767366000000000002",
			Line:               "with metadata",
			StructuredMetadata: map[string]string{loki.TimeMachineMetadataKey: "q-1", "stream": "stderr"},
			Parsed:             map[string]string{"level": "info"},
		},
		{Timestamp: "1767366000000000001", Line: "bare"},
	}, resp.Data.Result[0].Values)
	require.NotNil(t, resp.Data.Stats)
	assert.EqualValues(t, 42, resp.Data.Stats.Summary.TotalBytesProcessed)
	assert.Equal(t, []loki.FrameNotice{{
		Severity: "warning",
		Text:     "results are truncated to the time machine retention floor: no entries before 2026-01-02T14:00:00Z are available",
	}}, resp.Data.Notices)
}

func TestQueryDroppedLogs_DecodeErrors(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{name: "matrix result", body: `{"status":"success","data":{"resultType":"matrix","result":[]}}`, want: `unexpected dropped logs result type "matrix"`},
		{name: "short entry", body: `{"status":"success","data":{"resultType":"streams","result":[{"stream":{},"values":[["1"]]}]}}`, want: "want [timestamp, line, ...]"},
		{name: "not json", body: `nope`, want: "failed to parse dropped logs response"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := newDroppedLogsTestClient(t, http.StatusOK, tt.body, nil)
			_, err := client.QueryDroppedLogs(context.Background(), "logs-uid", loki.QueryRequest{Query: `{job="app"}`})
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.want)
		})
	}
}

func TestQueryDroppedLogs_HTTPErrors(t *testing.T) {
	tests := []struct {
		name             string
		status           int
		body             string
		wantMessage      string
		wantErrContains  string
		wantExperimental bool
	}{
		{
			name:            "budget exceeded",
			status:          http.StatusBadRequest,
			body:            `{"error":"too expensive","estimated_record_bytes":5000,"estimated_decoded_bytes":9000,"limit_bytes":1000,"hard_limit":false}`,
			wantMessage:     "the query would read an estimated 5000 bytes, over the time machine budget of 1000 bytes; narrow the stream selector or the time range",
			wantErrContains: "over the time machine budget",
		},
		{
			name:            "hard limit exceeded",
			status:          http.StatusBadRequest,
			body:            `{"error":"too expensive","estimated_record_bytes":5000,"estimated_decoded_bytes":9000,"limit_bytes":1000,"hard_limit":true}`,
			wantMessage:     "the query would read an estimated 5000 bytes, over the time machine hard limit of 1000 bytes; narrow the stream selector or the time range",
			wantErrContains: "over the time machine hard limit",
		},
		{
			name:             "metric query rejected",
			status:           http.StatusBadRequest,
			body:             "parse query: only log selector is supported",
			wantMessage:      "parse query: only log selector is supported",
			wantErrContains:  "only log selector is supported",
			wantExperimental: true,
		},
		{
			name:             "route absent",
			status:           http.StatusNotFound,
			body:             "404 page not found",
			wantErrContains:  "does not serve the Adaptive Logs time machine",
			wantExperimental: true,
		},
		{
			name:             "gateway timeout",
			status:           http.StatusGatewayTimeout,
			body:             "upstream timed out",
			wantErrContains:  "time machine query timed out",
			wantExperimental: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := newDroppedLogsTestClient(t, tt.status, tt.body, nil)
			_, err := client.QueryDroppedLogs(context.Background(), "logs-uid", loki.QueryRequest{Query: `{job="app"}`})
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErrContains)

			var apiErr *queryerror.APIError
			require.ErrorAs(t, err, &apiErr)
			assert.Equal(t, "loki", apiErr.Datasource)
			assert.Equal(t, "dropped logs query", apiErr.Operation)
			assert.Equal(t, tt.status, apiErr.StatusCode)
			assert.Equal(t, tt.wantExperimental, apiErr.Experimental)
			if tt.wantMessage != "" {
				assert.Equal(t, tt.wantMessage, apiErr.Message)
			}
		})
	}
}
