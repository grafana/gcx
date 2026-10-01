package httputils

import "net/http"

// HeaderTransport sets fixed headers on every outgoing request, overriding
// any value already present.
type HeaderTransport struct {
	Base    http.RoundTripper
	Headers map[string]string
}

func (t *HeaderTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	base := t.Base
	if base == nil {
		base = http.DefaultTransport
	}
	if len(t.Headers) == 0 {
		return base.RoundTrip(req)
	}

	clone := req.Clone(req.Context())
	for k, v := range t.Headers {
		clone.Header.Set(k, v)
	}

	return base.RoundTrip(clone)
}
