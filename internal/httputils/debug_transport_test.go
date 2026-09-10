package httputils_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/httputils"
	"github.com/grafana/gcx/internal/secrets"
	"github.com/grafana/grafana-app-sdk/logging"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestLoggingRoundTripper_Success(t *testing.T) {
	base := roundTripFunc(func(_ *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: http.NoBody}, nil
	})
	rt := &httputils.LoggingRoundTripper{Base: base}
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://example.com/api", nil)

	resp, err := rt.RoundTrip(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}

func TestLoggingRoundTripper_TransportError(t *testing.T) {
	wantErr := errors.New("connection refused")
	base := roundTripFunc(func(_ *http.Request) (*http.Response, error) {
		return nil, wantErr
	})
	rt := &httputils.LoggingRoundTripper{Base: base}
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://example.com/api", nil)

	resp, err := rt.RoundTrip(req)
	if resp != nil {
		defer resp.Body.Close()
	}
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected %v, got %v", wantErr, err)
	}
}

func TestLoggingRoundTripper_5xx(t *testing.T) {
	base := roundTripFunc(func(_ *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusBadGateway, Body: http.NoBody}, nil
	})
	rt := &httputils.LoggingRoundTripper{Base: base}
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://example.com/api", nil)

	resp, err := rt.RoundTrip(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d", resp.StatusCode)
	}
}

func TestRequestResponseLoggingRoundTripper_RedactsQueryAndPreservesBody(t *testing.T) {
	const secret = "credential-that-must-not-leak"
	var sentBody string
	base := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		body, err := io.ReadAll(req.Body)
		require.NoError(t, err)
		sentBody = string(body)
		return &http.Response{StatusCode: http.StatusOK, Body: http.NoBody}, nil
	})
	var logs bytes.Buffer
	logger := logging.NewSLogLogger(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	ctx := logging.Context(t.Context(), logger)
	ctx = secrets.WithRedactedURLQuery(ctx)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"https://artifacts.example.test/a.png?X-Amz-Credential="+secret, strings.NewReader("request-body"))
	require.NoError(t, err)

	resp, err := (httputils.RequestResponseLoggingRoundTripper{DecoratedTransport: base}).RoundTrip(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, "request-body", sentBody)
	assert.Contains(t, logs.String(), "?REDACTED")
	assert.NotContains(t, logs.String(), secret)
}
