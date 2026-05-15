package cloud_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/cloud"
)

func TestGCOMClient_GetStack_Success(t *testing.T) {
	want := cloud.StackInfo{
		ID:                         42,
		Slug:                       "mystack",
		Name:                       "My Stack",
		URL:                        "https://mystack.grafana.net",
		OrgID:                      100,
		OrgSlug:                    "myorg",
		Status:                     "active",
		RegionSlug:                 "us-central",
		HMInstancePromID:           1001,
		HMInstancePromURL:          "https://prometheus-prod-1.grafana.net",
		HLInstanceID:               2001,
		HLInstanceURL:              "https://logs-prod-1.grafana.net",
		HTInstanceID:               3001,
		HTInstanceURL:              "https://tempo-prod-1.grafana.net",
		HPInstanceID:               4001,
		HPInstanceURL:              "https://profiles-prod-1.grafana.net",
		AgentManagementInstanceID:  5001,
		AgentManagementInstanceURL: "https://fleet-management-prod-1.grafana.net",
		AMInstanceID:               6001,
		AMInstanceURL:              "https://alertmanager-prod-1.grafana.net",
	}

	var capturedAuth string
	var capturedPath string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedAuth = r.Header.Get("Authorization")
		capturedPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(want); err != nil {
			t.Errorf("failed to encode response: %v", err)
		}
	}))
	defer srv.Close()

	client, err := cloud.NewGCOMClient(srv.URL, "test-token")
	if err != nil {
		t.Fatalf("unexpected error creating client: %v", err)
	}
	got, err := client.GetStack(context.Background(), "mystack")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify request was sent correctly
	if capturedAuth != "Bearer test-token" {
		t.Errorf("expected Authorization: Bearer test-token, got %q", capturedAuth)
	}
	if capturedPath != "/api/instances/mystack" {
		t.Errorf("expected path /api/instances/mystack, got %q", capturedPath)
	}

	// Verify returned data
	if got.ID != want.ID {
		t.Errorf("ID: got %d, want %d", got.ID, want.ID)
	}
	if got.Slug != want.Slug {
		t.Errorf("Slug: got %q, want %q", got.Slug, want.Slug)
	}
	if got.AgentManagementInstanceURL != want.AgentManagementInstanceURL {
		t.Errorf("AgentManagementInstanceURL: got %q, want %q", got.AgentManagementInstanceURL, want.AgentManagementInstanceURL)
	}
	if got.HMInstancePromURL != want.HMInstancePromURL {
		t.Errorf("HMInstancePromURL: got %q, want %q", got.HMInstancePromURL, want.HMInstancePromURL)
	}
}

func TestGCOMClient_GetStack_NonSuccess(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		body       string
	}{
		{
			name:       "404 not found",
			statusCode: http.StatusNotFound,
			body:       `{"message":"stack not found"}`,
		},
		{
			name:       "401 unauthorized",
			statusCode: http.StatusUnauthorized,
			body:       `{"message":"unauthorized"}`,
		},
		{
			name:       "500 server error",
			statusCode: http.StatusInternalServerError,
			body:       `{"message":"internal error"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.statusCode)
				_, _ = w.Write([]byte(tt.body))
			}))
			defer srv.Close()

			client, err := cloud.NewGCOMClient(srv.URL, "test-token")
			if err != nil {
				t.Fatalf("unexpected error creating client: %v", err)
			}
			_, err = client.GetStack(context.Background(), "mystack")
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			errStr := err.Error()
			if !strings.Contains(errStr, http.StatusText(tt.statusCode)) && !strings.Contains(errStr, "404") &&
				!strings.Contains(errStr, "401") && !strings.Contains(errStr, "500") {
				t.Errorf("error %q does not contain status code info", errStr)
			}
		})
	}
}

func TestGCOMClient_GetStack_TypedHTTPError(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
	}{
		{"403 forbidden", http.StatusForbidden},
		{"401 unauthorized", http.StatusUnauthorized},
		{"404 not found", http.StatusNotFound},
		{"500 internal", http.StatusInternalServerError},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tt.statusCode)
				_, _ = w.Write([]byte(`{"message":"denied"}`))
			}))
			defer srv.Close()

			client, err := cloud.NewGCOMClient(srv.URL, "token")
			if err != nil {
				t.Fatalf("unexpected error creating client: %v", err)
			}
			_, err = client.GetStack(context.Background(), "mystack")
			if err == nil {
				t.Fatal("expected error, got nil")
			}

			var httpErr *cloud.GCOMHTTPError
			if !errors.As(err, &httpErr) {
				t.Fatalf("expected error to wrap *cloud.GCOMHTTPError, got %T: %v", err, err)
			}
			if httpErr.Status != tt.statusCode {
				t.Errorf("Status: got %d, want %d", httpErr.Status, tt.statusCode)
			}
		})
	}
}

func TestGCOMClient_GetStack_SlugEscaping(t *testing.T) {
	var capturedRawPath string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// RawPath holds the raw (encoded) path as sent by the client;
		// r.URL.Path is the decoded form and would not show %20 / %2F.
		capturedRawPath = r.URL.RawPath
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(cloud.StackInfo{})
	}))
	defer srv.Close()

	client, err := cloud.NewGCOMClient(srv.URL, "token")
	if err != nil {
		t.Fatalf("unexpected error creating client: %v", err)
	}
	// Slug with special chars — space and slash must be percent-encoded.
	_, err = client.GetStack(context.Background(), "my stack/test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if capturedRawPath != "/api/instances/my%20stack%2Ftest" {
		t.Errorf("expected raw path /api/instances/my%%20stack%%2Ftest, got %q", capturedRawPath)
	}
}

func TestGCOMClient_GetStack_TrailingSlash(t *testing.T) {
	var capturedPath string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(cloud.StackInfo{})
	}))
	defer srv.Close()

	// Base URL with trailing slash(es)
	client, err := cloud.NewGCOMClient(srv.URL+"///", "token")
	if err != nil {
		t.Fatalf("unexpected error creating client: %v", err)
	}
	_, err = client.GetStack(context.Background(), "mystack")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if capturedPath != "/api/instances/mystack" {
		t.Errorf("expected clean path, got %q", capturedPath)
	}
}

func TestNewGCOMClient_SchemeValidation(t *testing.T) {
	tests := []struct {
		name    string
		baseURL string
		wantErr bool
	}{
		{
			name:    "http non-localhost rejected",
			baseURL: "http://example.com",
			wantErr: true,
		},
		{
			name:    "https allowed",
			baseURL: "https://example.com",
			wantErr: false,
		},
		{
			name:    "http localhost allowed",
			baseURL: "http://localhost",
			wantErr: false,
		},
		{
			name:    "http localhost with port allowed",
			baseURL: "http://localhost:8080",
			wantErr: false,
		},
		{
			name:    "http 127.0.0.1 allowed",
			baseURL: "http://127.0.0.1",
			wantErr: false,
		},
		{
			name:    "http IPv6 loopback allowed",
			baseURL: "http://[::1]",
			wantErr: false,
		},
		{
			name:    "http IPv6 loopback with port allowed",
			baseURL: "http://[::1]:8080",
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := cloud.NewGCOMClient(tt.baseURL, "token")
			if tt.wantErr && err == nil {
				t.Errorf("expected error for URL %q, got nil", tt.baseURL)
			}
			if !tt.wantErr && err != nil {
				t.Errorf("unexpected error for URL %q: %v", tt.baseURL, err)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// ListStacks
// ---------------------------------------------------------------------------

func TestGCOMClient_ListStacks_Success(t *testing.T) {
	want := []cloud.StackInfo{
		{ID: 1, Slug: "stack-a", Name: "Stack A", Status: "active", OrgSlug: "myorg"},
		{ID: 2, Slug: "stack-b", Name: "Stack B", Status: "active", OrgSlug: "myorg"},
	}

	var capturedPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"items": want})
	}))
	defer srv.Close()

	client, err := cloud.NewGCOMClient(srv.URL, "test-token")
	if err != nil {
		t.Fatalf("unexpected error creating client: %v", err)
	}
	got, err := client.ListStacks(context.Background(), "myorg")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if capturedPath != "/api/orgs/myorg/instances" {
		t.Errorf("expected path /api/orgs/myorg/instances, got %q", capturedPath)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 stacks, got %d", len(got))
	}
	if got[0].Slug != "stack-a" {
		t.Errorf("Slug[0]: got %q, want %q", got[0].Slug, "stack-a")
	}
}

func TestGCOMClient_ListStacks_Error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"message":"forbidden"}`))
	}))
	defer srv.Close()

	client, err := cloud.NewGCOMClient(srv.URL, "token")
	if err != nil {
		t.Fatalf("unexpected error creating client: %v", err)
	}
	_, err = client.ListStacks(context.Background(), "myorg")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var httpErr *cloud.GCOMHTTPError
	if !errors.As(err, &httpErr) {
		t.Fatalf("expected *GCOMHTTPError, got %T", err)
	}
	if httpErr.Status != http.StatusForbidden {
		t.Errorf("Status: got %d, want %d", httpErr.Status, http.StatusForbidden)
	}
}

// ---------------------------------------------------------------------------
// CreateStack
// ---------------------------------------------------------------------------

func TestGCOMClient_CreateStack_Success(t *testing.T) {
	var capturedBody map[string]any
	var capturedMethod string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedMethod = r.Method
		_ = json.NewDecoder(r.Body).Decode(&capturedBody)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(cloud.StackInfo{
			ID: 42, Slug: "newstack", Name: "newstack", Status: "active", RegionSlug: "us",
		})
	}))
	defer srv.Close()

	client, err := cloud.NewGCOMClient(srv.URL, "token")
	if err != nil {
		t.Fatalf("unexpected error creating client: %v", err)
	}
	got, err := client.CreateStack(context.Background(), cloud.CreateStackRequest{
		Name:   "newstack",
		Slug:   "newstack",
		Region: "us",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if capturedMethod != http.MethodPost {
		t.Errorf("expected POST, got %q", capturedMethod)
	}
	if got.Slug != "newstack" {
		t.Errorf("Slug: got %q, want %q", got.Slug, "newstack")
	}
	if capturedBody["slug"] != "newstack" {
		t.Errorf("request body slug: got %v", capturedBody["slug"])
	}
}

func TestGCOMClient_CreateStack_Conflict(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"message":"slug already taken"}`))
	}))
	defer srv.Close()

	client, err := cloud.NewGCOMClient(srv.URL, "token")
	if err != nil {
		t.Fatalf("unexpected error creating client: %v", err)
	}
	_, err = client.CreateStack(context.Background(), cloud.CreateStackRequest{
		Name: "dup", Slug: "dup",
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var httpErr *cloud.GCOMHTTPError
	if !errors.As(err, &httpErr) || httpErr.Status != http.StatusConflict {
		t.Errorf("expected 409 GCOMHTTPError, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// UpdateStack
// ---------------------------------------------------------------------------

func TestGCOMClient_UpdateStack_Success(t *testing.T) {
	var capturedPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(cloud.StackInfo{
			ID: 42, Slug: "mystack", Description: "updated",
		})
	}))
	defer srv.Close()

	client, err := cloud.NewGCOMClient(srv.URL, "token")
	if err != nil {
		t.Fatalf("unexpected error creating client: %v", err)
	}
	desc := "updated"
	got, err := client.UpdateStack(context.Background(), "mystack", cloud.UpdateStackRequest{
		Description: &desc,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if capturedPath != "/api/instances/mystack" {
		t.Errorf("expected path /api/instances/mystack, got %q", capturedPath)
	}
	if got.Description != "updated" {
		t.Errorf("Description: got %q, want %q", got.Description, "updated")
	}
}

// ---------------------------------------------------------------------------
// DeleteStack
// ---------------------------------------------------------------------------

func TestGCOMClient_DeleteStack_Success(t *testing.T) {
	var capturedMethod string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedMethod = r.Method
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	client, err := cloud.NewGCOMClient(srv.URL, "token")
	if err != nil {
		t.Fatalf("unexpected error creating client: %v", err)
	}
	err = client.DeleteStack(context.Background(), "mystack")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if capturedMethod != http.MethodDelete {
		t.Errorf("expected DELETE, got %q", capturedMethod)
	}
}

func TestGCOMClient_DeleteStack_DeleteProtection(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"message":"delete protection is enabled"}`))
	}))
	defer srv.Close()

	client, err := cloud.NewGCOMClient(srv.URL, "token")
	if err != nil {
		t.Fatalf("unexpected error creating client: %v", err)
	}
	err = client.DeleteStack(context.Background(), "mystack")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var httpErr *cloud.GCOMHTTPError
	if !errors.As(err, &httpErr) || httpErr.Status != http.StatusConflict {
		t.Errorf("expected 409 GCOMHTTPError, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// ListRegions
// ---------------------------------------------------------------------------

func TestGCOMClient_ListRegions_Success(t *testing.T) {
	want := []cloud.Region{
		{ID: 1, Slug: "us", Name: "GCP US Central", Description: "United States", Provider: "gcp", Status: "active"},
		{ID: 2, Slug: "eu", Name: "GCP Belgium", Description: "Europe", Provider: "gcp", Status: "active"},
	}

	var capturedPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"items": want})
	}))
	defer srv.Close()

	client, err := cloud.NewGCOMClient(srv.URL, "token")
	if err != nil {
		t.Fatalf("unexpected error creating client: %v", err)
	}
	got, err := client.ListRegions(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if capturedPath != "/api/stack-regions" {
		t.Errorf("expected path /api/stack-regions, got %q", capturedPath)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 regions, got %d", len(got))
	}
	if got[0].Slug != "us" {
		t.Errorf("Slug[0]: got %q, want %q", got[0].Slug, "us")
	}
	if got[1].Provider != "gcp" {
		t.Errorf("Provider[1]: got %q, want %q", got[1].Provider, "gcp")
	}
}

func TestGCOMClient_GetStack_NoRedirectToDifferentDomain(t *testing.T) {
	// This server redirects to a different domain
	redirectTarget := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Should never reach here
		t.Error("redirect target was called — cross-domain redirect was followed")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(cloud.StackInfo{})
	}))
	defer redirectTarget.Close()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, redirectTarget.URL+"/api/instances/mystack", http.StatusFound)
	}))
	defer srv.Close()

	client, err := cloud.NewGCOMClient(srv.URL, "token")
	if err != nil {
		t.Fatalf("unexpected error creating client: %v", err)
	}
	_, err = client.GetStack(context.Background(), "mystack")
	if err == nil {
		t.Fatal("expected error when redirected to different domain, got nil")
	}
}
