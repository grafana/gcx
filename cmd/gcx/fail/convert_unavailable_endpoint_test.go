package fail_test

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/grafana/gcx/cmd/gcx/fail"
	"github.com/grafana/gcx/internal/queryerror"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestErrorToDetailedError_UnavailableEndpoint covers the cases where we are
// confident the route is absent/incompatible: a 404 whose body is Go's default
// "404 page not found" (route truly absent), and 405/501 (method/route
// mismatch). These render the actionable "not available" diagnosis.
func TestErrorToDetailedError_UnavailableEndpoint(t *testing.T) {
	cases := []struct {
		name    string
		status  int
		message string
	}{
		{"absent route 404", http.StatusNotFound, "404 page not found"},
		{"method not allowed", http.StatusMethodNotAllowed, "Method Not Allowed"},
		{"not implemented", http.StatusNotImplemented, "not implemented"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := fmt.Errorf("trace diff failed: %w",
				queryerror.New("tempo", "trace diff", tc.status, tc.message, "").WithAvailability(true, true))

			det := fail.ErrorToDetailedError(err)

			require.NotNil(t, det)
			assert.Contains(t, det.Summary, "not available")
			assert.NotEmpty(t, det.Suggestions)
		})
	}
}

// TestErrorToDetailedError_MissingResourceFallsThrough guards the ambiguity:
// a present endpoint returning 404 for a missing trace ("Not Found") must NOT
// be mislabelled as unavailable — it falls through to the normal query error.
func TestErrorToDetailedError_MissingResourceFallsThrough(t *testing.T) {
	err := fmt.Errorf("trace diff failed: %w",
		queryerror.New("tempo", "trace diff", http.StatusNotFound, "Not Found", "").WithAvailability(true, true))

	det := fail.ErrorToDetailedError(err)

	require.NotNil(t, det)
	assert.NotContains(t, det.Summary, "not available")
}

// TestErrorToDetailedError_RequestErrorFallsThrough covers a genuine request
// error (400) on a Cloud-only/experimental endpoint: it renders as a normal
// query error, not an availability diagnosis.
func TestErrorToDetailedError_RequestErrorFallsThrough(t *testing.T) {
	err := fmt.Errorf("trace diff failed: %w",
		queryerror.New("tempo", "trace diff", http.StatusBadRequest, "bad request", "").WithAvailability(true, true))

	det := fail.ErrorToDetailedError(err)

	require.NotNil(t, det)
	assert.NotContains(t, det.Summary, "not available")
}

// TestErrorToDetailedError_UnflaggedEndpointNotTreatedAsUnavailable ensures the
// generic converter only applies to errors explicitly flagged via
// WithAvailability; unflagged 404s stay normal query errors.
func TestErrorToDetailedError_UnflaggedEndpointNotTreatedAsUnavailable(t *testing.T) {
	err := fmt.Errorf("get trace failed: %w",
		queryerror.New("tempo", "get trace", http.StatusNotFound, "404 page not found", ""))

	det := fail.ErrorToDetailedError(err)

	require.NotNil(t, det)
	assert.NotContains(t, det.Summary, "not available")
}
