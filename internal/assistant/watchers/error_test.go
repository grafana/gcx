package watchers //nolint:testpackage // Tests error decoding with an interrupted response body.

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type failedReader struct{ err error }

func (r failedReader) Read(_ []byte) (int, error) { return 0, r.err }

func TestErrorBodyFailurePreservesStatusAndIdentity(t *testing.T) {
	bodyErr := errors.New("body interrupted")
	err := readAPIError(&http.Response{StatusCode: http.StatusForbidden, Body: io.NopCloser(failedReader{bodyErr})}, "get Watcher", false)
	var apiErr *APIError
	require.ErrorAs(t, err, &apiErr)
	assert.Equal(t, http.StatusForbidden, apiErr.HTTPStatusCode())
	require.ErrorIs(t, err, ErrPermissionDenied)
	require.ErrorIs(t, err, bodyErr)
}

func TestCapabilityUnavailableHasPlainTypedError(t *testing.T) {
	for _, tt := range []struct {
		name       string
		status     int
		operation  string
		collection bool
		body       string
	}{
		{"collection", http.StatusNotFound, "list Watchers", true, `{"message":"SECRET SERVER DETAIL"}`},
		{"calibration", http.StatusNotImplemented, "read Watcher calibration", false, `{"message":"SECRET SERVER DETAIL"}`},
		{"structured unavailable", http.StatusInternalServerError, "read Watcher automatic recalibration", false, `{"name":"NOT_IMPLEMENTED","message":"SECRET SERVER DETAIL"}`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			err := readAPIError(&http.Response{StatusCode: tt.status, Body: io.NopCloser(strings.NewReader(tt.body))}, tt.operation, tt.collection)
			_, plain := err.(*APIError) //nolint:errorlint // This contract requires a direct APIError, without wrappers.
			require.True(t, plain, "client must return the API error directly")
			var apiErr *APIError
			require.ErrorAs(t, err, &apiErr)
			assert.Equal(t, tt.status, apiErr.HTTPStatusCode())
			assert.Equal(t, "Assistant Watchers", apiErr.APIServiceName())
			require.ErrorIs(t, err, ErrCapabilityUnavailable)
			assert.Contains(t, err.Error(), tt.operation)
			assert.Contains(t, err.Error(), "unavailable")
			assert.Equal(t, err.Error(), apiErr.APIUserMessage())
			assert.NotContains(t, err.Error(), "\n")
			assert.NotContains(t, err.Error(), "Suggestions")
			assert.NotContains(t, err.Error(), "SECRET")
			encoded, encodeErr := json.Marshal(err)
			require.NoError(t, encodeErr)
			assert.NotContains(t, string(encoded), "SECRET")
			assert.NotContains(t, string(encoded), "NOT_IMPLEMENTED")
			assert.NotContains(t, string(encoded), `"Message":`)
			assert.NotContains(t, string(encoded), `"Code":`)
		})
	}
}
