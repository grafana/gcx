package providers

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// maxErrorBodyBytes caps how much of a non-2xx response body HandleErrorResponse
// reads, guarding against an unbounded read from a misbehaving proxy or server.
const maxErrorBodyBytes = 1 << 20 // 1 MiB

// ErrorResponse is the common JSON error-body shape returned by Grafana Cloud
// product plugin APIs. They disagree on the field name for the human-readable
// message, so all three variants are captured and read in preference order.
// TraceID, when present, is surfaced for supportability.
type ErrorResponse struct {
	Error   string `json:"error"`
	Message string `json:"message"`
	Msg     string `json:"msg"`
	TraceID string `json:"traceID"`
}

// message returns the first populated field: Error, then Message, then Msg.
func (e ErrorResponse) message() string {
	for _, m := range []string{e.Error, e.Message, e.Msg} {
		if m != "" {
			return m
		}
	}
	return ""
}

// HandleErrorResponse reads a non-2xx HTTP response body and returns a
// descriptive error via FormatError. It does not close resp.Body; callers
// remain responsible for that.
func HandleErrorResponse(resp *http.Response) error {
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxErrorBodyBytes))
	if err != nil {
		return fmt.Errorf("request failed with status %d (could not read body: %w)", resp.StatusCode, err)
	}
	return FormatError(resp.StatusCode, body)
}

// FormatError builds a descriptive error from an already-read non-2xx status
// code and body: the extracted JSON error message when the body unmarshals into
// ErrorResponse, otherwise the raw body text, otherwise a status-only message.
// Used by clients that read a proxied response into memory as []byte before this
// point (e.g. dual-mode datasource-proxy transports).
func FormatError(statusCode int, body []byte) error {
	var errResp ErrorResponse
	if err := json.Unmarshal(body, &errResp); err == nil {
		if msg := errResp.message(); msg != "" {
			if errResp.TraceID != "" {
				return fmt.Errorf("request failed with status %d: %s (traceID %s)", statusCode, msg, errResp.TraceID)
			}
			return fmt.Errorf("request failed with status %d: %s", statusCode, msg)
		}
	}

	if len(body) > 0 {
		return fmt.Errorf("request failed with status %d: %s", statusCode, string(body))
	}

	return fmt.Errorf("request failed with status %d", statusCode)
}
