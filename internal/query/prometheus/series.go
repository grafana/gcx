package prometheus

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// SeriesResponse represents the response from a /api/v1/series query.
type SeriesResponse struct {
	Status string              `json:"status"`
	Data   []map[string]string `json:"data"`
}

// Series returns time series matching the given selectors. A zero start or end
// omits that bound from the request.
func (c *Client) Series(ctx context.Context, datasourceUID string, match []string, start, end time.Time) (*SeriesResponse, error) {
	apiPath := c.buildSeriesPath(datasourceUID)

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, c.restConfig.Host+apiPath, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	q := httpReq.URL.Query()
	for _, m := range match {
		q.Add("match[]", m)
	}
	if !start.IsZero() {
		q.Set("start", strconv.FormatInt(start.Unix(), 10))
	}
	if !end.IsZero() {
		q.Set("end", strconv.FormatInt(end.Unix(), 10))
	}
	httpReq.URL.RawQuery = q.Encode()

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to get series: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("series query failed with status %d: %s", resp.StatusCode, string(respBody))
	}

	var result SeriesResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return &result, nil
}

func (c *Client) buildSeriesPath(datasourceUID string) string {
	return fmt.Sprintf("/api/datasources/uid/%s/resources/api/v1/series", url.PathEscape(datasourceUID))
}
