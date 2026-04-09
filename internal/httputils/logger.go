package httputils

import (
	"net/http"
	"net/http/httputil"

	"github.com/grafana/grafana-app-sdk/logging"
)

// RequestResponseLoggingRoundTripper logs full HTTP request and response bodies at Debug level
// via httputil.DumpRequest / httputil.DumpResponse (includes headers — may expose tokens).
// Enabled when --log-http-payload is set.
type RequestResponseLoggingRoundTripper struct {
	DecoratedTransport http.RoundTripper
}

func (rt RequestResponseLoggingRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	transport := http.DefaultTransport
	if rt.DecoratedTransport != nil {
		transport = rt.DecoratedTransport
	}

	reqStr, _ := httputil.DumpRequest(req, true)
	logging.FromContext(req.Context()).Debug(string(reqStr))

	resp, err := transport.RoundTrip(req)
	if err != nil {
		return resp, err
	}

	respStr, _ := httputil.DumpResponse(resp, true)
	logging.FromContext(req.Context()).Debug(string(respStr))

	return resp, err
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
