package publicdashboards

import (
	"context"
	"fmt"
	"net/http"
	"net/url"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/coreapi"
)

const listPath = "/api/dashboards/public-dashboards"

// Client is a Grafana Public Dashboards API client.
type Client struct {
	base *coreapi.Client
}

// NewClient creates a new Public Dashboards client bound to the provided REST config.
func NewClient(cfg config.NamespacedRESTConfig) (*Client, error) {
	base, err := coreapi.NewClient(cfg)
	if err != nil {
		return nil, err
	}
	return &Client{base: base}, nil
}

func dashboardPath(dashboardUID string) string {
	return fmt.Sprintf("/api/dashboards/uid/%s/public-dashboards", url.PathEscape(dashboardUID))
}

func dashboardItemPath(dashboardUID, pdUID string) string {
	return fmt.Sprintf("/api/dashboards/uid/%s/public-dashboards/%s", url.PathEscape(dashboardUID), url.PathEscape(pdUID))
}

// List returns all public dashboards in the stack.
func (c *Client) List(ctx context.Context) ([]PublicDashboard, error) {
	wrapper, err := coreapi.DoJSON[any, listResp](ctx, c.base, http.MethodGet, listPath, nil, http.StatusOK)
	if err != nil {
		return nil, err
	}
	return wrapper.PublicDashboards, nil
}

// Get returns the public dashboard config for the given parent dashboard UID.
func (c *Client) Get(ctx context.Context, dashboardUID string) (*PublicDashboard, error) {
	pd, err := coreapi.DoJSON[any, PublicDashboard](ctx, c.base, http.MethodGet, dashboardPath(dashboardUID), nil, http.StatusOK)
	if err != nil {
		return nil, err
	}
	return &pd, nil
}

// Create creates a new public dashboard config for the given parent dashboard UID.
func (c *Client) Create(ctx context.Context, dashboardUID string, pd *PublicDashboard) (*PublicDashboard, error) {
	created, err := coreapi.DoJSON[PublicDashboard, PublicDashboard](ctx, c.base, http.MethodPost, dashboardPath(dashboardUID), pd, http.StatusOK)
	if err != nil {
		return nil, err
	}
	return &created, nil
}

// Update patches an existing public dashboard config.
func (c *Client) Update(ctx context.Context, dashboardUID, pdUID string, pd *PublicDashboard) (*PublicDashboard, error) {
	updated, err := coreapi.DoJSON[PublicDashboard, PublicDashboard](ctx, c.base, http.MethodPatch, dashboardItemPath(dashboardUID, pdUID), pd, http.StatusOK)
	if err != nil {
		return nil, err
	}
	return &updated, nil
}

// Delete removes a public dashboard config.
func (c *Client) Delete(ctx context.Context, dashboardUID, pdUID string) error {
	return coreapi.DoStatus[any](ctx, c.base, http.MethodDelete, dashboardItemPath(dashboardUID, pdUID), nil, http.StatusOK)
}
