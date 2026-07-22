package login

import (
	"context"
	"testing"

	"github.com/grafana/gcx/internal/auth"
	"github.com/grafana/gcx/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveGrafanaTokenAuthCarriesRuntimeProxyDestination(t *testing.T) {
	const proxy = "https://proxy.example.invalid"
	method, grafana, err := resolveGrafanaAuth(t.Context(), Options{Inputs: Inputs{
		Server:               "https://grafana.example.invalid",
		GrafanaToken:         "stored-token",
		RuntimeProxyEndpoint: proxy,
		OrgID:                1,
	}}, TargetOnPrem)
	require.NoError(t, err)
	assert.Equal(t, "token", method)
	assert.Equal(t, proxy, grafana.ProxyEndpoint,
		"validation and persistence must retain the destination used by the binding preflight")
}

func TestRuntimeOnlyOAuthDestinationChecksBeforeAndAfterFlow(t *testing.T) {
	t.Run("known TLS mismatch fails before OAuth", func(t *testing.T) {
		var flowCalls int
		_, err := Run(t.Context(), &Options{
			Inputs: Inputs{
				Server:            "https://grafana.example.invalid",
				Target:            TargetOnPrem,
				UseOAuth:          true,
				TLS:               &config.TLS{Insecure: true},
				PreserveStoredTLS: true,
			},
			Hooks: Hooks{
				NewAuthFlow: func(string, auth.Options) AuthFlow {
					flowCalls++
					return nil
				},
			},
		})
		require.ErrorContains(t, err, "runtime-only Grafana proxy/TLS settings")
		assert.Zero(t, flowCalls, "known mismatch must fail before launching OAuth")
	})

	t.Run("issuer mismatch fails before credential validation", func(t *testing.T) {
		var validateCalls int
		_, err := Run(t.Context(), &Options{
			Inputs: Inputs{
				Server:                      "https://grafana.example.invalid",
				Target:                      TargetOnPrem,
				UseOAuth:                    true,
				PreserveStoredProxyEndpoint: true,
				StoredProxyEndpoint:         "https://runtime-proxy.example.invalid",
				RuntimeProxyEndpoint:        "https://runtime-proxy.example.invalid",
			},
			Hooks: Hooks{
				NewAuthFlow: func(string, auth.Options) AuthFlow {
					return &stubInternalAuthFlow{result: &auth.Result{
						Token:        "oauth-token",
						RefreshToken: "refresh-token",
						APIEndpoint:  "https://issuer-proxy.example.invalid",
					}}
				},
				ValidateFn: func(context.Context, Options, config.NamespacedRESTConfig) (string, error) {
					validateCalls++
					return "", nil
				},
			},
		})
		require.ErrorContains(t, err, "GRAFANA_PROXY_ENDPOINT conflicts with the proxy endpoint selected by the OAuth issuer")
		var destinationErr *RuntimeOnlyBearerDestinationError
		require.ErrorAs(t, err, &destinationErr)
		assert.True(t, destinationErr.OAuthIssuerProxyMismatch)
		assert.Equal(t, "https://runtime-proxy.example.invalid", destinationErr.RuntimeProxyEndpoint)
		assert.Equal(t, "https://issuer-proxy.example.invalid", destinationErr.OAuthIssuerProxyEndpoint)
		assert.Zero(t, validateCalls, "resolved mismatch must fail before presenting the OAuth credential")
	})

	t.Run("issuer endpoint matching runtime override remains persistable", func(t *testing.T) {
		const runtimeProxy = "https://runtime-proxy.example.invalid"
		source := config.ExplicitConfigFile(t.TempDir() + "/config.yaml")
		_, err := Run(t.Context(), &Options{
			Inputs: Inputs{
				Server:                      "https://grafana.example.invalid",
				ContextName:                 "default",
				Target:                      TargetOnPrem,
				UseOAuth:                    true,
				PreserveStoredProxyEndpoint: true,
				StoredProxyEndpoint:         "https://stored-proxy.example.invalid",
				RuntimeProxyEndpoint:        runtimeProxy,
			},
			Hooks: Hooks{
				ConfigSource: source,
				NewAuthFlow: func(string, auth.Options) AuthFlow {
					return &stubInternalAuthFlow{result: &auth.Result{
						Token:        "oauth-token",
						RefreshToken: "refresh-token",
						APIEndpoint:  runtimeProxy,
					}}
				},
				ValidateFn: func(context.Context, Options, config.NamespacedRESTConfig) (string, error) {
					return "12.0.0", nil
				},
			},
		})
		require.NoError(t, err)

		persisted, err := config.Load(t.Context(), source)
		require.NoError(t, err)
		assert.Equal(t, runtimeProxy, persisted.Contexts["default"].Grafana.ProxyEndpoint)
	})
}

type stubInternalAuthFlow struct {
	result *auth.Result
}

func (flow *stubInternalAuthFlow) Run(context.Context) (*auth.Result, error) {
	return flow.result, nil
}

func TestMergeGrafanaAuthMaterializesDanglingReferencedStack(t *testing.T) {
	cfg := config.Config{
		Version: config.ConfigVersion,
		Contexts: map[string]*config.Context{
			"repair": {Name: "repair", Stack: "missing"},
		},
		CurrentContext: "repair",
	}
	cfg.Resolve()
	existing := cfg.Contexts["repair"]
	require.Nil(t, existing.StackEntry)

	err := mergeGrafanaAuthIntoStack(&cfg, existing, &config.GrafanaConfig{
		Server:     "https://example.invalid",
		APIToken:   "fresh-token",
		AuthMethod: "token",
	}, 0, "")
	require.NoError(t, err)
	require.NotNil(t, cfg.Stacks["missing"])
	require.NotNil(t, cfg.Stacks["missing"].Grafana)
	assert.Equal(t, "fresh-token", cfg.Stacks["missing"].Grafana.APIToken)
	assert.Same(t, cfg.Stacks["missing"], existing.StackEntry)
}

func TestMergeGrafanaAuthBindsSameNamedStackWithoutReplacingSettings(t *testing.T) {
	cfg := config.Config{}
	cfg.SetStack("prod", config.StackConfig{
		Slug: "keep-slug",
		Grafana: &config.GrafanaConfig{
			Server: "https://prod.example.invalid",
			OrgID:  42,
		},
		Providers: map[string]map[string]string{
			"synth": {"sm-url": "https://sm.example.invalid"},
		},
		Resources: &config.ResourcesConfig{AssumeServerDryRun: []string{"folders.folder.grafana.app"}},
	})
	cfg.SetContext("prod", true, config.Context{})
	existing := cfg.Contexts["prod"]
	originalStack := cfg.Stacks["prod"]

	err := mergeGrafanaAuthIntoStack(&cfg, existing, &config.GrafanaConfig{
		Server:     "https://prod.example.invalid",
		APIToken:   "fresh-token",
		AuthMethod: "token",
	}, 0, "")
	require.NoError(t, err)

	assert.Equal(t, "prod", existing.Stack)
	assert.Same(t, originalStack, cfg.Stacks["prod"], "the owned same-named stack must be reused, not replaced")
	assert.Equal(t, "keep-slug", cfg.Stacks["prod"].Slug)
	assert.Equal(t, "https://sm.example.invalid", cfg.Stacks["prod"].Providers["synth"]["sm-url"])
	assert.Equal(t, []string{"folders.folder.grafana.app"}, cfg.Stacks["prod"].Resources.AssumeServerDryRun)
	assert.EqualValues(t, 42, cfg.Stacks["prod"].Grafana.OrgID)
	assert.Equal(t, "fresh-token", cfg.Stacks["prod"].Grafana.APIToken)
}
