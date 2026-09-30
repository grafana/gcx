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
	// Search holds fuzzy search terms (both servers currently accept at most
	// 32); repeated terms combine as OR.
	Search []string
	// Match holds PromQL series selectors restricting candidates.
	Match []string
	// Start and End bound the time range; a zero value omits that bound.
	Start, End time.Time
	// CaseSensitive controls Search matching. Always sent, since its zero
	// value differs from the server default (true).
	CaseSensitive bool
	// FuzzAlg is "subsequence" (server default) or "jarowinkler".
	FuzzAlg string
	// FuzzThreshold is the minimum match score, 0-100 (server default: 0).
	// With "jarowinkler", it applies only to fuzzy matches: substring matches
	// are always kept, and 0 disables fuzzy matching.
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

// LabelNameResult is one result from SearchLabelNames.
type LabelNameResult struct {
	Name  string  `json:"name"`
	Score float64 `json:"score,omitempty"`
}

// LabelValueResult is one result from SearchLabelValues.
type LabelValueResult struct {
	Value string  `json:"value"`
	Score float64 `json:"score,omitempty"`
}

// SearchResponse is the response from a search endpoint. Results is never
// nil.
type SearchResponse[T any] struct {
	Results  []T
	HasMore  bool
	Warnings []string
	// Incomplete is true when the stream ended without a completion trailer
	// (cut by decodeSearchStream's size cap, or by an interrupted upstream
	// connection) rather than because the server's own trailer said
	// has_more=true. Callers need this distinction: an incomplete stream is
	// never continuable by raising --limit — regardless of what --limit the
	// caller already used — while a real has_more=true page may be.
	Incomplete bool
}

// SearchMetricNames searches metric names (and, with IncludeMetadata, their
// type/help/unit) via the experimental search API.
func (c *Client) SearchMetricNames(ctx context.Context, datasourceUID string, opts SearchOptions) (*SearchResponse[MetricNameResult], error) {
	return search[MetricNameResult](ctx, c, c.buildSearchMetricNamesPath(datasourceUID), nil, opts, "search metric names")
}

// SearchLabelNames searches label names via the experimental search API.
func (c *Client) SearchLabelNames(ctx context.Context, datasourceUID string, opts SearchOptions) (*SearchResponse[LabelNameResult], error) {
	return search[LabelNameResult](ctx, c, c.buildSearchLabelNamesPath(datasourceUID), nil, opts, "search label names")
}

// SearchLabelValues searches the values of a single label via the
// experimental search API.
func (c *Client) SearchLabelValues(ctx context.Context, datasourceUID, label string, opts SearchOptions) (*SearchResponse[LabelValueResult], error) {
	extra := url.Values{"label": []string{label}}
	return search[LabelValueResult](ctx, c, c.buildSearchLabelValuesPath(datasourceUID), extra, opts, "search label values")
}

// search performs a GET against a search endpoint and streams the NDJSON
// response. extra carries endpoint-specific query parameters (e.g. "label"
// for search/label_values) alongside the parameters common to all three
// endpoints. It is a function rather than a method because methods cannot
// take type parameters.
func search[T any](ctx context.Context, c *Client, apiPath string, extra url.Values, opts SearchOptions, operation string) (*SearchResponse[T], error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, c.restConfig.Host+apiPath, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
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
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, readErr := httputils.ReadResponseBody(resp.Body, httputils.DefaultResponseLimit)
		if readErr != nil {
			// The status code is still known even though the body isn't; keep
			// it — and the auth/availability handling it drives — instead of
			// discarding it in a generic wrapped error.
			return nil, queryerror.New("prometheus", operation, resp.StatusCode, readErr.Error(), "").WithAvailability(false, true)
		}
		return nil, searchError(operation, resp.StatusCode, body, opts.Limit)
	}

	results, hasMore, warnings, incomplete, err := decodeSearchStream[T](ctx, resp.Body, httputils.DefaultResponseLimit, operation)
	if err != nil {
		return nil, err
	}
	return &SearchResponse[T]{Results: results, HasMore: hasMore, Warnings: warnings, Incomplete: incomplete}, nil
}

// addSearchParams appends the parameters common to all three search
// endpoints. Optional parameters are omitted when unset so the server's own
// default applies; Limit and CaseSensitive are always sent (see
// SearchOptions).
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
	q.Set("case_sensitive", strconv.FormatBool(opts.CaseSensitive))
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

// searchStreamFrame decodes one line of the NDJSON response. A batch frame
// carries Results; the final trailer frame carries a non-empty Status
// instead, so that field distinguishes the two — Results is absent from
// every trailer the API sends. Either kind may carry Warnings: Prometheus
// sends them on the first batch (the trailer only repeats a changed set),
// Mimir on the trailer.
type searchStreamFrame[T any] struct {
	Results   []T      `json:"results"`
	Status    string   `json:"status"`
	HasMore   bool     `json:"has_more"`
	Warnings  []string `json:"warnings"`
	ErrorType string   `json:"errorType"`
	Error     string   `json:"error"`
}

// decodeSearchStream reads an NDJSON search response: zero or more batch
// frames, then exactly one trailer frame. Results are decoded incrementally,
// since the response is not a single JSON document, and the body is capped
// at limit bytes so a huge or misbehaving stream cannot exhaust memory.
//
// A stream without a trailer — cut by the cap, or by an interrupted upstream
// connection, which Grafana's datasource proxy forwards as a clean end of
// stream — is not an error: the API's client contract requires tolerating an
// abrupt EOF without a trailer, so the results read so far are returned,
// flagged as truncated (hasMore forced true, incomplete true) with a warning
// explaining why, rather than discarded. incomplete distinguishes this case
// from a real trailer reporting has_more=true: callers must never treat an
// incomplete stream as continuable by raising --limit, no matter what
// --limit was already used, whereas a real has_more=true page may be. ctx
// cancellation is checked first and reported as such, rather than folding
// into the same "no trailer" handling: it is the caller aborting, not the
// server or proxy ending the stream.
func decodeSearchStream[T any](ctx context.Context, body io.Reader, limit int64, operation string) ([]T, bool, []string, bool, error) {
	// Read one byte past the cap so hitting it is distinguishable from a
	// stream that ends exactly at the cap.
	counter := &countingReader{r: io.LimitReader(body, limit+1)}
	dec := json.NewDecoder(counter)

	// Start non-nil so an empty result encodes as [] rather than null.
	results := []T{}
	var warnings []string
	for {
		var frame searchStreamFrame[T]
		if err := dec.Decode(&frame); err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return nil, false, nil, false, fmt.Errorf("search cancelled: %w", ctxErr)
			}
			if !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
				return nil, false, nil, false, fmt.Errorf("failed to parse response: %w", err)
			}
			warning := "search stream ended after " + strconv.Itoa(len(results)) + " results without a completion trailer (connection interrupted?); results may be incomplete"
			if counter.n > limit {
				warning = fmt.Sprintf("search response exceeded the %d MiB limit after %d results; results may be incomplete — request a smaller limit or narrow the match selectors", limit>>20, len(results))
			}
			return results, true, appendNewWarnings(warnings, []string{warning}), true, nil
		}

		warnings = appendNewWarnings(warnings, frame.Warnings)

		if frame.Status != "" {
			if frame.Status == "error" {
				message := frame.Error
				switch {
				case frame.ErrorType != "" && frame.Error != "":
					message = frame.ErrorType + ": " + frame.Error
				case frame.ErrorType != "":
					message = frame.ErrorType
				}
				apiErr := queryerror.New("prometheus", operation, searchTrailerStatusCode(frame.ErrorType), message, "").WithAvailability(false, true)
				apiErr.TransportStatus = http.StatusOK
				return nil, false, nil, false, apiErr
			}
			return results, frame.HasMore, warnings, false, nil
		}

		results = append(results, frame.Results...)
	}
}

// searchTrailerStatusCode maps an in-band error trailer's errorType to the
// HTTP status Prometheus's own getDefaultErrorCode (web/api/v1/api.go)
// would assign the same failure had it occurred before the first batch was
// sent (as a normal 4xx/5xx response instead of an in-band trailer) — so a
// timeout, cancellation, or bad-data error mid-stream gets the same
// status-driven handling as one that isn't. Mimir's Prometheus-compatible
// API uses the same errorType strings.
func searchTrailerStatusCode(errorType string) int {
	switch errorType {
	case "bad_data":
		return http.StatusBadRequest
	case "execution":
		return http.StatusUnprocessableEntity
	case "canceled":
		return 499 // Prometheus's statusClientClosedConnection; net/http has no named constant for it.
	case "timeout":
		return http.StatusServiceUnavailable
	default:
		return http.StatusInternalServerError
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
