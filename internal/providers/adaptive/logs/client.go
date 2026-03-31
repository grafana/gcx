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

	var exemption Exemption
	if err := json.NewDecoder(resp.Body).Decode(&exemption); err != nil {
		return nil, fmt.Errorf("failed to decode exemption response: %w", err)
	}

	return &exemption, nil
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

// ApplyRecommendations replaces the full set of adaptive log recommendations.
func (c *Client) ApplyRecommendations(ctx context.Context, recs []LogRecommendation) error {
	body, err := json.Marshal(recs)
	if err != nil {
		return fmt.Errorf("failed to marshal recommendations: %w", err)
	}

	resp, err := c.doRequest(ctx, http.MethodPost, "/adaptive-logs/recommendations", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to apply recommendations: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted && resp.StatusCode != http.StatusNoContent {
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
