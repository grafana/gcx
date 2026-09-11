package slo

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// maxErrorBodyBytes caps how much of a non-2xx response body is read,
// guarding against an unbounded read from a misbehaving proxy or server.
const maxErrorBodyBytes = 1 << 20 // 1 MiB

// apiError carries the HTTP status of a failing request. It deliberately
// exposes only Error, Unwrap, and HTTPStatusCode: gcx's CLI (which wraps this
// client for its `slo definitions` command) probes errors for that exact
// three-method shape structurally, not by concrete type, so this satisfies
// the same probe as gcx's internal error type without this package
// depending on internal/gcxerrors.
type apiError struct {
	status  int
	message string
	cause   error
}

func (e *apiError) Error() string       { return e.message }
func (e *apiError) Unwrap() error       { return e.cause }
func (e *apiError) HTTPStatusCode() int { return e.status }

// errorResponse is the common JSON error-body shape returned by Grafana
// Cloud product plugin APIs. They disagree on the field name for the
// human-readable message, so all variants are captured and read in
// preference order.
type errorResponse struct {
	Error   string          `json:"error"`
	Message string          `json:"message"`
	Err     json.RawMessage `json:"err"`
	Msg     string          `json:"msg"`
	TraceID string          `json:"traceID"`
}

// text returns the first populated field: Error, then Message, then Err, then Msg.
func (e errorResponse) text() string {
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

// handleErrorResponse reads a non-2xx HTTP response body and returns a
// descriptive error. It does not close resp.Body; callers remain
// responsible for that.
func handleErrorResponse(resp *http.Response) error {
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxErrorBodyBytes))
	if err != nil {
		return &apiError{
			status:  resp.StatusCode,
			cause:   err,
			message: fmt.Sprintf("request failed with status %d (could not read body: %v)", resp.StatusCode, err),
		}
	}

	var errResp errorResponse
	if err := json.Unmarshal(body, &errResp); err == nil {
		if msg := errResp.text(); msg != "" {
			if errResp.TraceID != "" {
				return &apiError{
					status:  resp.StatusCode,
					message: fmt.Sprintf("request failed with status %d: %s (traceID %s)", resp.StatusCode, msg, errResp.TraceID),
				}
			}
			return &apiError{
				status:  resp.StatusCode,
				message: fmt.Sprintf("request failed with status %d: %s", resp.StatusCode, msg),
			}
		}
	}

	if len(body) > 0 {
		return &apiError{
			status:  resp.StatusCode,
			message: fmt.Sprintf("request failed with status %d: %s", resp.StatusCode, string(body)),
		}
	}

	return &apiError{status: resp.StatusCode, message: fmt.Sprintf("request failed with status %d", resp.StatusCode)}
}
