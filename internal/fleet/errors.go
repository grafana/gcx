package fleet

import (
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
