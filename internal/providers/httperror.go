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

// pluginRouteDeniedMessage is the message Grafana's plugin proxy returns when
// the caller lacks the RBAC action a plugin route requires (its reqAction).
const pluginRouteDeniedMessage = "plugin proxy route access denied"

// PluginRouteDeniedError reports that Grafana's plugin proxy refused a route
// because the caller lacks the RBAC action the route requires. The raw 403
// body does not name the action, so the client that knows the route supplies
// it. cmd/gcx/fail renders the action, the role and the auth exit code.
type PluginRouteDeniedError struct {
	// Action is the RBAC action the route requires.
	Action string
	// Role is a role that grants Action.
	Role string
	// Cause is the error the client built from the response.
	Cause error
}

// Error keeps the client's message unchanged.
func (e *PluginRouteDeniedError) Error() string { return e.Cause.Error() }

func (e *PluginRouteDeniedError) Unwrap() error { return e.Cause }

// HTTPStatusCode lets the usage-event reporter record the status.
func (e *PluginRouteDeniedError) HTTPStatusCode() int { return http.StatusForbidden }

// IsPluginRouteDenied reports whether a response is the plugin proxy refusing
// a route. A 403 from the plugin backend itself returns false.
func IsPluginRouteDenied(statusCode int, body []byte) bool {
	if statusCode != http.StatusForbidden {
		return false
	}
	var errResp ErrorResponse
	return json.Unmarshal(body, &errResp) == nil && errResp.message() == pluginRouteDeniedMessage
}

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
