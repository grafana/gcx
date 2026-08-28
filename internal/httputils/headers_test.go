package httputils_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/grafana/gcx/internal/httputils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHeaderTransport_SetsHeaders(t *testing.T) {
	rec := &recordingTransport{}
	transport := &httputils.HeaderTransport{
		Base:    rec,
		Headers: map[string]string{"Cookie": "AWSELBAuthSessionCookie-0=abc", "X-Custom": "value"},
	}

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://example.com", nil)
	require.NoError(t, err)

	resp, err := transport.RoundTrip(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, "AWSELBAuthSessionCookie-0=abc", rec.req.Header.Get("Cookie"))
	assert.Equal(t, "value", rec.req.Header.Get("X-Custom"))
}

func TestHeaderTransport_DoesNotMutateOriginal(t *testing.T) {
	rec := &recordingTransport{}
	transport := &httputils.HeaderTransport{
		Base:    rec,
		Headers: map[string]string{"Cookie": "AWSELBAuthSessionCookie-0=abc"},
	}

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://example.com", nil)
	require.NoError(t, err)
	req.Header.Set("X-Existing", "keep")

	resp, err := transport.RoundTrip(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Empty(t, req.Header.Get("Cookie"), "original request must not be mutated")
	assert.Equal(t, "keep", rec.req.Header.Get("X-Existing"), "existing headers must be preserved")
}
