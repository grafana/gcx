package config_test

import (
	"sync"
	"testing"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/telemetry/capture"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func resetAuthCapture(t *testing.T) {
	t.Helper()
	capture.Reset()
	t.Cleanup(capture.Reset)
}

// The auth-method truth table, driven through EffectiveGrafanaAuthMethod —
// one of the real consumers of the capturing selector. The decided method is
// recorded even when the selection later fails validation: an invalid
// credential on a known method reports the method, because that failure mode
// is the field's reason to exist. Only an unclassifiable method is "unknown".
func TestGrafanaAuthSelectionCapturesMethod(t *testing.T) {
	server := "https://grafana.example.invalid"
	tests := map[string]struct {
		grafana config.GrafanaConfig
		want    string
		wantErr bool
	}{
		"explicit token": {
			grafana: config.GrafanaConfig{AuthMethod: "token", APIToken: "tok"},
			want:    "token",
		},
		"legacy oauth fields infer oauth": {
			grafana: config.GrafanaConfig{ProxyEndpoint: "https://assistant.example.invalid", OAuthToken: "oauth-tok"},
			want:    "oauth",
		},
		"legacy user infers basic": {
			grafana: config.GrafanaConfig{User: "admin", Password: "pw"},
			want:    "basic",
		},
		"legacy client certificate infers mtls": {
			grafana: config.GrafanaConfig{TLS: &config.TLS{CertData: []byte("cert"), KeyData: []byte("key")}},
			want:    "mtls",
		},
		"server with no credential material is anonymous": {
			grafana: config.GrafanaConfig{},
			want:    "anonymous",
		},
		"unsupported auth-method is unknown": {
			grafana: config.GrafanaConfig{AuthMethod: "kerberos"},
			want:    "unknown",
			wantErr: true,
		},
		"explicit token with an empty credential still reports token": {
			grafana: config.GrafanaConfig{AuthMethod: "token"},
			want:    "token",
			wantErr: true,
		},
		"explicit mtls missing its key still reports mtls": {
			grafana: config.GrafanaConfig{AuthMethod: "mtls", TLS: &config.TLS{CertData: []byte("cert")}},
			want:    "mtls",
			wantErr: true,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			resetAuthCapture(t)
			grafana := tc.grafana
			grafana.Server = server
			grafana.StackID = 12345
			ctx := config.Context{Name: "prod", Grafana: &grafana}

			_, err := ctx.EffectiveGrafanaAuthMethod()
			if tc.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			assert.Equal(t, tc.want, capture.CurrentGrafanaAuthMethod())
		})
	}
}

// A non-blank runtime GRAFANA_TOKEN is what actually authenticates the
// invocation, so it is the method reported — the persisted auth-method loses
// without being modified.
func TestGrafanaAuthCaptureRuntimeTokenOverridesPersistedOAuth(t *testing.T) {
	resetAuthCapture(t)
	t.Setenv("GRAFANA_TOKEN", "runtime-token")

	ctx := config.Context{Name: "prod", Grafana: &config.GrafanaConfig{
		Server:            "https://grafana.example.invalid",
		StackID:           12345,
		AuthMethod:        "oauth",
		ProxyEndpoint:     "https://assistant.example.invalid",
		OAuthRefreshToken: "refresh",
	}}
	require.NoError(t, config.ParseEnvIntoContext(&ctx))

	method, err := ctx.EffectiveGrafanaAuthMethod()
	require.NoError(t, err)
	require.Equal(t, "token", method)
	assert.Equal(t, "token", capture.CurrentGrafanaAuthMethod())
	assert.Equal(t, "oauth", ctx.Grafana.AuthMethod,
		"the runtime win comes from the override marker, never from rewriting the selector")
}

// Context.Validate runs on the load path before any transport is built, so a
// command that dies at credential validation still reports the method it
// selected. This is the path a hook on the validated wrapper would miss.
func TestContextValidateCapturesMethodOnCredentialFailure(t *testing.T) {
	resetAuthCapture(t)

	ctx := config.Context{Name: "prod", Grafana: &config.GrafanaConfig{
		Server:     "https://grafana.example.invalid",
		StackID:    12345,
		AuthMethod: "token", // no token: validation fails after selection
	}}

	require.Error(t, ctx.Validate(t.Context()))
	assert.Equal(t, "token", capture.CurrentGrafanaAuthMethod())
}

// The two undecided states — no context, a context with no Grafana block —
// must not erase a decision an earlier load made. Both are seeded with the
// opposite so a stray write would be visible.
func TestGrafanaAuthCaptureUndecidedNeverErasesDecided(t *testing.T) {
	resetAuthCapture(t)
	capture.SetGrafanaAuthMethod("oauth")

	noGrafana := config.Context{Name: "cloud-only"}
	_, err := noGrafana.EffectiveGrafanaAuthMethod()
	require.Error(t, err)
	assert.Equal(t, "oauth", capture.CurrentGrafanaAuthMethod(),
		"a context without a Grafana block selected nothing and must record nothing")

	var nilCtx *config.Context
	_, err = nilCtx.EffectiveGrafanaAuthMethod()
	require.Error(t, err)
	assert.Equal(t, "oauth", capture.CurrentGrafanaAuthMethod())
}

// Stack-level GrafanaConfig.Validate goes through the package-level selector,
// not the context method, and must not capture: it validates configuration
// that may belong to a context this invocation never uses.
func TestGrafanaConfigValidateDoesNotCapture(t *testing.T) {
	resetAuthCapture(t)
	capture.SetGrafanaAuthMethod("oauth")

	grafana := config.GrafanaConfig{
		Server:     "https://other-stack.example.invalid",
		StackID:    54321,
		AuthMethod: "token",
		APIToken:   "tok",
	}
	require.NoError(t, grafana.Validate(t.Context(), "other"))
	assert.Equal(t, "oauth", capture.CurrentGrafanaAuthMethod(),
		"validating another stack's config is not this invocation's auth decision")
}

// gcx config check resolves auth for every context concurrently. Two contexts
// on different methods must collapse to no answer — deterministically,
// whatever the write order — while agreeing writers keep their value. This is
// the -race test the capture package deliberately does not carry itself.
func TestGrafanaAuthCaptureConcurrentContexts(t *testing.T) {
	server := "https://grafana.example.invalid"
	tokenCtx := config.Context{Name: "a", Grafana: &config.GrafanaConfig{
		Server: server, StackID: 1, AuthMethod: "token", APIToken: "tok",
	}}
	oauthCtx := config.Context{Name: "b", Grafana: &config.GrafanaConfig{
		Server: server, StackID: 2, AuthMethod: "oauth",
		ProxyEndpoint: "https://assistant.example.invalid", OAuthToken: "oauth-tok",
	}}

	t.Run("different methods collapse to omitted", func(t *testing.T) {
		resetAuthCapture(t)
		var wg sync.WaitGroup
		for range 25 {
			for _, ctx := range []*config.Context{&tokenCtx, &oauthCtx} {
				wg.Add(1)
				go func() {
					defer wg.Done()
					_, _ = ctx.EffectiveGrafanaAuthMethod()
				}()
			}
		}
		wg.Wait()
		assert.Empty(t, capture.CurrentGrafanaAuthMethod(),
			"an invocation that used two methods has no single true answer")
	})

	t.Run("agreeing writers keep the value", func(t *testing.T) {
		resetAuthCapture(t)
		var wg sync.WaitGroup
		for range 50 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, _ = tokenCtx.EffectiveGrafanaAuthMethod()
			}()
		}
		wg.Wait()
		assert.Equal(t, "token", capture.CurrentGrafanaAuthMethod())
	})
}
