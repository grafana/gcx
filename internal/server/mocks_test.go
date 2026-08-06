package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestStaticProxyConfig_LoginPingReturns200 asserts that /api/login/ping is
// served as a mock returning HTTP 200, so the Grafana frontend's login-state
// ping succeeds under `gcx dev serve`.
func TestStaticProxyConfig_LoginPingReturns200(t *testing.T) {
	s := &Server{}

	body, ok := s.staticProxyConfig().MockGet["/api/login/ping"]
	if !ok {
		t.Fatal("/api/login/ping is not registered as a mock GET endpoint")
	}

	rec := httptest.NewRecorder()
	s.mockHandler(body)(rec, httptest.NewRequest(http.MethodGet, "/api/login/ping", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("expected Content-Type application/json, got %q", ct)
	}
	if rec.Body.String() != body {
		t.Errorf("expected body %q, got %q", body, rec.Body.String())
	}
}
