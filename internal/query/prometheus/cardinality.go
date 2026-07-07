package prometheus

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"

	"github.com/grafana/gcx/internal/httputils"
	"github.com/grafana/gcx/internal/queryerror"
)

// CardinalityLabelNamesResponse is the response from the Mimir cardinality
// analysis endpoint /api/v1/cardinality/label_names.
type CardinalityLabelNamesResponse struct {
	LabelValuesCountTotal int                    `json:"label_values_count_total"`
	LabelNamesCount       int                    `json:"label_names_count"`
	Cardinality           []LabelNameCardinality `json:"cardinality"`
}

// LabelNameCardinality reports the number of distinct values for a label name.
type LabelNameCardinality struct {
	LabelName        string `json:"label_name"`
	LabelValuesCount int    `json:"label_values_count"`
}

// CardinalityLabelValuesResponse is the response from the Mimir cardinality
// analysis endpoint /api/v1/cardinality/label_values.
type CardinalityLabelValuesResponse struct {
	SeriesCountTotal int                      `json:"series_count_total"`
	Labels           []LabelValuesCardinality `json:"labels"`
}

// LabelValuesCardinality reports, for one label name, its distinct values and
// the per-value series counts.
type LabelValuesCardinality struct {
	LabelName        string                  `json:"label_name"`
	LabelValuesCount int                     `json:"label_values_count"`
	SeriesCount      int                     `json:"series_count"`
	Cardinality      []LabelValueCardinality `json:"cardinality"`
}

// LabelValueCardinality reports the number of series carrying a label value.
type LabelValueCardinality struct {
	LabelValue  string `json:"label_value"`
	SeriesCount int    `json:"series_count"`
}

// CardinalityOptions holds the parameters shared by both cardinality endpoints.
type CardinalityOptions struct {
	// Selector is a PromQL series selector scoping the analysis; empty omits it.
	Selector string
	// CountMethod is "inmemory" or "active"; empty uses the server default.
	CountMethod string
	// Limit caps the number of returned items (server range 0-500); always sent.
	Limit int
}

// CardinalityLabelNames returns, per label name, the number of distinct values.
func (c *Client) CardinalityLabelNames(ctx context.Context, datasourceUID string, opts CardinalityOptions) (*CardinalityLabelNamesResponse, error) {
	respBody, err := c.getCardinality(ctx, c.buildCardinalityLabelNamesPath(datasourceUID), nil, opts, "cardinality label names query")
	if err != nil {
		return nil, err
	}

	var result CardinalityLabelNamesResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return &result, nil
}

// CardinalityLabelValues returns, for each requested label name, its distinct
// values and per-value series counts.
func (c *Client) CardinalityLabelValues(ctx context.Context, datasourceUID string, labelNames []string, opts CardinalityOptions) (*CardinalityLabelValuesResponse, error) {
	respBody, err := c.getCardinality(ctx, c.buildCardinalityLabelValuesPath(datasourceUID), labelNames, opts, "cardinality label values query")
	if err != nil {
		return nil, err
	}

	var result CardinalityLabelValuesResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return &result, nil
}

// getCardinality performs a GET against a cardinality endpoint and returns the
// raw response body, translating non-200 responses into typed errors.
func (c *Client) getCardinality(ctx context.Context, apiPath string, labelNames []string, opts CardinalityOptions, operation string) ([]byte, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, c.restConfig.Host+apiPath, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	q := httpReq.URL.Query()
	for _, name := range labelNames {
		q.Add("label_names[]", name)
	}
	if opts.Selector != "" {
		q.Set("selector", opts.Selector)
	}
	if opts.CountMethod != "" {
		q.Set("count_method", opts.CountMethod)
	}
	q.Set("limit", strconv.Itoa(opts.Limit))
	httpReq.URL.RawQuery = q.Encode()

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to query cardinality: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := httputils.ReadResponseBody(resp.Body, httputils.DefaultResponseLimit)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, cardinalityError(operation, resp.StatusCode, respBody)
	}

	return respBody, nil
}

// cardinalityError builds an error for a non-200 cardinality response. A 404 or
// 501 means the endpoint is unavailable — vanilla Prometheus, or Mimir with
// cardinality analysis disabled — so it returns a friendlier message while
// keeping the raw upstream error as the wrapped cause. Other statuses use the
// standard typed API error.
func cardinalityError(operation string, statusCode int, body []byte) error {
	apiErr := queryerror.FromBody("prometheus", operation, statusCode, body)
	if statusCode == http.StatusNotFound || statusCode == http.StatusNotImplemented {
		return fmt.Errorf("cardinality analysis is only available on Grafana Mimir (OSS) and Grafana Cloud; on self-hosted Mimir it requires -querier.cardinality-analysis-enabled: %w", apiErr)
	}
	return apiErr
}

func (c *Client) buildCardinalityLabelNamesPath(datasourceUID string) string {
	return fmt.Sprintf("/api/datasources/uid/%s/resources/api/v1/cardinality/label_names", url.PathEscape(datasourceUID))
}

func (c *Client) buildCardinalityLabelValuesPath(datasourceUID string) string {
	return fmt.Sprintf("/api/datasources/uid/%s/resources/api/v1/cardinality/label_values", url.PathEscape(datasourceUID))
}
