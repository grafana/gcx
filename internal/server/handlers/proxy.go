package handlers

import (
	"net/http"
	"net/http/httputil"

	"github.com/grafana/gcx/internal/resources"
)

// ResourceHandler describes a set of HTTP handlers that can be used to view or
// edit a specific resource type via a proxied UI.
type ResourceHandler interface {
	ResourceType() resources.Descriptor

	// Endpoints lists HTTP handlers to register on the proxy.
	Endpoints(proxy *httputil.ReverseProxy) []HTTPEndpoint

	// ProxyURL returns a URL path for a resource on the proxy
	ProxyURL(uid string) string

	// StaticEndpoints lists endpoints to be proxied transparently.
	StaticEndpoints() StaticProxyConfig
}

type HTTPEndpoint struct {
	Method  string
	URL     string
	Handler http.HandlerFunc
}

// StaticProxyConfig holds some static configuration to apply to the proxy.
// This allows resource handlers to declare routes to proxy or mock that are
// specific to them.
type StaticProxyConfig struct {
	// ProxyGet holds a list of routes to proxy when using the GET HTTP
	// method.
	// Example: /public/*
	ProxyGet []string

	// ProxyPost holds a list of routes to proxy when using the POST HTTP
	// method.
	// Example: /api/v1/eval
	ProxyPost []string

	// MockGet holds a map associating URLs to a mock response that they should
	// return for GET requests.
	// Note: the response is expected to be JSON.
	MockGet map[string]string

	// MockPost holds a map associating URLs to a mock response that they should
	// return for POST requests.
	// Note: the response is expected to be JSON.
	MockPost map[string]string
}
