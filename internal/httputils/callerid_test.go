package httputils_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/grafana/gcx/internal/httputils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCallerIDTransport_SetsHeader(t *testing.T) {
	rec := &recordingTransport{}
	transport := &httputils.CallerIDTransport{Base: rec}

	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, "http://example.com", nil)
	require.NoError(t, err)

	resp, err := transport.RoundTrip(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, "gcx", rec.req.Header.Get("X-Grafana-Caller-Id"))
}

func TestCallerIDTransport_DoesNotMutateOriginal(t *testing.T) {
	rec := &recordingTransport{}
	transport := &httputils.CallerIDTransport{Base: rec}

	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, "http://example.com", nil)
	require.NoError(t, err)
	req.Header.Set("X-Custom", "keep")

	resp, err := transport.RoundTrip(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Empty(t, req.Header.Get("X-Grafana-Caller-Id"), "original request must not be mutated")
	assert.Equal(t, "keep", rec.req.Header.Get("X-Custom"), "custom headers must be preserved")
}
