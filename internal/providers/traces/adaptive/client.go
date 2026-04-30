package traces

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// Client is an HTTP client for the Adaptive Traces API.
type Client struct {
	baseURL    string
	tenantID   int
	apiToken   string
	httpClient *http.Client
}

// NewClient creates a new Adaptive Traces client.
func NewClient(baseURL string, tenantID int, apiToken string, httpClient *http.Client) *Client {
	return &Client{
		baseURL:    strings.TrimRight(baseURL, "/"),
		tenantID:   tenantID,
		apiToken:   apiToken,
		httpClient: httpClient,
	}
}

// ListPolicies returns all sampling policies.
func (c *Client) ListPolicies(ctx context.Context) ([]Policy, error) {
	resp, err := c.doRequest(ctx, http.MethodGet, "/adaptive-traces/api/v1/policies", nil)
	if err != nil {
		return nil, fmt.Errorf("listing policies: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("adaptive-traces: list policies: %w", handleErrorResponse(resp))
	}

	var policies []Policy
	if err := json.NewDecoder(resp.Body).Decode(&policies); err != nil {
		return nil, fmt.Errorf("decoding policy list: %w", err)
	}

	if policies == nil {
		return []Policy{}, nil
	}

	return policies, nil
}

// GetPolicy returns a single policy by ID.
func (c *Client) GetPolicy(ctx context.Context, id string) (*Policy, error) {
	resp, err := c.doRequest(ctx, http.MethodGet, "/adaptive-traces/api/v1/policies/"+url.PathEscape(id), nil)
	if err != nil {
		return nil, fmt.Errorf("getting policy %q: %w", id, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("adaptive-traces: get policy: %w", handleErrorResponse(resp))
	}

	var policy Policy
	if err := json.NewDecoder(resp.Body).Decode(&policy); err != nil {
		return nil, fmt.Errorf("decoding policy: %w", err)
	}

	return &policy, nil
}

// CreatePolicy creates a new sampling policy.
func (c *Client) CreatePolicy(ctx context.Context, p *Policy) (*Policy, error) {
	body, err := json.Marshal(p)
	if err != nil {
		return nil, fmt.Errorf("marshalling policy: %w", err)
	}

	resp, err := c.doRequest(ctx, http.MethodPost, "/adaptive-traces/api/v1/policies", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("creating policy: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return nil, fmt.Errorf("adaptive-traces: create policy: %w", handleErrorResponse(resp))
	}

	var created Policy
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		return nil, fmt.Errorf("decoding created policy: %w", err)
	}

	return &created, nil
}

// UpdatePolicy updates an existing sampling policy by ID.
func (c *Client) UpdatePolicy(ctx context.Context, id string, p *Policy) (*Policy, error) {
	body, err := json.Marshal(p)
	if err != nil {
		return nil, fmt.Errorf("marshalling policy: %w", err)
	}

	resp, err := c.doRequest(ctx, http.MethodPut, "/adaptive-traces/api/v1/policies/"+url.PathEscape(id), bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("updating policy %q: %w", id, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("adaptive-traces: update policy: %w", handleErrorResponse(resp))
	}

	var updated Policy
	if err := json.NewDecoder(resp.Body).Decode(&updated); err != nil {
		return nil, fmt.Errorf("decoding updated policy: %w", err)
	}

	return &updated, nil
}

// DeletePolicy deletes a sampling policy by ID.
func (c *Client) DeletePolicy(ctx context.Context, id string) error {
	resp, err := c.doRequest(ctx, http.MethodDelete, "/adaptive-traces/api/v1/policies/"+url.PathEscape(id), nil)
	if err != nil {
		return fmt.Errorf("deleting policy %q: %w", id, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("adaptive-traces: delete policy: %w", handleErrorResponse(resp))
	}

	return nil
}

// ListRecommendations returns all sampling recommendations.
func (c *Client) ListRecommendations(ctx context.Context) ([]Recommendation, error) {
	resp, err := c.doRequest(ctx, http.MethodGet, "/adaptive-traces/api/v1/recommendations", nil)
	if err != nil {
		return nil, fmt.Errorf("listing recommendations: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("adaptive-traces: list recommendations: %w", handleErrorResponse(resp))
	}

	var recs []Recommendation
	if err := json.NewDecoder(resp.Body).Decode(&recs); err != nil {
		return nil, fmt.Errorf("decoding recommendation list: %w", err)
	}

	if recs == nil {
		return []Recommendation{}, nil
	}

	return recs, nil
}

// ApplyRecommendation applies a recommendation by ID.
func (c *Client) ApplyRecommendation(ctx context.Context, id string) error {
	resp, err := c.doRequest(ctx, http.MethodPost, "/adaptive-traces/api/v1/recommendations/"+url.PathEscape(id)+"/apply", nil)
	if err != nil {
		return fmt.Errorf("applying recommendation %q: %w", id, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("adaptive-traces: apply recommendation: %w", handleErrorResponse(resp))
	}

	return nil
}

// DismissRecommendation dismisses a recommendation by ID.
func (c *Client) DismissRecommendation(ctx context.Context, id string) error {
	resp, err := c.doRequest(ctx, http.MethodPost, "/adaptive-traces/api/v1/recommendations/"+url.PathEscape(id)+"/dismiss", nil)
	if err != nil {
		return fmt.Errorf("dismissing recommendation %q: %w", id, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("adaptive-traces: dismiss recommendation: %w", handleErrorResponse(resp))
	}

	return nil
}

func (c *Client) doRequest(ctx context.Context, method, path string, body io.Reader) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	req.SetBasicAuth(strconv.Itoa(c.tenantID), c.apiToken)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("executing request: %w", err)
	}

	return resp, nil
}

// handleErrorResponse reads the error response body and returns an error.
func handleErrorResponse(resp *http.Response) error {
	body, _ := io.ReadAll(resp.Body)
	return fmt.Errorf("unexpected status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
}
