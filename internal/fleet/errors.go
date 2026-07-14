package fleet

import (
	"fmt"
	"io"
	"net/http"
)

// ReadErrorBody reads and returns the response body as a string for error messages.
func ReadErrorBody(resp *http.Response) string {
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "(could not read body)"
	}
	return string(body)
}

// HTTPError represents a non-2xx HTTP response from the Fleet Management API.
// It is returned by the instrumentation client when the server returns an
// unexpected HTTP status code, enabling typed error detection in converters.
type HTTPError struct {
	// Status is the HTTP status code.
	Status int
	// Path is the Connect endpoint path.
	Path string
	// Body is the trimmed response body (for diagnostics).
	Body string
}

// Error returns the raw HTTP diagnostic. Actionable role/auth guidance for 401
// and 403 is added once, as DetailedError suggestions, by the converter in
// cmd/gcx/fail — it is deliberately not appended here to avoid emitting the same
// guidance twice in the rendered error.
func (e *HTTPError) Error() string {
	return fmt.Sprintf("fleet: HTTP %d from %s: %s", e.Status, e.Path, e.Body)
}

// StatusError wraps a non-2xx response as an *HTTPError, prefixed with op (the
// calling method name) for context. It reads the response body for diagnostics.
func StatusError(op, path string, resp *http.Response) error {
	return fmt.Errorf("%s: %w", op, &HTTPError{
		Status: resp.StatusCode,
		Path:   path,
		Body:   ReadErrorBody(resp),
	})
}
