package watchers //nolint:testpackage // Tests error decoding with an interrupted response body.

import (
	"errors"
	"io"
	"net/http"
	"testing"

	"github.com/grafana/gcx/internal/gcxerrors"
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

func TestCapabilityUnavailableHasDistinctSummaryAndParent(t *testing.T) {
	err := readAPIError(&http.Response{StatusCode: http.StatusNotFound, Body: io.NopCloser(failedReader{io.EOF})}, "list Watchers", true)
	var detailed *gcxerrors.DetailedError
	require.ErrorAs(t, err, &detailed)
	assert.Equal(t, "Endpoint not available", detailed.Summary)
	var apiErr *APIError
	require.ErrorAs(t, err, &apiErr)
	assert.Equal(t, http.StatusNotFound, apiErr.HTTPStatusCode())
	require.ErrorIs(t, err, ErrCapabilityUnavailable)
	assert.NotEmpty(t, detailed.Suggestions)
}
