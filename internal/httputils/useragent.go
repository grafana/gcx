package httputils

import (
	"net/http"

	"github.com/grafana/gcx/internal/version"
)

// UserAgentTransport injects the gcx User-Agent header into every outgoing request.
type UserAgentTransport struct {
	Base http.RoundTripper
}

func (t *UserAgentTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	base := t.Base
	if base == nil {
		base = http.DefaultTransport
	}

	clone := req.Clone(req.Context())
	clone.Header.Set("User-Agent", version.UserAgent())

	return base.RoundTrip(clone)
}
