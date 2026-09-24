package prometheus

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/grafana/gcx/internal/httputils"
	"github.com/grafana/gcx/internal/queryerror"
)

// SearchOptions holds the parameters shared by the experimental search
// endpoints (/api/v1/search/metric_names, label_names, label_values),
// available on both Prometheus and Mimir. All fields are optional except
// where the field doc says otherwise.
//
// Limit is always sent, even when zero: Mimir treats an explicit 0 as
// "unlimited" (distinct from its own default of 100), so a Go zero value
// must not be indistinguishable from that request. Prometheus rejects 0.
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
	// With "jarowinkler", 0 disables fuzzy matching, leaving substring
	// matches only.
	FuzzThreshold int
	// SortBy is "alpha" (server default) or "score".
	SortBy string
	// SortDir is "asc" (server default) or "dsc"; only valid with SortBy "alpha".
	SortDir string
	// Limit caps the number of results; 0 means unlimited on Mimir and is
	// rejected by Prometheus. Always sent.
	Limit int
	// IncludeScore adds a relevance score to each result.
	IncludeScore bool
	// IncludeMetadata attaches metric type/help/unit; metric_names only.
	IncludeMetadata bool
}

// MetricNameResult is one result from SearchMetricNames.
type MetricNameResult struct {
	Name  string  `json:"name"`
	Score float64 `json:"score,omitempty"`
	Type  string  `json:"type,omitempty"`
	Help  string  `json:"help,omitempty"`
	Unit  string  `json:"unit,omitempty"`
}

// SearchMetricNamesResponse is the response from SearchMetricNames.
type SearchMetricNamesResponse struct {
	Results  []MetricNameResult
	HasMore  bool
	Warnings []string
}

// LabelNameResult is one result from SearchLabelNames.
type LabelNameResult struct {
	Name  string  `json:"name"`
	Score float64 `json:"score,omitempty"`
}

// SearchLabelNamesResponse is the response from SearchLabelNames.
type SearchLabelNamesResponse struct {
	Results  []LabelNameResult
	HasMore  bool
	Warnings []string
}

// LabelValueResult is one result from SearchLabelValues.
type LabelValueResult struct {
	Value string  `json:"value"`
	Score float64 `json:"score,omitempty"`
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
		return nil, false, nil, searchError(operation, resp.StatusCode, body, opts.Limit)
	}

	results, hasMore, warnings, err := decodeSearchStream(resp.Body, httputils.DefaultResponseLimit)
	if err != nil {
		return nil, false, nil, fmt.Errorf("failed to %s: %w", operation, err)
	}
	return results, hasMore, warnings, nil
}

// addSearchParams appends the parameters common to all three search
// endpoints. Optional parameters are omitted when unset so the server's own
// default applies; Limit is always sent (see SearchOptions).
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
// every trailer the API sends. Either kind may carry Warnings: Prometheus
// sends them on the first batch (the trailer only repeats a changed set),
// Mimir on the trailer.
type searchStreamFrame struct {
	Results   []searchResultRaw `json:"results"`
	Status    string            `json:"status"`
	HasMore   bool              `json:"has_more"`
	Warnings  []string          `json:"warnings"`
	ErrorType string            `json:"errorType"`
	Error     string            `json:"error"`
}

// decodeSearchStream reads an NDJSON search response: zero or more batch
// frames, then exactly one trailer frame. Results are decoded incrementally,
// since the response is not a single JSON document, and the body is capped
// at limit bytes so a huge or misbehaving stream cannot exhaust memory.
//
// A stream without a trailer is an error, since the results read so far may
// be incomplete: it was cut by the cap, or by an interrupted upstream
// connection, which Grafana's datasource proxy forwards as a clean end of
// stream.
func decodeSearchStream(body io.Reader, limit int64) ([]searchResultRaw, bool, []string, error) {
	// Read one byte past the cap so hitting it is distinguishable from a
	// stream that ends exactly at the cap.
	counter := &countingReader{r: io.LimitReader(body, limit+1)}
	dec := json.NewDecoder(counter)

	var (
		results  []searchResultRaw
		warnings []string
	)
	for {
		var frame searchStreamFrame
		if err := dec.Decode(&frame); err != nil {
			if !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
				return nil, false, nil, fmt.Errorf("failed to parse response: %w", err)
			}
			if counter.n > limit {
				size := fmt.Sprintf("%d MiB", limit>>20)
				if limit < 1<<20 {
					size = fmt.Sprintf("%d-byte", limit)
				}
				return nil, false, nil, fmt.Errorf("search response exceeded the %s limit after %d results; request a smaller limit or narrow the match selectors", size, len(results))
			}
			return nil, false, nil, fmt.Errorf("search stream ended after %d results without a completion trailer (connection interrupted?); refusing to return possibly incomplete results", len(results))
		}

		warnings = appendNewWarnings(warnings, frame.Warnings)

		if frame.Status != "" {
			if frame.Status == "error" {
				return nil, false, nil, fmt.Errorf("%s (%s)", frame.Error, frame.ErrorType)
			}
			return results, frame.HasMore, warnings, nil
		}

		results = append(results, frame.Results...)
	}
}

// countingReader counts the bytes read through it.
type countingReader struct {
	r io.Reader
	n int64
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += int64(n)
	return n, err
}

// appendNewWarnings appends each warning in src not already in dst,
// preserving order. Prometheus re-sends the full warning set on the trailer
// when it changes after the first batch, so plain appending would duplicate.
func appendNewWarnings(dst, src []string) []string {
	for _, w := range src {
		if !slices.Contains(dst, w) {
			dst = append(dst, w)
		}
	}
	return dst
}

// searchErrorBody is the Prometheus-style JSON error both servers send for
// a non-200 search response.
type searchErrorBody struct {
	ErrorType string `json:"errorType"`
	Error     string `json:"error"`
}

// searchError builds an error for a non-200 search response. It keeps the
// upstream error as the wrapped cause and adds a hint for two failures whose
// raw messages don't say what to do:
//
//   - The search API is disabled (the default on both servers): Mimir
//     answers 404 feature_not_enabled, Prometheus unavailable "search API
//     disabled". Prometheus's status is not checked: it is 500 by default
//     and operators can override it.
//   - Prometheus rejected limit 0, which only Mimir accepts as unlimited.
//
// The error is flagged experimental so a bare 404 from a server that lacks
// the endpoint gets the CLI's generic route-absent handling rather than a
// claim that the feature is disabled.
func searchError(operation string, statusCode int, body []byte, limit int) error {
	apiErr := queryerror.FromBody("prometheus", operation, statusCode, body).WithAvailability(false, true)

	var parsed searchErrorBody
	_ = json.Unmarshal(body, &parsed)

	switch {
	case statusCode == http.StatusNotFound && parsed.ErrorType == "feature_not_enabled",
		parsed.ErrorType == "unavailable" && strings.Contains(parsed.Error, "search API disabled"):
		return fmt.Errorf("the experimental search API is not enabled on this server; enable it with --enable-feature=search-api on Prometheus or -querier.experimental-search-api-enabled on Mimir: %w", apiErr)
	case statusCode == http.StatusBadRequest && limit == 0 && strings.Contains(parsed.Error, "invalid limit"):
		return fmt.Errorf("limit 0 (unlimited) is only supported by Mimir; Prometheus requires a positive limit, capped by its --web.search.max-limit: %w", apiErr)
	default:
		return apiErr
	}
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
