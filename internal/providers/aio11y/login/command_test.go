package login_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/grafana/gcx/internal/cloud"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/providers/aio11y/login"
)

// fakeGCOM is a configurable GCOM stand-in covering the endpoints aio11y login
// touches: instance get/connections, access-policy create/list, token
// create/list/delete.
type fakeGCOM struct {
	stack cloud.StackInfo
	conns cloud.Connections

	policyConflict   bool               // POST accesspolicies → 409, GET returns existingPolicy
	permissionDenied bool               // POST accesspolicies → 403
	tokenConflict    bool               // first POST tokens → 409 (forces rotation)
	existingPolicy   cloud.AccessPolicy // returned by GET accesspolicies on conflict
	existingToken    cloud.Token        // returned by GET tokens on rotation

	mu             sync.Mutex
	tokenPostCount int
	deletedTokens  []string
}

func (f *fakeGCOM) server(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		path := r.URL.Path

		switch {
		case strings.HasPrefix(path, "/api/instances/") && strings.HasSuffix(path, "/connections"):
			writeJSON(t, w, f.conns)

		case strings.HasPrefix(path, "/api/instances/"):
			writeJSON(t, w, f.stack)

		case path == "/api/v1/accesspolicies" && r.Method == http.MethodPost:
			switch {
			case f.permissionDenied:
				w.WriteHeader(http.StatusForbidden)
				_, _ = w.Write([]byte(`{"message":"forbidden"}`))
			case f.policyConflict:
				w.WriteHeader(http.StatusConflict)
				_, _ = w.Write([]byte(`{"message":"name taken"}`))
			default:
				var req cloud.CreateAccessPolicyRequest
				_ = json.NewDecoder(r.Body).Decode(&req)
				writeJSON(t, w, cloud.AccessPolicy{ID: "pol-1", Name: req.Name, Scopes: req.Scopes, Realms: req.Realms})
			}

		case path == "/api/v1/accesspolicies" && r.Method == http.MethodGet:
			writeJSON(t, w, map[string]any{"items": []cloud.AccessPolicy{f.existingPolicy}})

		case path == "/api/v1/tokens" && r.Method == http.MethodPost:
			f.mu.Lock()
			first := f.tokenPostCount == 0
			f.tokenPostCount++
			f.mu.Unlock()
			if f.tokenConflict && first {
				w.WriteHeader(http.StatusConflict)
				_, _ = w.Write([]byte(`{"message":"token name taken"}`))
				return
			}
			var req cloud.CreateTokenRequest
			_ = json.NewDecoder(r.Body).Decode(&req)
			writeJSON(t, w, cloud.Token{ID: "tok-1", Name: req.Name, Token: "glc_minted"})

		case path == "/api/v1/tokens" && r.Method == http.MethodGet:
			writeJSON(t, w, map[string]any{"items": []cloud.Token{f.existingToken}})

		case strings.HasPrefix(path, "/api/v1/tokens/") && r.Method == http.MethodDelete:
			f.mu.Lock()
			f.deletedTokens = append(f.deletedTokens, strings.TrimPrefix(path, "/api/v1/tokens/"))
			f.mu.Unlock()
			w.WriteHeader(http.StatusOK)

		default:
			t.Errorf("unexpected request %s %s", r.Method, path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}

func writeJSON(t *testing.T, w http.ResponseWriter, v any) {
	t.Helper()
	if err := json.NewEncoder(w).Encode(v); err != nil {
		t.Errorf("encode: %v", err)
	}
}

func writeCfg(t *testing.T, apiURL string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "gcx.yaml")
	content := `
contexts:
  default:
    cloud:
      token: "glc_test"
      stack: "mystack"
      api-url: "` + apiURL + `"
current-context: default
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func newLoader(t *testing.T, apiURL string) *providers.ConfigLoader {
	t.Helper()
	loader := &providers.ConfigLoader{}
	loader.SetConfigFile(writeCfg(t, apiURL))
	loader.SetContextName("default")
	return loader
}

func runLogin(t *testing.T, loader *providers.ConfigLoader, args ...string) (string, string, error) {
	t.Helper()
	cmd := login.Command(loader)
	var out, errBuf bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errBuf)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), errBuf.String(), err
}

func TestLogin_ProvisionsTokenByDefault(t *testing.T) {
	f := &fakeGCOM{
		// regionSigilUrl is a different region than the OTLP host to prove the
		// GCOM field is used verbatim rather than derived.
		stack: cloud.StackInfo{ID: 42, Slug: "mystack", RegionSlug: "us", OrgSlug: "myorg", RegionSigilURL: "https://sigil-prod-us-east-0.grafana.net"},
		conns: cloud.Connections{OtlpHTTPURL: "https://otlp-gateway-prod-eu-west-2.grafana.net/otlp"},
	}
	srv := f.server(t)
	defer srv.Close()

	loader := newLoader(t, srv.URL)
	sigilPath := filepath.Join(t.TempDir(), "sigil", "config.env")

	out, _, err := runLogin(t, loader, "--config-path", sigilPath, "--content-capture-mode", "full", "-o", "text")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}

	got := readEnv(t, sigilPath)
	if got["SIGIL_ENDPOINT"] != "https://sigil-prod-us-east-0.grafana.net" {
		t.Errorf("SIGIL_ENDPOINT = %q (expected GCOM regionSigilUrl)", got["SIGIL_ENDPOINT"])
	}
	if got["SIGIL_AUTH_TENANT_ID"] != "42" {
		t.Errorf("SIGIL_AUTH_TENANT_ID = %q", got["SIGIL_AUTH_TENANT_ID"])
	}
	if got["SIGIL_AUTH_TOKEN"] != "glc_minted" {
		t.Errorf("SIGIL_AUTH_TOKEN = %q (expected the minted token, not the cloud token)", got["SIGIL_AUTH_TOKEN"])
	}
	if got["SIGIL_OTEL_EXPORTER_OTLP_ENDPOINT"] != "https://otlp-gateway-prod-eu-west-2.grafana.net/otlp" {
		t.Errorf("SIGIL_OTEL_EXPORTER_OTLP_ENDPOINT = %q", got["SIGIL_OTEL_EXPORTER_OTLP_ENDPOINT"])
	}
	if got["SIGIL_CONTENT_CAPTURE_MODE"] != "full" {
		t.Errorf("SIGIL_CONTENT_CAPTURE_MODE = %q", got["SIGIL_CONTENT_CAPTURE_MODE"])
	}
	if !strings.Contains(out, "/add-plugin grafana/sigil-sdk") {
		t.Errorf("expected Cursor hint in output:\n%s", out)
	}
}

func TestLogin_NoProvisionWritesContextToken(t *testing.T) {
	f := &fakeGCOM{
		stack: cloud.StackInfo{ID: 42, Slug: "mystack", RegionSlug: "us", RegionSigilURL: "https://sigil-prod-us-east-0.grafana.net"},
		conns: cloud.Connections{OtlpHTTPURL: "https://otlp-gateway-prod-eu-west-2.grafana.net/otlp"},
	}
	srv := f.server(t)
	defer srv.Close()

	loader := newLoader(t, srv.URL)
	sigilPath := filepath.Join(t.TempDir(), "config.env")

	if _, _, err := runLogin(t, loader, "--config-path", sigilPath, "--no-provision", "-o", "text"); err != nil {
		t.Fatalf("execute: %v", err)
	}

	got := readEnv(t, sigilPath)
	if got["SIGIL_AUTH_TOKEN"] != "glc_test" {
		t.Errorf("SIGIL_AUTH_TOKEN = %q (expected the context cloud token)", got["SIGIL_AUTH_TOKEN"])
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.tokenPostCount != 0 {
		t.Errorf("no-provision should not mint a token, got %d POSTs", f.tokenPostCount)
	}
}

func TestLogin_FallsBackWhenProvisioningForbidden(t *testing.T) {
	f := &fakeGCOM{
		stack:            cloud.StackInfo{ID: 42, Slug: "mystack", RegionSlug: "us", OrgSlug: "myorg", RegionSigilURL: "https://sigil-prod-us-east-0.grafana.net"},
		conns:            cloud.Connections{OtlpHTTPURL: "https://otlp-gateway-prod-eu-west-2.grafana.net/otlp"},
		permissionDenied: true,
	}
	srv := f.server(t)
	defer srv.Close()

	loader := newLoader(t, srv.URL)
	sigilPath := filepath.Join(t.TempDir(), "config.env")

	out, errOut, err := runLogin(t, loader, "--config-path", sigilPath, "-o", "json")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}

	got := readEnv(t, sigilPath)
	if got["SIGIL_AUTH_TOKEN"] != "glc_test" {
		t.Errorf("SIGIL_AUTH_TOKEN = %q (expected fallback to context token)", got["SIGIL_AUTH_TOKEN"])
	}
	if !strings.Contains(errOut, "accesspolicies:write") {
		t.Errorf("expected guidance about accesspolicies:write on stderr:\n%s", errOut)
	}

	var res struct {
		TokenSource string `json:"token_source"`
	}
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, out)
	}
	if res.TokenSource != "cloud-context" {
		t.Errorf("token_source = %q, want cloud-context", res.TokenSource)
	}
}

func TestLogin_ReusesPolicyAndRotatesToken(t *testing.T) {
	f := &fakeGCOM{
		stack:          cloud.StackInfo{ID: 42, Slug: "mystack", RegionSlug: "us", RegionSigilURL: "https://sigil-prod-us-east-0.grafana.net"},
		conns:          cloud.Connections{OtlpHTTPURL: "https://otlp-gateway-prod-eu-west-2.grafana.net/otlp"},
		policyConflict: true,
		tokenConflict:  true,
		existingPolicy: cloud.AccessPolicy{
			ID: "pol-existing", Name: "sigil-mystack",
			Realms: []cloud.Realm{{Type: "stack", Identifier: "42"}},
		},
		existingToken: cloud.Token{ID: "old-tok", Name: "sigil-mystack"},
	}
	srv := f.server(t)
	defer srv.Close()

	loader := newLoader(t, srv.URL)
	sigilPath := filepath.Join(t.TempDir(), "config.env")

	out, _, err := runLogin(t, loader, "--config-path", sigilPath, "-o", "json")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}

	got := readEnv(t, sigilPath)
	if got["SIGIL_AUTH_TOKEN"] != "glc_minted" {
		t.Errorf("SIGIL_AUTH_TOKEN = %q", got["SIGIL_AUTH_TOKEN"])
	}
	f.mu.Lock()
	deleted := append([]string(nil), f.deletedTokens...)
	f.mu.Unlock()
	if len(deleted) != 1 || deleted[0] != "old-tok" {
		t.Errorf("expected old token to be deleted on rotation, got %v", deleted)
	}

	var res struct {
		PolicyReused bool `json:"policy_reused"`
		TokenRotated bool `json:"token_rotated"`
	}
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !res.PolicyReused {
		t.Error("policy_reused = false, want true")
	}
	if !res.TokenRotated {
		t.Error("token_rotated = false, want true")
	}
}

func TestLogin_DryRunMintsNothingAndWritesNothing(t *testing.T) {
	f := &fakeGCOM{
		stack: cloud.StackInfo{ID: 7, Slug: "mystack", RegionSlug: "us"},
		conns: cloud.Connections{OtlpHTTPURL: "https://otlp-gateway-prod-us-east-0.grafana.net/otlp"},
	}
	srv := f.server(t)
	defer srv.Close()

	loader := newLoader(t, srv.URL)
	sigilPath := filepath.Join(t.TempDir(), "sigil", "config.env")

	out, _, err := runLogin(t, loader, "--config-path", sigilPath, "--dry-run", "-o", "text")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}

	if _, statErr := os.Stat(sigilPath); !os.IsNotExist(statErr) {
		t.Errorf("dry-run wrote a file (err=%v)", statErr)
	}
	if !strings.Contains(out, "sigil-prod-us-east-0") {
		t.Errorf("expected derived endpoint in dry-run output:\n%s", out)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.tokenPostCount != 0 {
		t.Errorf("dry-run must not mint a token, got %d POSTs", f.tokenPostCount)
	}
}

func TestLogin_TokenFlagSkipsProvisioning(t *testing.T) {
	f := &fakeGCOM{
		stack: cloud.StackInfo{ID: 42, Slug: "mystack", RegionSlug: "us", RegionSigilURL: "https://sigil-prod-us-east-0.grafana.net"},
		conns: cloud.Connections{OtlpHTTPURL: "https://otlp-gateway-prod-eu-west-2.grafana.net/otlp"},
	}
	srv := f.server(t)
	defer srv.Close()

	loader := newLoader(t, srv.URL)
	sigilPath := filepath.Join(t.TempDir(), "config.env")

	if _, _, err := runLogin(t, loader, "--config-path", sigilPath, "--token", "glc_byo", "-o", "text"); err != nil {
		t.Fatalf("execute: %v", err)
	}

	got := readEnv(t, sigilPath)
	if got["SIGIL_AUTH_TOKEN"] != "glc_byo" {
		t.Errorf("SIGIL_AUTH_TOKEN = %q, want glc_byo", got["SIGIL_AUTH_TOKEN"])
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.tokenPostCount != 0 {
		t.Errorf("--token should skip provisioning, got %d POSTs", f.tokenPostCount)
	}
}

func TestLogin_InvalidContentCaptureMode(t *testing.T) {
	if _, _, err := runLogin(t, &providers.ConfigLoader{}, "--content-capture-mode", "bogus"); err == nil {
		t.Fatal("expected error for invalid content-capture-mode")
	}
}

func TestLogin_InvalidTokenExpiry(t *testing.T) {
	if _, _, err := runLogin(t, &providers.ConfigLoader{}, "--token-expiry", "not-a-date"); err == nil {
		t.Fatal("expected error for invalid token-expiry")
	}
}

func TestLogin_TokenNotLeakedInJSON(t *testing.T) {
	f := &fakeGCOM{
		stack: cloud.StackInfo{ID: 99, Slug: "mystack", RegionSlug: "us", RegionSigilURL: "https://sigil-prod-us-east-0.grafana.net"},
		conns: cloud.Connections{OtlpHTTPURL: "https://otlp-gateway-prod-eu-west-2.grafana.net/otlp"},
	}
	srv := f.server(t)
	defer srv.Close()

	loader := newLoader(t, srv.URL)
	sigilPath := filepath.Join(t.TempDir(), "config.env")

	out, _, err := runLogin(t, loader, "--config-path", sigilPath, "-o", "json")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if strings.Contains(out, "glc_minted") || strings.Contains(out, "glc_test") {
		t.Errorf("token value leaked into JSON output:\n%s", out)
	}
}
