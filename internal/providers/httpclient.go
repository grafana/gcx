package providers

import (
	"net/http"
	"sync"
	"time"
)

// externalClient is the shared HTTP client singleton for external API calls.
var (
	externalClient     *http.Client  //nolint:gochecknoglobals // Singleton pattern for connection pool reuse.
	externalClientOnce sync.Once     //nolint:gochecknoglobals // Singleton pattern for connection pool reuse.
)

// ExternalHTTPClient returns a shared, well-tuned http.Client for providers
// calling external (non-Grafana) APIs. It does NOT carry Grafana bearer tokens
// or any other auth — providers must set their own auth headers per request.
//
// The client is safe for concurrent use across all providers. Do not modify
// the returned client's fields or close its idle connections.
func ExternalHTTPClient() *http.Client {
	externalClientOnce.Do(func() {
		externalClient = &http.Client{
			Timeout: 60 * time.Second,
			Transport: &http.Transport{
				MaxIdleConns:        100,
				MaxIdleConnsPerHost: 10,
				IdleConnTimeout:     90 * time.Second,
			},
		}
	})
	return externalClient
}
