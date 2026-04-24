package pyroscope_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/query/pyroscope"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"
)

func newTestClient(t *testing.T, server *httptest.Server) *pyroscope.Client {
	t.Helper()
	cfg := config.NamespacedRESTConfig{
		Config: rest.Config{Host: server.URL},
	}
	client, err := pyroscope.NewClient(cfg)
	require.NoError(t, err)
	return client
}

func TestClient_SelectSeries(t *testing.T) {
	tests := []struct {
		name       string
		req        pyroscope.SelectSeriesRequest
		handler    http.HandlerFunc
		wantSeries int
		wantErr    bool
	}{
		{
			name: "success with series data",
			req: pyroscope.SelectSeriesRequest{
				ProfileTypeID: "process_cpu:cpu:nanoseconds:cpu:nanoseconds",
				LabelSelector: `{service_name="frontend"}`,
			},
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodPost, r.Method)
				assert.Contains(t, r.URL.Path, "querier.v1.QuerierService/SelectSeries")
				assert.Equal(t, "application/json", r.Header.Get("Content-Type"))

				var body map[string]any
				assert.NoError(t, json.NewDecoder(r.Body).Decode(&body))
				assert.Equal(t, "process_cpu:cpu:nanoseconds:cpu:nanoseconds", body["profileTypeID"])
				assert.Equal(t, `{service_name="frontend"}`, body["labelSelector"])

				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{
					"series": [{
						"labels": [{"name": "service_name", "value": "frontend"}],
						"points": [
							{"value": 100, "timestamp": "1000"},
							{"value": 200, "timestamp": "2000"}
						]
					}]
				}`))
			},
			wantSeries: 1,
		},
		{
			name: "empty response",
			req: pyroscope.SelectSeriesRequest{
				ProfileTypeID: "process_cpu:cpu:nanoseconds:cpu:nanoseconds",
				LabelSelector: `{service_name="nonexistent"}`,
			},
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"series": []}`))
			},
			wantSeries: 0,
		},
		{
			name: "optional fields sent when set",
			req: pyroscope.SelectSeriesRequest{
				ProfileTypeID: "process_cpu:cpu:nanoseconds:cpu:nanoseconds",
				LabelSelector: `{}`,
				GroupBy:       []string{"namespace", "pod"},
				Step:          60.0,
				Aggregation:   "AVERAGE",
				Limit:         5,
			},
			handler: func(w http.ResponseWriter, r *http.Request) {
				var body map[string]any
				assert.NoError(t, json.NewDecoder(r.Body).Decode(&body))

				assert.Equal(t, []any{"namespace", "pod"}, body["groupBy"])
				assert.InDelta(t, 60.0, body["step"], 0.001)
				assert.Equal(t, "AVERAGE", body["aggregation"])
				assert.Equal(t, "5", body["limit"])

				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"series": []}`))
			},
			wantSeries: 0,
		},
		{
			name: "exemplarType forwarded and exemplars decoded",
			req: pyroscope.SelectSeriesRequest{
				ProfileTypeID: "process_cpu:cpu:nanoseconds:cpu:nanoseconds",
				LabelSelector: `{}`,
				ExemplarType:  pyroscope.ExemplarTypeIndividual,
			},
			handler: func(w http.ResponseWriter, r *http.Request) {
				var body map[string]any
				assert.NoError(t, json.NewDecoder(r.Body).Decode(&body))
				assert.Equal(t, pyroscope.ExemplarTypeIndividual, body["exemplarType"])

				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{
					"series": [{
						"labels": [{"name": "service_name", "value": "frontend"}],
						"points": [{
							"value": 100,
							"timestamp": "1000",
							"exemplars": [
								{"profileId": "p-1", "timestamp": "1100", "value": "5000", "spanId": "span-1"}
							]
						}]
					}]
				}`))
			},
			wantSeries: 1,
		},
		{
			name: "exemplarType omitted when empty",
			req: pyroscope.SelectSeriesRequest{
				ProfileTypeID: "process_cpu:cpu:nanoseconds:cpu:nanoseconds",
				LabelSelector: `{}`,
			},
			handler: func(w http.ResponseWriter, r *http.Request) {
				var body map[string]any
				assert.NoError(t, json.NewDecoder(r.Body).Decode(&body))
				_, hasExemplar := body["exemplarType"]
				assert.False(t, hasExemplar, "exemplarType should not be sent when empty")

				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"series": []}`))
			},
			wantSeries: 0,
		},
		{
			name: "optional fields omitted when empty",
			req: pyroscope.SelectSeriesRequest{
				ProfileTypeID: "process_cpu:cpu:nanoseconds:cpu:nanoseconds",
				LabelSelector: `{}`,
			},
			handler: func(w http.ResponseWriter, r *http.Request) {
				var body map[string]any
				assert.NoError(t, json.NewDecoder(r.Body).Decode(&body))

				_, hasGroupBy := body["groupBy"]
				_, hasStep := body["step"]
				_, hasAggregation := body["aggregation"]
				_, hasLimit := body["limit"]
				assert.False(t, hasGroupBy, "groupBy should not be sent when empty")
				assert.False(t, hasStep, "step should not be sent when zero")
				assert.False(t, hasAggregation, "aggregation should not be sent when empty")
				assert.False(t, hasLimit, "limit should not be sent when zero")

				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"series": []}`))
			},
			wantSeries: 0,
		},
		{
			name: "server error",
			req: pyroscope.SelectSeriesRequest{
				ProfileTypeID: "process_cpu:cpu:nanoseconds:cpu:nanoseconds",
				LabelSelector: `{}`,
			},
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte("internal error"))
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(tt.handler)
			defer server.Close()

			client := newTestClient(t, server)
			resp, err := client.SelectSeries(context.Background(), "test-uid", tt.req)

			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Len(t, resp.Series, tt.wantSeries)

			// For the exemplars case, spot-check the decoded Exemplar payload:
			// timestamp/value are json.Number, profileId/spanId propagate through.
			if tt.name == "exemplarType forwarded and exemplars decoded" {
				require.Len(t, resp.Series[0].Points, 1)
				require.Len(t, resp.Series[0].Points[0].Exemplars, 1)
				ex := resp.Series[0].Points[0].Exemplars[0]
				assert.Equal(t, "p-1", ex.ProfileID)
				assert.Equal(t, "span-1", ex.SpanID)
				assert.Equal(t, int64(1100), ex.TimestampMs())
				assert.Equal(t, int64(5000), ex.Int64Value())
			}
		})
	}
}

func TestClient_SelectHeatmap(t *testing.T) {
	tests := []struct {
		name      string
		req       pyroscope.SelectHeatmapRequest
		handler   http.HandlerFunc
		wantSlots int
		wantErr   bool
	}{
		{
			name: "forwards queryType/exemplarType and decodes span exemplars",
			req: pyroscope.SelectHeatmapRequest{
				ProfileTypeID: "process_cpu:cpu:nanoseconds:cpu:nanoseconds",
				LabelSelector: `{service_name="frontend"}`,
				Step:          10,
				QueryType:     pyroscope.HeatmapQueryTypeSpan,
				ExemplarType:  pyroscope.ExemplarTypeSpan,
				Limit:         25,
			},
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodPost, r.Method)
				assert.Contains(t, r.URL.Path, "querier.v1.QuerierService/SelectHeatmap")
				assert.Equal(t, "application/json", r.Header.Get("Content-Type"))

				var body map[string]any
				assert.NoError(t, json.NewDecoder(r.Body).Decode(&body))
				assert.Equal(t, "process_cpu:cpu:nanoseconds:cpu:nanoseconds", body["profileTypeID"])
				assert.Equal(t, pyroscope.HeatmapQueryTypeSpan, body["queryType"])
				assert.Equal(t, pyroscope.ExemplarTypeSpan, body["exemplarType"])
				assert.Equal(t, "25", body["limit"])
				assert.InDelta(t, 10.0, body["step"], 0.001)

				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{
					"series": [{
						"labels": [{"name": "service_name", "value": "frontend"}],
						"slots": [{
							"timestamp": "1500",
							"exemplars": [
								{"spanId": "span-abc", "timestamp": "1600", "value": "12345"}
							]
						}]
					}]
				}`))
			},
			wantSlots: 1,
		},
		{
			name: "optional fields omitted when empty",
			req: pyroscope.SelectHeatmapRequest{
				ProfileTypeID: "process_cpu:cpu:nanoseconds:cpu:nanoseconds",
				LabelSelector: `{}`,
			},
			handler: func(w http.ResponseWriter, r *http.Request) {
				var body map[string]any
				assert.NoError(t, json.NewDecoder(r.Body).Decode(&body))
				for _, k := range []string{"step", "queryType", "exemplarType", "limit"} {
					_, ok := body[k]
					assert.Falsef(t, ok, "%s must be omitted when zero", k)
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"series": []}`))
			},
			wantSlots: 0,
		},
		{
			name: "server error is surfaced",
			req: pyroscope.SelectHeatmapRequest{
				ProfileTypeID: "process_cpu:cpu:nanoseconds:cpu:nanoseconds",
				LabelSelector: `{}`,
			},
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte(`{"code": "internal", "message": "boom"}`))
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(tt.handler)
			defer server.Close()

			client := newTestClient(t, server)
			resp, err := client.SelectHeatmap(context.Background(), "test-uid", tt.req)

			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)

			gotSlots := 0
			for _, s := range resp.Series {
				gotSlots += len(s.Slots)
			}
			assert.Equal(t, tt.wantSlots, gotSlots)

			if tt.name == "forwards queryType/exemplarType and decodes span exemplars" {
				require.Len(t, resp.Series, 1)
				require.Len(t, resp.Series[0].Slots, 1)
				require.Len(t, resp.Series[0].Slots[0].Exemplars, 1)
				ex := resp.Series[0].Slots[0].Exemplars[0]
				assert.Equal(t, "span-abc", ex.SpanID)
				assert.Equal(t, int64(1600), ex.TimestampMs())
				assert.Equal(t, int64(12345), ex.Int64Value())
			}
		})
	}
}
