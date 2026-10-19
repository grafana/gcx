package k6

import (
	"context"
	"fmt"
	"net/http"
)

// testRunsPageSize is the largest page k6 Cloud returns for a run listing.
const testRunsPageSize = 1000

type doJSONFunc func(ctx context.Context, method, path string, body any) (*http.Response, error)

// listTestRuns lists a load test's runs newest first. It pages until limit
// runs are collected, or until a short page ends the listing when limit is 0.
func listTestRuns(ctx context.Context, doJSON doJSONFunc, loadTestID, limit int) ([]TestRunStatus, error) {
	runs := []TestRunStatus{}
	for {
		pageSize := testRunsPageSize
		if limit > 0 {
			pageSize = min(pageSize, limit-len(runs))
		}
		path := fmt.Sprintf(loadTestsPath+"/%d/test_runs?$orderby=created%%20desc&$top=%d&$skip=%d",
			loadTestID, pageSize, len(runs))
		page, err := getTestRunsPage(ctx, doJSON, path, loadTestID)
		if err != nil {
			return nil, err
		}
		runs = append(runs, page...)
		if len(page) < pageSize || (limit > 0 && len(runs) >= limit) {
			return runs, nil
		}
	}
}

func getTestRunsPage(ctx context.Context, doJSON doJSONFunc, path string, loadTestID int) ([]TestRunStatus, error) {
	resp, err := doJSON(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, fmt.Errorf("k6: list test runs: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("k6: list test runs for %d: status %d: %s", loadTestID, resp.StatusCode, readErrorBody(resp))
	}
	result, err := decodeJSON[testRunsResponse](resp)
	if err != nil {
		return nil, err
	}
	return result.Value, nil
}
