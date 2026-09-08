package k6 //nolint:testpackage // The tests exercise private v5 path builders and the shared cloud executor.

import (
	"context"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/spf13/pflag"
	"github.com/stretchr/testify/require"
)

type observabilityCloudExecutor struct {
	requests []cloudRequest
	response cloudResponse
	err      error
}

func (e *observabilityCloudExecutor) doCloud(_ context.Context, request cloudRequest) (cloudResponse, error) {
	e.requests = append(e.requests, request)
	return e.response, e.err
}

func TestQueryTestRunMetricsBuildsEscapedFunctionPath(t *testing.T) {
	executor := &observabilityCloudExecutor{response: cloudResponse{
		StatusCode: http.StatusOK,
		Body:       []byte(`{"status":"success","data":{"resultType":"vector","result":null}}`),
	}}
	operations := &cloudOperations{executor: executor}
	start := time.Date(2026, time.September, 8, 10, 0, 0, 0, time.UTC)
	end := start.Add(time.Minute)

	response, err := operations.QueryTestRunMetrics(context.Background(), 42, MetricsQueryRequest{
		Expression: `p(95) or 'quoted'`,
		Metric:     `http_req_duration{scenario='api'}`,
		Start:      start,
		End:        end,
		Step:       15 * time.Second,
	})
	require.NoError(t, err)
	require.Empty(t, response.Data.Result)
	require.Len(t, executor.requests, 1)
	request := executor.requests[0]
	require.Equal(t, cloudTargetCloud, request.Target)
	require.Equal(t, cloudAuthConfigured, request.Auth)
	require.Equal(t, http.MethodGet, request.Method)
	require.NotContains(t, request.Path, " ")

	decodedPath, err := url.PathUnescape(request.Path)
	require.NoError(t, err)
	require.Equal(t,
		"/cloud/v5/test_runs/42/query_range_k6(query='p(95) or ''quoted''',metric='http_req_duration{scenario='"+
			"'api''}',step=15,start=2026-09-08T10:00:00Z,end=2026-09-08T10:01:00Z)",
		decodedPath,
	)
}

func TestListLoadTestMetricsBuildsRunSelection(t *testing.T) {
	executor := &observabilityCloudExecutor{response: cloudResponse{
		StatusCode: http.StatusOK,
		Body:       []byte(`{"value":null}`),
	}}
	operations := &cloudOperations{executor: executor}

	response, err := operations.ListLoadTestMetrics(context.Background(), 9, MetricsRunSelection{RunIDs: []int{4, 7}})
	require.NoError(t, err)
	require.Empty(t, response.Value)
	require.Len(t, executor.requests, 1)
	decodedPath, err := url.PathUnescape(executor.requests[0].Path)
	require.NoError(t, err)
	require.Equal(t, "/cloud/v5/load_tests/9/metrics(test_run_ids=[4,7])", decodedPath)
}

func TestListTestRunLabelValuesEscapesLabelAndMatchers(t *testing.T) {
	executor := &observabilityCloudExecutor{response: cloudResponse{
		StatusCode: http.StatusOK,
		Body:       []byte(`{"status":"success","data":null}`),
	}}
	operations := &cloudOperations{executor: executor}

	response, err := operations.ListTestRunLabelValues(
		context.Background(), 12, "path/name", []string{`http_reqs{scenario="api canary"}`},
	)
	require.NoError(t, err)
	require.Empty(t, response.Data)
	require.Len(t, executor.requests, 1)
	require.Equal(t,
		"/cloud/v5/test_runs/12/label/path%2Fname/values?match%5B%5D=http_reqs%7Bscenario%3D%22api+canary%22%7D",
		executor.requests[0].Path,
	)
}

func TestMetricsRunSelectionValidation(t *testing.T) {
	tests := []struct {
		name      string
		selection MetricsRunSelection
		required  bool
		wantError string
	}{
		{name: "requires a selector", required: true, wantError: "one of --run-count or --run-id is required"},
		{name: "rejects both selectors", selection: MetricsRunSelection{RunCount: 2, RunIDs: []int{1}}, wantError: "mutually exclusive"},
		{name: "rejects negative count", selection: MetricsRunSelection{RunCount: -1}, wantError: "greater than 0"},
		{name: "accepts server default", selection: MetricsRunSelection{}, required: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := test.selection.validate(test.required)
			if test.wantError == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, test.wantError)
		})
	}
}

func TestSeriesSelectorsRequireMetricName(t *testing.T) {
	seriesOpts := &listTestRunSeriesOpts{}
	seriesOpts.setup(pflag.NewFlagSet("series", pflag.ContinueOnError))
	require.ErrorContains(t, seriesOpts.Validate([]string{`{scenario="api"}`}), "metric name")

	labelOpts := &listTestRunLabelsOpts{}
	labelOpts.setup(pflag.NewFlagSet("labels", pflag.ContinueOnError))
	labelOpts.Matches = []string{`{scenario="api"}`}
	require.ErrorContains(t, labelOpts.Validate(), "metric name")
}
