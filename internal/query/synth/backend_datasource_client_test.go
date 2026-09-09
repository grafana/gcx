package synth_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/query/dataframe"
	"github.com/grafana/gcx/internal/query/synth"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"
)

// uptimeFrames is a range response shaped like what the SM backend returns for
// checks_uptime: a time field and a value field, plus the expression the backend
// built, which it reports in frame metadata.
const uptimeFrames = `{"results":{"A":{"status":200,"frames":[{"schema":{"refId":"A",` +
	`"meta":{"executedQueryString":"Expr: max by () (max_over_time(probe_success{job=\"test\"}[60s]))"},` +
	`"fields":[{"name":"Time","type":"time"},{"name":"Value","type":"number"}]},` +
	`"data":{"values":[[1000,2000,3000,4000],[1,1,0,1]]}}]}}}`

func newNamedClient(t *testing.T, handler http.HandlerFunc) *synth.BackendDatasourceClient {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	client, err := synth.NewBackendDatasourceClient(config.NamespacedRESTConfig{
		Config:    rest.Config{Host: server.URL},
		Namespace: "default",
	})
	require.NoError(t, err)

	return client
}

func TestNamedQuery_RequestBodyShape(t *testing.T) {
	var body map[string]any

	client := newNamedClient(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(uptimeFrames))
	})

	_, err := client.Query(context.Background(), "sm-uid", synth.NamedQuery{
		Name: "checks_uptime",
		Params: map[string]any{
			"job":       "test",
			"instance":  "https://grafana.com",
			"frequency": 60000,
		},
	}, time.UnixMilli(1000), time.UnixMilli(4000))
	require.NoError(t, err)

	// The time range is shared across the request, in epoch milliseconds.
	assert.Equal(t, "1000", body["from"])
	assert.Equal(t, "4000", body["to"])

	queries, ok := body["queries"].([]any)
	require.True(t, ok, "queries must be a list")
	require.Len(t, queries, 1)

	q, ok := queries[0].(map[string]any)
	require.True(t, ok)

	// The name travels as queryType and the parameters sit alongside it at the top
	// level of the query object -- that is what the backend unmarshals into its
	// params struct. Nesting them under a "params" key would resolve to empty
	// params and the backend would reject the query as missing job and instance.
	assert.Equal(t, "checks_uptime", q["queryType"])
	assert.Equal(t, "test", q["job"])
	assert.Equal(t, "https://grafana.com", q["instance"])
	assert.InDelta(t, float64(60000), q["frequency"], 0)

	assert.Equal(t, "A", q["refId"])
	assert.Equal(t, map[string]any{
		"type": "synthetic-monitoring-datasource",
		"uid":  "sm-uid",
	}, q["datasource"])

	// No expression: the whole point is that the client sends none.
	assert.NotContains(t, q, "expr")
}

func TestNamedQuery_ParamsCannotClobberEnvelope(t *testing.T) {
	var body map[string]any

	client := newNamedClient(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&body)
		_, _ = w.Write([]byte(uptimeFrames))
	})

	// A caller passing a param that collides with the envelope must not be able to
	// redirect the query at another datasource or rename the query.
	_, err := client.Query(context.Background(), "sm-uid", synth.NamedQuery{
		Name: "checks_uptime",
		Params: map[string]any{
			"job":        "test",
			"refId":      "hijacked",
			"queryType":  "something_else",
			"datasource": map[string]any{"uid": "elsewhere"},
		},
	}, time.UnixMilli(1000), time.UnixMilli(4000))
	require.NoError(t, err)

	q, ok := body["queries"].([]any)[0].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "A", q["refId"])
	assert.Equal(t, "checks_uptime", q["queryType"])

	ds, ok := q["datasource"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "sm-uid", ds["uid"])
}

func TestNamedQuery_ReturnsFramesAndExecutedExpression(t *testing.T) {
	client := newNamedClient(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(uptimeFrames))
	})

	res, err := client.Query(context.Background(), "sm-uid", synth.NamedQuery{
		Name:   "checks_uptime",
		Params: map[string]any{"job": "test"},
	}, time.UnixMilli(1000), time.UnixMilli(4000))
	require.NoError(t, err)
	require.Len(t, res.Frames, 1)

	// gcx never builds the expression, but it can show what the backend ran.
	assert.Contains(t, res.ExecutedQuery, "max_over_time(probe_success")
}

func TestNamedQuery_PerQueryErrorBecomesError(t *testing.T) {
	// The plugin reports an unauthorized or unknown query per refId, so the error
	// has to be read out of the body rather than the HTTP status.
	client := newNamedClient(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"results":{"A":{"status":403,` +
			`"error":"mark is not allowed to query the prometheus datasource",` +
			`"errorSource":"plugin","frames":[]}}}`))
	})

	_, err := client.Query(context.Background(), "sm-uid", synth.NamedQuery{
		Name:   "checks_uptime",
		Params: map[string]any{"job": "test"},
	}, time.UnixMilli(1000), time.UnixMilli(4000))

	require.Error(t, err)
	assert.Contains(t, err.Error(), "not allowed to query")

	// The subject must read as a human-facing datasource name plus the query
	// that failed, not the wire-format datasource type ("synthetic-monitoring-
	// datasource") -- that string is meaningless to a CLI user.
	assert.Contains(t, err.Error(), "synthetic monitoring checks_uptime")
}

func TestNamedQuery_UnknownQueryNameIsRejectedLocally(t *testing.T) {
	// Catch the typo before spending a round trip on it.
	client := newNamedClient(t, func(w http.ResponseWriter, _ *http.Request) {
		t.Error("no request should be made for an empty query name")
	})

	_, err := client.Query(context.Background(), "sm-uid", synth.NamedQuery{},
		time.UnixMilli(1000), time.UnixMilli(4000))
	require.Error(t, err)
}

func TestMean(t *testing.T) {
	tests := []struct {
		name   string
		values []any
		want   float64
		wantOK bool
	}{
		{
			name:   "averages the value field",
			values: []any{1.0, 1.0, 0.0, 1.0},
			want:   0.75,
			wantOK: true,
		},
		{
			// Prometheus leaves gaps as nulls; counting them as zero would report a
			// check as failing when it simply was not scraped.
			name:   "skips nulls rather than treating them as zero",
			values: []any{1.0, nil, 1.0},
			want:   1,
			wantOK: true,
		},
		{
			name:   "reports no value when every point is null",
			values: []any{nil, nil},
			wantOK: false,
		},
		{
			name:   "reports no value when there are no points",
			values: []any{},
			wantOK: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := synth.Mean(resultWithValues(tt.values))
			assert.Equal(t, tt.wantOK, ok)
			if tt.wantOK {
				assert.InDelta(t, tt.want, got, 1e-9)
			}
		})
	}
}

func TestMean_NoFrames(t *testing.T) {
	_, ok := synth.Mean(&synth.NamedResult{})
	assert.False(t, ok)
}

func TestMean_LogFrameIsNotAveragedAsNumbers(t *testing.T) {
	// A log query's first non-time field is a string (the log line). Averaging
	// it as if it were a metric would silently produce a wrong number instead
	// of a clear "not reducible" signal.
	res := &synth.NamedResult{
		Frames: []dataframe.Frame{{
			Schema: dataframe.Schema{
				Fields: []dataframe.Field{
					{Name: "Time", Type: "time"},
					{Name: "Line", Type: "string"},
				},
			},
			Data: dataframe.Data{Values: [][]any{
				{1000.0, 2000.0},
				{"level=error msg=timeout", "level=error msg=refused"},
			}},
		}},
	}

	_, ok := synth.Mean(res)
	assert.False(t, ok)
	assert.False(t, synth.HasNumericField(res), "a log frame has no numeric field")
}

func TestHasNumericField(t *testing.T) {
	assert.True(t, synth.HasNumericField(resultWithValues([]any{1.0, 2.0})),
		"a metric frame has a numeric field even before checking its values")
	assert.False(t, synth.HasNumericField(&synth.NamedResult{}), "no frames at all")
	assert.False(t, synth.HasNumericField(nil))
}

func resultWithValues(values []any) *synth.NamedResult {
	times := make([]any, len(values))
	for i := range values {
		times[i] = float64(i * 1000)
	}

	return &synth.NamedResult{
		Frames: []dataframe.Frame{{
			Schema: dataframe.Schema{
				Fields: []dataframe.Field{
					{Name: "Time", Type: "time"},
					{Name: "Value", Type: "number"},
				},
			},
			Data: dataframe.Data{Values: [][]any{times, values}},
		}},
	}
}
