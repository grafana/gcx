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

func (e *HTTPError) Error() string {
	return fmt.Sprintf("fleet: HTTP %d from %s: %s", e.Status, e.Path, e.Body)
}
