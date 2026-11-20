package k6

import (
	"context"
	"fmt"
	"net/http"
)

// listTestRuns pages through a load test's runs with the k6 v6 API's OData
// $skip/$top parameters. A single unpaged request is capped by the server at
// 1000 runs, so without paging a test with more runs is silently truncated.
// If limit > 0, at most limit runs are returned.
func listTestRuns(
	ctx context.Context,
	doJSON func(ctx context.Context, method, path string, body any) (*http.Response, error),
	loadTestID, limit int,
) ([]TestRunStatus, error) {
	const defaultPageSize = 100

	var (
		all         []TestRunStatus
		prevFirstID int
		havePrev    bool
	)

	for {
		pageSize := defaultPageSize
		if limit > 0 {
			if remaining := limit - len(all); remaining < pageSize {
				pageSize = remaining
			}
		}

		path := fmt.Sprintf(loadTestsPath+"/%d/test_runs?$skip=%d&$top=%d", loadTestID, len(all), pageSize)

		resp, err := doJSON(ctx, http.MethodGet, path, nil)
		if err != nil {
			return nil, fmt.Errorf("k6: list test runs: %w", err)
		}

		if resp.StatusCode >= 400 {
			body := readErrorBody(resp)
			resp.Body.Close()

			return nil, fmt.Errorf("k6: list test runs for %d: status %d: %s", loadTestID, resp.StatusCode, body)
		}

		result, err := decodeJSON[testRunsResponse](resp)
		resp.Body.Close()

		if err != nil {
			return nil, err
		}

		// Guard against a server that ignores $skip and repeats the previous page.
		if len(result.Value) > 0 && havePrev && result.Value[0].ID == prevFirstID {
			break
		}

		if len(result.Value) > 0 {
			prevFirstID, havePrev = result.Value[0].ID, true
		}

		all = append(all, result.Value...)

		if limit > 0 && len(all) >= limit {
			all = all[:limit]
			break
		}

		if len(result.Value) < pageSize || (result.Count > 0 && len(all) >= result.Count) {
			break
		}
	}

	return all, nil
}
