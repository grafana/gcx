package gfc

import (
	"encoding/json"
	"fmt"
)

// HTTPStatusError represents an HTTP error with a status code.
type HTTPStatusError struct {
	Status  int
	Message string
	Cause   error
}

func (e *HTTPStatusError) Error() string       { return e.Message }
func (e *HTTPStatusError) Unwrap() error       { return e.Cause }
func (e *HTTPStatusError) HTTPStatusCode() int { return e.Status }

// errorResponse is the common error shape returned by Grafana APIs.
type errorResponse struct {
	Error   string          `json:"error"`
	Message string          `json:"message"`
	Err     json.RawMessage `json:"err"`
	Msg     string          `json:"msg"`
	TraceID string          `json:"traceID"`
}

func (e errorResponse) message() string {
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

// FormatError builds an [HTTPStatusError] from a status code and raw response body.
// It attempts to extract a structured error message from the JSON body.
func FormatError(statusCode int, body []byte) error {
	var errResp errorResponse
	if err := json.Unmarshal(body, &errResp); err == nil {
		if msg := errResp.message(); msg != "" {
			if errResp.TraceID != "" {
				return statusError(statusCode,
					"request failed with status %d: %s (traceID %s)", statusCode, msg, errResp.TraceID)
			}
			return statusError(statusCode, "request failed with status %d: %s", statusCode, msg)
		}
	}
	if len(body) > 0 {
		return statusError(statusCode, "request failed with status %d: %s", statusCode, string(body))
	}
	return statusError(statusCode, "request failed with status %d", statusCode)
}

func statusError(status int, format string, args ...any) error {
	return &HTTPStatusError{
		Status:  status,
		Message: fmt.Sprintf(format, args...),
	}
}
