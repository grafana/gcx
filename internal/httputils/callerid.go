package httputils

import (
	"net/http"
)

const (
	// CallerIDHeader identifies the application that initiated a request so it
	// can be attributed end-to-end at upstream datasource gateways. Grafana
	// forwards it through its unified query API (/api/ds/query) via the
	// tracing-header allowlist, where the User-Agent is otherwise dropped.
	CallerIDHeader = "X-Grafana-Caller-Id"

	// CallerIDValue is gcx's identifier. It must match the value the gateway AI
	// agent middleware maps to the "gcx" attribution label.
	CallerIDValue = "gcx"
)

// CallerIDTransport injects the X-Grafana-Caller-Id header into every outgoing
// request so gcx-originated datasource queries can be attributed at upstream
// gateways even when they traverse Grafana's unified query API.
type CallerIDTransport struct {
	Base http.RoundTripper
}

func (t *CallerIDTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	base := t.Base
	if base == nil {
		base = http.DefaultTransport
	}

	clone := req.Clone(req.Context())
	clone.Header.Set(CallerIDHeader, CallerIDValue)

	return base.RoundTrip(clone)
}
