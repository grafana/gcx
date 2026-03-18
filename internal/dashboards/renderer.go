package dashboards

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"

	"github.com/grafana/grafanactl/internal/config"
	"k8s.io/client-go/rest"
)

// RenderRequest holds parameters for a dashboard render request.
type RenderRequest struct {
	// UID is the dashboard UID. Required.
	UID string

	// PanelID, if non-zero, renders a single panel via /render/d-solo/.
	PanelID int

	// OrgID is the Grafana organization ID. Defaults to 1.
	OrgID int

	// Width and Height of the rendered image in pixels.
	Width  int
	Height int

	// Theme is "light" or "dark".
	Theme string

	// From and To define the time range (RFC3339, Unix timestamp, or relative like "now-1h").
	From string
	To   string

	// Tz is the timezone string (e.g. "UTC", "America/New_York").
	Tz string

	// Vars holds dashboard template variable overrides (key → value).
	// Each entry is sent as var-{key}={value} on the render URL.
	Vars map[string]string
}

// Client is an HTTP client for Grafana's image renderer API.
type Client struct {
	restConfig config.NamespacedRESTConfig
	httpClient *http.Client
}

// NewClient creates a new renderer client using auth from the active grafanactl context.
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

// Render performs a GET request to the Grafana image renderer and returns the raw PNG bytes.
func (c *Client) Render(ctx context.Context, req RenderRequest) ([]byte, error) {
	renderURL, err := c.buildRenderURL(req)
	if err != nil {
		return nil, fmt.Errorf("failed to build render URL: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, renderURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to execute render request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 50*1024*1024))
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		excerpt := string(body)
		if len(excerpt) > 256 {
			excerpt = excerpt[:256]
		}
		return nil, fmt.Errorf("render request failed with status %d: %s", resp.StatusCode, excerpt)
	}

	if len(body) == 0 {
		return nil, errors.New("render response body is empty")
	}

	return body, nil
}

func (c *Client) buildRenderURL(req RenderRequest) (string, error) {
	var path string
	if req.PanelID != 0 {
		path = fmt.Sprintf("/render/d-solo/%s/", url.PathEscape(req.UID))
	} else {
		path = fmt.Sprintf("/render/d/%s/", url.PathEscape(req.UID))
	}

	u, err := url.Parse(c.restConfig.Host + path)
	if err != nil {
		return "", err
	}

	q := u.Query()

	orgID := req.OrgID
	if orgID == 0 {
		orgID = 1
	}
	q.Set("orgId", strconv.Itoa(orgID))

	if req.PanelID != 0 {
		q.Set("panelId", strconv.Itoa(req.PanelID))
	}

	if req.Width != 0 {
		q.Set("width", strconv.Itoa(req.Width))
	}
	if req.Height != 0 {
		q.Set("height", strconv.Itoa(req.Height))
	}
	if req.From != "" {
		q.Set("from", req.From)
	}
	if req.To != "" {
		q.Set("to", req.To)
	}
	if req.Tz != "" {
		q.Set("tz", req.Tz)
	}
	if req.Theme != "" {
		q.Set("theme", req.Theme)
	}

	// Kiosk mode removes sidebar, nav bar, and other UI chrome so the
	// rendered image contains only dashboard content.
	q.Set("kiosk", "true")
	q.Set("hideNav", "true")
	q.Set("fullPageImage", "true")

	for k, v := range req.Vars {
		q.Set("var-"+k, v)
	}

	u.RawQuery = q.Encode()
	return u.String(), nil
}
