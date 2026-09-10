package k6 //nolint:testpackage // The tests exercise private response joins and the shared cloud executor.

import (
	"bytes"
	"context"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestListRunLogsMapsRequestAndDecodesEntries(t *testing.T) {
	executor := &observabilityCloudExecutor{response: cloudResponse{
		StatusCode: http.StatusOK,
		Body: []byte(`{
			"status":"success",
			"data":{"resultType":"streams","result":[{
				"stream":{"source":"k6"},
				"values":[["1788778328000000000","message",{"level":"info"}]]
			}]}
		}`),
	}}
	operations := &cloudOperations{executor: executor}
	start := time.Unix(1788778328, 0)
	end := time.Unix(1788778644, 0)

	response, err := operations.ListRunLogs(context.Background(), 12345, RunLogsRequest{
		Query: `{test_run_id="12345"} |= "ready"`, Start: start, End: end, Direction: "backward", Limit: 10,
	})
	require.NoError(t, err)
	require.Len(t, response.Data.Result, 1)
	require.Equal(t, "message", response.Data.Result[0].Values[0].Line)
	require.Equal(t, map[string]string{"level": "info"}, response.Data.Result[0].Values[0].StructuredMetadata)

	require.Len(t, executor.requests, 1)
	request := executor.requests[0]
	require.Equal(t, cloudTargetLogs, request.Target)
	require.Equal(t, "12345", request.Headers.Get("X-K6testrun-Id"))
	parsed, err := url.Parse(request.Path)
	require.NoError(t, err)
	require.Equal(t, runLogsPath, parsed.Path)
	require.Equal(t, `{test_run_id="12345"} |= "ready"`, parsed.Query().Get("query"))
	require.Equal(t, "1788778328", parsed.Query().Get("start"))
	require.Equal(t, "1788778644", parsed.Query().Get("end"))
	require.Equal(t, "10", parsed.Query().Get("limit"))
}

func TestDecodeRunLogsRejectsShortValue(t *testing.T) {
	_, err := decodeRunLogs([]byte(`{"status":"success","data":{"result":[{"values":[["1"]]}]}}`))
	require.ErrorContains(t, err, "fewer than two fields")
}

func TestInsightOperationPathsAndEmptyArrays(t *testing.T) {
	tests := []struct {
		name     string
		body     string
		wantPath string
		call     func(*cloudOperations) error
	}{
		{
			name: "executions", body: `{"executions":null}`,
			wantPath: "/insights/api/v1/testrun/77/executions",
			call: func(operations *cloudOperations) error {
				response, err := operations.ListInsightExecutions(context.Background(), 77)
				require.Empty(t, response.Executions)
				return err
			},
		},
		{
			name: "definitions", body: `{"audits":null}`,
			wantPath: "/insights/api/v1/testrun/77/executions/execution-id/audits",
			call: func(operations *cloudOperations) error {
				response, err := operations.ListInsightAudits(context.Background(), 77, "execution-id")
				require.Empty(t, response.Audits)
				return err
			},
		},
		{
			name: "results", body: `{"audits":null}`,
			wantPath: "/insights/api/v1/testrun/77/executions/execution-id/audits/results",
			call: func(operations *cloudOperations) error {
				response, err := operations.ListInsightAuditResults(context.Background(), 77, "execution-id")
				require.Empty(t, response.Audits)
				return err
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			executor := &observabilityCloudExecutor{response: cloudResponse{StatusCode: http.StatusOK, Body: []byte(test.body)}}
			require.NoError(t, test.call(&cloudOperations{executor: executor}))
			require.Len(t, executor.requests, 1)
			require.Equal(t, cloudTargetInsights, executor.requests[0].Target)
			require.Equal(t, test.wantPath, executor.requests[0].Path)
		})
	}
}

func TestJoinRunInsightsPreservesDefinitionOrderAndUnmatchedResults(t *testing.T) {
	execution := &InsightExecution{ID: "latest", Version: 2}
	definitions := &InsightAuditsResponse{Audits: []InsightAudit{
		{ID: "a", Title: "Audit A"},
		{ID: "b", Title: "Audit B"},
	}}
	results := &InsightAuditResultsResponse{Audits: []InsightAuditResult{
		{AuditID: "b", Status: "passed"},
		{AuditID: "unknown", Status: "failed"},
	}}

	joined := joinRunInsights(execution, "execution-schema", definitions, results)
	require.Equal(t, execution, joined.Execution)
	require.Equal(t, []string{"a", "b"}, []string{joined.Audits[0].Definition.ID, joined.Audits[1].Definition.ID})
	require.Nil(t, joined.Audits[0].Result)
	require.Equal(t, "passed", joined.Audits[1].Result.Status)
	require.Equal(t, "unknown", joined.UnmatchedResults[0].AuditID)
}

func TestRunInsightsTableCodecIncludesMatchedAndUnmatchedResults(t *testing.T) {
	score := &InsightScore{Type: "numeric", Value: []byte(`0.95`)}
	insights := &RunInsights{
		Audits: []JoinedInsightAudit{{
			Definition: InsightAudit{ID: "audit-a", Title: "Audit A"},
			Result:     &InsightAuditResult{AuditID: "audit-a", Status: "passed", Score: score},
		}},
		UnmatchedResults: []InsightAuditResult{{AuditID: "audit-b", Status: "failed"}},
	}
	var output bytes.Buffer

	require.NoError(t, (&runInsightsTableCodec{}).Encode(&output, insights))
	require.Contains(t, output.String(), "Audit A")
	require.Contains(t, output.String(), "numeric:0.95")
	require.Contains(t, output.String(), "audit-b")
}

func TestRunsListLogsRejectsAnUnscopedQuery(t *testing.T) {
	command := newRunsListLogsCommand(nil)
	command.SetArgs([]string{"123", `{job="other"}`})

	err := command.Execute()
	require.ErrorContains(t, err, "log pipeline must start with '|'")
}
