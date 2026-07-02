package providers

import (
	"fmt"
	"net/http"

	"github.com/grafana/gcx/internal/config"
	"k8s.io/client-go/rest"
)

// NewHTTPClient creates a TLS/auth-aware HTTP client from a namespaced REST
// config. This is the boilerplate shared by provider HTTP clients: call
// rest.HTTPClientFor and wrap any error with context. Provider packages
// should use this instead of hand-rolling the same rest.HTTPClientFor call.
func NewHTTPClient(cfg config.NamespacedRESTConfig) (*http.Client, error) {
	httpClient, err := rest.HTTPClientFor(&cfg.Config)
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP client: %w", err)
	}
	return httpClient, nil
}
