package grafana

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// mustParse parses a URL for tests, failing the test on error.
func mustParse(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parsing %q: %v", raw, err)
	}
	return u
}

func TestJoinProxyPath(t *testing.T) {
	cases := []struct {
		name    string
		base    string
		reqPath string
		want    string
	}{
		{"empty base returns request path unchanged", "", "/grafana/d/uid/slug", "/grafana/d/uid/slug"},
		{"prefix prepended to request path", "/api/cli/v1/proxy", "/d/uid/slug", "/api/cli/v1/proxy/d/uid/slug"},
		{"base with trailing slash", "/api/cli/v1/proxy/", "/d/uid/slug", "/api/cli/v1/proxy/d/uid/slug"},
		{"request path without leading slash", "/api", "d/uid", "/api/d/uid"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := joinProxyPath(tc.base, tc.reqPath); got != tc.want {
				t.Errorf("joinProxyPath(%q, %q) = %q, want %q", tc.base, tc.reqPath, got, tc.want)
			}
		})
	}
}

// TestAuthenticateAndProxyHandler_DirectModeDoesNotDoubleSubpath asserts that a
// Grafana served under a subpath (direct mode) does not get the subpath doubled
// onto the proxied request. In direct mode ProxyTarget() leaves the target Path
// empty, so the subpath already carried by the incoming request path is
// forwarded verbatim.
func TestAuthenticateAndProxyHandler_DirectModeDoesNotDoubleSubpath(t *testing.T) {
	var gotPath string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("dashboard"))
	}))
	defer upstream.Close()

	// Direct mode: ProxyTarget zeroes the path.
	target := mustParse(t, upstream.URL)
	target.Path = ""

	handler := AuthenticateAndProxyHandler(target, http.DefaultTransport)

	rec := httptest.NewRecorder()
	// The route is registered at subpath + "/d/{uid}/{slug}", so the incoming
	// request path already carries the /grafana subpath.
	req := httptest.NewRequest(http.MethodGet, "/grafana/d/uid/slug", nil)
	handler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if gotPath != "/grafana/d/uid/slug" {
		t.Errorf("expected upstream path /grafana/d/uid/slug (no doubling), got %q", gotPath)
	}
}

// TestAuthenticateAndProxyHandler_OAuthPrefixPrepended asserts that in OAuth
// proxy mode the proxy prefix carried by target.Path is prepended to the
// outgoing request.
func TestAuthenticateAndProxyHandler_OAuthPrefixPrepended(t *testing.T) {
	var gotPath string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	target := mustParse(t, upstream.URL+"/api/cli/v1/proxy")

	handler := AuthenticateAndProxyHandler(target, http.DefaultTransport)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/d/uid/slug", nil)
	handler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if want := "/api/cli/v1/proxy/d/uid/slug"; gotPath != want {
		t.Errorf("expected upstream path %q, got %q", want, gotPath)
	}
}

// TestAuthenticateAndProxyHandler_OAuthRedirectReappliesPrefix asserts that when
// the upstream returns a relative redirect (which resolves against the
// proxy-prefixed URL and would otherwise drop the prefix), the followed request
// still routes through the proxy prefix.
func TestAuthenticateAndProxyHandler_OAuthRedirectReappliesPrefix(t *testing.T) {
	var gotPaths []string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPaths = append(gotPaths, r.URL.Path)
		if r.URL.Path == "/api/cli/v1/proxy/d/uid/slug" {
			// Relative redirect, as Grafana canonicalizing a dashboard slug.
			http.Redirect(w, r, "/d/uid/canonical", http.StatusFound)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("dashboard"))
	}))
	defer upstream.Close()

	target := mustParse(t, upstream.URL+"/api/cli/v1/proxy")

	handler := AuthenticateAndProxyHandler(target, http.DefaultTransport)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/d/uid/slug", nil)
	handler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 after following redirect, got %d", rec.Code)
	}
	want := []string{"/api/cli/v1/proxy/d/uid/slug", "/api/cli/v1/proxy/d/uid/canonical"}
	if len(gotPaths) != len(want) {
		t.Fatalf("expected upstream paths %v, got %v", want, gotPaths)
	}
	for i := range want {
		if gotPaths[i] != want[i] {
			t.Errorf("upstream path[%d] = %q, want %q (prefix must be re-applied)", i, gotPaths[i], want[i])
		}
	}
}

// TestAuthenticateAndProxyHandler_LoginRedirectReturnsAuthError asserts that a
// redirect to the Grafana login page is intercepted and surfaced as a friendly
// authentication error rather than being followed.
func TestAuthenticateAndProxyHandler_LoginRedirectReturnsAuthError(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/login", http.StatusFound)
	}))
	defer upstream.Close()

	target := mustParse(t, upstream.URL)
	target.Path = ""

	handler := AuthenticateAndProxyHandler(target, http.DefaultTransport)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/d/uid/slug", nil)
	handler(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 on login redirect, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Authentication error") {
		t.Errorf("expected authentication error page, got %q", rec.Body.String())
	}
}

func TestAuthenticateAndProxyHandler_EmptyTargetReturnsBadRequest(t *testing.T) {
	handler := AuthenticateAndProxyHandler(&url.URL{}, http.DefaultTransport)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/d/uid/slug", nil)
	handler(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for empty target, got %d", rec.Code)
	}
}
