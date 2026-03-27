package logs

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
)

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

	var created Exemption
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		return nil, fmt.Errorf("failed to decode create exemption response: %w", err)
	}

	return &created, nil
}

// UpdateExemption updates an existing log stream exemption by ID.
//
//nolint:dupl // Structural similarity with UpdateSegment is expected for typed CRUD methods.
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

	var updated Exemption
	if err := json.NewDecoder(resp.Body).Decode(&updated); err != nil {
		return nil, fmt.Errorf("failed to decode update exemption response: %w", err)
	}

	return &updated, nil
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
//
//nolint:dupl // Structural similarity with UpdateExemption is expected for typed CRUD methods.
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

// handleErrorResponse reads an error response body and returns a formatted error.
func handleErrorResponse(resp *http.Response) error {
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("request failed with status %d (could not read body: %w)", resp.StatusCode, err)
	}

	var errResp struct {
		Error   string `json:"error"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(body, &errResp); err == nil {
		if errResp.Error != "" {
			return fmt.Errorf("request failed with status %d: %s", resp.StatusCode, errResp.Error)
		}
		if errResp.Message != "" {
			return fmt.Errorf("request failed with status %d: %s", resp.StatusCode, errResp.Message)
		}
	}

	if len(body) > 0 {
		return fmt.Errorf("request failed with status %d: %s", resp.StatusCode, string(body))
	}

	return fmt.Errorf("request failed with status %d", resp.StatusCode)
}
