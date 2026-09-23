package prometheus

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/grafana/gcx/internal/httputils"
	"github.com/grafana/gcx/internal/queryerror"
)

// SearchOptions holds the parameters shared by the experimental search
// endpoints (/api/v1/search/metric_names, label_names, label_values),
// available on both Prometheus and Mimir. All fields are optional except
// where the field doc says otherwise.
//
// Limit and BatchSize are always sent, even when zero: the server treats an
// explicit 0 for Limit as "unlimited" (distinct from its own default of
// 100), so a Go zero value must not be indistinguishable from that request.
type SearchOptions struct {
	// Search holds fuzzy search terms (max 32); repeated terms combine as OR.
	Search []string
	// Match holds PromQL series selectors restricting candidates.
	Match []string
	// Start and End bound the time range; a zero value omits that bound.
	Start, End time.Time
	// CaseSensitive controls Search matching; nil omits the parameter
	// (server default: true).
	CaseSensitive *bool
	// FuzzAlg is "subsequence" (server default) or "jarowinkler".
	FuzzAlg string
	// FuzzThreshold is the minimum match score, 0-100 (server default: 0).
	FuzzThreshold int
	// SortBy is "alpha" (server default) or "score".
	SortBy string
	// SortDir is "asc" (server default) or "dsc"; only valid with SortBy "alpha".
	SortDir string
	// Limit caps the number of results; 0 means unlimited. Always sent.
	Limit int
	// BatchSize caps results per streamed NDJSON batch. Always sent.
	BatchSize int
	// IncludeScore adds a relevance score to each result.
	IncludeScore bool
	// IncludeMetadata attaches metric type/help/unit; metric_names only.
	IncludeMetadata bool
}

// MetricNameResult is one result from SearchMetricNames.
type MetricNameResult struct {
	Name  string
	Score float64
	Type  string
	Help  string
	Unit  string
}

// SearchMetricNamesResponse is the response from SearchMetricNames.
type SearchMetricNamesResponse struct {
	Results  []MetricNameResult
	HasMore  bool
	Warnings []string
}

// LabelNameResult is one result from SearchLabelNames.
type LabelNameResult struct {
	Name  string
	Score float64
}

// SearchLabelNamesResponse is the response from SearchLabelNames.
type SearchLabelNamesResponse struct {
	Results  []LabelNameResult
	HasMore  bool
	Warnings []string
}

// LabelValueResult is one result from SearchLabelValues.
type LabelValueResult struct {
	Value string
	Score float64
}

// SearchLabelValuesResponse is the response from SearchLabelValues.
type SearchLabelValuesResponse struct {
	Results  []LabelValueResult
	HasMore  bool
	Warnings []string
}

// SearchMetricNames searches metric names (and, with IncludeMetadata, their
// type/help/unit) via the experimental search API.
func (c *Client) SearchMetricNames(ctx context.Context, datasourceUID string, opts SearchOptions) (*SearchMetricNamesResponse, error) {
	results, hasMore, warnings, err := c.search(ctx, c.buildSearchMetricNamesPath(datasourceUID), nil, opts, "search metric names")
	if err != nil {
		return nil, err
	}

	out := make([]MetricNameResult, 0, len(results))
	for _, r := range results {
		out = append(out, MetricNameResult{Name: r.Name, Score: r.Score, Type: r.Type, Help: r.Help, Unit: r.Unit})
	}
	return &SearchMetricNamesResponse{Results: out, HasMore: hasMore, Warnings: warnings}, nil
}

// SearchLabelNames searches label names via the experimental search API.
func (c *Client) SearchLabelNames(ctx context.Context, datasourceUID string, opts SearchOptions) (*SearchLabelNamesResponse, error) {
	results, hasMore, warnings, err := c.search(ctx, c.buildSearchLabelNamesPath(datasourceUID), nil, opts, "search label names")
	if err != nil {
		return nil, err
	}

	out := make([]LabelNameResult, 0, len(results))
	for _, r := range results {
		out = append(out, LabelNameResult{Name: r.Name, Score: r.Score})
	}
	return &SearchLabelNamesResponse{Results: out, HasMore: hasMore, Warnings: warnings}, nil
}

// SearchLabelValues searches the values of a single label via the
// experimental search API.
func (c *Client) SearchLabelValues(ctx context.Context, datasourceUID, label string, opts SearchOptions) (*SearchLabelValuesResponse, error) {
	extra := url.Values{"label": []string{label}}
	results, hasMore, warnings, err := c.search(ctx, c.buildSearchLabelValuesPath(datasourceUID), extra, opts, "search label values")
	if err != nil {
		return nil, err
	}

	out := make([]LabelValueResult, 0, len(results))
	for _, r := range results {
		out = append(out, LabelValueResult{Value: r.Value, Score: r.Score})
	}
	return &SearchLabelValuesResponse{Results: out, HasMore: hasMore, Warnings: warnings}, nil
}

// search performs a GET against a search endpoint and streams the NDJSON
// response. extra carries endpoint-specific query parameters (e.g. "label"
// for search/label_values) alongside the parameters common to all three
// endpoints.
func (c *Client) search(ctx context.Context, apiPath string, extra url.Values, opts SearchOptions, operation string) ([]searchResultRaw, bool, []string, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, c.restConfig.Host+apiPath, nil)
	if err != nil {
		return nil, false, nil, fmt.Errorf("failed to create request: %w", err)
	}

	q := httpReq.URL.Query()
	for k, vs := range extra {
		for _, v := range vs {
			q.Add(k, v)
		}
	}
	addSearchParams(q, opts)
	httpReq.URL.RawQuery = q.Encode()

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, false, nil, fmt.Errorf("failed to %s: %w", operation, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, readErr := httputils.ReadResponseBody(resp.Body, httputils.DefaultResponseLimit)
		if readErr != nil {
			return nil, false, nil, fmt.Errorf("failed to read response: %w", readErr)
		}
		return nil, false, nil, searchError(operation, resp.StatusCode, body)
	}

	results, hasMore, warnings, err := decodeSearchStream(resp.Body)
	if err != nil {
		return nil, false, nil, fmt.Errorf("failed to %s: %w", operation, err)
	}
	return results, hasMore, warnings, nil
}

// addSearchParams appends the parameters common to all three search
// endpoints. Optional parameters are omitted when unset so the server's own
// default applies; Limit and BatchSize are always sent (see SearchOptions).
func addSearchParams(q url.Values, opts SearchOptions) {
	for _, s := range opts.Search {
		q.Add("search[]", s)
	}
	for _, m := range opts.Match {
		q.Add("match[]", m)
	}
	if !opts.Start.IsZero() {
		q.Set("start", strconv.FormatInt(opts.Start.Unix(), 10))
	}
	if !opts.End.IsZero() {
		q.Set("end", strconv.FormatInt(opts.End.Unix(), 10))
	}
	if opts.CaseSensitive != nil {
		q.Set("case_sensitive", strconv.FormatBool(*opts.CaseSensitive))
	}
	if opts.FuzzAlg != "" {
		q.Set("fuzz_alg", opts.FuzzAlg)
	}
	if opts.FuzzThreshold != 0 {
		q.Set("fuzz_threshold", strconv.Itoa(opts.FuzzThreshold))
	}
	if opts.SortBy != "" {
		q.Set("sort_by", opts.SortBy)
	}
	if opts.SortDir != "" {
		q.Set("sort_dir", opts.SortDir)
	}
	q.Set("limit", strconv.Itoa(opts.Limit))
	q.Set("batch_size", strconv.Itoa(opts.BatchSize))
	if opts.IncludeScore {
		q.Set("include_score", "true")
	}
	if opts.IncludeMetadata {
		q.Set("include_metadata", "true")
	}
}

// searchResultRaw carries every field any of the three search endpoints may
// return per result; a field the endpoint doesn't use decodes to its zero
// value.
type searchResultRaw struct {
	Name  string  `json:"name"`
	Value string  `json:"value"`
	Score float64 `json:"score"`
	Type  string  `json:"type"`
	Help  string  `json:"help"`
	Unit  string  `json:"unit"`
}

// searchStreamFrame decodes one line of the NDJSON response. A batch frame
// carries Results; the final trailer frame carries a non-empty Status
// instead, so that field distinguishes the two — Results is absent from
// every trailer the API sends.
type searchStreamFrame struct {
	Results   []searchResultRaw `json:"results"`
	Status    string            `json:"status"`
	HasMore   bool              `json:"has_more"`
	Warnings  []string          `json:"warnings"`
	ErrorType string            `json:"errorType"`
	Error     string            `json:"error"`
}

// decodeSearchStream reads an NDJSON search response: zero or more batch
// frames, then exactly one trailer frame. The body is capped at
// httputils.DefaultResponseLimit so a huge or misbehaving stream cannot
// exhaust memory; results are decoded incrementally rather than buffered
// whole, since the response is not a single JSON document.
func decodeSearchStream(body io.Reader) ([]searchResultRaw, bool, []string, error) {
	dec := json.NewDecoder(io.LimitReader(body, httputils.DefaultResponseLimit))

	var results []searchResultRaw
	for {
		var frame searchStreamFrame
		if err := dec.Decode(&frame); err != nil {
			if errors.Is(err, io.EOF) {
				return nil, false, nil, errors.New("stream ended without a trailer")
			}
			return nil, false, nil, fmt.Errorf("failed to parse response: %w", err)
		}

		if frame.Status != "" {
			if frame.Status == "error" {
				return nil, false, nil, fmt.Errorf("%s (%s)", frame.Error, frame.ErrorType)
			}
			return results, frame.HasMore, frame.Warnings, nil
		}

		results = append(results, frame.Results...)
	}
}

// searchError builds an error for a non-200 search response. A 404 means the
// endpoint is unavailable because the experimental search API is disabled —
// on both Prometheus and Mimir it ships off by default — so it returns a
// friendlier message while keeping the raw upstream error as the wrapped
// cause.
func searchError(operation string, statusCode int, body []byte) error {
	apiErr := queryerror.FromBody("prometheus", operation, statusCode, body)
	if statusCode == http.StatusNotFound {
		return fmt.Errorf("search API is experimental and disabled by default; enable it with --enable-feature=search-api on Prometheus or -querier.experimental-search-api-enabled on Mimir: %w", apiErr)
	}
	return apiErr
}

func (c *Client) buildSearchMetricNamesPath(datasourceUID string) string {
	return fmt.Sprintf("/api/datasources/uid/%s/resources/api/v1/search/metric_names", url.PathEscape(datasourceUID))
}

func (c *Client) buildSearchLabelNamesPath(datasourceUID string) string {
	return fmt.Sprintf("/api/datasources/uid/%s/resources/api/v1/search/label_names", url.PathEscape(datasourceUID))
}

func (c *Client) buildSearchLabelValuesPath(datasourceUID string) string {
	return fmt.Sprintf("/api/datasources/uid/%s/resources/api/v1/search/label_values", url.PathEscape(datasourceUID))
}
