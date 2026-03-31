package probes

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/providers/synth/smcfg"
)

// Client is an HTTP client for the Synthetic Monitoring probes API.
type Client struct {
	baseURL    string
	token      string
	httpClient *http.Client
}

// NewClient creates a new SM probes client.
// baseURL is the SM service root (e.g. "https://synthetic-monitoring-api.grafana.net").
func NewClient(baseURL, token string) *Client {
	return &Client{
		baseURL:    strings.TrimRight(baseURL, "/") + "/api/v1",
		token:      token,
		httpClient: providers.ExternalHTTPClient(),
	}
}

// List returns all probes visible to the authenticated tenant.
func (c *Client) List(ctx context.Context) ([]Probe, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/probe/list", nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.token)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("executing request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, smcfg.HandleErrorResponse(resp)
	}

	var probeList []Probe
	if err := json.NewDecoder(resp.Body).Decode(&probeList); err != nil {
		return nil, fmt.Errorf("decoding probe list: %w", err)
	}

	if probeList == nil {
		return []Probe{}, nil
	}

	return probeList, nil
}
