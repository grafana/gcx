package providers

import (
	"encoding/json"
	"errors"
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
// message, so all variants are captured and read in preference order.
// TraceID, when present, is surfaced for supportability.
type ErrorResponse struct {
	Error   string          `json:"error"`
	Message string          `json:"message"`
	Err     json.RawMessage `json:"err"`
	Msg     string          `json:"msg"`
	TraceID string          `json:"traceID"`
}

// message returns the first populated field: Error, then Message, then Err, then Msg.
func (e ErrorResponse) message() string {
	for _, m := range []string{e.Error, e.Message} {
		if m != "" {
			return m
		}
	}

	var errMessage string
	if err := json.Unmarshal(e.Err, &errMessage); err == nil && errMessage != "" {
		return errMessage
	}

	return e.Msg
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
		statusErr := statusError(resp.StatusCode, err, "", "",
			"request failed with status %d (could not read body: %v)", resp.StatusCode, err)
		statusErr.ContentType = resp.Header.Get("Content-Type")
		return statusErr
	}
	formatted := FormatError(resp.StatusCode, body)
	var statusErr *gcxerrors.HTTPStatusError
	if errors.As(formatted, &statusErr) {
		statusErr.ContentType = resp.Header.Get("Content-Type")
	}
	return formatted
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
	var traceID string
	if err := json.Unmarshal(body, &errResp); err == nil {
		traceID = errResp.TraceID
		if msg := errResp.message(); msg != "" {
			if errResp.TraceID != "" {
				return statusError(statusCode, nil, msg, errResp.TraceID,
					"request failed with status %d: %s (traceID %s)", statusCode, msg, errResp.TraceID)
			}
			return statusError(statusCode, nil, msg, "", "request failed with status %d: %s", statusCode, msg)
		}
	}

	if len(body) > 0 {
		return statusError(statusCode, nil, string(body), traceID, "request failed with status %d: %s", statusCode, string(body))
	}

	return statusError(statusCode, nil, "", "", "request failed with status %d", statusCode)
}

// statusError renders one of the message forms above into the typed carrier.
// cause is nil for the forms that never wrapped anything, preserving each
// call site's pre-migration unwrap shape.
func statusError(status int, cause error, serverMessage, traceID, format string, args ...any) *gcxerrors.HTTPStatusError {
	return &gcxerrors.HTTPStatusError{
		Status:        status,
		Message:       fmt.Sprintf(format, args...),
		ServerMessage: serverMessage,
		TraceID:       traceID,
		Cause:         cause,
	}
}
