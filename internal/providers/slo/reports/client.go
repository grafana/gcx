package reports

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/resources/adapter"
)

// ErrNotFound is returned when a requested report does not exist (HTTP 404).
var ErrNotFound = fmt.Errorf("report %w", adapter.ErrNotFound)

const (
	basePath        = "/api/plugins/grafana-slo-app/resources/v1/report"
	reportByUUIDFmt = basePath + "/%s"
)

// Client is an HTTP client for the Grafana SLO Reports API.
type Client struct {
	restConfig config.NamespacedRESTConfig
	httpClient *http.Client
}

// List returns all SLO reports.
func (c *Client) List(ctx context.Context, opts adapter.ListOptions) ([]Report, error) {
	resp, err := c.doRequest(ctx, http.MethodGet, basePath, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to list reports: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, providers.HandleErrorResponse(resp)
	}

	var listResp ReportListResponse
	if err := json.NewDecoder(resp.Body).Decode(&listResp); err != nil {
		return nil, fmt.Errorf("failed to decode report list response: %w", err)
	}

	if listResp.Reports == nil {
		return []Report{}, nil
	}

	return adapter.TruncateSlice(listResp.Reports, opts.Limit), nil
}

// Get returns a single SLO report by UUID.
func (c *Client) Get(ctx context.Context, uuid string) (*Report, error) {
	resp, err := c.doRequest(ctx, http.MethodGet, fmt.Sprintf(reportByUUIDFmt, uuid), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get report %s: %w", uuid, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, ErrNotFound
	}

	if resp.StatusCode != http.StatusOK {
		return nil, providers.HandleErrorResponse(resp)
	}

	var report Report
	if err := json.NewDecoder(resp.Body).Decode(&report); err != nil {
		return nil, fmt.Errorf("failed to decode report response: %w", err)
	}

	return &report, nil
}

// Create creates a new SLO report.
func (c *Client) Create(ctx context.Context, report *Report) (*Report, error) {
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
		return nil, providers.HandleErrorResponse(resp)
	}

	var createResp ReportCreateResponse
	if err := json.NewDecoder(resp.Body).Decode(&createResp); err != nil {
		return nil, fmt.Errorf("failed to decode report create response: %w", err)
	}

	// The API can return 202; preserve the accepted request without requiring
	// immediate read-after-write consistency.
	created := *report
	created.UUID = createResp.UUID
	return &created, nil
}

// Update updates an existing SLO report.
func (c *Client) Update(ctx context.Context, uuid string, report *Report) (*Report, error) {
	body, err := json.Marshal(report)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal report: %w", err)
	}

	resp, err := c.doRequest(ctx, http.MethodPut, fmt.Sprintf(reportByUUIDFmt, uuid), bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to update report %s: %w", uuid, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted {
		return nil, providers.HandleErrorResponse(resp)
	}

	updated := *report
	updated.UUID = uuid
	return &updated, nil
}

// Delete deletes an SLO report by UUID.
func (c *Client) Delete(ctx context.Context, uuid string) error {
	resp, err := c.doRequest(ctx, http.MethodDelete, fmt.Sprintf(reportByUUIDFmt, uuid), nil)
	if err != nil {
		return fmt.Errorf("failed to delete report %s: %w", uuid, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		return providers.HandleErrorResponse(resp)
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

// Report clients expose only the capabilities their API supports.
var (
	_ adapter.Lister[Report]  = (*Client)(nil)
	_ adapter.Getter[Report]  = (*Client)(nil)
	_ adapter.Creator[Report] = (*Client)(nil)
	_ adapter.Updater[Report] = (*Client)(nil)
	_ adapter.Deleter[Report] = (*Client)(nil)
)
