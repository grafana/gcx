package checks_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/providers/synth/checks"
	"github.com/grafana/gcx/internal/query/dataframe"
	"github.com/grafana/gcx/internal/query/loki"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"
)

func TestBuildAdHocLogsQuery(t *testing.T) {
	got := checks.BuildAdHocLogsQuery("abc-123")
	assert.Equal(t, `{type="adhoc"} |~ "abc-123" | json`, got)
}

// adHocLine builds the raw JSON body a probe pushes into Loki for one ad-hoc
// execution, matching the shape the Synthetic Monitoring app decodes
// (types.adhoc-check.ts AdHocResultLine): logs plus an embedded probe_success
// gauge in timeseries.
func adHocLine(t *testing.T, probeName string, success bool) string {
	t.Helper()
	value := 1.0
	if !success {
		value = 0
	}
	line := map[string]any{
		"id":    "abc-123",
		"probe": probeName,
		"logs":  []any{map[string]any{"level": "info", "msg": "starting probe"}},
		"timeseries": []any{
			map[string]any{
				"name": "probe_success",
				"metric": []any{
					map[string]any{"gauge": map[string]any{"value": value}},
				},
			},
		},
	}
	data, err := json.Marshal(line)
	require.NoError(t, err)
	return string(data)
}

// fakeGrafanaQueryServer returns an httptest server that serves the given
// pre-encoded Loki log lines from any Grafana datasource-query endpoint
// (/apis/query.grafana.app/... and /api/ds/query alike), regardless of path.
func fakeGrafanaQueryServer(t *testing.T, lines ...string) *httptest.Server {
	t.Helper()

	values := make([]any, len(lines))
	for i, l := range lines {
		values[i] = l
	}

	resp := dataframe.Response{
		Results: map[string]dataframe.Result{
			"A": {
				Frames: []dataframe.Frame{
					{
						Schema: dataframe.Schema{
							Fields: []dataframe.Field{
								{Name: "labels", Type: "other"},
								{Name: "Time", Type: "time"},
								{Name: "Line", Type: "string"},
							},
						},
						Data: dataframe.Data{
							Values: [][]any{
								repeat(map[string]any{"type": "adhoc"}, len(lines)),
								repeatTimes(len(lines)),
								values,
							},
						},
					},
				},
			},
		},
	}

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		assert.NoError(t, json.NewEncoder(w).Encode(resp))
	}))
}

func repeat(v any, n int) []any {
	out := make([]any, n)
	for i := range out {
		out[i] = v
	}
	return out
}

func repeatTimes(n int) []any {
	out := make([]any, n)
	for i := range out {
		out[i] = float64(1711893600000)
	}
	return out
}

func TestPollAdHocResults_SuccessAndFailure(t *testing.T) {
	srv := fakeGrafanaQueryServer(t, adHocLine(t, "Oregon", true), adHocLine(t, "Paris", false))
	defer srv.Close()

	cfg := config.NamespacedRESTConfig{Config: rest.Config{Host: srv.URL}}
	lokiClient, err := loki.NewClient(cfg)
	require.NoError(t, err)

	probeNames := map[int64]string{1: "Oregon", 2: "Paris"}
	results, err := checks.PollAdHocResults(context.Background(), lokiClient, "loki-uid", "abc-123", probeNames, time.Second)
	require.NoError(t, err)
	require.Len(t, results, 2)

	assert.Equal(t, int64(1), results[0].ProbeID)
	assert.Equal(t, "Oregon", results[0].ProbeName)
	assert.Equal(t, checks.AdHocSuccess, results[0].Status)
	assert.Equal(t, 1, results[0].LogCount)

	assert.Equal(t, int64(2), results[1].ProbeID)
	assert.Equal(t, "Paris", results[1].ProbeName)
	assert.Equal(t, checks.AdHocFailure, results[1].Status)
}

func TestPollAdHocResults_TimesOutUnreportedProbes(t *testing.T) {
	srv := fakeGrafanaQueryServer(t, adHocLine(t, "Oregon", true))
	defer srv.Close()

	cfg := config.NamespacedRESTConfig{Config: rest.Config{Host: srv.URL}}
	lokiClient, err := loki.NewClient(cfg)
	require.NoError(t, err)

	probeNames := map[int64]string{1: "Oregon", 2: "Paris"}
	results, err := checks.PollAdHocResults(context.Background(), lokiClient, "loki-uid", "abc-123", probeNames, 50*time.Millisecond)
	require.NoError(t, err)
	require.Len(t, results, 2)

	assert.Equal(t, checks.AdHocSuccess, results[0].Status)
	assert.Equal(t, "Paris", results[1].ProbeName)
	assert.Equal(t, checks.AdHocTimeout, results[1].Status)
}
