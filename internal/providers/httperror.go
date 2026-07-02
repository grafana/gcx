package providers

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// maxErrorBodyBytes caps how much of a non-2xx response body ParseErrorBody
// reads into memory. Error bodies are expected to be a short JSON message or
// a short text blob; this guards against an unbounded read from a
// misbehaving proxy or server.
const maxErrorBodyBytes = 1 << 20 // 1 MiB

// ErrorResponse is the common JSON error-body shape returned by Grafana Cloud
// product plugin APIs. Different APIs use different field names for the
// human-readable message ("error", "message", or "msg"); all three are
// captured here so ParseErrorBody/ParseErrorBytes can extract whichever is
// present.
type ErrorResponse struct {
	Error   string `json:"error"`
	Message string `json:"message"`
	Msg     string `json:"msg"`
}

// message returns the first populated field, preferring Error, then Message,
// then Msg.
func (e ErrorResponse) message() string {
	switch {
	case e.Error != "":
		return e.Error
	case e.Message != "":
		return e.Message
	case e.Msg != "":
		return e.Msg
	default:
		return ""
	}
}

// ParseErrorBody reads a non-2xx HTTP response body and returns a descriptive
// error: the extracted JSON error message when the body unmarshals into
// ErrorResponse, otherwise the raw body text, otherwise a status-only
// message. It does not close resp.Body; callers remain responsible for that.
func ParseErrorBody(resp *http.Response) error {
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxErrorBodyBytes))
	if err != nil {
		return fmt.Errorf("request failed with status %d (could not read body: %w)", resp.StatusCode, err)
	}
	return ParseErrorBytes(resp.StatusCode, body)
}

// ParseErrorBytes builds a descriptive error from an already-read non-2xx
// status code and body. Used by clients that read a proxied response into
// memory as []byte before this point (e.g. dual-mode datasource-proxy
// transports), so there is no *http.Response to read from directly.
func ParseErrorBytes(statusCode int, body []byte) error {
	var errResp ErrorResponse
	if err := json.Unmarshal(body, &errResp); err == nil {
		if msg := errResp.message(); msg != "" {
			return fmt.Errorf("request failed with status %d: %s", statusCode, msg)
		}
	}

	if len(body) > 0 {
		return fmt.Errorf("request failed with status %d: %s", statusCode, string(body))
	}

	return fmt.Errorf("request failed with status %d", statusCode)
}
