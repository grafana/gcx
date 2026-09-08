package k6 //nolint:testpackage // Tests private request mapping and download safety helpers.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type diagnosticsCloudExecutor struct {
	requestIndex int
	requests     []cloudRequest
	responses    []cloudResponse
}

func (e *diagnosticsCloudExecutor) doCloud(_ context.Context, request cloudRequest) (cloudResponse, error) {
	e.requests = append(e.requests, request)
	response := e.responses[e.requestIndex]
	e.requestIndex++
	return response, nil
}

func TestListRunTracesMapsRequestAndNormalizesEmptyTraces(t *testing.T) {
	executor := &diagnosticsCloudExecutor{responses: []cloudResponse{{
		StatusCode: http.StatusOK,
		Body:       []byte(`{"metrics":{"completedJobs":1},"traces":null}`),
	}}}
	operations := &cloudOperations{executor: executor}

	result, err := operations.ListRunTraces(t.Context(), 42, RunTracesRequest{
		Query: `{ name = "iteration" }`, Start: 100, End: 200, Limit: 25,
	})
	require.NoError(t, err)
	assert.Empty(t, result.Traces)
	require.Len(t, executor.requests, 1)
	request := executor.requests[0]
	assert.Equal(t, cloudTargetLogs, request.Target)
	assert.Equal(t, "42", request.Headers.Get("X-K6testrun-Id"))
	parsed, err := url.Parse(request.Path)
	require.NoError(t, err)
	assert.Equal(t, runTracesSearchPath, parsed.Path)
	assert.Equal(t, `{ name = "iteration" }`, parsed.Query().Get("q"))
	assert.Equal(t, "100", parsed.Query().Get("start"))
	assert.Equal(t, "200", parsed.Query().Get("end"))
	assert.Equal(t, "25", parsed.Query().Get("limit"))
}

func TestGetRunTraceMapsRunHeaderAndTraceID(t *testing.T) {
	executor := &diagnosticsCloudExecutor{responses: []cloudResponse{{
		StatusCode: http.StatusOK, Body: []byte(`{"batches":null}`),
	}}}
	operations := &cloudOperations{executor: executor}

	result, err := operations.GetRunTrace(t.Context(), 42, "0123456789abcdef0123456789abcdef")
	require.NoError(t, err)
	assert.Empty(t, result.Batches)
	require.Len(t, executor.requests, 1)
	assert.Equal(t, runTracesGetPath+"/0123456789abcdef0123456789abcdef", executor.requests[0].Path)
	assert.Equal(t, "42", executor.requests[0].Headers.Get("X-K6testrun-Id"))
}

func TestRunArtifactOperationsMapHeaderAndSigningBody(t *testing.T) {
	executor := &diagnosticsCloudExecutor{responses: []cloudResponse{
		{StatusCode: http.StatusOK, Body: []byte(`null`)},
		{StatusCode: http.StatusOK, Body: []byte(`{"urls":[{"name":"42/files/screenshots/a.png","pre_signed_url":"https://example.test/a"}]}`)},
	}}
	operations := &cloudOperations{executor: executor}

	names, err := operations.ListRunArtifacts(t.Context(), 42)
	require.NoError(t, err)
	assert.Empty(t, names)
	downloads, err := operations.SignRunArtifactDownloads(t.Context(), 42, []string{"42/files/screenshots/a.png"})
	require.NoError(t, err)
	require.Len(t, downloads, 1)
	assert.Equal(t, "https://example.test/a", downloads[0].PreSignedURL)

	require.Len(t, executor.requests, 2)
	assert.Equal(t, runArtifactsPath+"/index", executor.requests[0].Path)
	assert.Equal(t, "42", executor.requests[0].Headers.Get("X-K6testrun-Id"))
	assert.Equal(t, runArtifactsPath+"/generate-pre-signed-url", executor.requests[1].Path)
	assert.Equal(t, "42", executor.requests[1].Headers.Get("X-K6testrun-Id"))
	var body map[string]any
	require.NoError(t, json.Unmarshal(executor.requests[1].Body, &body))
	assert.Equal(t, "aws_s3", body["service"])
	assert.Equal(t, "download", body["operation"])
}

func TestArtifactOutputPathRejectsTraversalAndWrongRun(t *testing.T) {
	outputDir := t.TempDir()
	path, err := artifactOutputPath(42, outputDir, "42/files/screenshots/screenshots/a.png")
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(outputDir, "screenshots", "screenshots", "a.png"), path)

	for _, name := range []string{
		"41/files/screenshots/a.png",
		"42/files/../../secret",
		"42/files/",
	} {
		_, err := artifactOutputPath(42, outputDir, name)
		assert.Error(t, err, name)
	}
}

func TestDownloadRunArtifactsWritesReceiptAndDoesNotOverwrite(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write([]byte("png-data"))
	}))
	t.Cleanup(server.Close)
	name := "42/files/screenshots/screenshots/a.png"
	outputDir := t.TempDir()
	downloads := []RunArtifactDownload{{Name: name, PreSignedURL: server.URL + "/a.png"}}

	receipt, err := downloadRunArtifacts(t.Context(), 42, outputDir, []string{name}, downloads, server.Client())
	require.NoError(t, err)
	assert.Equal(t, 1, receipt.Summary.Succeeded)
	require.Len(t, receipt.Files, 1)
	content, err := os.ReadFile(receipt.Files[0].Path)
	require.NoError(t, err)
	assert.Equal(t, "png-data", string(content))

	receipt, err = downloadRunArtifacts(t.Context(), 42, outputDir, []string{name}, downloads, server.Client())
	require.Error(t, err)
	assert.Zero(t, receipt.Summary.Succeeded)
	assert.Equal(t, 1, receipt.Summary.Failed)
}

type sequenceRunGetter struct {
	runs  []*TestRun
	calls int
}

func (g *sequenceRunGetter) GetTestRun(context.Context, int) (*TestRun, error) {
	index := g.calls
	g.calls++
	if index >= len(g.runs) {
		index = len(g.runs) - 1
	}
	return g.runs[index], nil
}

func TestWaitForRunWaitsThroughMetricProcessing(t *testing.T) {
	passed := "passed"
	getter := &sequenceRunGetter{runs: []*TestRun{
		{ID: 42, Status: "running"},
		{ID: 42, Status: "processing_metrics"},
		{ID: 42, Status: "completed", Result: &passed},
	}}

	run, err := waitForRun(t.Context(), getter, 42, time.Second, time.Millisecond)
	require.NoError(t, err)
	assert.Equal(t, "completed", run.Status)
	assert.Equal(t, 3, getter.calls)
}

func TestWaitForRunReportsLastStatusOnTimeout(t *testing.T) {
	getter := &sequenceRunGetter{runs: []*TestRun{{ID: 42, Status: "processing_metrics"}}}

	_, err := waitForRun(t.Context(), getter, 42, 5*time.Millisecond, time.Hour)
	require.ErrorContains(t, err, "timeout after 5ms")
	assert.ErrorContains(t, err, `last status was "processing_metrics"`)
}

func TestRunDiagnosticsCommandsRejectInvalidInputsBeforeLoadingConfig(t *testing.T) {
	tests := []struct {
		name    string
		command func(CloudConfigLoader) *cobra.Command
		args    []string
		want    string
	}{
		{name: "empty trace query", command: newRunsTracesListCommand, args: []string{"42", "--query", ""}, want: "non-empty TraceQL"},
		{name: "zero trace limit", command: newRunsTracesListCommand, args: []string{"42", "--limit", "0"}, want: "positive integer"},
		{name: "invalid trace ID", command: newRunsTracesGetCommand, args: []string{"42", "not-a-trace"}, want: "32 hexadecimal"},
		{name: "empty output directory", command: newRunsArtifactsDownloadCommand, args: []string{"42", "--output-dir", ""}, want: "non-empty directory"},
		{name: "zero wait timeout", command: newRunsWaitCommand, args: []string{"42", "--timeout", "0s"}, want: "positive duration"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			command := test.command(nil)
			command.SetArgs(test.args)
			err := command.Execute()
			require.ErrorContains(t, err, test.want)
		})
	}
}
