package plugins_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/plugins"
	"k8s.io/client-go/rest"
)

func newClient(t *testing.T, handler http.Handler) *plugins.Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	c, err := plugins.NewClient(config.NamespacedRESTConfig{Config: rest.Config{Host: server.URL}})
	if err != nil {
		t.Fatalf("failed to create plugins client: %v", err)
	}
	return c
}

func TestIsInstalled_True(t *testing.T) {
	c := newClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/plugins/grafana-azure-data-explorer-datasource/settings" {
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"id":"grafana-azure-data-explorer-datasource","enabled":true,"info":{"version":"5.0.0"}}`))
	}))

	ok, err := c.IsInstalled(context.Background(), "grafana-azure-data-explorer-datasource")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("expected plugin to be reported installed")
	}
}

func TestIsInstalled_FalseOn404(t *testing.T) {
	c := newClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"message":"Plugin not found"}`))
	}))

	ok, err := c.IsInstalled(context.Background(), "missing-plugin")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Fatal("expected plugin to be reported not installed")
	}
}

func TestGet_ErrorOnNon200(t *testing.T) {
	c := newClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"message":"forbidden"}`))
	}))

	if _, err := c.Get(context.Background(), "x"); err == nil {
		t.Fatal("expected error on HTTP 403")
	}
}

func TestInstall(t *testing.T) {
	var called bool
	c := newClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		if r.Method != http.MethodPost {
			t.Fatalf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/api/plugins/grafana-azure-data-explorer-datasource/install" {
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
	}))

	if err := c.Install(context.Background(), "grafana-azure-data-explorer-datasource", ""); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Fatal("expected install endpoint to be called")
	}
}

func TestInstall_ErrorSurfaced(t *testing.T) {
	c := newClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"message":"license required"}`))
	}))

	if err := c.Install(context.Background(), "grafana-azurecosmosdb-datasource", ""); err == nil {
		t.Fatal("expected error to be surfaced from failed install")
	}
}
