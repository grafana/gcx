package alert

import (
	"context"
	"fmt"
	"net/http"

	alertclient "github.com/grafana/gcx/client/alert"
	"github.com/grafana/gcx/internal/config"
	"k8s.io/client-go/rest"
)

// ErrNotFound is returned when a requested alert rule or group does not exist.
var ErrNotFound = alertclient.ErrNotFound

// ListOptions configures filtering for List operations.
type ListOptions = alertclient.ListOptions

// Client fetches alert rules and groups from the Prometheus-compatible API,
// and notification history from the alerting historian API. The rules methods
// delegate to the public client/alert package; the remaining method groups in
// this package still talk to their APIs directly.
type Client struct {
	httpClient *http.Client
	host       string
	namespace  string
	rules      *alertclient.Client
}

// NewClient creates a new alert client.
func NewClient(cfg config.NamespacedRESTConfig) (*Client, error) {
	httpClient, err := rest.HTTPClientFor(&cfg.Config)
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP client: %w", err)
	}
	return &Client{
		httpClient: httpClient,
		host:       cfg.Host,
		namespace:  cfg.Namespace,
		rules:      alertclient.NewClient(httpClient, cfg.Host),
	}, nil
}

// List returns rules matching the given options.
func (c *Client) List(ctx context.Context, opts ListOptions) (*RulesResponse, error) {
	return c.rules.List(ctx, opts)
}

// GetRule returns a single rule by UID.
func (c *Client) GetRule(ctx context.Context, uid string) (*RuleStatus, error) {
	return c.rules.GetRule(ctx, uid)
}

// ListGroups returns all groups.
func (c *Client) ListGroups(ctx context.Context) ([]RuleGroup, error) {
	return c.rules.ListGroups(ctx)
}

// GetGroup returns a single group by name with all its rules.
func (c *Client) GetGroup(ctx context.Context, name string) (*RuleGroup, error) {
	return c.rules.GetGroup(ctx, name)
}
