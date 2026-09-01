package fleet

import (
	"fmt"
	"io"
	"net/http"
	"strings"
)

// maxResponseBodyBytes caps Fleet response bodies at 1 MiB.
const maxResponseBodyBytes int64 = 1 << 20

// pluginMissingMarkers are the response bodies that Grafana returns when the
// collector app plugin cannot serve a proxy route. Grafana returns "Plugin not
// found" when no plugin with that identifier is installed or enabled. Grafana
// returns "plugin route match not found" when the plugin is installed but has
// no route for the path. The markers are lower case, because the comparison
// ignores case.
//
//nolint:gochecknoglobals // an immutable lookup table, not mutable state.
var pluginMissingMarkers = []string{
	"plugin not found",
	"plugin route match not found",
	"plugin is not enabled",
}

// IsPluginMissingBody reports whether the response body says that the collector
// app plugin cannot serve the proxy route. Callers use it to tell a missing
// plugin apart from a missing resource, because both arrive as HTTP 404. The
// comparison ignores case, because Grafana does not capitalize these messages
// in the same way.
func IsPluginMissingBody(body string) bool {
	lower := strings.ToLower(body)
	for _, marker := range pluginMissingMarkers {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
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
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("fleet: HTTP %d from %s: %s", e.Status, e.Path, e.Body)
}
