package activation //nolint:testpackage // Tests cover unexported HTTP wiring alongside the exported API.

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	internalconfig "github.com/grafana/gcx/internal/config"
	"k8s.io/client-go/rest"
)

func testRESTConfig(t *testing.T, handler http.HandlerFunc) internalconfig.NamespacedRESTConfig {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return internalconfig.NamespacedRESTConfig{
		Config:    rest.Config{Host: srv.URL},
		Namespace: "stacks-12345",
	}
}

func TestIsActivated_Enabled(t *testing.T) {
	cfg := testRESTConfig(t, func(w http.ResponseWriter, r *http.Request) {
		if got, want := r.URL.Path, "/api/plugin-proxy/grafana-app-observability-app/provisioned-plugin-settings"; got != want {
			t.Errorf("path = %q, want %q", got, want)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"jsonData":{}}`)) //nolint:errcheck // test helper
	})

	activated, err := IsActivated(context.Background(), cfg)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if !activated {
		t.Error("expected activated = true")
	}
}

func TestIsActivated_NotFound(t *testing.T) {
	cfg := testRESTConfig(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})

	activated, err := IsActivated(context.Background(), cfg)
	if err != nil {
		t.Fatalf("expected 404 to resolve to (false, nil), got err = %v", err)
	}
	if activated {
		t.Error("expected activated = false")
	}
}

func TestIsActivated_ServerError(t *testing.T) {
	cfg := testRESTConfig(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})

	activated, err := IsActivated(context.Background(), cfg)
	if err == nil {
		t.Fatal("expected an error for a non-404 non-2xx status")
	}
	if activated {
		t.Error("expected activated = false on error")
	}
}

func TestIsActivated_Forbidden(t *testing.T) {
	cfg := testRESTConfig(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	})

	activated, err := IsActivated(context.Background(), cfg)
	if err == nil {
		t.Fatal("expected an error for a 403 status")
	}
	if activated {
		t.Error("expected activated = false on error")
	}
}

// TestIsActivated_TransportFailure exercises a connection-level failure (the
// server accepts and immediately closes the connection without a response)
// rather than an HTTP-level error status, confirming httpClient.Do's error
// path is classified as inconclusive too.
func TestIsActivated_TransportFailure(t *testing.T) {
	var lc net.ListenConfig
	ln, err := lc.Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to open listener: %v", err)
	}
	t.Cleanup(func() { ln.Close() })

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			conn.Close()
		}
	}()

	cfg := internalconfig.NamespacedRESTConfig{
		Config:    rest.Config{Host: "http://" + ln.Addr().String()},
		Namespace: "stacks-12345",
	}

	activated, err := IsActivated(context.Background(), cfg)
	if err == nil {
		t.Fatal("expected a transport-level error")
	}
	if activated {
		t.Error("expected activated = false on error")
	}
}

func TestGate_NotFoundBlocks(t *testing.T) {
	cfg := testRESTConfig(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})

	err := Gate(context.Background(), cfg)
	if err == nil {
		t.Fatal("expected a definitive not-activated error")
	}
	if err.Error() != NotActivatedError().Error() {
		t.Errorf("err = %v, want NotActivatedError", err)
	}
}

func TestGate_EnabledPasses(t *testing.T) {
	cfg := testRESTConfig(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"jsonData":{}}`)) //nolint:errcheck // test helper
	})

	if err := Gate(context.Background(), cfg); err != nil {
		t.Errorf("err = %v, want nil", err)
	}
}

func TestGate_InconclusiveDoesNotBlock(t *testing.T) {
	cfg := testRESTConfig(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})

	if err := Gate(context.Background(), cfg); err != nil {
		t.Errorf("err = %v, want nil — an inconclusive activation check must not block the command", err)
	}
}

func TestNotActivatedError(t *testing.T) {
	err := NotActivatedError()
	if err == nil {
		t.Fatal("expected a non-nil error")
	}
}
