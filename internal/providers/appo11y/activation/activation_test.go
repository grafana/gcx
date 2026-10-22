package activation //nolint:testpackage // Tests cover unexported HTTP wiring alongside the exported API.

import (
	"bytes"
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
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

// TestGate_NotFoundWarns confirms Gate never blocks: it surfaces a
// definitive not-activated result as a stderr warning and returns, letting
// the caller's own query decide the outcome.
func TestGate_NotFoundWarns(t *testing.T) {
	cfg := testRESTConfig(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})

	var stderr bytes.Buffer
	Gate(context.Background(), cfg, &stderr)
	if !strings.Contains(stderr.String(), NotActivatedError().Error()) {
		t.Errorf("stderr = %q, want it to contain the not-activated message", stderr.String())
	}
}

func TestGate_EnabledIsSilent(t *testing.T) {
	cfg := testRESTConfig(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"jsonData":{}}`)) //nolint:errcheck // test helper
	})

	var stderr bytes.Buffer
	Gate(context.Background(), cfg, &stderr)
	if stderr.Len() != 0 {
		t.Errorf("stderr = %q, want no warning when the plugin is active", stderr.String())
	}
}

// TestGate_InconclusiveWarns confirms an inconclusive check (auth failure,
// 5xx, transport error) still surfaces something on stderr rather than
// vanishing silently — it just doesn't block.
func TestGate_InconclusiveWarns(t *testing.T) {
	cfg := testRESTConfig(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})

	var stderr bytes.Buffer
	Gate(context.Background(), cfg, &stderr)
	if stderr.Len() == 0 {
		t.Error("expected a warning for an inconclusive activation check, got none")
	}
}
