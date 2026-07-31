package config_test

import (
	"crypto/tls"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/grafana/gcx/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExplicitGrafanaAuthMethodControlsRESTTransport(t *testing.T) {
	server := "https://grafana.example.invalid"
	proxy := "https://assistant.example.invalid"
	tests := map[string]struct {
		grafana GrafanaConfigFixture
		assert  func(*testing.T, config.NamespacedRESTConfig)
	}{
		"OAuth ignores stale token and Basic fields": {
			grafana: GrafanaConfigFixture{config.GrafanaConfig{
				AuthMethod:        "oauth",
				ProxyEndpoint:     proxy,
				OAuthRefreshToken: "refresh-only-is-valid",
				APIToken:          "stale-token",
				User:              "stale-user",
				Password:          "stale-password",
			}},
			assert: func(t *testing.T, got config.NamespacedRESTConfig) {
				t.Helper()
				assert.True(t, got.IsOAuthProxy())
				assert.Equal(t, proxy+"/api/cli/v1/proxy", got.Host)
				assert.Empty(t, got.BearerToken)
				assert.Empty(t, got.Username)
			},
		},
		"token ignores stale OAuth Basic and mTLS identity": {
			grafana: GrafanaConfigFixture{config.GrafanaConfig{
				AuthMethod:        "token",
				APIToken:          "selected-token",
				ProxyEndpoint:     proxy,
				OAuthToken:        "stale-oauth",
				OAuthRefreshToken: "stale-refresh",
				User:              "stale-user",
				Password:          "stale-password",
				TLS: &config.TLS{
					CertData: []byte("stale-client-certificate"),
					KeyData:  []byte("stale-client-key"),
					CAData:   []byte("preserved-ca"),
					Insecure: true,
				},
			}},
			assert: func(t *testing.T, got config.NamespacedRESTConfig) {
				t.Helper()
				assert.False(t, got.IsOAuthProxy())
				assert.Equal(t, server, got.Host)
				assert.Equal(t, "selected-token", got.BearerToken)
				assert.Empty(t, got.Username)
				assert.Empty(t, got.CertData)
				assert.Empty(t, got.KeyData)
				assert.Equal(t, []byte("preserved-ca"), got.CAData)
				assert.True(t, got.Insecure)
			},
		},
		"Basic ignores stale OAuth and token fields": {
			grafana: GrafanaConfigFixture{config.GrafanaConfig{
				AuthMethod:        "basic",
				User:              "selected-user",
				Password:          "selected-password",
				APIToken:          "stale-token",
				ProxyEndpoint:     proxy,
				OAuthToken:        "stale-oauth",
				OAuthRefreshToken: "stale-refresh",
			}},
			assert: func(t *testing.T, got config.NamespacedRESTConfig) {
				t.Helper()
				assert.False(t, got.IsOAuthProxy())
				assert.Equal(t, server, got.Host)
				assert.Empty(t, got.BearerToken)
				assert.Equal(t, "selected-user", got.Username)
				assert.Equal(t, "selected-password", got.Password)
			},
		},
		"mTLS ignores every stale HTTP credential": {
			grafana: GrafanaConfigFixture{config.GrafanaConfig{
				AuthMethod:        "mtls",
				APIToken:          "stale-token",
				ProxyEndpoint:     proxy,
				OAuthToken:        "stale-oauth",
				OAuthRefreshToken: "stale-refresh",
				User:              "stale-user",
				Password:          "stale-password",
				TLS: &config.TLS{
					CertData: []byte("selected-client-certificate"),
					KeyData:  []byte("selected-client-key"),
				},
			}},
			assert: func(t *testing.T, got config.NamespacedRESTConfig) {
				t.Helper()
				assert.False(t, got.IsOAuthProxy())
				assert.Equal(t, server, got.Host)
				assert.Empty(t, got.BearerToken)
				assert.Empty(t, got.Username)
				assert.Equal(t, []byte("selected-client-certificate"), got.CertData)
				assert.Equal(t, []byte("selected-client-key"), got.KeyData)
			},
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			grafana := test.grafana.GrafanaConfig
			grafana.Server = server
			grafana.StackID = 12345
			got, err := config.NewNamespacedRESTConfig(t.Context(), config.Context{Name: "prod", Grafana: &grafana})
			require.NoError(t, err)
			test.assert(t, got)
		})
	}
}

// GrafanaConfigFixture prevents gofmt from making the auth-precedence table's
// composite literals visually ambiguous with the outer test-case literal.
type GrafanaConfigFixture struct {
	config.GrafanaConfig
}

func TestLegacyGrafanaAuthPrecedenceAndTLSCompatibility(t *testing.T) {
	server := "https://grafana.example.invalid"
	proxy := "https://assistant.example.invalid"
	tests := map[string]struct {
		grafana    config.GrafanaConfig
		wantMethod string
		assert     func(*testing.T, config.NamespacedRESTConfig)
	}{
		"OAuth refresh-only outranks token and Basic": {
			grafana: config.GrafanaConfig{
				ProxyEndpoint:     proxy,
				OAuthRefreshToken: "refresh-token",
				APIToken:          "lower-token",
				User:              "lower-user",
			},
			wantMethod: "oauth",
			assert: func(t *testing.T, got config.NamespacedRESTConfig) {
				t.Helper()
				assert.True(t, got.IsOAuthProxy())
			},
		},
		"token outranks Basic": {
			grafana:    config.GrafanaConfig{APIToken: "selected-token", User: "lower-user"},
			wantMethod: "token",
			assert: func(t *testing.T, got config.NamespacedRESTConfig) {
				t.Helper()
				assert.Equal(t, "selected-token", got.BearerToken)
				assert.Empty(t, got.Username)
			},
		},
		"user-only Basic remains compatible": {
			grafana:    config.GrafanaConfig{User: "legacy-user"},
			wantMethod: "basic",
			assert: func(t *testing.T, got config.NamespacedRESTConfig) {
				t.Helper()
				assert.Equal(t, "legacy-user", got.Username)
				assert.Empty(t, got.Password)
			},
		},
		"legacy token retains combined mTLS transport": {
			grafana: config.GrafanaConfig{
				APIToken: "selected-token",
				TLS: &config.TLS{
					CertData: []byte("legacy-client-certificate"),
					KeyData:  []byte("legacy-client-key"),
				},
			},
			wantMethod: "token",
			assert: func(t *testing.T, got config.NamespacedRESTConfig) {
				t.Helper()
				assert.Equal(t, "selected-token", got.BearerToken)
				assert.Equal(t, []byte("legacy-client-certificate"), got.CertData)
				assert.Equal(t, []byte("legacy-client-key"), got.KeyData)
			},
		},
		"no credentials remains anonymous": {
			wantMethod: "unknown",
			assert: func(t *testing.T, got config.NamespacedRESTConfig) {
				t.Helper()
				assert.Empty(t, got.BearerToken)
				assert.Empty(t, got.Username)
				assert.False(t, got.IsOAuthProxy())
			},
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			grafana := test.grafana
			grafana.Server = server
			grafana.StackID = 12345
			ctx := &config.Context{Name: "prod", Grafana: &grafana}
			method, err := ctx.EffectiveGrafanaAuthMethod()
			require.NoError(t, err)
			assert.Equal(t, test.wantMethod, method)
			got, err := ctx.ToRESTConfig(t.Context())
			require.NoError(t, err)
			test.assert(t, got)
		})
	}
}

func TestInvalidOrIncompleteExplicitGrafanaAuthFailsBeforeNetwork(t *testing.T) {
	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(server.Close)

	tests := map[string]config.GrafanaConfig{
		"unknown method": {
			AuthMethod: "future-auth",
			APIToken:   "must-not-be-used",
		},
		"token without token": {
			AuthMethod: "token",
			User:       "must-not-fall-back",
			Password:   "must-not-fall-back",
		},
		"Basic without password": {
			AuthMethod: "basic",
			User:       "admin",
			APIToken:   "must-not-fall-back",
		},
		"OAuth without proxy": {
			AuthMethod:        "oauth",
			OAuthRefreshToken: "refresh-token",
			APIToken:          "must-not-fall-back",
		},
		"OAuth without token material": {
			AuthMethod:    "oauth",
			ProxyEndpoint: "https://assistant.example.invalid",
			APIToken:      "must-not-fall-back",
		},
		"mTLS without private key": {
			AuthMethod: "mtls",
			APIToken:   "must-not-fall-back",
			TLS:        &config.TLS{CertFile: "/must-not-be-read/client.pem"},
		},
	}

	for name, grafana := range tests {
		t.Run(name, func(t *testing.T) {
			grafana.Server = server.URL
			_, err := config.NewNamespacedRESTConfig(t.Context(), config.Context{Name: "prod", Grafana: &grafana})
			require.Error(t, err)
			var validation config.ValidationError
			require.ErrorAs(t, err, &validation)
			assert.Zero(t, hits.Load(), "invalid auth must fail before namespace discovery")
		})
	}
}

func TestLegacyPartialOAuthDoesNotFallThroughToToken(t *testing.T) {
	tests := map[string]config.GrafanaConfig{
		"proxy only": {
			ProxyEndpoint: "https://assistant.example.invalid",
			APIToken:      "must-not-fall-back",
		},
		"access token without proxy": {
			OAuthToken: "partial-oauth-token",
			APIToken:   "must-not-fall-back",
		},
		"refresh token without proxy": {
			OAuthRefreshToken: "partial-refresh-token",
			APIToken:          "must-not-fall-back",
		},
	}

	for name, grafana := range tests {
		t.Run(name, func(t *testing.T) {
			grafana.Server = "https://example.invalid"
			grafana.StackID = 12345
			_, err := config.NewNamespacedRESTConfig(t.Context(), config.Context{Name: "prod", Grafana: &grafana})
			require.ErrorContains(t, err, "OAuth authentication requires")
		})
	}
}

func TestExplicitNonMTLSAuthStripsClientIdentityFromBootdataDiscovery(t *testing.T) {
	material := newTestTLSMaterial(t)
	tests := map[string]func(*testing.T, *config.Context) error{
		"context validation": func(t *testing.T, ctx *config.Context) error {
			t.Helper()
			return ctx.Validate(t.Context())
		},
		"REST config construction": func(t *testing.T, ctx *config.Context) error {
			t.Helper()
			_, err := ctx.ToRESTConfig(t.Context())
			return err
		},
	}

	for name, operation := range tests {
		t.Run(name, func(t *testing.T) {
			var hits atomic.Int32
			var peerCertificateSent atomic.Bool
			server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				hits.Add(1)
				if r.TLS != nil {
					peerCertificateSent.Store(len(r.TLS.PeerCertificates) > 0)
				}
				_ = json.NewEncoder(w).Encode(map[string]any{
					"settings": map[string]any{"namespace": "stacks-12345"},
				})
			}))
			server.TLS = &tls.Config{
				MinVersion: tls.VersionTLS12,
				ClientAuth: tls.RequestClientCert,
			}
			server.StartTLS()
			t.Cleanup(server.Close)

			ctx := &config.Context{
				Name: "prod",
				Grafana: &config.GrafanaConfig{
					Server:     server.URL,
					AuthMethod: "token",
					APIToken:   "selected-token",
					TLS: &config.TLS{
						Insecure: true,
						CertData: material.certPEM,
						KeyData:  material.keyPEM,
					},
				},
			}
			require.NoError(t, operation(t, ctx))
			assert.Equal(t, int32(1), hits.Load())
			assert.False(t, peerCertificateSent.Load(), "explicit token auth must not send a stale client certificate during bootdata discovery")
		})
	}
}

func TestEffectiveGrafanaTLSReturnsIndependentSelectedView(t *testing.T) {
	source := &config.TLS{
		Insecure:   true,
		ServerName: "grafana.example.invalid",
		CertData:   []byte("stale-cert"),
		KeyData:    []byte("stale-key"),
		CAData:     []byte("selected-ca"),
		NextProtos: []string{"h2", "http/1.1"},
	}
	ctx := &config.Context{
		Name: "prod",
		Grafana: &config.GrafanaConfig{
			AuthMethod: "token",
			APIToken:   "selected-token",
			TLS:        source,
		},
	}

	selected, err := ctx.EffectiveGrafanaTLS()
	require.NoError(t, err)
	require.NotNil(t, selected)
	assert.Empty(t, selected.CertData)
	assert.Empty(t, selected.KeyData)
	assert.Equal(t, source.CAData, selected.CAData)
	assert.Equal(t, source.NextProtos, selected.NextProtos)
	assert.True(t, selected.Insecure)
	assert.Equal(t, source.ServerName, selected.ServerName)

	selected.CAData[0] = 'X'
	selected.NextProtos[0] = "mutated"
	assert.Equal(t, []byte("selected-ca"), source.CAData)
	assert.Equal(t, []string{"h2", "http/1.1"}, source.NextProtos)
}
