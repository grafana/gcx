package auth_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/grafana/gcx/internal/auth"
)

func TestRefreshTransport_SetsAuthorizationHeader(t *testing.T) {
	var gotHeader string
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeader = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()

	transport := &auth.RefreshTransport{
		Base:          http.DefaultTransport,
		ProxyEndpoint: backend.URL,
		Token:         "gat_test-token",
		ExpiresAt:     time.Now().Add(1 * time.Hour),
	}

	client := &http.Client{Transport: transport}
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, backend.URL+"/api/test", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	if gotHeader != "Bearer gat_test-token" {
		t.Fatalf("expected Authorization header %q, got %q", "Bearer gat_test-token", gotHeader)
	}
}

func TestRefreshTransport_SkipsRefreshWhenTokenFresh(t *testing.T) {
	var refreshCalls atomic.Int32
	refreshServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/cli/v1/auth/refresh" {
			refreshCalls.Add(1)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer refreshServer.Close()

	transport := &auth.RefreshTransport{
		Base:          http.DefaultTransport,
		ProxyEndpoint: refreshServer.URL,
		Token:         "gat_fresh",
		RefreshToken:  "gar_refresh",
		ExpiresAt:     time.Now().Add(1 * time.Hour), // well above threshold
	}

	client := &http.Client{Transport: transport}
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, refreshServer.URL+"/test", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	if refreshCalls.Load() != 0 {
		t.Fatalf("expected no refresh calls, got %d", refreshCalls.Load())
	}
}

func TestRefreshTransport_RefreshesWhenTokenExpiring(t *testing.T) {
	refreshServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/cli/v1/auth/refresh" {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": map[string]any{
					"token":              "gat_refreshed",
					"expires_at":         time.Now().Add(1 * time.Hour).Format(time.RFC3339),
					"refresh_token":      "gar_new-refresh",
					"refresh_expires_at": time.Now().Add(24 * time.Hour).Format(time.RFC3339),
				},
			})
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer refreshServer.Close()

	transport := &auth.RefreshTransport{
		Base:          http.DefaultTransport,
		ProxyEndpoint: refreshServer.URL,
		Token:         "gat_old",
		RefreshToken:  "gar_old",
		ExpiresAt:     time.Now().Add(1 * time.Minute), // within refresh threshold
	}

	client := &http.Client{Transport: transport}
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, refreshServer.URL+"/test", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	if transport.Token != "gat_refreshed" {
		t.Fatalf("expected token to be refreshed to %q, got %q", "gat_refreshed", transport.Token)
	}
}

func TestRefreshTransport_CallsOnRefreshCallback(t *testing.T) {
	refreshServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/cli/v1/auth/refresh" {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": map[string]any{
					"token":              "gat_new",
					"expires_at":         time.Now().Add(1 * time.Hour).Format(time.RFC3339),
					"refresh_token":      "gar_new",
					"refresh_expires_at": time.Now().Add(24 * time.Hour).Format(time.RFC3339),
				},
			})
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer refreshServer.Close()

	var callbackCalled atomic.Bool
	var savedToken, savedRefresh string

	transport := &auth.RefreshTransport{
		Base:          http.DefaultTransport,
		ProxyEndpoint: refreshServer.URL,
		Token:         "gat_expiring",
		RefreshToken:  "gar_old",
		ExpiresAt:     time.Now().Add(1 * time.Minute), // within threshold
		OnRefresh: func(token, refreshToken, expiresAt, refreshExpiresAt string) error {
			callbackCalled.Store(true)
			savedToken = token
			savedRefresh = refreshToken
			return nil
		},
	}

	client := &http.Client{Transport: transport}
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, refreshServer.URL+"/test", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	if !callbackCalled.Load() {
		t.Fatal("expected OnRefresh callback to be called")
	}
	if savedToken != "gat_new" {
		t.Fatalf("expected saved token %q, got %q", "gat_new", savedToken)
	}
	if savedRefresh != "gar_new" {
		t.Fatalf("expected saved refresh token %q, got %q", "gar_new", savedRefresh)
	}
}

func TestRefreshTransport_RejectsExpiredRefreshToken(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()

	transport := &auth.RefreshTransport{
		Base:             http.DefaultTransport,
		ProxyEndpoint:    backend.URL,
		Token:            "gat_old",
		RefreshToken:     "gar_expired",
		ExpiresAt:        time.Now().Add(1 * time.Minute),        // within refresh threshold
		RefreshExpiresAt: time.Now().Add(-1 * time.Hour),         // already expired
	}

	client := &http.Client{Transport: transport}
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, backend.URL+"/test", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_, err = client.Do(req)
	if err == nil {
		t.Fatal("expected error for expired refresh token, got nil")
	}
	if !errors.Is(err, auth.ErrRefreshTokenExpired) {
		t.Fatalf("expected ErrRefreshTokenExpired, got: %v", err)
	}
}
