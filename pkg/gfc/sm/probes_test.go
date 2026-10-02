package sm_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/grafana/gcx/pkg/gfc/sm"
)

func TestProbesClient_List(t *testing.T) {
	probes := []sm.Probe{
		{ID: 1, Name: "Atlanta"},
		{ID: 2, Name: "Paris"},
	}
	body := mustJSON(t, probes)

	transport := &stubTransport{status: http.StatusOK, body: body}
	client := &sm.ProbesClient{T: transport}

	got, err := client.List(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d probes, want 2", len(got))
	}
	if got[0].Name != "Atlanta" {
		t.Errorf("got name %q, want %q", got[0].Name, "Atlanta")
	}
}

func TestProbesClient_Get(t *testing.T) {
	probes := []sm.Probe{
		{ID: 1, Name: "Atlanta"},
		{ID: 2, Name: "Paris"},
	}
	body := mustJSON(t, probes)

	transport := &stubTransport{status: http.StatusOK, body: body}
	client := &sm.ProbesClient{T: transport}

	got, err := client.Get(context.Background(), 2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Name != "Paris" {
		t.Errorf("got name %q, want %q", got.Name, "Paris")
	}
}

func TestProbesClient_Get_NotFound(t *testing.T) {
	body := mustJSON(t, []sm.Probe{{ID: 1, Name: "Atlanta"}})

	transport := &stubTransport{status: http.StatusOK, body: body}
	client := &sm.ProbesClient{T: transport}

	_, err := client.Get(context.Background(), 99)
	if err == nil {
		t.Fatal("expected error for missing probe")
	}
}

func TestProbesClient_Delete(t *testing.T) {
	transport := &stubTransport{status: http.StatusOK, body: []byte(`{"msg":"ok"}`)}
	client := &sm.ProbesClient{T: transport}

	err := client.Delete(context.Background(), 3)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if transport.lastMethod != http.MethodDelete {
		t.Errorf("method = %q, want DELETE", transport.lastMethod)
	}
	if transport.lastPath != "probe/delete/3" {
		t.Errorf("path = %q, want probe/delete/3", transport.lastPath)
	}
}
