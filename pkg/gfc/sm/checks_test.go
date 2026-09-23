package sm_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	"github.com/grafana/gcx/pkg/gfc/sm"
)

// mustJSON marshals v for use as a stub response body.
func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	body, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshalling stub response: %v", err)
	}
	return body
}

type stubTransport struct {
	status int
	body   []byte
	err    error

	lastMethod string
	lastPath   string
	lastBody   []byte
}

func (s *stubTransport) Do(_ context.Context, method, path string, body []byte) (int, []byte, error) {
	s.lastMethod = method
	s.lastPath = path
	s.lastBody = body
	return s.status, s.body, s.err
}

func TestChecksClient_List(t *testing.T) {
	checks := []sm.Check{
		{ID: 1, Job: "test", Target: "https://example.com"},
		{ID: 2, Job: "ping", Target: "1.1.1.1"},
	}
	body := mustJSON(t, checks)

	transport := &stubTransport{status: http.StatusOK, body: body}
	client := &sm.ChecksClient{T: transport}

	got, err := client.List(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d checks, want 2", len(got))
	}
	if got[0].Job != "test" {
		t.Errorf("got job %q, want %q", got[0].Job, "test")
	}
	if transport.lastMethod != http.MethodGet {
		t.Errorf("method = %q, want GET", transport.lastMethod)
	}
	if transport.lastPath != "check/list" {
		t.Errorf("path = %q, want check/list", transport.lastPath)
	}
}

func TestChecksClient_List_Empty(t *testing.T) {
	transport := &stubTransport{status: http.StatusOK, body: []byte("null")}
	client := &sm.ChecksClient{T: transport}

	got, err := client.List(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got == nil {
		t.Fatal("expected non-nil slice for null response")
	}
	if len(got) != 0 {
		t.Fatalf("got %d checks, want 0", len(got))
	}
}

func TestChecksClient_Get(t *testing.T) {
	check := sm.Check{ID: 42, Job: "test", Target: "https://example.com"}
	body := mustJSON(t, check)

	transport := &stubTransport{status: http.StatusOK, body: body}
	client := &sm.ChecksClient{T: transport}

	got, err := client.Get(context.Background(), 42)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.ID != 42 {
		t.Errorf("got ID %d, want 42", got.ID)
	}
	if transport.lastPath != "check/42" {
		t.Errorf("path = %q, want check/42", transport.lastPath)
	}
}

func TestChecksClient_Get_NotFound(t *testing.T) {
	transport := &stubTransport{status: http.StatusNotFound, body: []byte(`{"error":"not found"}`)}
	client := &sm.ChecksClient{T: transport}

	_, err := client.Get(context.Background(), 99)
	if err == nil {
		t.Fatal("expected error for 404")
	}
	if !errors.Is(err, sm.ErrCheckNotFound) {
		t.Errorf("got error %v, want ErrCheckNotFound", err)
	}
}

func TestChecksClient_Create(t *testing.T) {
	created := sm.Check{ID: 10, Job: "new", Target: "https://new.example.com"}
	body := mustJSON(t, created)

	transport := &stubTransport{status: http.StatusOK, body: body}
	client := &sm.ChecksClient{T: transport}

	got, err := client.Create(context.Background(), sm.Check{Job: "new", Target: "https://new.example.com"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.ID != 10 {
		t.Errorf("got ID %d, want 10", got.ID)
	}
	if transport.lastMethod != http.MethodPost {
		t.Errorf("method = %q, want POST", transport.lastMethod)
	}
	if transport.lastPath != "check/add" {
		t.Errorf("path = %q, want check/add", transport.lastPath)
	}
}

func TestChecksClient_Delete(t *testing.T) {
	transport := &stubTransport{status: http.StatusOK, body: []byte(`{"msg":"ok"}`)}
	client := &sm.ChecksClient{T: transport}

	err := client.Delete(context.Background(), 5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if transport.lastMethod != http.MethodDelete {
		t.Errorf("method = %q, want DELETE", transport.lastMethod)
	}
	if transport.lastPath != "check/delete/5" {
		t.Errorf("path = %q, want check/delete/5", transport.lastPath)
	}
}
