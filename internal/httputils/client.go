package httputils

import (
	"context"
	"crypto/tls"
	"net/http"
	"time"

	"github.com/grafana/gcx/internal/retry"
)

// Middleware wraps an http.RoundTripper, e.g. for logging or tracing.
type Middleware func(http.RoundTripper) http.RoundTripper

// LoggingMiddleware wraps a transport with LoggingRoundTripper (method, URL, status).
func LoggingMiddleware(rt http.RoundTripper) http.RoundTripper {
	return &LoggingRoundTripper{Base: rt}
}

// RequestResponseLoggingMiddleware wraps a transport with RequestResponseLoggingRoundTripper
// (full request/response body dump via httputil.DumpRequest/DumpResponse).
func RequestResponseLoggingMiddleware(rt http.RoundTripper) http.RoundTripper {
	return &RequestResponseLoggingRoundTripper{DecoratedTransport: rt}
}

// ClientOpts configures NewClient. See NewDefaultClient for common usage.
type ClientOpts struct {
	TLSConfig   *tls.Config
	Timeout     time.Duration // default: 60s
	Middlewares []Middleware  // default: []Middleware{LoggingMiddleware}
}

// NewClient returns a configured *http.Client.
// Middlewares are applied in order, wrapping the base transport.
func NewClient(opts ClientOpts) *http.Client {
	timeout := opts.Timeout
	if timeout == 0 {
		timeout = 60 * time.Second
	}
	middlewares := opts.Middlewares
	if middlewares == nil {
		middlewares = []Middleware{LoggingMiddleware}
	}

	var rt http.RoundTripper = NewTransport(opts.TLSConfig)
	for _, mw := range middlewares {
		rt = mw(rt)
	}
	// Outermost layers: User-Agent injection, then retry for rate limiting (429) and transient errors.
	rt = &retry.Transport{Base: rt}
	rt = &UserAgentTransport{Base: rt}
	return &http.Client{Timeout: timeout, Transport: rt}
}

// NewDefaultClient returns an *http.Client with LoggingRoundTripper, 60s timeout,
// and default TLS settings. It does NOT carry Grafana bearer tokens — callers
// must set auth headers per request.
//
// Reads context for configuration:
//   - PayloadLogging(ctx): when true, adds RequestResponseLoggingMiddleware for full
//     request/response body dumps (includes headers — may expose tokens).
func NewDefaultClient(ctx context.Context) *http.Client {
	return NewDefaultClientWithTLS(ctx, nil)
}

// NewDefaultClientWithTLS is like NewDefaultClient but accepts an optional
// *tls.Config for mTLS or custom CA scenarios. When tlsConfig is nil it
// behaves identically to NewDefaultClient.
func NewDefaultClientWithTLS(ctx context.Context, tlsConfig *tls.Config) *http.Client {
	opts := ClientOpts{TLSConfig: tlsConfig}
	if PayloadLogging(ctx) {
		opts.Middlewares = []Middleware{LoggingMiddleware, RequestResponseLoggingMiddleware}
	}
	return NewClient(opts)
}

// NewTransport returns an *http.Transport with sensible defaults.
// If tlsConfig is nil, a default TLS config (TLS 1.2+, verify enabled) is used.
func NewTransport(tlsConfig *tls.Config) *http.Transport {
	if tlsConfig == nil {
		tlsConfig = &tls.Config{InsecureSkipVerify: false, MinVersion: tls.VersionTLS12}
	}
	return &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   10,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		TLSClientConfig:       tlsConfig,
	}
}
