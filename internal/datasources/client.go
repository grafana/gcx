package datasources

import (
	"fmt"

	dsclient "github.com/grafana/gcx/client/datasources"
	"github.com/grafana/gcx/internal/config"
	"k8s.io/client-go/rest"
)

const maxResponseBytes = 10 << 20 // 10 MB

// The datasource REST API client and its wire types live in the public
// client/datasources package. The CLI uses them directly rather than keeping a
// second copy.
type (
	// Client talks to the legacy /api/datasources REST API. It implements Transport.
	Client = dsclient.Client
	// Datasource is the datasource domain type exchanged with /api/datasources.
	Datasource = dsclient.Datasource
	// HealthResult is the outcome of a datasource health check.
	HealthResult = dsclient.HealthResult
	// PluginType is an installed datasource plugin type.
	PluginType = dsclient.PluginType
	// APIError is a typed error returned by the Grafana datasource REST API.
	APIError = dsclient.APIError
)

// NewAPIError builds an APIError from a non-2xx datasource API response.
func NewAPIError(operation, identifier string, statusCode int, body []byte) *APIError {
	return dsclient.NewAPIError(operation, identifier, statusCode, body)
}

// IsNotFound reports whether err is a datasource not-found (HTTP 404) error.
func IsNotFound(err error) bool {
	return dsclient.IsNotFound(err)
}

// NewClient creates a client backed by the given REST config's transport, so
// OAuth proxy mode and token refresh are respected.
func NewClient(cfg config.NamespacedRESTConfig) (*Client, error) {
	httpClient, err := rest.HTTPClientFor(&cfg.Config)
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP client: %w", err)
	}
	return dsclient.NewClient(httpClient, cfg.Host), nil
}
