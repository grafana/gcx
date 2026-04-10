package httputils

import (
	"net/http"
	"testing"

	"github.com/grafana/gcx/internal/version"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type recordingTransport struct {
	req *http.Request
}

func (rt *recordingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	rt.req = req
	return &http.Response{StatusCode: http.StatusOK}, nil
}

func TestUserAgentTransport_SetsHeader(t *testing.T) {
	version.Set("2.0.0")
	t.Cleanup(func() { version.Set("") })

	rec := &recordingTransport{}
	transport := &UserAgentTransport{Base: rec}

	req, err := http.NewRequest(http.MethodGet, "http://example.com", nil)
	require.NoError(t, err)

	_, err = transport.RoundTrip(req)
	require.NoError(t, err)

	assert.Contains(t, rec.req.Header.Get("User-Agent"), "gcx/2.0.0")
}

func TestUserAgentTransport_DoesNotMutateOriginal(t *testing.T) {
	version.Set("2.0.0")
	t.Cleanup(func() { version.Set("") })

	rec := &recordingTransport{}
	transport := &UserAgentTransport{Base: rec}

	req, err := http.NewRequest(http.MethodGet, "http://example.com", nil)
	require.NoError(t, err)
	req.Header.Set("X-Custom", "keep")

	_, err = transport.RoundTrip(req)
	require.NoError(t, err)

	assert.Empty(t, req.Header.Get("User-Agent"), "original request must not be mutated")
	assert.Equal(t, "keep", rec.req.Header.Get("X-Custom"), "custom headers must be preserved")
}
