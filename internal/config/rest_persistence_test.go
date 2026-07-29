package config_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/grafana/gcx/internal/auth"
	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/credentials"
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

func TestResolveTokenPersistenceSource_PicksHighestAtomicStackOwner(t *testing.T) {
	dir := t.TempDir()
	systemFile := filepath.Join(dir, "system.yaml")
	userFile := filepath.Join(dir, "user.yaml")
	localFile := filepath.Join(dir, "local.yaml")

	require.NoError(t, os.WriteFile(systemFile, []byte(`
version: 1
stacks:
  default:
    grafana:
      oauth-token: gat_sys
contexts:
  default:
    stack: default
`), 0o600))
	require.NoError(t, os.WriteFile(userFile, []byte(`
version: 1
stacks:
  default:
    grafana:
      oauth-token: gat_user
contexts:
  default:
    stack: default
`), 0o600))
	require.NoError(t, os.WriteFile(localFile, []byte(`
version: 1
stacks:
  default:
    grafana:
      server: https://local-owner.example.invalid
      token: glsa_local
contexts:
  default:
    stack: default
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

func TestResolveTokenPersistenceSource_FallsBackToUserWhenStackNotFound(t *testing.T) {
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

func TestResolveTokenPersistenceSource_FailsClosedOnMalformedHigherLayer(t *testing.T) {
	dir := t.TempDir()
	userFile := filepath.Join(dir, "user.yaml")
	localFile := filepath.Join(dir, "local.yaml")
	writeTestConfigFile(t, userFile, `
version: 1
stacks:
  default:
    grafana:
      oauth-token: gat_user
contexts:
  default:
    stack: default
`)
	require.NoError(t, os.WriteFile(localFile, []byte("version: [malformed\n"), 0o600))

	resolved := config.ResolveTokenPersistenceSource(
		t.Context(),
		config.StandardLocation(),
		"default",
		[]config.ConfigSource{
			{Path: userFile, Type: "user"},
			{Path: localFile, Type: "local"},
		},
	)
	_, err := resolved()
	require.ErrorContains(t, err, "rescan OAuth persistence source")
}

func TestWireTokenPersistence_MissingOwnerFailsBeforeNetworkRefresh(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "config.yaml")
	writeTestConfigFile(t, file, `
version: 1
stacks:
  default:
    grafana:
      server: https://grafana.invalid
      proxy-endpoint: https://proxy.invalid
      oauth-token: gat_old
      oauth-refresh-token: gar_old
      oauth-token-expires-at: "2020-01-01T00:00:00Z"
      oauth-refresh-expires-at: "2099-01-01T00:00:00Z"
contexts:
  default:
    stack: default
current-context: default
`)
	cfg, err := config.Load(t.Context(), config.ExplicitConfigFile(file))
	require.NoError(t, err)
	restCfg, err := config.NewNamespacedRESTConfig(t.Context(), *cfg.Contexts["default"])
	require.NoError(t, err)
	restCfg.WireTokenPersistence(
		t.Context(),
		config.ExplicitConfigFile(file),
		"default",
		"default",
		[]config.ConfigSource{{Path: file, Type: "explicit"}},
	)
	writeTestConfigFile(t, file, `
version: 1
contexts:
  default: {}
current-context: default
`)

	_, err = restCfg.FreshOAuthToken(t.Context())
	require.ErrorContains(t, err, "credential owner \"default\" disappeared")
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
version: 1
stacks:
  default:
    grafana:
      server: "`+srv.URL+`"
      proxy-endpoint: "`+srv.URL+`"
      oauth-token: gat_old
      oauth-refresh-token: gar_old
      oauth-token-expires-at: "2020-01-01T00:00:00Z"
      oauth-refresh-expires-at: "2099-01-01T00:00:00Z"
      stack-id: 1
contexts:
  default:
    stack: default
current-context: default
`)
	writeTestConfigFile(t, userFile, `
version: 1
stacks:
  default:
    grafana:
      oauth-token: gat_user
contexts:
  default:
    stack: default
current-context: default
`)
	writeTestConfigFile(t, localFile, `
version: 1
stacks:
  default:
    grafana:
      oauth-token: gat_local
contexts:
  default:
    stack: default
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
func refresher(t *testing.T, rc config.NamespacedRESTConfig) func(previousRefreshToken, token, refreshToken, expiresAt, refreshExpiresAt string) error {
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
version: 1
stacks:
  default:
    grafana:
      server: https://example.invalid
      proxy-endpoint: https://example.invalid
      oauth-token: gat_old
      oauth-refresh-token: gar_old
      oauth-token-expires-at: "2020-01-01T00:00:00Z"
      oauth-refresh-expires-at: "2099-01-01T00:00:00Z"
      stack-id: 1
contexts:
  default:
    stack: default
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
		"default",
		[]config.ConfigSource{{Path: explicitFile, Type: "explicit"}},
	)
	cancel()

	err := refresher(t, restCfg)(
		"gar_old",
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

	// Retrying after a write that committed but reported uncertain durability
	// is an idempotent success only when every persisted field is the exact new
	// generation.
	err = refresher(t, restCfg)(
		"gar_old",
		"gat_rotated",
		"gar_rotated",
		"2099-01-01T00:00:00Z",
		"2099-02-01T00:00:00Z",
	)
	require.NoError(t, err)
	idempotentRaw, err := os.ReadFile(explicitFile)
	require.NoError(t, err)
	assert.Equal(t, raw, idempotentRaw)
}

func TestWireTokenPersistence_PendingGenerationCannotOverwriteRelogin(t *testing.T) {
	var refreshCalls, protectedCalls atomic.Int32
	var protectedAuthorization atomic.Value
	protectedAuthorization.Store("")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/cli/v1/auth/refresh" {
			refreshCalls.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": map[string]any{
					"token":              "gat_pending",
					"expires_at":         "2099-01-01T00:00:00Z",
					"refresh_token":      "gar_pending",
					"refresh_expires_at": "2099-02-01T00:00:00Z",
				},
			})
			return
		}
		protectedCalls.Add(1)
		protectedAuthorization.Store(r.Header.Get("Authorization"))
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	dir := t.TempDir()
	file := filepath.Join(dir, "config.yaml")
	writeGeneration := func(token, refreshToken, expiresAt string) {
		writeTestConfigFile(t, file, `
version: 1
stacks:
  default:
    grafana:
      server: "`+server.URL+`"
      proxy-endpoint: "`+server.URL+`"
      oauth-token: `+token+`
      oauth-refresh-token: `+refreshToken+`
      oauth-token-expires-at: "`+expiresAt+`"
      oauth-refresh-expires-at: "2099-02-01T00:00:00Z"
      stack-id: 1
contexts:
  default:
    stack: default
current-context: default
`)
	}
	writeGeneration("gat_old", "gar_old", "2020-01-01T00:00:00Z")

	cfg, err := config.Load(t.Context(), config.ExplicitConfigFile(file))
	require.NoError(t, err)
	restCfg, err := config.NewNamespacedRESTConfig(t.Context(), *cfg.Contexts["default"])
	require.NoError(t, err)
	restCfg.WireTokenPersistence(
		t.Context(),
		config.ExplicitConfigFile(file),
		"default",
		"default",
		[]config.ConfigSource{{Path: file, Type: "explicit"}},
	)
	realPersist := restCfg.OnRefreshForTest()
	require.NotNil(t, realPersist)
	var persistCalls atomic.Int32
	restCfg.SetOnRefresh(func(previousRefreshToken, token, refreshToken, expiresAt, refreshExpiresAt string) error {
		if persistCalls.Add(1) == 1 {
			return errors.New("injected pre-write persistence failure")
		}
		return realPersist(previousRefreshToken, token, refreshToken, expiresAt, refreshExpiresAt)
	})

	client := &http.Client{Transport: restCfg.WrapTransport(http.DefaultTransport)}
	request := func() error {
		req, reqErr := http.NewRequestWithContext(t.Context(), http.MethodGet, server.URL+"/protected", nil)
		if reqErr != nil {
			return reqErr
		}
		resp, reqErr := client.Do(req)
		if resp != nil {
			_ = resp.Body.Close()
		}
		return reqErr
	}

	err = request()
	require.ErrorContains(t, err, "injected pre-write persistence failure")
	assert.Equal(t, int32(1), refreshCalls.Load())
	assert.Zero(t, protectedCalls.Load())

	// A new browser login wins while the failed rotated generation is pending.
	writeGeneration("gat_relogin", "gar_relogin", "2099-03-01T00:00:00Z")
	err = request()
	require.Error(t, err)
	require.ErrorIs(t, err, auth.ErrTokenGenerationChanged)
	assert.Equal(t, int32(1), refreshCalls.Load(), "CAS failure must not rotate again")
	assert.Zero(t, protectedCalls.Load())
	raw, readErr := os.ReadFile(file)
	require.NoError(t, readErr)
	assert.Contains(t, string(raw), "gat_relogin")
	assert.Contains(t, string(raw), "gar_relogin")
	assert.NotContains(t, string(raw), "gat_pending")

	// The conflicting pending generation is discarded. The next call reloads
	// and adopts the fresh re-login generation before touching the API.
	require.NoError(t, request())
	assert.Equal(t, int32(1), refreshCalls.Load())
	assert.Equal(t, int32(1), protectedCalls.Load())
	assert.Equal(t, "Bearer gat_relogin", protectedAuthorization.Load())
}

func TestWireTokenPersistence_KeychainUnavailableRetainsPendingGeneration(t *testing.T) {
	store := withFakeStore(t)
	var refreshCalls, protectedCalls atomic.Int32
	var protectedAuthorization atomic.Value
	protectedAuthorization.Store("")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/cli/v1/auth/refresh" {
			refreshCalls.Add(1)
			// Rotation has happened, but the keychain becomes unavailable before
			// the persistence callback can verify the old generation.
			store.setGetErr(credentials.ErrUnavailable)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": map[string]any{
					"token":              "gat_pending",
					"expires_at":         "2099-01-01T00:00:00Z",
					"refresh_token":      "gar_pending",
					"refresh_expires_at": "2099-02-01T00:00:00Z",
				},
			})
			return
		}
		protectedCalls.Add(1)
		protectedAuthorization.Store(r.Header.Get("Authorization"))
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	dir := t.TempDir()
	file := filepath.Join(dir, "config.yaml")
	writeTestConfigFile(t, file, `
version: 1
stacks:
  default:
    grafana:
      server: "`+server.URL+`"
      proxy-endpoint: "`+server.URL+`"
      oauth-token: gat_old
      oauth-refresh-token: gar_old
      oauth-token-expires-at: "2020-01-01T00:00:00Z"
      oauth-refresh-expires-at: "2099-01-01T00:00:00Z"
      stack-id: 1
contexts:
  default:
    stack: default
current-context: default
`)

	cfg, err := config.Load(t.Context(), config.ExplicitConfigFile(file))
	require.NoError(t, err)
	require.Equal(t, "gat_old", cfg.Contexts["default"].Grafana.OAuthToken)
	require.Equal(t, "gar_old", cfg.Contexts["default"].Grafana.OAuthRefreshToken)
	restCfg, err := config.NewNamespacedRESTConfig(t.Context(), *cfg.Contexts["default"])
	require.NoError(t, err)
	restCfg.WireTokenPersistence(
		t.Context(),
		config.ExplicitConfigFile(file),
		"default",
		"default",
		[]config.ConfigSource{{Path: file, Type: "explicit"}},
	)
	client := &http.Client{Transport: restCfg.WrapTransport(http.DefaultTransport)}
	request := func() error {
		req, reqErr := http.NewRequestWithContext(t.Context(), http.MethodGet, server.URL+"/protected", nil)
		if reqErr != nil {
			return reqErr
		}
		resp, reqErr := client.Do(req)
		if resp != nil {
			_ = resp.Body.Close()
		}
		return reqErr
	}

	err = request()
	require.ErrorIs(t, err, credentials.ErrUnavailable)
	require.NotErrorIs(t, err, auth.ErrTokenGenerationChanged)
	assert.Equal(t, int32(1), refreshCalls.Load())
	assert.Zero(t, protectedCalls.Load(), "unpersisted tokens must not reach the protected API")

	store.setGetErr(nil)
	require.NoError(t, request())
	assert.Equal(t, int32(1), refreshCalls.Load(), "pending persistence retry must not rotate again")
	assert.Equal(t, int32(1), protectedCalls.Load())
	assert.Equal(t, "Bearer gat_pending", protectedAuthorization.Load())

	reloaded, err := config.Load(t.Context(), config.ExplicitConfigFile(file))
	require.NoError(t, err)
	assert.Equal(t, "gat_pending", reloaded.Contexts["default"].Grafana.OAuthToken)
	assert.Equal(t, "gar_pending", reloaded.Contexts["default"].Grafana.OAuthRefreshToken)
}

func TestWireTokenPersistence_KeychainEntryMissingRetainsPendingGeneration(t *testing.T) {
	store := withFakeStore(t)
	var refreshCalls, protectedCalls atomic.Int32
	var removedAccount string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/cli/v1/auth/refresh" {
			refreshCalls.Add(1)
			// The proxy has consumed the old refresh generation. Simulate the
			// corresponding keychain item disappearing before persistence reloads
			// the unchanged sentinel from disk.
			store.mu.Lock()
			for account, value := range store.entries {
				if value == "gar_old" {
					removedAccount = account
					delete(store.entries, account)
					break
				}
			}
			store.mu.Unlock()
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": map[string]any{
					"token":              "gat_pending",
					"expires_at":         "2099-01-01T00:00:00Z",
					"refresh_token":      "gar_pending",
					"refresh_expires_at": "2099-02-01T00:00:00Z",
				},
			})
			return
		}
		protectedCalls.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	file := filepath.Join(t.TempDir(), "config.yaml")
	writeTestConfigFile(t, file, `
version: 1
stacks:
  default:
    grafana:
      server: "`+server.URL+`"
      proxy-endpoint: "`+server.URL+`"
      oauth-token: gat_old
      oauth-refresh-token: gar_old
      oauth-token-expires-at: "2020-01-01T00:00:00Z"
      oauth-refresh-expires-at: "2099-01-01T00:00:00Z"
      stack-id: 1
contexts:
  default:
    stack: default
current-context: default
`)

	cfg, err := config.Load(t.Context(), config.ExplicitConfigFile(file))
	require.NoError(t, err)
	restCfg, err := config.NewNamespacedRESTConfig(t.Context(), *cfg.Contexts["default"])
	require.NoError(t, err)
	restCfg.WireTokenPersistence(
		t.Context(),
		config.ExplicitConfigFile(file),
		"default",
		"default",
		[]config.ConfigSource{{Path: file, Type: "explicit"}},
	)
	client := &http.Client{Transport: restCfg.WrapTransport(http.DefaultTransport)}
	request := func() error {
		req, reqErr := http.NewRequestWithContext(t.Context(), http.MethodGet, server.URL+"/protected", nil)
		require.NoError(t, reqErr)
		resp, reqErr := client.Do(req)
		if resp != nil {
			_ = resp.Body.Close()
		}
		return reqErr
	}

	err = request()
	require.ErrorIs(t, err, credentials.ErrNotFound)
	require.NotErrorIs(t, err, auth.ErrTokenGenerationChanged)
	assert.NotEmpty(t, removedAccount)
	assert.Equal(t, int32(1), refreshCalls.Load())
	assert.Zero(t, protectedCalls.Load())

	require.NoError(t, store.Set(removedAccount, "gar_old"))
	require.NoError(t, request())
	assert.Equal(t, int32(1), refreshCalls.Load(), "pending persistence retry must not rotate again")
	assert.Equal(t, int32(1), protectedCalls.Load())

	reloaded, err := config.Load(t.Context(), config.ExplicitConfigFile(file))
	require.NoError(t, err)
	assert.Equal(t, "gat_pending", reloaded.Contexts["default"].Grafana.OAuthToken)
	assert.Equal(t, "gar_pending", reloaded.Contexts["default"].Grafana.OAuthRefreshToken)
}

func TestWireTokenPersistence_TLSFileSwapRejectsRotatedGeneration(t *testing.T) {
	var refreshCalls, protectedCalls atomic.Int32
	caFile := filepath.Join(t.TempDir(), "ca.pem")
	require.NoError(t, os.WriteFile(caFile, []byte("initial-ca-material"), 0o600))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/cli/v1/auth/refresh" {
			refreshCalls.Add(1)
			// The request used the TLS material captured with the original OAuth
			// generation. Swap the file before persistence reloads the destination.
			if err := os.WriteFile(caFile, []byte("replacement-ca-material"), 0o600); err != nil {
				t.Errorf("replace CA material: %v", err)
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": map[string]any{
					"token":              "gat_pending",
					"expires_at":         "2099-01-01T00:00:00Z",
					"refresh_token":      "gar_pending",
					"refresh_expires_at": "2099-02-01T00:00:00Z",
				},
			})
			return
		}
		protectedCalls.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	file := filepath.Join(t.TempDir(), "config.yaml")
	writeTestConfigFile(t, file, `
version: 1
stacks:
  default:
    grafana:
      server: "`+server.URL+`"
      proxy-endpoint: "`+server.URL+`"
      oauth-token: gat_old
      oauth-refresh-token: gar_old
      oauth-token-expires-at: "2020-01-01T00:00:00Z"
      oauth-refresh-expires-at: "2099-01-01T00:00:00Z"
      stack-id: 1
      tls:
        ca-file: "`+caFile+`"
contexts:
  default:
    stack: default
current-context: default
`)

	cfg, err := config.Load(t.Context(), config.ExplicitConfigFile(file))
	require.NoError(t, err)
	restCfg, err := config.NewNamespacedRESTConfig(t.Context(), *cfg.Contexts["default"])
	require.NoError(t, err)
	restCfg.WireTokenPersistence(
		t.Context(),
		config.ExplicitConfigFile(file),
		"default",
		"default",
		[]config.ConfigSource{{Path: file, Type: "explicit"}},
	)
	client := &http.Client{Transport: restCfg.WrapTransport(http.DefaultTransport)}
	request := func() error {
		req, reqErr := http.NewRequestWithContext(t.Context(), http.MethodGet, server.URL+"/protected", nil)
		require.NoError(t, reqErr)
		resp, reqErr := client.Do(req)
		if resp != nil {
			_ = resp.Body.Close()
		}
		return reqErr
	}

	for range 2 {
		err = request()
		require.ErrorContains(t, err, "OAuth credential destination")
		require.ErrorContains(t, err, "changed")
	}
	assert.Equal(t, int32(1), refreshCalls.Load(), "pending generation must not trigger another refresh")
	assert.Zero(t, protectedCalls.Load())
	raw, err := os.ReadFile(file)
	require.NoError(t, err)
	assert.Contains(t, string(raw), "gar_old")
	assert.NotContains(t, string(raw), "gar_pending")
}

func TestWireTokenPersistence_InvalidRefreshPersistsRecoveryGeneration(t *testing.T) {
	_ = withFakeStore(t)
	var refreshCalls, protectedCalls atomic.Int32
	var presentedRefresh, protectedAuthorization atomic.Value
	presentedRefresh.Store("")
	protectedAuthorization.Store("")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/cli/v1/auth/refresh" {
			var body struct {
				RefreshToken string `json:"refresh_token"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			presentedRefresh.Store(body.RefreshToken)
			switch refreshCalls.Add(1) {
			case 1:
				// The server rotated the refresh generation but omitted the access
				// token and its expiry. Persist the replacement fail-closed.
				_ = json.NewEncoder(w).Encode(map[string]any{
					"data": map[string]any{
						"token":              "",
						"expires_at":         "",
						"refresh_token":      "gar_recovery",
						"refresh_expires_at": "2099-02-01T00:00:00Z",
					},
				})
			case 2:
				_ = json.NewEncoder(w).Encode(map[string]any{
					"data": map[string]any{
						"token":              "gat_final",
						"expires_at":         "2099-03-01T00:00:00Z",
						"refresh_token":      "gar_final",
						"refresh_expires_at": "2099-04-01T00:00:00Z",
					},
				})
			default:
				w.WriteHeader(http.StatusInternalServerError)
			}
			return
		}
		protectedCalls.Add(1)
		protectedAuthorization.Store(r.Header.Get("Authorization"))
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	dir := t.TempDir()
	file := filepath.Join(dir, "config.yaml")
	writeTestConfigFile(t, file, `
version: 1
stacks:
  default:
    grafana:
      server: "`+server.URL+`"
      proxy-endpoint: "`+server.URL+`"
      oauth-token: gat_old
      oauth-refresh-token: gar_old
      oauth-token-expires-at: "2020-01-01T00:00:00Z"
      oauth-refresh-expires-at: "2099-01-01T00:00:00Z"
      stack-id: 1
contexts:
  default:
    stack: default
current-context: default
`)
	newClient := func() (*http.Client, config.Config) {
		cfg, err := config.Load(t.Context(), config.ExplicitConfigFile(file))
		require.NoError(t, err)
		restCfg, err := config.NewNamespacedRESTConfig(t.Context(), *cfg.Contexts["default"])
		require.NoError(t, err)
		restCfg.WireTokenPersistence(
			t.Context(),
			config.ExplicitConfigFile(file),
			"default",
			"default",
			[]config.ConfigSource{{Path: file, Type: "explicit"}},
		)
		return &http.Client{Transport: restCfg.WrapTransport(http.DefaultTransport)}, cfg
	}
	request := func(client *http.Client) error {
		req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, server.URL+"/protected", nil)
		require.NoError(t, err)
		resp, err := client.Do(req)
		if resp != nil {
			_ = resp.Body.Close()
		}
		return err
	}

	firstClient, _ := newClient()
	require.ErrorIs(t, request(firstClient), auth.ErrInvalidRefreshResponse)
	assert.Equal(t, "gar_old", presentedRefresh.Load())
	assert.Equal(t, int32(1), refreshCalls.Load())
	assert.Zero(t, protectedCalls.Load())

	// The malformed generation remains terminal for this transport; it cannot
	// consume the newly persisted refresh token in a blind retry loop.
	require.ErrorIs(t, request(firstClient), auth.ErrInvalidRefreshResponse)
	assert.Equal(t, int32(1), refreshCalls.Load())
	assert.Zero(t, protectedCalls.Load())

	secondClient, recovered := newClient()
	assert.Equal(t, "gat_old", recovered.Contexts["default"].Grafana.OAuthToken)
	assert.Equal(t, "gar_recovery", recovered.Contexts["default"].Grafana.OAuthRefreshToken)
	recoveryExpiry, err := time.Parse(time.RFC3339, recovered.Contexts["default"].Grafana.OAuthTokenExpiresAt)
	require.NoError(t, err)
	assert.True(t, recoveryExpiry.Before(time.Now()))

	// A fresh transport can recover using only the persisted replacement
	// generation and then reach the protected API with a valid access token.
	require.NoError(t, request(secondClient))
	assert.Equal(t, "gar_recovery", presentedRefresh.Load())
	assert.Equal(t, int32(2), refreshCalls.Load())
	assert.Equal(t, int32(1), protectedCalls.Load())
	assert.Equal(t, "Bearer gat_final", protectedAuthorization.Load())
}

func TestWireTokenPersistence_BlankExpiriesRemainUsableAcrossInvocations(t *testing.T) {
	_ = withFakeStore(t)
	var refreshCalls, protectedCalls atomic.Int32
	var protectedAuthorizations []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/cli/v1/auth/refresh" {
			if refreshCalls.Add(1) > 1 {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": map[string]any{
					"token":              "gat_rotated",
					"expires_at":         "  \t",
					"refresh_token":      "gar_rotated",
					"refresh_expires_at": " \t ",
				},
			})
			return
		}
		protectedCalls.Add(1)
		protectedAuthorizations = append(protectedAuthorizations, r.Header.Get("Authorization"))
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	dir := t.TempDir()
	file := filepath.Join(dir, "config.yaml")
	writeTestConfigFile(t, file, `
version: 1
stacks:
  default:
    grafana:
      server: "`+srv.URL+`"
      proxy-endpoint: "`+srv.URL+`"
      oauth-token: gat_old
      oauth-refresh-token: gar_old
      oauth-token-expires-at: "2020-01-01T00:00:00Z"
      oauth-refresh-expires-at: "2099-01-01T00:00:00Z"
      stack-id: 1
contexts:
  default:
    stack: default
current-context: default
`)

	newClient := func() (*http.Client, config.Config) {
		cfg, err := config.Load(t.Context(), config.ExplicitConfigFile(file))
		require.NoError(t, err)
		restCfg, err := config.NewNamespacedRESTConfig(t.Context(), *cfg.Contexts["default"])
		require.NoError(t, err)
		restCfg.WireTokenPersistence(
			t.Context(),
			config.ExplicitConfigFile(file),
			"default",
			"default",
			[]config.ConfigSource{{Path: file, Type: "explicit"}},
		)
		return &http.Client{Transport: restCfg.WrapTransport(http.DefaultTransport)}, cfg
	}
	request := func(client *http.Client) {
		req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, srv.URL+"/protected", nil)
		require.NoError(t, err)
		resp, err := client.Do(req)
		require.NoError(t, err)
		require.NoError(t, resp.Body.Close())
	}

	first, _ := newClient()
	request(first)
	assert.Equal(t, int32(1), refreshCalls.Load())
	assert.Equal(t, int32(1), protectedCalls.Load())

	second, persisted := newClient()
	assert.Equal(t, "gat_rotated", persisted.Contexts["default"].Grafana.OAuthToken)
	assert.Equal(t, "gar_rotated", persisted.Contexts["default"].Grafana.OAuthRefreshToken)
	assert.Empty(t, persisted.Contexts["default"].Grafana.OAuthTokenExpiresAt)
	assert.Empty(t, persisted.Contexts["default"].Grafana.OAuthRefreshExpiresAt)
	request(second)

	assert.Equal(t, int32(1), refreshCalls.Load(), "fresh invocation must not immediately refresh an unknown-expiry token")
	assert.Equal(t, int32(2), protectedCalls.Load())
	assert.Equal(t, []string{"Bearer gat_rotated", "Bearer gat_rotated"}, protectedAuthorizations)
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
version: 1
stacks:
  default:
    grafana:
      server: "`+srv.URL+`"
      proxy-endpoint: "`+srv.URL+`"
      oauth-token: gat_old
      oauth-refresh-token: gar_old
      oauth-token-expires-at: "2020-01-01T00:00:00Z"
      oauth-refresh-expires-at: "2099-01-01T00:00:00Z"
      stack-id: 1
contexts:
  default:
    stack: default
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
			"default",
			[]config.ConfigSource{{Path: file, Type: "explicit"}},
		)
		return &http.Client{Transport: rc.WrapTransport(http.DefaultTransport)}
	}
	// Snapshot both transports before either request can refresh. Constructing a
	// transport inside each goroutine would let the second one simply load the
	// already-rotated file and would not exercise lock/reload coordination.
	clients := [2]*http.Client{newTransport(), newTransport()}

	var wg sync.WaitGroup
	var errs [2]error
	start := make(chan struct{})
	for i := range errs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, srv.URL+"/test", nil)
			if err != nil {
				errs[i] = err
				return
			}
			resp, err := clients[i].Do(req)
			if resp != nil {
				_ = resp.Body.Close()
			}
			errs[i] = err
		}(i)
	}
	close(start)
	wg.Wait()

	for i, err := range errs {
		require.NoError(t, err, "process %d should not fail", i)
	}
	raw, err := os.ReadFile(file)
	require.NoError(t, err)
	assert.Contains(t, string(raw), "gat_new")
	assert.Contains(t, string(raw), "gar_new")
	assert.Equal(t, int32(1), refreshCalls.Load())
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
		nextRefresh := "gar_rotated"
		if body.RefreshToken == "gar_rotated" {
			nextRefresh = "gar_rotated_again"
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{
				"token":              "gat_rotated",
				"expires_at":         "2020-01-01T00:00:00Z", // still stale so a second invocation re-refreshes
				"refresh_token":      nextRefresh,
				"refresh_expires_at": "2099-02-01T00:00:00Z",
			},
		})
	}))
	defer srv.Close()

	dir := t.TempDir()
	file := filepath.Join(dir, "config.yaml")
	writeTestConfigFile(t, file, `
version: 1
stacks:
  default:
    grafana:
      server: "`+srv.URL+`"
      proxy-endpoint: "`+srv.URL+`"
      oauth-token: gat_old
      oauth-refresh-token: gar_old
      oauth-token-expires-at: "2020-01-01T00:00:00Z"
      oauth-refresh-expires-at: "2099-01-01T00:00:00Z"
      stack-id: 1
contexts:
  default:
    stack: default
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
