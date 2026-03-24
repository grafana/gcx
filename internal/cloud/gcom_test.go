package cloud_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/grafana/grafanactl/internal/cloud"
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
