//nolint:testpackage // Tests package-private operation request mappings and validators.
package k6

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type v6RecordingExecutor struct {
	request  cloudRequest
	response cloudResponse
}

type v6StackRecordingExecutor struct {
	v6RecordingExecutor

	stackID int
}

func (e *v6StackRecordingExecutor) selectedStackID() int { return e.stackID }

type v6PagedRunsExecutor struct{ paths []string }

func (e *v6PagedRunsExecutor) doCloud(_ context.Context, request cloudRequest) (cloudResponse, error) {
	e.paths = append(e.paths, request.Path)
	parsed, _ := url.Parse(request.Path)
	skip, _ := strconv.Atoi(parsed.Query().Get("$skip"))
	if skip == 0 {
		values := make([]TestRun, 100)
		for i := range values {
			values[i] = TestRun{ID: 101 - i, Created: fmt.Sprintf("2026-01-%02dT00:00:00Z", 31-(i%31))}
		}
		body, err := json.Marshal(TestRunList{Value: values, Count: 101})
		if err != nil {
			return cloudResponse{}, err
		}
		return cloudResponse{StatusCode: http.StatusOK, Body: body}, nil
	}
	body, err := json.Marshal(TestRunList{Value: []TestRun{{ID: 1, Created: "2025-12-31T23:59:59Z"}}, Count: 101})
	if err != nil {
		return cloudResponse{}, err
	}
	return cloudResponse{StatusCode: http.StatusOK, Body: body}, nil
}

func (e *v6RecordingExecutor) doCloud(_ context.Context, request cloudRequest) (cloudResponse, error) {
	e.request = request
	return e.response, nil
}

func TestValidateCloudAuthRejectsStackMismatch(t *testing.T) {
	executor := &v6StackRecordingExecutor{
		v6RecordingExecutor: v6RecordingExecutor{
			response: cloudResponse{StatusCode: http.StatusOK, Body: []byte(`{"stack_id":2,"default_project_id":3}`)},
		},
		stackID: 1,
	}
	operations := &cloudOperations{executor: executor}

	_, err := operations.ValidateCloudAuth(t.Context())
	require.ErrorContains(t, err, "selected stack 1, token stack 2")
}

func TestV6OperationsRequestMapping(t *testing.T) {
	tests := []struct {
		name       string
		response   cloudResponse
		call       func(context.Context, *cloudOperations) error
		method     string
		path       string
		auth       cloudAuthRoute
		accept     string
		headerName string
		header     string
	}{
		{
			name: "auth uses direct stack URL route", response: cloudResponse{StatusCode: http.StatusOK, Body: []byte(`{"stack_id":1,"default_project_id":2}`)},
			call: func(ctx context.Context, operations *cloudOperations) error {
				_, err := operations.ValidateCloudAuth(ctx)
				return err
			},
			method: http.MethodGet, path: authV6Path, auth: cloudAuthDirectStackURL, accept: "application/json",
		},
		{
			name: "start sends idempotency key", response: cloudResponse{StatusCode: http.StatusOK, Body: []byte(`{"id":8,"test_id":3,"status":"created"}`)},
			call: func(ctx context.Context, operations *cloudOperations) error {
				_, err := operations.StartLoadTest(ctx, 3, "key-1")
				return err
			},
			method: http.MethodPost, path: "/cloud/v6/load_tests/3/start", auth: cloudAuthConfigured, accept: "application/json", headerName: "K6-Idempotency-Key", header: "key-1",
		},
		{
			name: "run script sends requested type", response: cloudResponse{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"text/javascript"}}, Body: []byte("export default function() {}")},
			call: func(ctx context.Context, operations *cloudOperations) error {
				_, err := operations.DownloadTestRunScript(ctx, 9, "text/javascript")
				return err
			},
			method: http.MethodGet, path: "/cloud/v6/test_runs/9/script", auth: cloudAuthConfigured, accept: "text/javascript",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			executor := &v6RecordingExecutor{response: test.response}
			operations := &cloudOperations{executor: executor}
			require.NoError(t, test.call(t.Context(), operations))
			assert.Equal(t, test.method, executor.request.Method)
			assert.Equal(t, test.path, executor.request.Path)
			assert.Equal(t, test.auth, executor.request.Auth)
			assert.Equal(t, test.accept, executor.request.Accept)
			if test.headerName != "" {
				assert.Equal(t, test.header, executor.request.Headers.Get(test.headerName))
			}
		})
	}
}

func TestListProjectLimitsRequestMapping(t *testing.T) {
	executor := &v6RecordingExecutor{response: cloudResponse{StatusCode: http.StatusOK, Body: []byte(`{"value":[],"@count":0}`)}}
	operations := &cloudOperations{executor: executor}
	result, err := operations.ListProjectLimits(t.Context(), []int{7, 8}, 25)
	require.NoError(t, err)
	assert.Empty(t, result.Value)
	assert.Contains(t, executor.request.Path, "%24count=true")
	assert.Contains(t, executor.request.Path, "%24top=25")
	assert.Contains(t, executor.request.Path, "project_id_in=7%2C8")
}

func TestListAllTestRunsPaginatesNewestFirst(t *testing.T) {
	executor := &v6PagedRunsExecutor{}
	operations := &cloudOperations{executor: executor}
	result, err := operations.ListAllTestRuns(t.Context(), TestRunListParams{})
	require.NoError(t, err)
	require.Len(t, result.Value, 101)
	assert.Len(t, executor.paths, 2)
	assert.Equal(t, 101, result.Value[0].ID)
	assert.Equal(t, 1, result.Value[100].ID)
	assert.Equal(t, 101, result.Count)
	for _, path := range executor.paths {
		parsed, parseErr := url.Parse(path)
		require.NoError(t, parseErr)
		assert.Equal(t, "created desc", parsed.Query().Get("$orderby"))
	}
}

func TestListAllTestRunsAppliesLimitAfterNewestFirstOrder(t *testing.T) {
	executor := &v6RecordingExecutor{response: cloudResponse{
		StatusCode: http.StatusOK,
		Body:       []byte(`{"value":[{"id":101,"created":"2026-12-31T23:59:59Z"}],"@count":200}`),
	}}
	operations := &cloudOperations{executor: executor}
	result, err := operations.ListAllTestRuns(t.Context(), TestRunListParams{Top: 1})
	require.NoError(t, err)
	require.Len(t, result.Value, 1)
	assert.Equal(t, 101, result.Value[0].ID)
	assert.Equal(t, 200, result.Count)

	parsed, err := url.Parse(executor.request.Path)
	require.NoError(t, err)
	assert.Equal(t, "true", parsed.Query().Get("$count"))
	assert.Equal(t, "created desc", parsed.Query().Get("$orderby"))
	assert.Equal(t, "1", parsed.Query().Get("$top"))
	assert.Equal(t, "0", parsed.Query().Get("$skip"))
}

func TestFlexibleNumberAcceptsNumberAndString(t *testing.T) {
	for _, input := range []string{`12.5`, `"12.5"`} {
		var value FlexibleNumber
		require.NoError(t, json.Unmarshal([]byte(input), &value))
		assert.InDelta(t, 12.5, float64(value), 0.000001)
	}
}

func TestValidateOptionsAllowsNullReductionValues(t *testing.T) {
	var result ValidateOptionsResult
	err := json.Unmarshal([]byte(`{"vuh_usage":"0.01","breakdown":{"protocol_vuh":"0.01","browser_vuh":0,"base_total_vuh":"0.01","reduction_rate":null,"reduction_rate_breakdown":null}}`), &result)
	require.NoError(t, err)
	assert.InDelta(t, 0.01, float64(result.VUHUsage), 0.000001)
	assert.Nil(t, result.Breakdown.ReductionRate)
}

func TestScheduleModelSupportsPublishedFields(t *testing.T) {
	var schedule Schedule
	err := json.Unmarshal([]byte(`{
		"id":10,
		"load_test_id":5,
		"starts":"2026-06-01T10:00:00Z",
		"recurrence_rule":null,
		"cron":{"schedule":"0 7 * * 1","time_zone":"UTC"},
		"deactivated":false,
		"next_run":null,
		"created_by":"user@example.com"
	}`), &schedule)
	require.NoError(t, err)
	require.NotNil(t, schedule.Cron)
	assert.Equal(t, "0 7 * * 1", schedule.Cron.Schedule)
	assert.Equal(t, "UTC", schedule.Cron.TimeZone)
	assert.Nil(t, schedule.NextRun)
	require.NotNil(t, schedule.CreatedBy)
	assert.Equal(t, "user@example.com", *schedule.CreatedBy)

	until := "2026-12-31T00:00:00Z"
	count := 10
	request := ScheduleRequest{
		Starts: "2026-06-01T10:00:00Z",
		RecurrenceRule: &RecurrenceRule{
			Frequency: "WEEKLY",
			Interval:  2,
			ByDay:     []string{"SA", "SU"},
			Until:     &until,
			Count:     &count,
		},
	}
	data, err := json.Marshal(request)
	require.NoError(t, err)
	assert.JSONEq(t, `{
		"starts":"2026-06-01T10:00:00Z",
		"recurrence_rule":{"frequency":"WEEKLY","interval":2,"byday":["SA","SU"],"until":"2026-12-31T00:00:00Z","count":10}
	}`, string(data))
}

func TestTestRunAllowsNumericResultDetailCode(t *testing.T) {
	var result TestRun
	err := json.Unmarshal([]byte(`{"id":1,"result_details":{"type":"error","message":"failed","code":42}}`), &result)
	require.NoError(t, err)
	require.NotNil(t, result.ResultDetails)
	require.NotNil(t, result.ResultDetails.Code)
	assert.Equal(t, 42, *result.ResultDetails.Code)
}

func TestV6StaticValidation(t *testing.T) {
	validDescription := "description"
	tests := []struct {
		name     string
		err      error
		contains string
	}{
		{name: "empty label create", err: validateLabelKeyCreate(LabelKeyCreateRequest{}), contains: "1 to 100"},
		{name: "invalid label character", err: validateLabelKeyCreate(LabelKeyCreateRequest{Value: []LabelKeyCreateItem{{Key: "bad key"}}}), contains: "invalid"},
		{name: "duplicate label", err: validateLabelKeyCreate(LabelKeyCreateRequest{Value: []LabelKeyCreateItem{{Key: "key"}, {Key: "key", Description: &validDescription}}}), contains: "duplicated"},
		{name: "empty label patch", err: validateLabelKeyPatch(LabelKeyPatch{}), contains: "empty"},
		{name: "empty limits patch", err: validateProjectLimitsPatch(ProjectLimitsPatch{}), contains: "empty"},
		{name: "project label needs one key", err: validateProjectLabels(ProjectLabelPutRequest{Value: []ProjectLabelPutItem{{Value: "value"}}}), contains: "exactly one"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require.Error(t, test.err)
			assert.ErrorContains(t, test.err, test.contains)
		})
	}
}
