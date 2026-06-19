package cloud_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/grafana/gcx/internal/cloud"
)

func TestGCOMClient_GetStackV1_Success(t *testing.T) {
	desc := "production stack"
	want := cloud.StackV1{
		ID:               42,
		Slug:             "mystack",
		Name:             "My Stack",
		Description:      &desc,
		Region:           "us",
		OrgID:            100,
		OrgSlug:          "myorg",
		URL:              "https://mystack.grafana.net",
		DeleteProtection: true,
		Labels:           map[string]string{"env": "prod"},
	}

	var capturedPath, capturedAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		capturedAuth = r.Header.Get("Authorization")
		_ = json.NewEncoder(w).Encode(want)
	}))
	defer srv.Close()

	client, err := cloud.NewGCOMClient(srv.URL, "test-token")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got, err := client.GetStackV1(context.Background(), "mystack")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if capturedPath != "/api/v1/stacks/mystack" {
		t.Errorf("path: got %q, want /api/v1/stacks/mystack", capturedPath)
	}
	if capturedAuth != "Bearer test-token" {
		t.Errorf("auth: got %q", capturedAuth)
	}
	if got.ID != want.ID || got.Slug != want.Slug || got.Region != want.Region {
		t.Errorf("got %+v, want %+v", got, want)
	}
	if got.Description == nil || *got.Description != desc {
		t.Errorf("description: got %v, want %q", got.Description, desc)
	}
}

func TestGCOMClient_ListStacksV1_Paginates(t *testing.T) {
	// Two pages of one item each, total 2.
	var capturedQueries []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedQueries = append(capturedQueries, r.URL.RawQuery)
		page := r.URL.Query().Get("page")
		item := cloud.StackV1{Slug: "stack-" + page, Name: "Stack " + page}
		pageNum, _ := strconv.Atoi(page)
		resp := map[string]any{
			"total":    2,
			"pages":    2,
			"page":     pageNum,
			"pageSize": 1,
			"items":    []cloud.StackV1{item},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	client, _ := cloud.NewGCOMClient(srv.URL, "test-token")
	got, err := client.ListStacksV1(context.Background(), "myorg")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(got) != 2 {
		t.Fatalf("expected 2 stacks across 2 pages, got %d", len(got))
	}
	if len(capturedQueries) != 2 {
		t.Fatalf("expected 2 page requests, got %d: %v", len(capturedQueries), capturedQueries)
	}
	if want := "org=myorg&page=1&pageSize=100"; capturedQueries[0] != want {
		t.Errorf("first query: got %q, want %q", capturedQueries[0], want)
	}
}

func TestGCOMClient_CreateStackV1_Success(t *testing.T) {
	var capturedMethod, capturedPath string
	var capturedBody cloud.CreateStackRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedMethod = r.Method
		capturedPath = r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&capturedBody)
		_ = json.NewEncoder(w).Encode(cloud.StackV1{Slug: "new", Name: "New"})
	}))
	defer srv.Close()

	client, _ := cloud.NewGCOMClient(srv.URL, "test-token")
	_, err := client.CreateStackV1(context.Background(), cloud.CreateStackRequest{
		Name: "New", Slug: "new", Org: "myorg", Region: "us",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if capturedMethod != http.MethodPost || capturedPath != "/api/v1/stacks" {
		t.Errorf("got %s %s, want POST /api/v1/stacks", capturedMethod, capturedPath)
	}
	if capturedBody.Slug != "new" {
		t.Errorf("body slug: got %q", capturedBody.Slug)
	}
}

func TestGCOMClient_DeleteStackV1_Method(t *testing.T) {
	var capturedMethod, capturedPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedMethod = r.Method
		capturedPath = r.URL.Path
		_ = json.NewEncoder(w).Encode(cloud.StackV1{Slug: "mystack"})
	}))
	defer srv.Close()

	client, _ := cloud.NewGCOMClient(srv.URL, "test-token")
	if err := client.DeleteStackV1(context.Background(), "mystack"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if capturedMethod != http.MethodDelete || capturedPath != "/api/v1/stacks/mystack" {
		t.Errorf("got %s %s, want DELETE /api/v1/stacks/mystack", capturedMethod, capturedPath)
	}
}

func TestGCOMClient_DeleteStackV1_DeleteProtection(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusConflict)
		fmt.Fprint(w, `{"message":"delete protection enabled"}`)
	}))
	defer srv.Close()

	client, _ := cloud.NewGCOMClient(srv.URL, "test-token")
	err := client.DeleteStackV1(context.Background(), "mystack")
	var httpErr *cloud.GCOMHTTPError
	if !errors.As(err, &httpErr) {
		t.Fatalf("expected GCOMHTTPError, got %v", err)
	}
	if httpErr.Status != http.StatusConflict {
		t.Errorf("status: got %d, want 409", httpErr.Status)
	}
}

func TestGCOMClient_ListRegionsV1_Success(t *testing.T) {
	var capturedPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		_ = json.NewEncoder(w).Encode(map[string]any{
			"items": []cloud.RegionV1{
				{ID: 1, Slug: "us", Name: "US Central", Provider: "gcp", Visibility: "public"},
			},
		})
	}))
	defer srv.Close()

	client, _ := cloud.NewGCOMClient(srv.URL, "test-token")
	got, err := client.ListRegionsV1(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if capturedPath != "/api/v1/stack-regions" {
		t.Errorf("path: got %q, want /api/v1/stack-regions", capturedPath)
	}
	if len(got) != 1 || got[0].Slug != "us" || got[0].Visibility != "public" {
		t.Errorf("unexpected regions: %+v", got)
	}
}
