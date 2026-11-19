package config_test

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/grafana/gcx/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// An expired OIDC ID token is refreshed at the identity provider, sent to the
// Grafana server directly, and persisted alongside the unchanged refresh token.
func TestOIDCRefreshIsSentToGrafanaAndPersisted(t *testing.T) {
	store := withFakeStore(t)

	var issuer string
	var refreshes atomic.Int32
	var gotAuthorization atomic.Value
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/openid-configuration":
			_ = json.NewEncoder(w).Encode(map[string]string{
				"issuer":                 issuer,
				"authorization_endpoint": issuer + "/authorize",
				"token_endpoint":         issuer + "/token",
			})
		case "/token":
			refreshes.Add(1)
			if !assert.NoError(t, r.ParseForm()) {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			assert.Equal(t, "refresh_token", r.PostForm.Get("grant_type"))
			assert.Equal(t, "refresh-1", r.PostForm.Get("refresh_token"))
			// No refresh_token: the provider does not rotate it.
			_ = json.NewEncoder(w).Encode(map[string]string{"id_token": testIDToken(issuer, "client")})
		default:
			gotAuthorization.Store(r.Header.Get("Authorization"))
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer srv.Close()
	issuer = srv.URL

	dir := t.TempDir()
	file := filepath.Join(dir, "config.yaml")
	writeTestConfigFile(t, file, `
version: 1
stacks:
  default:
    grafana:
      server: "`+srv.URL+`"
      auth-method: oidc
      oidc-issuer: "`+issuer+`"
      oidc-client-id: client
      oauth-token: old-id-token
      oauth-refresh-token: refresh-1
      oauth-token-expires-at: "2020-01-01T00:00:00Z"
      org-id: 1
contexts:
  default:
    stack: default
current-context: default
`)

	restCfg, err := config.NewNamespacedRESTConfig(t.Context(), config.Context{
		Grafana: &config.GrafanaConfig{
			Server:              srv.URL,
			AuthMethod:          "oidc",
			OIDCIssuer:          issuer,
			OIDCClientID:        "client",
			OAuthToken:          "old-id-token",
			OAuthRefreshToken:   "refresh-1",
			OAuthTokenExpiresAt: "2020-01-01T00:00:00Z",
			OrgID:               1,
		},
	})
	require.NoError(t, err)
	assert.False(t, restCfg.IsOAuthProxy())
	restCfg.WireTokenPersistence(t.Context(), config.ExplicitConfigFile(file), "default", "default",
		[]config.ConfigSource{{Path: file, Type: "explicit"}})

	client := &http.Client{Transport: restCfg.WrapTransport(http.DefaultTransport)}
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, srv.URL+"/api/user", nil)
	require.NoError(t, err)
	resp, err := client.Do(req)
	require.NoError(t, err)
	_ = resp.Body.Close()

	newToken := testIDToken(issuer, "client")
	assert.Equal(t, int32(1), refreshes.Load())
	assert.Equal(t, "Bearer "+newToken, gotAuthorization.Load())
	assert.True(t, store.containsValue(newToken), "refreshed ID token must be persisted")
	assert.True(t, store.containsValue("refresh-1"), "non-rotated refresh token must be kept")
}

// testIDToken is deterministic for a given issuer and audience so the test can
// compare the persisted value.
func testIDToken(issuer, audience string) string {
	exp := time.Date(2099, 1, 1, 0, 0, 0, 0, time.UTC).Unix()
	payload, err := json.Marshal(map[string]any{"iss": issuer, "aud": audience, "exp": exp})
	if err != nil {
		panic(err)
	}
	return "eyJhbGciOiJSUzI1NiJ9." + base64.RawURLEncoding.EncodeToString(payload) + ".sig"
}
