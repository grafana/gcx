package httputils

import (
	"net/http"
	"net/http/httputil"

	"github.com/grafana/grafana-app-sdk/logging"
)

// Log messages of the payload dumps. Users search the debug log for these,
// because a wire dump holds no word that identifies it.
const (
	requestDumpMessage  = "http request dump"
	responseDumpMessage = "http response dump"
)

// RequestResponseLoggingRoundTripper logs full HTTP request and response bodies at Debug level
// via httputil.DumpRequestOut / httputil.DumpResponse (includes headers — may expose tokens).
// Enabled when --insecure-log-http-payload is set.
//
// It is the innermost transport layer, so the dump shows every header that an
// outer layer adds (bearer token, caller id, user agent).
type RequestResponseLoggingRoundTripper struct {
	DecoratedTransport http.RoundTripper
}

func (rt RequestResponseLoggingRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	transport := http.DefaultTransport
	if rt.DecoratedTransport != nil {
		transport = rt.DecoratedTransport
	}

	logger := logging.FromContext(req.Context())

	// DumpRequestOut is the dump call for an outgoing request: it returns the
	// exact wire bytes, including Content-Length and Accept-Encoding. It needs
	// an http or https scheme, so fall back to DumpRequest for other schemes.
	reqStr, err := httputil.DumpRequestOut(req, true)
	if err != nil {
		reqStr, err = httputil.DumpRequest(req, true)
	}
	if err != nil {
		logger.Warn("cannot dump http request", "err", err)
	} else {
		logger.Debug(requestDumpMessage + "\n" + string(reqStr))
	}

	resp, err := transport.RoundTrip(req)
	if err != nil {
		// The round trip failed, so there is no response to dump. The error
		// reaches the log through LoggingRoundTripper.
		return resp, err
	}

	respStr, err := httputil.DumpResponse(resp, true)
	if err != nil {
		logger.Warn("cannot dump http response", "err", err)
	} else {
		logger.Debug(responseDumpMessage + "\n" + string(respStr))
	}

	return resp, nil
}

// LoggingRoundTripper logs HTTP method, URL, and response status at appropriate levels.
//
// Successful responses (2xx/3xx) and client errors (4xx) are logged at Debug,
// visible with -vvv. Server errors (5xx) and transport failures are logged at
// Warn, visible with -v.
type LoggingRoundTripper struct {
	Base http.RoundTripper
}

func (t *LoggingRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	logger := logging.FromContext(req.Context())
	logger.Debug("http request", "method", req.Method, "url", req.URL.String())

	resp, err := t.Base.RoundTrip(req)
	if err != nil {
		logger.Warn("http error", "method", req.Method, "url", req.URL.String(), "error", err)
		return nil, err
	}

	if resp.StatusCode >= 500 {
		logger.Warn("http response", "method", req.Method, "url", req.URL.String(), "status", resp.StatusCode)
	} else {
		logger.Debug("http response", "method", req.Method, "url", req.URL.String(), "status", resp.StatusCode)
	}
	return resp, nil
}
