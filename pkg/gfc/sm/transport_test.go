package sm_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/grafana/gcx/pkg/gfc/sm"
)

// grafanaAuthInjector mimics an auth-injecting round-tripper such as
// oauth2.Transport, which overwrites any existing Authorization header.
type grafanaAuthInjector struct{ token string }

func (g *grafanaAuthInjector) RoundTrip(req *http.Request) (*http.Response, error) {
	req.Header.Set("Authorization", "Bearer "+g.token)
	return http.DefaultTransport.RoundTrip(req)
}

type staticFallback struct{ baseURL, token string }

func (s staticFallback) LoadSMConfig(context.Context) (string, string, error) {
	return s.baseURL, s.token, nil
}

// TestTransport_DirectPathDoesNotLeakGrafanaCredential is the regression guard
// for the dual-client split: when the proxy denies access, the direct SM API
// request must carry the SM token, not the Grafana credential. Reusing the
// Grafana-authenticated client would send the Grafana credential to the SM host
// whenever its round-tripper overwrites the Authorization header.
func TestTransport_DirectPathDoesNotLeakGrafanaCredential(t *testing.T) {
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer proxy.Close()

	var directAuth string
	direct := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		directAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`[]`))
	}))
	defer direct.Close()

	transport, err := sm.NewTransport(sm.TransportConfig{
		HTTPClient:       &http.Client{Transport: &grafanaAuthInjector{token: "grafana-credential"}},
		DirectHTTPClient: &http.Client{},
		GrafanaHost:      proxy.URL,
		DatasourceUID:    "sm-ds",
		Fallback:         staticFallback{baseURL: direct.URL, token: "sm-token"},
	})
	if err != nil {
		t.Fatalf("NewTransport: %v", err)
	}

	status, _, err := transport.Do(context.Background(), http.MethodGet, "check/list", nil)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200", status)
	}

	if want := "Bearer sm-token"; directAuth != want {
		t.Errorf("direct Authorization = %q, want %q", directAuth, want)
	}
	if directAuth == "Bearer grafana-credential" {
		t.Error("Grafana credential leaked to the SM API host")
	}
}

// TestTransport_ProxyPathUsesGrafanaClient confirms the primary path still
// carries the caller's Grafana credential.
func TestTransport_ProxyPathUsesGrafanaClient(t *testing.T) {
	var gotAuth, gotPath, gotClientID string
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotPath = r.URL.Path
		gotClientID = r.Header.Get("X-Client-Id")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`[]`))
	}))
	defer proxy.Close()

	transport, err := sm.NewTransport(sm.TransportConfig{
		HTTPClient:    &http.Client{Transport: &grafanaAuthInjector{token: "grafana-credential"}},
		GrafanaHost:   proxy.URL,
		DatasourceUID: "sm-ds",
		ClientID:      "gcx",
	})
	if err != nil {
		t.Fatalf("NewTransport: %v", err)
	}

	if _, _, err := transport.Do(context.Background(), http.MethodGet, "check/list", nil); err != nil {
		t.Fatalf("Do: %v", err)
	}

	if want := "Bearer grafana-credential"; gotAuth != want {
		t.Errorf("proxy Authorization = %q, want %q", gotAuth, want)
	}
	if want := "/api/datasources/proxy/uid/sm-ds/sm/check/list"; gotPath != want {
		t.Errorf("proxy path = %q, want %q", gotPath, want)
	}
	if gotClientID != "gcx" {
		t.Errorf("X-Client-Id = %q, want %q", gotClientID, "gcx")
	}
}

// TestTransport_NoFallbackConfigured checks that a proxy-only transport reports
// a clear error instead of panicking when the proxy denies access.
func TestTransport_NoFallbackConfigured(t *testing.T) {
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer proxy.Close()

	transport, err := sm.NewTransport(sm.TransportConfig{
		HTTPClient:    &http.Client{},
		GrafanaHost:   proxy.URL,
		DatasourceUID: "sm-ds",
	})
	if err != nil {
		t.Fatalf("NewTransport: %v", err)
	}

	if _, _, err := transport.Do(context.Background(), http.MethodGet, "check/list", nil); err == nil {
		t.Fatal("expected an error when the proxy 403s with no fallback configured")
	}
}

// TestTransport_RequiresHTTPClient guards the constructor precondition.
func TestTransport_RequiresHTTPClient(t *testing.T) {
	if _, err := sm.NewTransport(sm.TransportConfig{GrafanaHost: "https://example.grafana.net"}); err == nil {
		t.Fatal("expected an error when HTTPClient is nil")
	}
}

// TestTransport_ArbitraryMethods confirms the transport forwards verbs beyond
// GET/POST/DELETE, which other SM-adjacent domains need.
func TestTransport_ArbitraryMethods(t *testing.T) {
	for _, method := range []string{http.MethodPut, http.MethodPatch, http.MethodOptions} {
		t.Run(method, func(t *testing.T) {
			var gotMethod string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotMethod = r.Method
				w.WriteHeader(http.StatusOK)
			}))
			defer srv.Close()

			transport, err := sm.NewTransport(sm.TransportConfig{
				HTTPClient:    &http.Client{},
				GrafanaHost:   srv.URL,
				DatasourceUID: "sm-ds",
			})
			if err != nil {
				t.Fatalf("NewTransport: %v", err)
			}

			if _, _, err := transport.Do(context.Background(), method, "probe/update", nil); err != nil {
				t.Fatalf("Do: %v", err)
			}
			if gotMethod != method {
				t.Errorf("method = %q, want %q", gotMethod, method)
			}
		})
	}
}
