package fleet

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// maxResponseBodyBytes caps Fleet response bodies at 1 MiB.
const maxResponseBodyBytes int64 = 1 << 20

// IsResourceNotFoundBody reports whether a response is a Connect error whose
// code identifies an absent resource. Other 404 bodies indicate an unavailable
// plugin, route, or RPC endpoint, even when their message says "not found".
func IsResourceNotFoundBody(body string) bool {
	var connectError struct {
		Code string `json:"code"`
	}
	return json.Unmarshal([]byte(body), &connectError) == nil && connectError.Code == "not_found"
}

// ReadErrorBody reads up to 1 MiB of a response body for error messages.
func ReadErrorBody(resp *http.Response) string {
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBodyBytes))
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
	// ContentType is the response Content-Type header, when available.
	ContentType string
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("fleet: HTTP %d from %s: %s", e.Status, e.Path, e.Body)
}
