package logs

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

const maxBodyReadBytes = 64 * 1024

// maxAdaptiveDropRulesBodyBytes caps drop-rule HTTP response bodies. The list endpoint can
// return large JSON; a 64KiB read silently truncated responses and broke decoding with
// "unexpected end of JSON input".
const maxAdaptiveDropRulesBodyBytes = 10 << 20 // 10 MiB

// readDropRulesResponseBody reads resp.Body and closes it. It errors if the body is larger
// than maxAdaptiveDropRulesBodyBytes (without truncating).
func readDropRulesResponseBody(resp *http.Response) ([]byte, error) {
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxAdaptiveDropRulesBodyBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > maxAdaptiveDropRulesBodyBytes {
		return nil, fmt.Errorf("response body exceeds maximum size (%d bytes)", maxAdaptiveDropRulesBodyBytes)
	}
	return body, nil
}

// dropRulesPath is the log-template-service route for adaptive log drop rules.
const dropRulesPath = "/adaptive-logs/drop-rules"

// Client is an HTTP client for the Adaptive Logs API.
type Client struct {
	baseURL    string
	tenantID   int
	apiToken   string
	httpClient *http.Client
}

// NewClient creates a new Adaptive Logs client.
func NewClient(baseURL string, tenantID int, apiToken string, httpClient *http.Client) *Client {
	return &Client{
		baseURL:    baseURL,
		tenantID:   tenantID,
		apiToken:   apiToken,
		httpClient: httpClient,
	}
}

// ListExemptions returns all log stream exemptions.
// The API wraps the result in {"result": [...]}.
func (c *Client) ListExemptions(ctx context.Context) ([]Exemption, error) {
	resp, err := c.doRequest(ctx, http.MethodGet, "/adaptive-logs/exemptions", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to list exemptions: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, handleErrorResponse(resp)
	}

	var wrapper struct {
		Result []Exemption `json:"result"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&wrapper); err != nil {
		return nil, fmt.Errorf("failed to decode exemptions response: %w", err)
	}

	if wrapper.Result == nil {
		return []Exemption{}, nil
	}

	return wrapper.Result, nil
}

// GetExemption returns a single exemption by ID.
func (c *Client) GetExemption(ctx context.Context, id string) (*Exemption, error) {
	resp, err := c.doRequest(ctx, http.MethodGet, "/adaptive-logs/exemptions/"+url.PathEscape(id), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get exemption %s: %w", id, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, handleErrorResponse(resp)
	}

	exemption, err := decodeExemptionResponse(resp.Body)
	if err != nil {
		return nil, err
	}

	return exemption, nil
}

// CreateExemption creates a new log stream exemption.
func (c *Client) CreateExemption(ctx context.Context, e *Exemption) (*Exemption, error) {
	body, err := json.Marshal(e)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal exemption: %w", err)
	}

	resp, err := c.doRequest(ctx, http.MethodPost, "/adaptive-logs/exemptions", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create exemption: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusAccepted {
		return nil, handleErrorResponse(resp)
	}

	created, err := decodeExemptionResponse(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to decode create exemption response: %w", err)
	}

	return created, nil
}

// UpdateExemption updates an existing log stream exemption by ID.
// The API uses PUT /adaptive-logs/exemptions/{id}.
func (c *Client) UpdateExemption(ctx context.Context, id string, e *Exemption) (*Exemption, error) {
	body, err := json.Marshal(e)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal exemption: %w", err)
	}

	resp, err := c.doRequest(ctx, http.MethodPut, "/adaptive-logs/exemptions/"+url.PathEscape(id), bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to update exemption %s: %w", id, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted {
		return nil, handleErrorResponse(resp)
	}

	updated, err := decodeExemptionResponse(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to decode update exemption response: %w", err)
	}

	return updated, nil
}

// DeleteExemption deletes a log stream exemption by ID.
func (c *Client) DeleteExemption(ctx context.Context, id string) error {
	resp, err := c.doRequest(ctx, http.MethodDelete, "/adaptive-logs/exemptions/"+url.PathEscape(id), nil)
	if err != nil {
		return fmt.Errorf("failed to delete exemption %s: %w", id, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		return handleErrorResponse(resp)
	}

	return nil
}

// ListRecommendations returns all adaptive log pattern recommendations.
// For each recommendation, the Pattern field is populated using Label() if empty.
func (c *Client) ListRecommendations(ctx context.Context) ([]LogRecommendation, error) {
	resp, err := c.doRequest(ctx, http.MethodGet, "/adaptive-logs/recommendations", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to list recommendations: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, handleErrorResponse(resp)
	}

	var recs []LogRecommendation
	if err := json.NewDecoder(resp.Body).Decode(&recs); err != nil {
		return nil, fmt.Errorf("failed to decode recommendations response: %w", err)
	}

	for i := range recs {
		if recs[i].Pattern == "" {
			recs[i].Pattern = recs[i].Label()
		}
	}

	return recs, nil
}

// ListSegments returns all adaptive log segments.
// The API returns a bare JSON array (no wrapper).
func (c *Client) ListSegments(ctx context.Context) ([]LogSegment, error) {
	resp, err := c.doRequest(ctx, http.MethodGet, "/adaptive-logs/segments", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to list segments: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, handleErrorResponse(resp)
	}

	var segments []LogSegment
	if err := json.NewDecoder(resp.Body).Decode(&segments); err != nil {
		return nil, fmt.Errorf("failed to decode segments response: %w", err)
	}

	if segments == nil {
		return []LogSegment{}, nil
	}

	return segments, nil
}

// GetSegment returns a single segment by ID.
// The API uses a query parameter: GET /adaptive-logs/segment?segment=<id>.
func (c *Client) GetSegment(ctx context.Context, id string) (*LogSegment, error) {
	resp, err := c.doRequest(ctx, http.MethodGet, "/adaptive-logs/segment?segment="+url.QueryEscape(id), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get segment %s: %w", id, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, handleErrorResponse(resp)
	}

	var segment LogSegment
	if err := json.NewDecoder(resp.Body).Decode(&segment); err != nil {
		return nil, fmt.Errorf("failed to decode segment response: %w", err)
	}

	return &segment, nil
}

// CreateSegment creates a new adaptive log segment.
func (c *Client) CreateSegment(ctx context.Context, s *LogSegment) (*LogSegment, error) {
	body, err := json.Marshal(s)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal segment: %w", err)
	}

	resp, err := c.doRequest(ctx, http.MethodPost, "/adaptive-logs/segment", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create segment: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusAccepted {
		return nil, handleErrorResponse(resp)
	}

	var created LogSegment
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		return nil, fmt.Errorf("failed to decode create segment response: %w", err)
	}

	return &created, nil
}

// UpdateSegment updates an existing adaptive log segment by ID.
// The API uses a query parameter: PUT /adaptive-logs/segment?segment=<id>.
func (c *Client) UpdateSegment(ctx context.Context, id string, s *LogSegment) (*LogSegment, error) {
	body, err := json.Marshal(s)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal segment: %w", err)
	}

	resp, err := c.doRequest(ctx, http.MethodPut, "/adaptive-logs/segment?segment="+url.QueryEscape(id), bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to update segment %s: %w", id, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted {
		return nil, handleErrorResponse(resp)
	}

	var updated LogSegment
	if err := json.NewDecoder(resp.Body).Decode(&updated); err != nil {
		return nil, fmt.Errorf("failed to decode update segment response: %w", err)
	}

	return &updated, nil
}

// DeleteSegment deletes an adaptive log segment by ID.
// The API uses a query parameter: DELETE /adaptive-logs/segment?segment=<id>.
func (c *Client) DeleteSegment(ctx context.Context, id string) error {
	resp, err := c.doRequest(ctx, http.MethodDelete, "/adaptive-logs/segment?segment="+url.QueryEscape(id), nil)
	if err != nil {
		return fmt.Errorf("failed to delete segment %s: %w", id, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		return handleErrorResponse(resp)
	}

	return nil
}

// decodeDropRulesListBody parses the list endpoint body: a bare JSON array, or the same
// {"result": [...]} envelope as other adaptive logs endpoints; empty/whitespace bodies are
// treated as an empty list (some stacks return HTTP 200 with no body when there are no rules).
func decodeDropRulesListBody(body []byte) ([]DropRule, error) {
	body = bytes.TrimSpace(body)
	if len(body) == 0 {
		return []DropRule{}, nil
	}
	var rules []DropRule
	if err := json.Unmarshal(body, &rules); err == nil {
		if rules == nil {
			return []DropRule{}, nil
		}
		return rules, nil
	}
	var env struct {
		Result []DropRule `json:"result"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		return nil, err
	}
	if env.Result == nil {
		return []DropRule{}, nil
	}
	return env.Result, nil
}

// ListDropRules lists adaptive log drop rules.
// The API returns a bare JSON array (no wrapper). Optional filters are passed as query parameters.
func (c *Client) ListDropRules(ctx context.Context, q DropRuleListQuery) ([]DropRule, error) {
	vals := url.Values{}
	if q.SegmentID != "" {
		vals.Set("segment_id", q.SegmentID)
	}
	exp := q.ExpirationFilter
	if exp == "" {
		exp = "all"
	}
	vals.Set("expiration_filter", exp)

	query := "?" + vals.Encode()

	resp, err := c.doRequest(ctx, http.MethodGet, dropRulesPath+query, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to list drop rules: %w", err)
	}
	body, err := readDropRulesResponseBody(resp)
	if err != nil {
		return nil, fmt.Errorf("failed to read drop rules response: %w", err)
	}

	switch resp.StatusCode {
	case http.StatusOK:
		rules, err := decodeDropRulesListBody(body)
		if err != nil {
			return nil, fmt.Errorf("failed to decode drop rules response: %w", err)
		}
		return rules, nil
	case http.StatusNotFound:
		return nil, fmt.Errorf("adaptive logs drop rules: not found at %q (HTTP 404)", dropRulesPath)
	default:
		return nil, apiErrorFromResponseBody(resp.StatusCode, body)
	}
}

// GetDropRule returns a single drop rule by ID.
func (c *Client) GetDropRule(ctx context.Context, id string) (*DropRule, error) {
	pathID := "/" + url.PathEscape(id)

	resp, err := c.doRequest(ctx, http.MethodGet, dropRulesPath+pathID, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get drop rule %s: %w", id, err)
	}
	body, err := readDropRulesResponseBody(resp)
	if err != nil {
		return nil, fmt.Errorf("failed to read drop rule response: %w", err)
	}

	switch resp.StatusCode {
	case http.StatusOK:
		var rule DropRule
		if err := json.Unmarshal(body, &rule); err != nil {
			return nil, fmt.Errorf("failed to decode drop rule response: %w", err)
		}
		return &rule, nil
	case http.StatusNotFound:
		return nil, fmt.Errorf("no adaptive log drop rule with id %q (HTTP 404)", id)
	default:
		return nil, apiErrorFromResponseBody(resp.StatusCode, body)
	}
}

// CreateDropRule creates a new adaptive log drop rule.
func (c *Client) CreateDropRule(ctx context.Context, dr *DropRule) (*DropRule, error) {
	payload, err := dropRuleCreatePayload(dr)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal drop rule create payload: %w", err)
	}

	resp, err := c.doRequest(ctx, http.MethodPost, dropRulesPath, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("failed to create drop rule: %w", err)
	}
	body, err := readDropRulesResponseBody(resp)
	if err != nil {
		return nil, fmt.Errorf("failed to read create drop rule response: %w", err)
	}

	switch resp.StatusCode {
	case http.StatusOK, http.StatusCreated, http.StatusAccepted:
		var created DropRule
		if err := json.Unmarshal(body, &created); err != nil {
			return nil, fmt.Errorf("failed to decode create drop rule response: %w", err)
		}
		return &created, nil
	case http.StatusNotFound:
		return nil, fmt.Errorf("adaptive logs drop rules create: not found at %q (HTTP 404)", dropRulesPath)
	default:
		return nil, apiErrorFromResponseBody(resp.StatusCode, body)
	}
}

// UpdateDropRule updates an existing drop rule by ID.
// The API expects a DropRuleUpdate-shaped body only (not full DropRule).
func (c *Client) UpdateDropRule(ctx context.Context, id string, dr *DropRule) (*DropRule, error) {
	payload, err := dropRuleUpdatePayload(dr)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal drop rule update payload: %w", err)
	}

	pathID := "/" + url.PathEscape(id)

	resp, err := c.doRequest(ctx, http.MethodPut, dropRulesPath+pathID, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("failed to update drop rule %s: %w", id, err)
	}
	body, err := readDropRulesResponseBody(resp)
	if err != nil {
		return nil, fmt.Errorf("failed to read update drop rule response: %w", err)
	}

	switch resp.StatusCode {
	case http.StatusOK, http.StatusAccepted:
		var updated DropRule
		if err := json.Unmarshal(body, &updated); err != nil {
			return nil, fmt.Errorf("failed to decode update drop rule response: %w", err)
		}
		return &updated, nil
	case http.StatusNotFound:
		return nil, fmt.Errorf("no adaptive log drop rule with id %q (HTTP 404)", id)
	default:
		return nil, apiErrorFromResponseBody(resp.StatusCode, body)
	}
}

// DeleteDropRule deletes a drop rule by ID.
func (c *Client) DeleteDropRule(ctx context.Context, id string) error {
	pathID := "/" + url.PathEscape(id)

	resp, err := c.doRequest(ctx, http.MethodDelete, dropRulesPath+pathID, nil)
	if err != nil {
		return fmt.Errorf("failed to delete drop rule %s: %w", id, err)
	}
	body, err := readDropRulesResponseBody(resp)
	if err != nil {
		return fmt.Errorf("failed to read delete drop rule response: %w", err)
	}

	switch resp.StatusCode {
	case http.StatusOK, http.StatusNoContent:
		return nil
	case http.StatusNotFound:
		return fmt.Errorf("no adaptive log drop rule with id %q (HTTP 404)", id)
	default:
		return apiErrorFromResponseBody(resp.StatusCode, body)
	}
}

// decodeExemptionResponse decodes the Adaptive Logs API envelope
// {"result": ...} (webtools.APIResponse). Single-exemption endpoints use this shape;
// list uses {"result": [...]} and is decoded separately in ListExemptions.
func decodeExemptionResponse(r io.Reader) (*Exemption, error) {
	var env struct {
		Result *Exemption `json:"result"`
	}
	if err := json.NewDecoder(io.LimitReader(r, maxBodyReadBytes)).Decode(&env); err != nil {
		return nil, fmt.Errorf("failed to decode exemption response: %w", err)
	}
	if env.Result == nil {
		return nil, errors.New("exemption response missing result")
	}
	return env.Result, nil
}

// doRequest builds and executes an HTTP request against the Adaptive Logs API.
func (c *Client) doRequest(ctx context.Context, method, path string, body io.Reader) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.SetBasicAuth(strconv.Itoa(c.tenantID), c.apiToken)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to execute request: %w", err)
	}

	return resp, nil
}

// APIError is a typed error for non-OK HTTP responses from the Adaptive Logs API.
// It carries the status code and extracted message so that the fail package can
// render a meaningful summary instead of the generic "Unexpected error".
type APIError struct {
	StatusCode int
	Message    string
}

func (e *APIError) Error() string {
	if e.Message != "" {
		return fmt.Sprintf("API request failed (HTTP %d): %s", e.StatusCode, e.Message)
	}
	return fmt.Sprintf("API request failed (HTTP %d)", e.StatusCode)
}

// handleErrorResponse reads an error response body and returns an *APIError.
func handleErrorResponse(resp *http.Response) error {
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyReadBytes))
	if err != nil {
		return &APIError{StatusCode: resp.StatusCode, Message: fmt.Sprintf("could not read response body: %v", err)}
	}
	return apiErrorFromResponseBody(resp.StatusCode, body)
}

func apiErrorFromResponseBody(statusCode int, body []byte) error {
	var errResp struct {
		Error   string `json:"error"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(body, &errResp); err == nil {
		if errResp.Error != "" {
			return &APIError{StatusCode: statusCode, Message: errResp.Error}
		}
		if errResp.Message != "" {
			return &APIError{StatusCode: statusCode, Message: errResp.Message}
		}
	}

	if len(body) > 0 {
		return &APIError{StatusCode: statusCode, Message: adaptiveErrorBodySummary(statusCode, body)}
	}

	return &APIError{StatusCode: statusCode}
}

// adaptiveErrorBodySummary turns a non-JSON or unmapped JSON error body into a short, single-line message.
// Proxies often return HTML on 404; surfacing a prefix helps explain "Unexpected error" failures.
func adaptiveErrorBodySummary(statusCode int, body []byte) string {
	s := strings.TrimSpace(string(body))
	s = strings.ReplaceAll(s, "\r\n", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	if s == "" {
		if t := http.StatusText(statusCode); t != "" {
			return t
		}
		return "empty response body"
	}
	const max = 240
	if len(s) > max {
		return s[:max] + "..."
	}
	return s
}
