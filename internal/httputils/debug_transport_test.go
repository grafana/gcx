package httputils_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/grafana/gcx/internal/httputils"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestLoggingRoundTripper_Success(t *testing.T) {
	base := roundTripFunc(func(_ *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: http.NoBody}, nil
	})
	rt := &httputils.LoggingRoundTripper{Base: base}
	req := httptest.NewRequest(http.MethodGet, "http://example.com/api", nil)

	resp, err := rt.RoundTrip(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
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
	req := httptest.NewRequest(http.MethodGet, "http://example.com/api", nil)

	_, err := rt.RoundTrip(req)
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected %v, got %v", wantErr, err)
	}
}

func TestLoggingRoundTripper_5xx(t *testing.T) {
	base := roundTripFunc(func(_ *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusBadGateway, Body: http.NoBody}, nil
	})
	rt := &httputils.LoggingRoundTripper{Base: base}
	req := httptest.NewRequest(http.MethodGet, "http://example.com/api", nil)

	resp, err := rt.RoundTrip(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d", resp.StatusCode)
	}
}
