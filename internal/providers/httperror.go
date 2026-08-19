package providers

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/grafana/gcx/internal/gcxerrors"
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
//
// The returned error is a *gcxerrors.HTTPStatusError, so the usage-event
// reporter can read the status without parsing the message. On the
// body-read-failure path the reader error stays reachable through Unwrap,
// exactly as the previous %w wrapping exposed it.
func HandleErrorResponse(resp *http.Response) error {
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxErrorBodyBytes))
	if err != nil {
		return statusError(resp.StatusCode, err,
			"request failed with status %d (could not read body: %v)", resp.StatusCode, err)
	}
	return FormatError(resp.StatusCode, body)
}

// FormatError builds a descriptive error from an already-read non-2xx status
// code and body: the extracted JSON error message when the body unmarshals into
// ErrorResponse, otherwise the raw body text, otherwise a status-only message.
// Used by clients that read a proxied response into memory as []byte before this
// point (e.g. dual-mode datasource-proxy transports).
//
// Every branch returns through statusError, so a future message form cannot
// silently lose http_status coverage. The rendered messages are load-bearing
// contract — converters in cmd/gcx/fail string-match on them and tests pin
// them exactly — and must never change, byte for byte.
func FormatError(statusCode int, body []byte) error {
	var errResp ErrorResponse
	if err := json.Unmarshal(body, &errResp); err == nil {
		if msg := errResp.message(); msg != "" {
			if errResp.TraceID != "" {
				return statusError(statusCode, nil,
					"request failed with status %d: %s (traceID %s)", statusCode, msg, errResp.TraceID)
			}
			return statusError(statusCode, nil, "request failed with status %d: %s", statusCode, msg)
		}
	}

	if len(body) > 0 {
		return statusError(statusCode, nil, "request failed with status %d: %s", statusCode, string(body))
	}

	return statusError(statusCode, nil, "request failed with status %d", statusCode)
}

// statusError renders one of the message forms above into the typed carrier.
// cause is nil for the forms that never wrapped anything, preserving each
// call site's pre-migration unwrap shape.
func statusError(status int, cause error, format string, args ...any) error {
	return &gcxerrors.HTTPStatusError{
		Status:  status,
		Message: fmt.Sprintf(format, args...),
		Cause:   cause,
	}
}
