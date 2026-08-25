package instances //nolint:testpackage // Tests cover unexported activation-check logic.

import (
	"context"
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

func TestCheckActivation_Enabled(t *testing.T) {
	// Real shape confirmed against Grafana's own "ops" stack via `gcx api`:
	// GET .../dbo11yconfigs/global -> 200 {"spec": {"enabled": true}}.
	cfg := testRESTConfig(t, func(w http.ResponseWriter, r *http.Request) {
		if got, want := r.URL.Path, "/apis/productactivation.ext.grafana.com/v1alpha1/namespaces/stacks-12345/dbo11yconfigs/global"; got != want {
			t.Errorf("path = %q, want %q", got, want)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"spec":{"enabled":true}}`)) //nolint:errcheck // test helper
	})

	activated, err := checkActivation(context.Background(), cfg)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if !activated {
		t.Error("expected activated = true")
	}
}

func TestCheckActivation_EnabledFalse(t *testing.T) {
	cfg := testRESTConfig(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"spec":{"enabled":false}}`)) //nolint:errcheck // test helper
	})

	activated, err := checkActivation(context.Background(), cfg)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if activated {
		t.Error("expected activated = false")
	}
}

func TestCheckActivation_NotFound(t *testing.T) {
	// Real shape confirmed against a stack that never went through Database
	// Observability's onboarding flow: 404, config resource never created.
	cfg := testRESTConfig(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})

	activated, err := checkActivation(context.Background(), cfg)
	if err != nil {
		t.Fatalf("expected 404 to resolve to (false, nil), got err = %v", err)
	}
	if activated {
		t.Error("expected activated = false")
	}
}

func TestCheckActivation_UnexpectedError(t *testing.T) {
	cfg := testRESTConfig(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})

	if _, err := checkActivation(context.Background(), cfg); err == nil {
		t.Fatal("expected an error for a non-404 non-2xx status")
	}
}
