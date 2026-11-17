package loki

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/grafana/gcx/internal/httputils"
	"github.com/grafana/gcx/internal/queryerror"
)

// IndexStatsResponse is Loki's index/stats summary of the streams, chunks,
// bytes and entries a selector matches over a time range.
type IndexStatsResponse struct {
	Streams uint64 `json:"streams"`
	Chunks  uint64 `json:"chunks"`
	Bytes   uint64 `json:"bytes"`
	Entries uint64 `json:"entries"`
}

// IndexStats calls Loki's index/stats endpoint for a stream selector, returning
// what the selector matches over [start, end) without running a query.
func (c *Client) IndexStats(ctx context.Context, datasourceUID, selector string, start, end time.Time) (*IndexStatsResponse, error) {
	if !start.Before(end) {
		return nil, fmt.Errorf("invalid time range: start (%s) must be before end (%s)", start, end)
	}

	apiPath := fmt.Sprintf("/api/datasources/uid/%s/resources/index/stats", url.PathEscape(datasourceUID))
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, c.restConfig.Host+apiPath, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	q := httpReq.URL.Query()
	q.Set("query", selector)
	q.Set("start", strconv.FormatInt(start.UnixNano(), 10))
	q.Set("end", strconv.FormatInt(end.UnixNano(), 10))
	httpReq.URL.RawQuery = q.Encode()

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to get index stats: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := httputils.ReadResponseBody(resp.Body, httputils.DefaultResponseLimit)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, queryerror.FromBody("loki", "index stats query", resp.StatusCode, respBody)
	}

	var result IndexStatsResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}
	return &result, nil
}
