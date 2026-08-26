package httputils

import (
	"net/http"
)

// HeaderTransport stamps a fixed set of headers onto every outgoing request.
// Used to carry identity-aware proxy credentials (e.g. an AWS ALB session
// cookie) that sit outside the Grafana auth layer and must reach the edge
// proxy before any auth token is evaluated.
type HeaderTransport struct {
	Base    http.RoundTripper
	Headers map[string]string
}

func (t *HeaderTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	base := t.Base
	if base == nil {
		base = http.DefaultTransport
	}

	clone := req.Clone(req.Context())
	for key, value := range t.Headers {
		clone.Header.Set(key, value)
	}

	return base.RoundTrip(clone)
}
