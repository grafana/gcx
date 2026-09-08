package k6

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/grafana/gcx/internal/query/prometheus"
)

const k6MetricsV5Path = "/cloud/v5"

// TestRunMetric is metric metadata for one test run.
type TestRunMetric struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Origin    string `json:"origin"`
	TestRunID int    `json:"test_run_id"`
	Type      string `json:"type"`
}

// LoadTestMetric is metric metadata combined across selected test runs.
type LoadTestMetric struct {
	Name   string   `json:"name"`
	Type   string   `json:"type"`
	Labels []string `json:"labels"`
}

// TestRunMetricsResponse is the v5 test-run metrics response.
type TestRunMetricsResponse struct {
	Value []TestRunMetric `json:"value"`
}

// LoadTestMetricsResponse is the v5 load-test metrics response.
type LoadTestMetricsResponse struct {
	Value []LoadTestMetric `json:"value"`
}

// MetricsRunSelection chooses recent runs or explicit run IDs.
type MetricsRunSelection struct {
	RunCount int
	RunIDs   []int
}

func (s MetricsRunSelection) validate(required bool) error {
	if s.RunCount != 0 && len(s.RunIDs) != 0 {
		return errors.New("--run-count and --run-id are mutually exclusive")
	}
	if s.RunCount < 0 {
		return fmt.Errorf("invalid --run-count %d: must be greater than 0", s.RunCount)
	}
	for _, id := range s.RunIDs {
		if id <= 0 {
			return fmt.Errorf("invalid --run-id %d: must be greater than 0", id)
		}
	}
	if required && s.RunCount == 0 && len(s.RunIDs) == 0 {
		return errors.New("one of --run-count or --run-id is required")
	}
	return nil
}

// MetricsQueryRequest defines a range or aggregate query for one test run.
type MetricsQueryRequest struct {
	Expression string
	Metric     string
	Aggregate  bool
	Start      time.Time
	End        time.Time
	Step       time.Duration
}

func (r MetricsQueryRequest) validate() error {
	if strings.TrimSpace(r.Expression) == "" {
		return errors.New("metric query expression must not be empty")
	}
	if strings.TrimSpace(r.Metric) == "" {
		return errors.New("metric selector must not be empty")
	}
	if r.Start.IsZero() != r.End.IsZero() {
		return errors.New("metric query start and end must be provided together")
	}
	if !r.Start.IsZero() && !r.Start.Before(r.End) {
		return errors.New("metric query start must be before end")
	}
	if r.Step < 0 {
		return errors.New("metric query step must be greater than 0")
	}
	if r.Step > 0 && r.Step%time.Second != 0 {
		return fmt.Errorf("metric query step %q must be a whole number of seconds", r.Step)
	}
	if r.Aggregate && r.Step != 0 {
		return errors.New("metric query step is not supported for aggregate queries")
	}
	return nil
}

// LoadTestMetricsQueryRequest defines an aggregate query across test runs.
type LoadTestMetricsQueryRequest struct {
	Expression string
	Metric     string
	Selection  MetricsRunSelection
}

func (r LoadTestMetricsQueryRequest) validate() error {
	if strings.TrimSpace(r.Expression) == "" {
		return errors.New("metric query expression must not be empty")
	}
	if strings.TrimSpace(r.Metric) == "" {
		return errors.New("metric selector must not be empty")
	}
	return r.Selection.validate(true)
}

type k6V5FunctionParam struct {
	name  string
	value string
	quote bool
}

func buildK6V5FunctionPath(base string, params []k6V5FunctionParam) string {
	parts := make([]string, 0, len(params))
	for _, param := range params {
		value := param.value
		if param.quote {
			value = "'" + strings.ReplaceAll(value, "'", "''") + "'"
		}
		parts = append(parts, param.name+"="+value)
	}
	rawPath := base + "(" + strings.Join(parts, ",") + ")"
	return (&url.URL{Path: rawPath}).EscapedPath()
}

func selectionParam(selection MetricsRunSelection) (k6V5FunctionParam, bool) {
	if selection.RunCount > 0 {
		return k6V5FunctionParam{name: "test_run_count", value: strconv.Itoa(selection.RunCount)}, true
	}
	if len(selection.RunIDs) == 0 {
		return k6V5FunctionParam{}, false
	}
	ids := make([]string, len(selection.RunIDs))
	for i, id := range selection.RunIDs {
		ids[i] = strconv.Itoa(id)
	}
	return k6V5FunctionParam{name: "test_run_ids", value: "[" + strings.Join(ids, ",") + "]"}, true
}

func matchPath(path string, matches []string) string {
	if len(matches) == 0 {
		return path
	}
	q := url.Values{}
	for _, match := range matches {
		q.Add("match[]", match)
	}
	return path + "?" + q.Encode()
}

// ListTestRunMetrics lists metric metadata for one test run.
func (c *cloudOperations) ListTestRunMetrics(ctx context.Context, runID int) (*TestRunMetricsResponse, error) {
	path := fmt.Sprintf(k6MetricsV5Path+"/test_runs/%d/metrics", runID)
	resp, err := c.executor.doCloud(ctx, cloudRequest{
		Target: cloudTargetCloud, Auth: cloudAuthConfigured, Method: http.MethodGet, Path: path, Accept: "application/json",
	})
	if err != nil {
		return nil, fmt.Errorf("k6: list test run metrics: %w", err)
	}
	if err := checkCloudStatus(resp, "list test run metrics", http.StatusOK); err != nil {
		return nil, err
	}
	result, err := decodeCloudJSON[TestRunMetricsResponse](resp)
	if err != nil {
		return nil, err
	}
	if result.Value == nil {
		result.Value = []TestRunMetric{}
	}
	return &result, nil
}

// ListLoadTestMetrics lists metric metadata across selected test runs.
func (c *cloudOperations) ListLoadTestMetrics(
	ctx context.Context, loadTestID int, selection MetricsRunSelection,
) (*LoadTestMetricsResponse, error) {
	if err := selection.validate(false); err != nil {
		return nil, err
	}
	path := fmt.Sprintf(k6MetricsV5Path+"/load_tests/%d/metrics", loadTestID)
	if param, ok := selectionParam(selection); ok {
		path = buildK6V5FunctionPath(path, []k6V5FunctionParam{param})
	}
	resp, err := c.executor.doCloud(ctx, cloudRequest{
		Target: cloudTargetCloud, Auth: cloudAuthConfigured, Method: http.MethodGet, Path: path, Accept: "application/json",
	})
	if err != nil {
		return nil, fmt.Errorf("k6: list load test metrics: %w", err)
	}
	if err := checkCloudStatus(resp, "list load test metrics", http.StatusOK); err != nil {
		return nil, err
	}
	result, err := decodeCloudJSON[LoadTestMetricsResponse](resp)
	if err != nil {
		return nil, err
	}
	if result.Value == nil {
		result.Value = []LoadTestMetric{}
	}
	return &result, nil
}

// ListTestRunSeries lists time series for one test run.
func (c *cloudOperations) ListTestRunSeries(
	ctx context.Context, runID int, matches []string,
) (*prometheus.SeriesResponse, error) {
	path := matchPath(fmt.Sprintf(k6MetricsV5Path+"/test_runs/%d/series", runID), matches)
	resp, err := c.executor.doCloud(ctx, cloudRequest{
		Target: cloudTargetCloud, Auth: cloudAuthConfigured, Method: http.MethodGet, Path: path, Accept: "application/json",
	})
	if err != nil {
		return nil, fmt.Errorf("k6: list test run series: %w", err)
	}
	if err := checkCloudStatus(resp, "list test run series", http.StatusOK); err != nil {
		return nil, err
	}
	result, err := decodeCloudJSON[prometheus.SeriesResponse](resp)
	if err != nil {
		return nil, err
	}
	if result.Data == nil {
		result.Data = []map[string]string{}
	}
	return &result, nil
}

// ListTestRunLabels lists label names for one test run.
func (c *cloudOperations) ListTestRunLabels(
	ctx context.Context, runID int, matches []string,
) (*prometheus.LabelsResponse, error) {
	path := matchPath(fmt.Sprintf(k6MetricsV5Path+"/test_runs/%d/labels", runID), matches)
	resp, err := c.executor.doCloud(ctx, cloudRequest{
		Target: cloudTargetCloud, Auth: cloudAuthConfigured, Method: http.MethodGet, Path: path, Accept: "application/json",
	})
	if err != nil {
		return nil, fmt.Errorf("k6: list test run labels: %w", err)
	}
	if err := checkCloudStatus(resp, "list test run labels", http.StatusOK); err != nil {
		return nil, err
	}
	result, err := decodeCloudJSON[prometheus.LabelsResponse](resp)
	if err != nil {
		return nil, err
	}
	if result.Data == nil {
		result.Data = []string{}
	}
	return &result, nil
}

// ListTestRunLabelValues lists values for one label in one test run.
func (c *cloudOperations) ListTestRunLabelValues(
	ctx context.Context, runID int, label string, matches []string,
) (*prometheus.LabelsResponse, error) {
	path := fmt.Sprintf(k6MetricsV5Path+"/test_runs/%d/label/%s/values", runID, url.PathEscape(label))
	resp, err := c.executor.doCloud(ctx, cloudRequest{
		Target: cloudTargetCloud, Auth: cloudAuthConfigured, Method: http.MethodGet,
		Path: matchPath(path, matches), Accept: "application/json",
	})
	if err != nil {
		return nil, fmt.Errorf("k6: list test run label values: %w", err)
	}
	if err := checkCloudStatus(resp, "list test run label values", http.StatusOK); err != nil {
		return nil, err
	}
	result, err := decodeCloudJSON[prometheus.LabelsResponse](resp)
	if err != nil {
		return nil, err
	}
	if result.Data == nil {
		result.Data = []string{}
	}
	return &result, nil
}

// QueryTestRunMetrics queries metric values for one test run.
func (c *cloudOperations) QueryTestRunMetrics(
	ctx context.Context, runID int, request MetricsQueryRequest,
) (*prometheus.QueryResponse, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	operation := "query_range_k6"
	if request.Aggregate {
		operation = "query_aggregate_k6"
	}
	params := []k6V5FunctionParam{
		{name: "query", value: request.Expression, quote: true},
		{name: "metric", value: request.Metric, quote: true},
	}
	if request.Step > 0 {
		params = append(params, k6V5FunctionParam{name: "step", value: strconv.FormatInt(int64(request.Step/time.Second), 10)})
	}
	if !request.Start.IsZero() {
		params = append(params,
			k6V5FunctionParam{name: "start", value: request.Start.Format(time.RFC3339Nano)},
			k6V5FunctionParam{name: "end", value: request.End.Format(time.RFC3339Nano)},
		)
	}
	path := buildK6V5FunctionPath(
		fmt.Sprintf(k6MetricsV5Path+"/test_runs/%d/%s", runID, operation), params,
	)
	resp, err := c.executor.doCloud(ctx, cloudRequest{
		Target: cloudTargetCloud, Auth: cloudAuthConfigured, Method: http.MethodGet, Path: path, Accept: "application/json",
	})
	if err != nil {
		return nil, fmt.Errorf("k6: query test run metrics: %w", err)
	}
	if err := checkCloudStatus(resp, "query test run metrics", http.StatusOK); err != nil {
		return nil, err
	}
	result, err := decodeCloudJSON[prometheus.QueryResponse](resp)
	if err != nil {
		return nil, err
	}
	if result.Data.Result == nil {
		result.Data.Result = []prometheus.Sample{}
	}
	return &result, nil
}

// QueryLoadTestMetrics runs one aggregate query across selected test runs.
func (c *cloudOperations) QueryLoadTestMetrics(
	ctx context.Context, loadTestID int, request LoadTestMetricsQueryRequest,
) (*prometheus.QueryResponse, error) {
	if err := request.validate(); err != nil {
		return nil, err
	}
	selection, _ := selectionParam(request.Selection)
	path := buildK6V5FunctionPath(
		fmt.Sprintf(k6MetricsV5Path+"/load_tests/%d/query_aggregate_k6", loadTestID),
		[]k6V5FunctionParam{
			{name: "query", value: request.Expression, quote: true},
			{name: "metric", value: request.Metric, quote: true},
			selection,
		},
	)
	resp, err := c.executor.doCloud(ctx, cloudRequest{
		Target: cloudTargetCloud, Auth: cloudAuthConfigured, Method: http.MethodGet, Path: path, Accept: "application/json",
	})
	if err != nil {
		return nil, fmt.Errorf("k6: query load test metrics: %w", err)
	}
	if err := checkCloudStatus(resp, "query load test metrics", http.StatusOK); err != nil {
		return nil, err
	}
	result, err := decodeCloudJSON[prometheus.QueryResponse](resp)
	if err != nil {
		return nil, err
	}
	if result.Data.Result == nil {
		result.Data.Result = []prometheus.Sample{}
	}
	return &result, nil
}
