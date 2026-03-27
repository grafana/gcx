package reports

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/grafana/gcx/internal/config"
	"k8s.io/client-go/rest"
)

// ErrNotFound is returned when a requested report does not exist (HTTP 404).
var ErrNotFound = errors.New("report not found")

const basePath = "/api/plugins/grafana-slo-app/resources/v1/report"

// Client is an HTTP client for the Grafana SLO Reports API.
type Client struct {
	restConfig config.NamespacedRESTConfig
	httpClient *http.Client
}

// NewClient creates a new SLO reports client.
func NewClient(cfg config.NamespacedRESTConfig) (*Client, error) {
	httpClient, err := rest.HTTPClientFor(&cfg.Config)
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP client: %w", err)
	}

	return &Client{
		restConfig: cfg,
		httpClient: httpClient,
	}, nil
}

// List returns all SLO reports.
func (c *Client) List(ctx context.Context) ([]Report, error) {
	resp, err := c.doRequest(ctx, http.MethodGet, basePath, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to list reports: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, handleErrorResponse(resp)
	}

	var listResp ReportListResponse
	if err := json.NewDecoder(resp.Body).Decode(&listResp); err != nil {
		return nil, fmt.Errorf("failed to decode report list response: %w", err)
	}

	if listResp.Reports == nil {
		return []Report{}, nil
	}

	return listResp.Reports, nil
}

// Get returns a single SLO report by UUID.
func (c *Client) Get(ctx context.Context, uuid string) (*Report, error) {
	resp, err := c.doRequest(ctx, http.MethodGet, basePath+"/"+uuid, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get report %s: %w", uuid, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, ErrNotFound
	}

	if resp.StatusCode != http.StatusOK {
		return nil, handleErrorResponse(resp)
	}

	var report Report
	if err := json.NewDecoder(resp.Body).Decode(&report); err != nil {
		return nil, fmt.Errorf("failed to decode report response: %w", err)
	}

	return &report, nil
}

// Create creates a new SLO report.
func (c *Client) Create(ctx context.Context, report *Report) (*ReportCreateResponse, error) {
	body, err := json.Marshal(report)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal report: %w", err)
	}

	resp, err := c.doRequest(ctx, http.MethodPost, basePath, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create report: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted {
		return nil, handleErrorResponse(resp)
	}

	var createResp ReportCreateResponse
	if err := json.NewDecoder(resp.Body).Decode(&createResp); err != nil {
		return nil, fmt.Errorf("failed to decode report create response: %w", err)
	}

	return &createResp, nil
}

// Update updates an existing SLO report.
func (c *Client) Update(ctx context.Context, uuid string, report *Report) error {
	body, err := json.Marshal(report)
	if err != nil {
		return fmt.Errorf("failed to marshal report: %w", err)
	}

	resp, err := c.doRequest(ctx, http.MethodPut, basePath+"/"+uuid, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to update report %s: %w", uuid, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted {
		return handleErrorResponse(resp)
	}

	return nil
}

// Delete deletes an SLO report by UUID.
func (c *Client) Delete(ctx context.Context, uuid string) error {
	resp, err := c.doRequest(ctx, http.MethodDelete, basePath+"/"+uuid, nil)
	if err != nil {
		return fmt.Errorf("failed to delete report %s: %w", uuid, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		return handleErrorResponse(resp)
	}

	return nil
}

// doRequest builds and executes an HTTP request against the Grafana SLO Reports API.
func (c *Client) doRequest(ctx context.Context, method, path string, body io.Reader) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.restConfig.Host+path, body)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

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

	var errResp ErrorResponse
	if err := json.Unmarshal(body, &errResp); err == nil && errResp.Error != "" {
		return fmt.Errorf("request failed with status %d: %s", resp.StatusCode, errResp.Error)
	}

	if len(body) > 0 {
		return fmt.Errorf("request failed with status %d: %s", resp.StatusCode, string(body))
	}

	return fmt.Errorf("request failed with status %d", resp.StatusCode)
}
