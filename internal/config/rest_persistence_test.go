package config_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/grafana/gcx/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeTestConfigFile(t *testing.T, path, content string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(strings.TrimSpace(content)+"\n"), 0o600))
}

func TestResolveTokenPersistenceSource_ExplicitWins(t *testing.T) {
	dir := t.TempDir()
	explicitFile := filepath.Join(dir, "explicit.yaml")
	require.NoError(t, os.WriteFile(explicitFile, []byte("contexts: {}\n"), 0o600))

	got := config.ResolveTokenPersistenceSource(
		t.Context(),
		config.StandardLocation(),
		"default",
		[]config.ConfigSource{{Path: explicitFile, Type: "explicit"}},
	)
	path, err := got()
	require.NoError(t, err)
	assert.Equal(t, explicitFile, path)
}

func TestResolveTokenPersistenceSource_PicksHighestSourceWithOAuthFields(t *testing.T) {
	dir := t.TempDir()
	systemFile := filepath.Join(dir, "system.yaml")
	userFile := filepath.Join(dir, "user.yaml")
	localFile := filepath.Join(dir, "local.yaml")

	require.NoError(t, os.WriteFile(systemFile, []byte(`
contexts:
  default:
    grafana:
      oauth-token: gat_sys
`), 0o600))
	require.NoError(t, os.WriteFile(userFile, []byte(`
contexts:
  default:
    grafana:
      oauth-token: gat_user
`), 0o600))
	require.NoError(t, os.WriteFile(localFile, []byte(`
contexts:
  default:
    grafana:
      oauth-token: gat_local
`), 0o600))

	got := config.ResolveTokenPersistenceSource(
		t.Context(),
		config.StandardLocation(),
		"default",
		[]config.ConfigSource{
			{Path: systemFile, Type: "system"},
			{Path: userFile, Type: "user"},
			{Path: localFile, Type: "local"},
		},
	)
	path, err := got()
	require.NoError(t, err)
	assert.Equal(t, localFile, path)
}

func TestResolveTokenPersistenceSource_FallsBackToUserWhenContextNotFound(t *testing.T) {
	dir := t.TempDir()
	userFile := filepath.Join(dir, "user.yaml")
	localFile := filepath.Join(dir, "local.yaml")

	require.NoError(t, os.WriteFile(userFile, []byte("contexts:\n  other: {}\n"), 0o600))
	require.NoError(t, os.WriteFile(localFile, []byte("contexts:\n  other: {}\n"), 0o600))

	got := config.ResolveTokenPersistenceSource(
		t.Context(),
		config.StandardLocation(),
		"default",
		[]config.ConfigSource{
			{Path: userFile, Type: "user"},
			{Path: localFile, Type: "local"},
		},
	)
	path, err := got()
	require.NoError(t, err)
	assert.Equal(t, userFile, path)
}

func TestWireTokenPersistence_ExplicitModeWritesToExplicitSource(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/cli/v1/auth/refresh":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": map[string]any{
					"token":              "gat_explicit_new",
					"expires_at":         "2099-01-01T00:00:00Z",
					"refresh_token":      "gar_explicit_new",
					"refresh_expires_at": "2099-02-01T00:00:00Z",
				},
			})
		case "/bootdata":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"user": map[string]any{"orgId": 1},
			})
		default:
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer srv.Close()

	dir := t.TempDir()
	explicitFile := filepath.Join(dir, "explicit.yaml")
	userFile := filepath.Join(dir, "user.yaml")
	localFile := filepath.Join(dir, "local.yaml")

	writeTestConfigFile(t, explicitFile, `
contexts:
  default:
    grafana:
      server: "`+srv.URL+`"
      proxy-endpoint: "`+srv.URL+`"
      oauth-token: gat_old
      oauth-refresh-token: gar_old
      oauth-token-expires-at: "2020-01-01T00:00:00Z"
      oauth-refresh-expires-at: "2099-01-01T00:00:00Z"
      stack-id: 1
current-context: default
`)
	writeTestConfigFile(t, userFile, `
contexts:
  default:
    grafana:
      oauth-token: gat_user
current-context: default
`)
	writeTestConfigFile(t, localFile, `
contexts:
  default:
    grafana:
      oauth-token: gat_local
`)

	restCfg, _ := config.NewNamespacedRESTConfig(t.Context(), config.Context{
		Grafana: &config.GrafanaConfig{
			Server:                srv.URL,
			ProxyEndpoint:         srv.URL,
			OAuthToken:            "gat_old",
			OAuthRefreshToken:     "gar_old",
			OAuthTokenExpiresAt:   "2020-01-01T00:00:00Z",
			OAuthRefreshExpiresAt: "2099-01-01T00:00:00Z",
			StackID:               1,
		},
	})
	restCfg.WireTokenPersistence(
		t.Context(),
		config.ExplicitConfigFile(explicitFile),
		"default",
		[]config.ConfigSource{
			{Path: explicitFile, Type: "explicit"},
			{Path: userFile, Type: "user"},
			{Path: localFile, Type: "local"},
		},
	)

	client := &http.Client{Transport: restCfg.WrapTransport(http.DefaultTransport)}
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, srv.URL+"/test", nil)
	require.NoError(t, err)
	resp, err := client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	explicitRaw, err := os.ReadFile(explicitFile)
	require.NoError(t, err)
	explicitContents := string(explicitRaw)
	assert.Contains(t, explicitContents, "gat_explicit_new")
	assert.Contains(t, explicitContents, "gar_explicit_new")

	userRaw, err := os.ReadFile(userFile)
	require.NoError(t, err)
	assert.NotContains(t, string(userRaw), "gat_explicit_new")
	assert.NotContains(t, string(userRaw), "gar_explicit_new")

	localRaw, err := os.ReadFile(localFile)
	require.NoError(t, err)
	assert.NotContains(t, string(localRaw), "gat_explicit_new")
	assert.NotContains(t, string(localRaw), "gar_explicit_new")
}

// refresher returns the OnRefresh callback wired by WireTokenPersistence, so
// tests can invoke persistence directly without a full HTTP round-trip.
func refresher(t *testing.T, rc config.NamespacedRESTConfig) func(token, refreshToken, expiresAt, refreshExpiresAt string) error {
	t.Helper()
	fn := rc.OnRefreshForTest()
	require.NotNil(t, fn, "expected WireTokenPersistence to install an OnRefresh callback")
	return fn
}

// Bug 1 — WireTokenPersistence must complete its Load/Write even after the
// command context that built the REST config is cancelled. Otherwise a
// rotated refresh token is issued by the server but never written to disk,
// leaving the user locked out on the next invocation.
func TestWireTokenPersistence_WritesAfterContextCancelled(t *testing.T) {
	dir := t.TempDir()
	explicitFile := filepath.Join(dir, "explicit.yaml")
	writeTestConfigFile(t, explicitFile, `
contexts:
  default:
    grafana:
      server: https://example.invalid
      proxy-endpoint: https://example.invalid
      oauth-token: gat_old
      oauth-refresh-token: gar_old
      oauth-token-expires-at: "2020-01-01T00:00:00Z"
      oauth-refresh-expires-at: "2099-01-01T00:00:00Z"
      stack-id: 1
current-context: default
`)

	ctx, cancel := context.WithCancel(t.Context())
	restCfg, _ := config.NewNamespacedRESTConfig(ctx, config.Context{
		Grafana: &config.GrafanaConfig{
			Server:                "https://example.invalid",
			ProxyEndpoint:         "https://example.invalid",
			OAuthToken:            "gat_old",
			OAuthRefreshToken:     "gar_old",
			OAuthTokenExpiresAt:   "2020-01-01T00:00:00Z",
			OAuthRefreshExpiresAt: "2099-01-01T00:00:00Z",
			StackID:               1,
		},
	})
	restCfg.WireTokenPersistence(
		ctx,
		config.ExplicitConfigFile(explicitFile),
		"default",
		[]config.ConfigSource{{Path: explicitFile, Type: "explicit"}},
	)
	cancel()

	err := refresher(t, restCfg)(
		"gat_rotated",
		"gar_rotated",
		"2099-01-01T00:00:00Z",
		"2099-02-01T00:00:00Z",
	)
	require.NoError(t, err, "OnRefresh must not fail when the request ctx is cancelled")

	raw, err := os.ReadFile(explicitFile)
	require.NoError(t, err)
	assert.Contains(t, string(raw), "gat_rotated")
	assert.Contains(t, string(raw), "gar_rotated")
}

// Bug 2 — Two concurrent gcx invocations must not both consume the same
// refresh token. The first to acquire the lock refreshes; the second should
// observe the freshly-written tokens on disk and adopt them without calling
// the refresh endpoint a second time.
func TestWireTokenPersistence_ConcurrentRefreshesSerializeViaFileLock(t *testing.T) {
	var refreshCalls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/cli/v1/auth/refresh" {
			w.WriteHeader(http.StatusOK)
			return
		}
		n := refreshCalls.Add(1)
		if n > 1 {
			// Proxy-style rotation: second caller presents a now-consumed refresh token.
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"statusCode":401,"message":"invalid or expired refresh token"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{
				"token":              "gat_new",
				"expires_at":         "2099-01-01T00:00:00Z",
				"refresh_token":      "gar_new",
				"refresh_expires_at": "2099-02-01T00:00:00Z",
			},
		})
	}))
	defer srv.Close()

	dir := t.TempDir()
	file := filepath.Join(dir, "config.yaml")
	writeTestConfigFile(t, file, `
contexts:
  default:
    grafana:
      server: "`+srv.URL+`"
      proxy-endpoint: "`+srv.URL+`"
      oauth-token: gat_old
      oauth-refresh-token: gar_old
      oauth-token-expires-at: "2020-01-01T00:00:00Z"
      oauth-refresh-expires-at: "2099-01-01T00:00:00Z"
      stack-id: 1
current-context: default
`)

	newTransport := func() *http.Client {
		cfg, err := config.Load(t.Context(), config.ExplicitConfigFile(file))
		require.NoError(t, err)
		rc, _ := config.NewNamespacedRESTConfig(t.Context(), *cfg.Contexts["default"])
		rc.WireTokenPersistence(
			t.Context(),
			config.ExplicitConfigFile(file),
			"default",
			[]config.ConfigSource{{Path: file, Type: "explicit"}},
		)
		return &http.Client{Transport: rc.WrapTransport(http.DefaultTransport)}
	}

	var wg sync.WaitGroup
	var errs [2]error
	for i := range errs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			c := newTransport()
			req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, srv.URL+"/test", nil)
			if err != nil {
				errs[i] = err
				return
			}
			resp, err := c.Do(req)
			if resp != nil {
				_ = resp.Body.Close()
			}
			errs[i] = err
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		require.NoError(t, err, "process %d should not fail", i)
	}
	raw, err := os.ReadFile(file)
	require.NoError(t, err)
	assert.Contains(t, string(raw), "gat_new")
	assert.Contains(t, string(raw), "gar_new")
}

// Bug 5 — Tokens persisted in one "invocation" must be re-loadable and usable
// for the next. Simulates two sequential gcx invocations sharing a config file.
func TestWireTokenPersistence_RoundTripAcrossInvocations(t *testing.T) {
	var refreshCalls atomic.Int32
	var presentedRefresh atomic.Value // string
	presentedRefresh.Store("")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/cli/v1/auth/refresh" {
			w.WriteHeader(http.StatusOK)
			return
		}
		refreshCalls.Add(1)
		var body struct {
			RefreshToken string `json:"refresh_token"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		presentedRefresh.Store(body.RefreshToken)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{
				"token":              "gat_rotated",
				"expires_at":         "2020-01-01T00:00:00Z", // still stale so a second invocation re-refreshes
				"refresh_token":      "gar_rotated",
				"refresh_expires_at": "2099-02-01T00:00:00Z",
			},
		})
	}))
	defer srv.Close()

	dir := t.TempDir()
	file := filepath.Join(dir, "config.yaml")
	writeTestConfigFile(t, file, `
contexts:
  default:
    grafana:
      server: "`+srv.URL+`"
      proxy-endpoint: "`+srv.URL+`"
      oauth-token: gat_old
      oauth-refresh-token: gar_old
      oauth-token-expires-at: "2020-01-01T00:00:00Z"
      oauth-refresh-expires-at: "2099-01-01T00:00:00Z"
      stack-id: 1
current-context: default
`)

	runInvocation := func() {
		cfg, err := config.Load(t.Context(), config.ExplicitConfigFile(file))
		require.NoError(t, err)
		rc, _ := config.NewNamespacedRESTConfig(t.Context(), *cfg.Contexts["default"])
		rc.WireTokenPersistence(
			t.Context(),
			config.ExplicitConfigFile(file),
			"default",
			[]config.ConfigSource{{Path: file, Type: "explicit"}},
		)
		c := &http.Client{Transport: rc.WrapTransport(http.DefaultTransport)}
		req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, srv.URL+"/test", nil)
		require.NoError(t, err)
		resp, err := c.Do(req)
		require.NoError(t, err)
		_ = resp.Body.Close()
	}

	runInvocation() // refresh with gar_old
	assert.Equal(t, "gar_old", presentedRefresh.Load())

	runInvocation() // second invocation must present the rotated gar_rotated
	assert.Equal(t, "gar_rotated", presentedRefresh.Load(), "second invocation must load the rotated refresh token from disk")
}
