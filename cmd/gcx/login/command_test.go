// White-box tests: most targets (structuredMissingFieldsError,
// structuredClarificationError, loginTextCodec, loginOpts.Validate) are
// unexported. Using the _test package would require exporting them solely
// for tests, which the project avoids.
//
//nolint:testpackage // see comment above
package login

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	configcmd "github.com/grafana/gcx/cmd/gcx/config"
	"github.com/grafana/gcx/internal/agent"
	internalauth "github.com/grafana/gcx/internal/auth"
	"github.com/grafana/gcx/internal/config"
	gcxerrors "github.com/grafana/gcx/internal/gcxerrors"
	internallogin "github.com/grafana/gcx/internal/login"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type stubCloudAuthFlow struct {
	result *internalauth.GCOMResult
	err    error
}

func (s *stubCloudAuthFlow) Run(_ context.Context) (*internalauth.GCOMResult, error) {
	return s.result, s.err
}

// TestStructuredMissingFieldsError verifies that structuredMissingFieldsError
// maps ErrNeedInput.Fields into a DetailedError whose suggestions mention
// the expected flags/env vars for each field.
func TestStructuredMissingFieldsError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		err            *internallogin.ErrNeedInput
		wantSummary    string
		wantDetailSubs []string
		wantSuggestSub []string
	}{
		{
			name:           "missing_server_only",
			err:            &internallogin.ErrNeedInput{Fields: []string{"server"}},
			wantSummary:    "Login requires additional input",
			wantDetailSubs: []string{"server"},
			wantSuggestSub: []string{"--server", "GRAFANA_SERVER"},
		},
		{
			name:           "missing_grafana_auth",
			err:            &internallogin.ErrNeedInput{Fields: []string{"grafana-auth"}},
			wantSummary:    "Login requires additional input",
			wantDetailSubs: []string{"grafana-auth"},
			wantSuggestSub: []string{"--oauth", "--token", "GRAFANA_TOKEN"},
		},
		{
			name:           "missing_cloud_token",
			err:            &internallogin.ErrNeedInput{Fields: []string{"cloud-token"}},
			wantSummary:    "Login requires additional input",
			wantDetailSubs: []string{"cloud-token"},
			wantSuggestSub: []string{"--cloud-token", "GRAFANA_CLOUD_TOKEN", "--yes"},
		},
		{
			name: "multiple_fields_with_hint",
			err: &internallogin.ErrNeedInput{
				Fields: []string{"server", "grafana-auth"},
				Hint:   "connect to your Grafana instance first",
			},
			wantSummary:    "Login requires additional input",
			wantDetailSubs: []string{"server", "grafana-auth", "connect to your Grafana instance first"},
			wantSuggestSub: []string{"--server", "--token"},
		},
		{
			name:           "unknown_field_fallback",
			err:            &internallogin.ErrNeedInput{Fields: []string{"some_custom_field"}},
			wantSummary:    "Login requires additional input",
			wantDetailSubs: []string{"some_custom_field"},
			wantSuggestSub: []string{"--some-custom-field"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := structuredMissingFieldsError(tt.err)
			require.Error(t, err)

			var det gcxerrors.DetailedError
			require.ErrorAs(t, err, &det, "expected gcxerrors.DetailedError, got %T", err)

			assert.Equal(t, tt.wantSummary, det.Summary)
			for _, sub := range tt.wantDetailSubs {
				assert.Contains(t, det.Details, sub, "details should mention %q", sub)
			}
			joined := strings.Join(det.Suggestions, "\n")
			for _, sub := range tt.wantSuggestSub {
				assert.Contains(t, joined, sub, "suggestions should mention %q", sub)
			}
		})
	}
}

// TestResolveNonInteractiveTokens verifies the non-interactive credential
// fallback: empty token flags are filled from the (env-overridden) source
// context, explicit flags win, and interactive logins are left untouched.
func TestResolveNonInteractiveTokens(t *testing.T) {
	t.Parallel()

	ctxWithTokens := &config.Context{
		Grafana:    &config.GrafanaConfig{APIToken: "glsa_env"},
		CloudEntry: &config.CloudEntry{Token: "glc_env"},
	}

	tests := []struct {
		name           string
		flagToken      string
		flagCloud      string
		sourceCtx      *config.Context
		interactive    bool
		wantToken      string
		wantCloudToken string
	}{
		{
			name:           "non_interactive_fills_from_context",
			sourceCtx:      ctxWithTokens,
			wantToken:      "glsa_env",
			wantCloudToken: "glc_env",
		},
		{
			name:        "interactive_leaves_flags_untouched",
			sourceCtx:   ctxWithTokens,
			interactive: true,
		},
		{
			name:           "explicit_flags_win_over_context",
			flagToken:      "glsa_flag",
			flagCloud:      "glc_flag",
			sourceCtx:      ctxWithTokens,
			wantToken:      "glsa_flag",
			wantCloudToken: "glc_flag",
		},
		{
			name: "nil_source_context_is_noop",
		},
		{
			name:      "oauth_context_without_api_token_stays_empty",
			sourceCtx: &config.Context{Grafana: &config.GrafanaConfig{OAuthToken: "oauth"}},
		},
		{
			name:      "fills_grafana_when_cloud_block_absent",
			sourceCtx: &config.Context{Grafana: &config.GrafanaConfig{APIToken: "glsa_env"}},
			wantToken: "glsa_env",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			gotToken, gotCloud := resolveNonInteractiveTokens(tt.flagToken, tt.flagCloud, tt.sourceCtx, tt.interactive)
			assert.Equal(t, tt.wantToken, gotToken, "grafana token mismatch")
			assert.Equal(t, tt.wantCloudToken, gotCloud, "cloud token mismatch")
		})
	}
}

func TestResolveNonInteractiveTokensUsesEnvForNewContext(t *testing.T) {
	t.Setenv("GRAFANA_TOKEN", "new-context-grafana-token")
	t.Setenv("GRAFANA_CLOUD_TOKEN", "new-context-cloud-token")

	grafanaToken, cloudToken := resolveNonInteractiveTokens("", "", nil, false)
	assert.Equal(t, "new-context-grafana-token", grafanaToken)
	assert.Equal(t, "new-context-cloud-token", cloudToken)
}

func TestUseExistingCloudEntryPreservesCredentialKindAndMetadata(t *testing.T) {
	t.Parallel()

	future := time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	tests := []struct {
		name     string
		entry    *config.CloudEntry
		wantOK   bool
		wantKind internallogin.CloudCredentialKind
	}{
		{
			name: "CAP remains CAP",
			entry: &config.CloudEntry{
				Token:    "cap-token",
				OAuthUrl: "https://grafana-ops.com",
				APIUrl:   "https://grafana-ops.com",
			},
			wantOK:   true,
			wantKind: internallogin.CloudCredentialCAP,
		},
		{
			name: "OAuth preserves metadata",
			entry: &config.CloudEntry{
				OAuthToken:          "oauth-token",
				OAuthTokenExpiresAt: future,
				OAuthScopes:         []string{"stacks:read", "fleet-management:read"},
				OAuthUrl:            "https://grafana-dev.com",
				APIUrl:              "https://grafana-dev.com",
			},
			wantOK:   true,
			wantKind: internallogin.CloudCredentialOAuth,
		},
		{
			name: "expired OAuth cannot be kept",
			entry: &config.CloudEntry{
				OAuthToken:          "expired",
				OAuthTokenExpiresAt: time.Now().Add(-time.Hour).UTC().Format(time.RFC3339),
			},
			wantOK: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var opts internallogin.Options
			got := useExistingCloudEntry(&opts, tt.entry, true, "")
			assert.Equal(t, tt.wantOK, got)
			if !tt.wantOK {
				return
			}
			assert.Equal(t, tt.wantKind, opts.CloudCredentialKind)
			assert.True(t, opts.CloudTokenTrusted)
			wantToken, err := tt.entry.ResolveToken()
			require.NoError(t, err)
			assert.Equal(t, wantToken, opts.CloudToken)
			assert.Equal(t, tt.entry.OAuthTokenExpiresAt, opts.CloudOAuthTokenExpiresAt)
			assert.Equal(t, tt.entry.OAuthScopes, opts.CloudOAuthScopes)
			assert.Equal(t, tt.entry.OAuthUrl, opts.CloudOAuthURL)
			assert.Equal(t, tt.entry.APIUrl, opts.CloudAPIURL)
		})
	}
}

func TestRunCloudOAuthPersistsResponseMetadataAndEndpointIntent(t *testing.T) {
	t.Parallel()

	var gotFlowOpts internalauth.GCOMOptions
	opts := internallogin.Options{
		Inputs: internallogin.Inputs{
			Server:      "https://custom.example.com",
			CloudAPIURL: "https://grafana-ops.com",
		},
		Hooks: internallogin.Hooks{
			NewCloudAuthFlow: func(flowOpts internalauth.GCOMOptions) internallogin.CloudAuthFlow {
				gotFlowOpts = flowOpts
				return &stubCloudAuthFlow{result: &internalauth.GCOMResult{
					AccessToken: "oauth-token",
					Scope:       "stacks:read fleet-management:read",
					ExpiresAt:   "2030-01-01T00:00:00Z",
				}}
			},
		},
	}

	require.NoError(t, runCloudOAuth(context.Background(), &opts))
	assert.Equal(t, "https://grafana-ops.com", gotFlowOpts.GCOMURL)
	assert.Equal(t, "https://grafana-ops.com", opts.CloudOAuthURL)
	assert.Equal(t, "https://grafana-ops.com", opts.CloudAPIURL)
	assert.Equal(t, internallogin.CloudCredentialOAuth, opts.CloudCredentialKind)
	assert.True(t, opts.CloudTokenTrusted)
	assert.Equal(t, "2030-01-01T00:00:00Z", opts.CloudOAuthTokenExpiresAt)
	assert.Equal(t, []string{"stacks:read", "fleet-management:read"}, opts.CloudOAuthScopes)
}

func TestUseExistingCloudEntryEndpointChangeFailsClosed(t *testing.T) {
	t.Parallel()

	future := time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	t.Run("OAuth requires a fresh flow", func(t *testing.T) {
		t.Parallel()
		opts := internallogin.Options{Inputs: internallogin.Inputs{
			Server:        "https://stack.grafana-ops.net",
			CloudOAuthURL: "https://grafana-ops.com",
			CloudAPIURL:   "https://grafana-ops.com",
		}}
		entry := &config.CloudEntry{
			OAuthToken:          "dev-oauth-token",
			OAuthTokenExpiresAt: future,
			OAuthUrl:            "https://grafana-dev.com",
			APIUrl:              "https://grafana-dev.com",
		}

		assert.False(t, useExistingCloudEntry(&opts, entry, true, "https://stack.grafana-dev.net"))
		assert.Empty(t, opts.CloudToken)
	})

	t.Run("CAP also requires a fresh credential", func(t *testing.T) {
		t.Parallel()
		opts := internallogin.Options{Inputs: internallogin.Inputs{
			Server:        "https://stack.grafana-ops.net",
			CloudOAuthURL: "https://grafana-ops.com",
			CloudAPIURL:   "https://grafana-ops.com",
		}}
		entry := &config.CloudEntry{
			Token:    "prod-cap",
			OAuthUrl: "https://grafana.com",
			APIUrl:   "https://grafana.com",
		}

		assert.False(t, useExistingCloudEntry(&opts, entry, true, "https://stack.grafana.net"))
		assert.Empty(t, opts.CloudToken)
		assert.Equal(t, "https://grafana-ops.com", opts.CloudOAuthURL)
		assert.Equal(t, "https://grafana-ops.com", opts.CloudAPIURL)
	})

	t.Run("server-derived endpoint change rejects legacy CAP", func(t *testing.T) {
		t.Parallel()
		opts := internallogin.Options{Inputs: internallogin.Inputs{
			Server:     "https://stack.grafana-ops.net",
			CloudToken: "copied-prod-cap",
		}}
		sourceCtx := &config.Context{
			Grafana:    &config.GrafanaConfig{Server: "https://stack.grafana.net"},
			CloudEntry: &config.CloudEntry{Token: "copied-prod-cap"},
		}

		err := reuseNonInteractiveCloudCredential(&opts, false, sourceCtx, false)
		require.ErrorContains(t, err, "cannot be reused for different endpoints")
		assert.Empty(t, opts.CloudToken, "the copied token must be cleared before validation")
	})
}

func TestServerChangeRejectsStoredGrafanaTokenBeforeNetwork(t *testing.T) {
	t.Setenv("GCX_AGENT_MODE", "false")
	unsetEnvForTest(t, "GRAFANA_TOKEN")
	agent.ResetForTesting()
	t.Cleanup(agent.ResetForTesting)

	path := filepath.Join(t.TempDir(), "config.yaml")
	seed := config.Config{}
	seed.SetStack("default", config.StackConfig{Grafana: &config.GrafanaConfig{
		Server:     "https://old.example.invalid",
		APIToken:   "stored-old-token",
		AuthMethod: "token",
		OrgID:      1,
	}})
	seed.SetContext("default", true, config.Context{Stack: "default"})
	require.NoError(t, config.Write(t.Context(), config.ExplicitConfigFile(path), seed))

	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		requests.Add(1)
	}))
	t.Cleanup(server.Close)

	cmd := Command()
	cmd.SilenceErrors = true
	cmd.SilenceUsage = true
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{
		"default",
		"--config", path,
		"--server", server.URL,
		"--allow-server-override",
		"--yes",
	})

	err := cmd.ExecuteContext(t.Context())
	require.ErrorContains(t, err, "Stored Grafana token cannot be reused")
	assert.Zero(t, requests.Load(), "destination mismatch must fail before any server probe")
}

func TestExistingGrafanaTokenIsNotOfferedForDifferentServer(t *testing.T) {
	t.Parallel()
	sourceCtx := &config.Context{Grafana: &config.GrafanaConfig{
		Server:   "https://old.example.invalid",
		APIToken: "old-token",
	}}

	assert.Empty(t, existingGrafanaTokenForServer("https://new.example.invalid", sourceCtx))
	assert.Equal(t, "old-token", existingGrafanaTokenForServer("https://old.example.invalid", sourceCtx))
}

func TestEnvironmentTokensCountAsExplicitCredentialIntent(t *testing.T) {
	t.Setenv("GRAFANA_TOKEN", "fresh-grafana-token")
	t.Setenv("GRAFANA_CLOUD_TOKEN", "fresh-cloud-token")
	assert.True(t, credentialProvided("", "GRAFANA_TOKEN"))
	assert.True(t, credentialProvided("", "GRAFANA_CLOUD_TOKEN"))

	opts := internallogin.Options{Inputs: internallogin.Inputs{
		Server:        "https://stack.grafana-ops.net",
		CloudToken:    "fresh-cloud-token",
		CloudOAuthURL: "https://grafana-ops.com",
		CloudAPIURL:   "https://grafana-ops.com",
	}}
	sourceCtx := &config.Context{
		Grafana: &config.GrafanaConfig{Server: "https://stack.grafana.net"},
		CloudEntry: &config.CloudEntry{
			Token:    "fresh-cloud-token",
			OAuthUrl: "https://grafana.com",
			APIUrl:   "https://grafana.com",
		},
	}

	require.NoError(t, reuseNonInteractiveCloudCredential(&opts, true, sourceCtx, false))
	assert.Equal(t, "fresh-cloud-token", opts.CloudToken)
	assert.False(t, opts.CloudTokenTrusted, "environment credentials must still be validated")
}

func TestLoadLoginSourceContextAppliesEnvToPositionalTarget(t *testing.T) {
	t.Setenv("GRAFANA_TOKEN", "target-env-token")
	path := filepath.Join(t.TempDir(), "config.yaml")
	seed := config.Config{}
	seed.SetStack("current", config.StackConfig{Grafana: &config.GrafanaConfig{Server: "https://current.invalid", APIToken: "current-token"}})
	seed.SetStack("target", config.StackConfig{Grafana: &config.GrafanaConfig{Server: "https://target.invalid", APIToken: "target-token"}})
	seed.SetContext("current", true, config.Context{Stack: "current"})
	seed.SetContext("target", false, config.Context{Stack: "target"})
	require.NoError(t, config.Write(t.Context(), config.ExplicitConfigFile(path), seed))

	flags := &loginOpts{Config: configcmd.Options{ConfigFile: path}}
	_, sourceCtx, name, err := loadLoginSourceContext(t.Context(), flags, "target")
	require.NoError(t, err)
	assert.Equal(t, "target", name)
	require.NotNil(t, sourceCtx)
	assert.Equal(t, "target-env-token", sourceCtx.Grafana.APIToken)
}

func TestPersistedLoginSourceContextIgnoresRuntimeEnvironmentOverrides(t *testing.T) {
	t.Setenv("GRAFANA_SERVER", "https://runtime.invalid")
	t.Setenv("GRAFANA_CLOUD_API_URL", "https://grafana-ops.com")
	unsetEnvForTest(t, "GRAFANA_CLOUD_OAUTH_URL")

	path := filepath.Join(t.TempDir(), "config.yaml")
	seed := config.Config{}
	seed.SetStack("target", config.StackConfig{Grafana: &config.GrafanaConfig{Server: "https://stored.invalid"}})
	seed.SetCloudEntry("grafana-com", config.CloudEntry{
		OAuthUrl: "https://grafana.com",
		APIUrl:   "https://grafana.com",
	})
	seed.SetContext("target", true, config.Context{Stack: "target", Cloud: "grafana-com"})
	require.NoError(t, config.Write(t.Context(), config.ExplicitConfigFile(path), seed))

	flags := &loginOpts{Config: configcmd.Options{ConfigFile: path}}
	_, runtimeCtx, name, err := loadLoginSourceContext(t.Context(), flags, "target")
	require.NoError(t, err)
	require.Equal(t, "target", name)
	require.NotNil(t, runtimeCtx)
	assert.Equal(t, "https://runtime.invalid", runtimeCtx.Grafana.Server)
	assert.Equal(t, "https://grafana-ops.com", runtimeCtx.CloudEntry.APIUrl)

	persistedCtx, err := loadPersistedLoginSourceContext(t.Context(), config.ExplicitConfigFile(path), name)
	require.NoError(t, err)
	require.NotNil(t, persistedCtx)
	assert.Equal(t, "https://stored.invalid", persistedCtx.Grafana.Server)
	assert.Equal(t, "https://grafana.com", persistedCtx.CloudEntry.OAuthUrl)
	assert.Equal(t, "https://grafana.com", persistedCtx.CloudEntry.APIUrl)
}

func TestLoadPersistedLoginSourceContextAllowsNewExplicitConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "new", "config.yaml")
	persistedCtx, err := loadPersistedLoginSourceContext(
		t.Context(),
		config.ExplicitConfigFile(path),
		"new-context",
	)
	require.NoError(t, err)
	assert.Nil(t, persistedCtx)
}

func TestRequestedLoginServerUsesEnvironmentForNewContext(t *testing.T) {
	t.Setenv("GRAFANA_SERVER", "https://new-context.example.invalid")
	assert.Equal(t, "https://new-context.example.invalid", requestedLoginServer("", nil))
	assert.Equal(t, "https://flag.example.invalid", requestedLoginServer("https://flag.example.invalid", nil),
		"an explicit flag must win over the environment")
}

func TestLoginTLSFromEnvironmentSupportsNewContext(t *testing.T) {
	t.Setenv("GRAFANA_TLS_CERT_FILE", "/tmp/client.crt")
	t.Setenv("GRAFANA_TLS_KEY_FILE", "/tmp/client.key")
	t.Setenv("GRAFANA_TLS_CA_FILE", "/tmp/ca.crt")

	tlsConfig := loginTLSFromEnvironment()
	require.NotNil(t, tlsConfig)
	assert.Equal(t, "/tmp/client.crt", tlsConfig.CertFile)
	assert.Equal(t, "/tmp/client.key", tlsConfig.KeyFile)
	assert.Equal(t, "/tmp/ca.crt", tlsConfig.CAFile)
}

func TestCloudLoginEndpointsUseCoherentEnvironmentIntent(t *testing.T) {
	stored := &config.Context{CloudEntry: &config.CloudEntry{
		OAuthUrl: "https://grafana.com",
		APIUrl:   "https://grafana.com",
	}}

	t.Run("new context API env selects both endpoints", func(t *testing.T) {
		t.Setenv("GRAFANA_CLOUD_API_URL", "grafana-ops.com")
		unsetEnvForTest(t, "GRAFANA_CLOUD_OAUTH_URL")
		apiURL, oauthURL, err := cloudLoginEndpoints(&loginOpts{}, nil, false)
		require.NoError(t, err)
		assert.Equal(t, "https://grafana-ops.com", apiURL)
		assert.Equal(t, "https://grafana-ops.com", oauthURL)
	})

	t.Run("existing context OAuth env replaces the stored pair", func(t *testing.T) {
		unsetEnvForTest(t, "GRAFANA_CLOUD_API_URL")
		t.Setenv("GRAFANA_CLOUD_OAUTH_URL", "grafana-dev.com")
		apiURL, oauthURL, err := cloudLoginEndpoints(&loginOpts{}, stored, false)
		require.NoError(t, err)
		assert.Equal(t, "https://grafana-dev.com", apiURL)
		assert.Equal(t, "https://grafana-dev.com", oauthURL)
	})

	t.Run("two explicit env endpoints remain an exact pair", func(t *testing.T) {
		t.Setenv("GRAFANA_CLOUD_API_URL", "https://grafana-ops.com")
		t.Setenv("GRAFANA_CLOUD_OAUTH_URL", "https://grafana-dev.com")
		apiURL, oauthURL, err := cloudLoginEndpoints(&loginOpts{}, stored, false)
		require.NoError(t, err)
		assert.Equal(t, "https://grafana-ops.com", apiURL)
		assert.Equal(t, "https://grafana-dev.com", oauthURL)
	})

	t.Run("CLI endpoint wins and selects both", func(t *testing.T) {
		t.Setenv("GRAFANA_CLOUD_API_URL", "https://grafana-ops.com")
		t.Setenv("GRAFANA_CLOUD_OAUTH_URL", "https://grafana-dev.com")
		apiURL, oauthURL, err := cloudLoginEndpoints(&loginOpts{CloudAPIURL: "https://explicit.invalid"}, stored, true)
		require.NoError(t, err)
		assert.Equal(t, "https://explicit.invalid", apiURL)
		assert.Equal(t, "https://explicit.invalid", oauthURL)
	})
}

func TestLoginEnvironmentServerChangeRequiresPreflightConfirmation(t *testing.T) {
	t.Setenv("GCX_AGENT_MODE", "false")
	t.Setenv("GRAFANA_SERVER", "https://new.example.invalid")
	t.Setenv("GRAFANA_TOKEN", "fresh-env-token")
	unsetEnvForTest(t, "GRAFANA_CLOUD_API_URL")
	unsetEnvForTest(t, "GRAFANA_CLOUD_OAUTH_URL")
	agent.ResetForTesting()
	t.Cleanup(agent.ResetForTesting)

	path := filepath.Join(t.TempDir(), "config.yaml")
	seed := config.Config{}
	seed.SetStack("default", config.StackConfig{Grafana: &config.GrafanaConfig{Server: "https://old.example.invalid"}})
	seed.SetContext("default", true, config.Context{Stack: "default"})
	require.NoError(t, config.Write(t.Context(), config.ExplicitConfigFile(path), seed))

	cmd := Command()
	cmd.SilenceErrors = true
	cmd.SilenceUsage = true
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"default", "--config", path, "--yes"})

	err := cmd.ExecuteContext(t.Context())
	require.ErrorContains(t, err, "Login would overwrite an existing context")
	require.ErrorContains(t, err, "--allow-server-override")
}

func TestLoginRejectsAmbiguousLayeredWriteBeforeNetwork(t *testing.T) {
	t.Setenv("GCX_AGENT_MODE", "false")
	t.Setenv("HOME", t.TempDir())
	userDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", userDir)
	t.Setenv("XDG_CONFIG_DIRS", t.TempDir())
	t.Setenv("GCX_CONFIG", "")
	unsetEnvForTest(t, "GRAFANA_TOKEN")
	agent.ResetForTesting()
	t.Cleanup(agent.ResetForTesting)
	workDir := t.TempDir()
	t.Chdir(workDir)

	contents := []byte(`version: 1
stacks:
  default:
    grafana:
      server: https://old.example.invalid
      token: old-token
      auth-method: token
      org-id: 1
contexts:
  default:
    stack: default
current-context: default
`)
	userPath := filepath.Join(userDir, "gcx", "config.yaml")
	require.NoError(t, os.MkdirAll(filepath.Dir(userPath), 0o755))
	require.NoError(t, os.WriteFile(userPath, contents, 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(workDir, ".gcx.yaml"), contents, 0o600))

	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		requests.Add(1)
	}))
	t.Cleanup(server.Close)

	cmd := Command()
	cmd.SilenceErrors = true
	cmd.SilenceUsage = true
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{
		"default",
		"--server", server.URL,
		"--token", "fresh-token",
		"--allow-server-override",
		"--yes",
	})

	err := cmd.ExecuteContext(t.Context())
	require.ErrorContains(t, err, "write target is ambiguous")
	require.ErrorContains(t, err, "--config <path>")
	assert.Zero(t, requests.Load(), "ambiguous persistence must fail before authentication or validation")
}

func TestLoginRejectsFreshCredentialsForAutoLocalBeforeNetwork(t *testing.T) {
	tests := []struct {
		name string
		env  string
		args []string
	}{
		{name: "Grafana token flag", args: []string{"--token", "fresh-grafana-token"}},
		{name: "Grafana token environment", env: "GRAFANA_TOKEN"},
		{name: "Grafana OAuth", args: []string{"--oauth"}},
		{name: "Cloud token flag", args: []string{"--cloud-token", "fresh-cloud-token"}},
		{name: "Cloud token environment", env: "GRAFANA_CLOUD_TOKEN"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			workDir := isolateAutoLocalLoginEnv(t)
			if tt.env != "" {
				t.Setenv(tt.env, "fresh-environment-token")
			}

			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
				requests.Add(1)
			}))
			t.Cleanup(server.Close)

			localPath := filepath.Join(workDir, config.LocalConfigFileName)
			original := fmt.Appendf(nil, `version: 1
stacks:
  default:
    grafana:
      server: %s
      proxy-endpoint: https://repository-proxy.invalid
      tls:
        insecure: true
contexts:
  default:
    stack: default
current-context: default
`, server.URL)
			require.NoError(t, os.WriteFile(localPath, original, 0o600))

			cmd := Command()
			cmd.SilenceErrors = true
			cmd.SilenceUsage = true
			cmd.SetOut(&bytes.Buffer{})
			cmd.SetErr(&bytes.Buffer{})
			args := []string{"default", "--server", server.URL, "--yes"}
			args = append(args, tt.args...)
			cmd.SetArgs(args)

			err := cmd.ExecuteContext(t.Context())
			require.ErrorContains(t, err, "auto-discovered repository config")
			require.ErrorContains(t, err, "--config "+localPath)
			assert.Zero(t, requests.Load(), "fresh credentials must fail before target detection or validation")
			raw, readErr := os.ReadFile(localPath)
			require.NoError(t, readErr)
			assert.Equal(t, original, raw)
		})
	}
}

func TestAutoLocalCredentialPolicyAllowsOnlyBoundKeep(t *testing.T) {
	t.Parallel()
	target := config.ConfigSource{Path: "/repo/.gcx.yaml", Type: "local"}
	sourceCtx := &config.Context{
		Grafana: &config.GrafanaConfig{
			Server:   "https://stack.grafana.net",
			APIToken: "stored-grafana-token",
		},
		CloudEntry: &config.CloudEntry{
			Token:    "stored-cloud-token",
			OAuthUrl: "https://grafana.com",
			APIUrl:   "https://grafana.com",
		},
	}
	keep := &internallogin.Options{Inputs: internallogin.Inputs{
		Server:              "https://stack.grafana.net",
		GrafanaToken:        "stored-grafana-token",
		CloudToken:          "stored-cloud-token",
		CloudOAuthURL:       "https://grafana.com",
		CloudAPIURL:         "https://grafana.com",
		CloudCredentialKind: internallogin.CloudCredentialCAP,
	}}
	require.NoError(t, enforceAutoLocalCredentialPolicy(keep, sourceCtx, target))

	freshGrafana := *keep
	freshGrafana.GrafanaToken = "fresh-grafana-token"
	require.ErrorContains(t, enforceAutoLocalCredentialPolicy(&freshGrafana, sourceCtx, target), "auto-discovered repository config")

	freshCloud := *keep
	freshCloud.CloudToken = "fresh-cloud-token"
	require.ErrorContains(t, enforceAutoLocalCredentialPolicy(&freshCloud, sourceCtx, target), "auto-discovered repository config")

	freshOAuth := *keep
	freshOAuth.GrafanaToken = ""
	freshOAuth.UseOAuth = true
	require.ErrorContains(t, enforceAutoLocalCredentialPolicy(&freshOAuth, sourceCtx, target), "auto-discovered repository config")
}

func isolateAutoLocalLoginEnv(t *testing.T) string {
	t.Helper()
	t.Setenv("GCX_AGENT_MODE", "false")
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_DIRS", t.TempDir())
	t.Setenv("GCX_CONFIG", "")
	t.Setenv("GRAFANA_TOKEN", "")
	t.Setenv("GRAFANA_CLOUD_TOKEN", "")
	workDir := t.TempDir()
	t.Chdir(workDir)
	agent.ResetForTesting()
	t.Cleanup(agent.ResetForTesting)
	return workDir
}

//nolint:usetesting // t.Setenv cannot represent an absent variable or restore it as absent.
func unsetEnvForTest(t *testing.T, key string) {
	t.Helper()
	value, existed := os.LookupEnv(key)
	require.NoError(t, os.Unsetenv(key))
	t.Cleanup(func() {
		if existed {
			require.NoError(t, os.Setenv(key, value))
			return
		}
		require.NoError(t, os.Unsetenv(key))
	})
}

// TestDefaultOAuthFromContext verifies that a non-interactive re-auth of an
// existing OAuth context defaults to OAuth, while interactive logins, explicit
// flags, stored tokens, and non-OAuth contexts are left untouched (issue #854).
func TestDefaultOAuthFromContext(t *testing.T) {
	t.Parallel()

	oauthCtx := &config.Context{Grafana: &config.GrafanaConfig{AuthMethod: "oauth"}}
	tokenCtx := &config.Context{Grafana: &config.GrafanaConfig{AuthMethod: "token"}}

	tests := []struct {
		name         string
		useOAuth     bool
		grafanaToken string
		sourceCtx    *config.Context
		interactive  bool
		want         bool
	}{
		{
			name:      "non_interactive_oauth_context_defaults_oauth",
			sourceCtx: oauthCtx,
			want:      true,
		},
		{
			name:        "interactive_oauth_context_unchanged",
			sourceCtx:   oauthCtx,
			interactive: true,
			want:        false,
		},
		{
			name:      "token_context_not_defaulted",
			sourceCtx: tokenCtx,
			want:      false,
		},
		{
			name:         "stored_token_wins_over_oauth_default",
			grafanaToken: "glsa_env",
			sourceCtx:    oauthCtx,
			want:         false,
		},
		{
			name:      "explicit_oauth_flag_preserved",
			useOAuth:  true,
			sourceCtx: tokenCtx,
			want:      true,
		},
		{
			name: "nil_source_context",
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := defaultOAuthFromContext(tt.useOAuth, tt.grafanaToken, tt.sourceCtx, tt.interactive)
			assert.Equal(t, tt.want, got)
		})
	}
}

// TestStructuredClarificationError verifies that structuredClarificationError
// returns the right DetailedError variant for each Field ("allow-override",
// "save-unvalidated", ambiguous target).
func TestStructuredClarificationError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		err            *internallogin.ErrNeedClarification
		wantSummary    string
		wantDetailSubs []string
		wantSuggestSub []string
	}{
		{
			name: "allow_override",
			err: &internallogin.ErrNeedClarification{
				Field:    "allow-override",
				Question: "Context \"prod\" already exists. Overwrite?",
			},
			wantSummary:    "Login would overwrite an existing context",
			wantDetailSubs: []string{"prod", "Overwrite"},
			wantSuggestSub: []string{"--allow-server-override"},
		},
		{
			name: "save_unvalidated",
			err: &internallogin.ErrNeedClarification{
				Field:    "save-unvalidated",
				Question: "Connectivity failed: context deadline exceeded.",
			},
			wantSummary:    "Connectivity validation failed",
			wantDetailSubs: []string{"Connectivity failed"},
			wantSuggestSub: []string{"Re-run interactively", "server URL"},
		},
		{
			name: "ambiguous_cloud_vs_onprem",
			err: &internallogin.ErrNeedClarification{
				Field:    "target",
				Question: "Is this a Grafana Cloud instance or an on-premises Grafana?",
				Choices:  []string{"cloud", "on-prem"},
			},
			wantSummary:    "Login requires clarification",
			wantDetailSubs: []string{"Cloud", "on-prem"},
			wantSuggestSub: []string{"--cloud", "--yes"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := structuredClarificationError(tt.err)
			require.Error(t, err)

			var det gcxerrors.DetailedError
			require.ErrorAs(t, err, &det, "expected gcxerrors.DetailedError, got %T", err)

			assert.Equal(t, tt.wantSummary, det.Summary)
			for _, sub := range tt.wantDetailSubs {
				assert.Contains(t, det.Details, sub, "details should mention %q", sub)
			}
			joined := strings.Join(det.Suggestions, "\n")
			for _, sub := range tt.wantSuggestSub {
				assert.Contains(t, joined, sub, "suggestions should mention %q", sub)
			}
		})
	}
}

// TestLoginOptsValidate covers the F4 arg-vs-flag conflict detection.
// Validate() must error when positional CONTEXT_NAME and --context are both
// set, and succeed otherwise.
func TestLoginOptsValidate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		args          []string
		contextFlag   string
		oauthFlag     bool
		tokenFlag     string
		wantErr       bool
		wantErrSubstr string
	}{
		{
			name:          "conflict_positional_and_flag",
			args:          []string{"prod"},
			contextFlag:   "staging",
			wantErr:       true,
			wantErrSubstr: "conflicting context specification",
		},
		{
			name:        "only_positional",
			args:        []string{"prod"},
			contextFlag: "",
			wantErr:     false,
		},
		{
			name:        "only_flag",
			args:        []string{},
			contextFlag: "staging",
			wantErr:     false,
		},
		{
			name:        "neither",
			args:        []string{},
			contextFlag: "",
			wantErr:     false,
		},
		{
			name:          "conflict_oauth_and_token",
			args:          []string{},
			oauthFlag:     true,
			tokenFlag:     "glsa_xxx",
			wantErr:       true,
			wantErrSubstr: "conflicting authentication methods",
		},
		{
			name:      "oauth_alone",
			args:      []string{},
			oauthFlag: true,
			wantErr:   false,
		},
		{
			name:      "token_alone",
			args:      []string{},
			tokenFlag: "glsa_xxx",
			wantErr:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			opts := &loginOpts{
				Config: configcmd.Options{Context: tt.contextFlag},
				OAuth:  tt.oauthFlag,
				Token:  tt.tokenFlag,
			}
			// Bind flags into a throwaway FlagSet so IO.Validate() has a
			// populated flag set to inspect (otherwise --json handling is
			// a no-op, which is fine for these cases).
			opts.IO.RegisterCustomCodec("text", &loginTextCodec{})
			opts.IO.DefaultFormat("text")
			opts.IO.BindFlags(pflag.NewFlagSet("test", pflag.ContinueOnError))

			err := opts.Validate(tt.args)
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErrSubstr)
				return
			}
			require.NoError(t, err)
		})
	}
}

// TestOAuthFlagParses confirms setup() registers the --oauth flag and that it
// parses into loginOpts.OAuth, which runLogin maps to login.Inputs.UseOAuth so
// OAuth is reachable non-interactively (issue #854).
func TestOAuthFlagParses(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		args      []string
		wantOAuth bool
	}{
		{
			name:      "oauth_flag_set",
			args:      []string{"--server", "https://example.grafana.net", "--oauth"},
			wantOAuth: true,
		},
		{
			name:      "oauth_flag_absent",
			args:      []string{"--server", "https://example.grafana.net"},
			wantOAuth: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			opts := &loginOpts{}
			flags := pflag.NewFlagSet("test", pflag.ContinueOnError)
			opts.setup(flags)

			require.NoError(t, flags.Parse(tt.args))
			assert.Equal(t, tt.wantOAuth, opts.OAuth)
		})
	}
}

// TestPrintResult_TextCodec is a golden comparison that confirms the text
// codec produces the expected multi-line summary for representative
// LoginResult fixtures, and that advisory guidance goes to stderr (never
// stdout) so JSON/YAML consumers receive a clean stream.
//
// Intentionally not run with t.Parallel(): subtests mutate process-level env
// vars via t.Setenv (to disable agent-mode detection that would otherwise
// flip the default codec to json), and t.Setenv is incompatible with parallel
// tests.
func TestPrintResult_TextCodec(t *testing.T) {
	tests := []struct {
		name           string
		server         string
		result         internallogin.Result
		wantStdout     string
		wantStderrSubs []string
		noStderr       bool
	}{
		{
			name:   "cloud_with_cap_token",
			server: "https://mystack.grafana.net",
			result: internallogin.Result{
				ContextName:    "mystack",
				AuthMethod:     "oauth",
				IsCloud:        true,
				HasCloudToken:  true,
				GrafanaVersion: "12.0.0",
				StackSlug:      "mystack",
			},
			wantStdout: `Logged in to https://mystack.grafana.net
  Context:     mystack
  Auth method: oauth
  Version:     12.0.0
  Grafana Cloud: yes
  Stack:       mystack
`,
			wantStderrSubs: []string{
				"Verify access anytime with: gcx config check",
			},
		},
		{
			name:   "cloud_without_cap_token_emits_advisory_on_stderr",
			server: "https://stack.grafana.net",
			result: internallogin.Result{
				ContextName:   "stack",
				AuthMethod:    "token",
				IsCloud:       true,
				HasCloudToken: false,
				StackSlug:     "stack",
			},
			wantStdout: `Logged in to https://stack.grafana.net
  Context:     stack
  Auth method: token
  Grafana Cloud: yes
  Stack:       stack
`,
			wantStderrSubs: []string{
				"Verify access anytime with: gcx config check",
				"authenticated for the Grafana API",
				"requires a Cloud Access Policy (CAP) token.",
				"grafana.com/docs/grafana-cloud/security-and-account-management",
				"gcx login --context stack --cloud-token",
			},
		},
		{
			name:   "onprem_no_advisory",
			server: "https://grafana.local",
			result: internallogin.Result{
				ContextName:    "local",
				AuthMethod:     "token",
				IsCloud:        false,
				GrafanaVersion: "11.5.0",
			},
			wantStdout: `Logged in to https://grafana.local
  Context:     local
  Auth method: token
  Version:     11.5.0
  Grafana Cloud: no
`,
			wantStderrSubs: []string{
				"Verify access anytime with: gcx config check",
			},
		},
		{
			name:   "empty_server_falls_back_to_context_name",
			server: "",
			result: internallogin.Result{
				ContextName: "prod",
				AuthMethod:  "token",
				IsCloud:     false,
			},
			wantStdout: `Logged in to prod
  Context:     prod
  Auth method: token
  Grafana Cloud: no
`,
			wantStderrSubs: []string{
				"Verify access anytime with: gcx config check",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// No t.Parallel() — this subtest sets env vars via t.Setenv to
			// disable agent-mode detection, which would otherwise flip the
			// default output format to "json" and break the golden-text
			// comparisons. t.Setenv is incompatible with t.Parallel().
			disableAgentMode(t)

			cmd := &cobra.Command{}
			var stdout, stderr bytes.Buffer
			cmd.SetOut(&stdout)
			cmd.SetErr(&stderr)

			ioOpts := &cmdio.Options{}
			ioOpts.RegisterCustomCodec("text", &loginTextCodec{})
			ioOpts.DefaultFormat("text")
			fs := pflag.NewFlagSet("test", pflag.ContinueOnError)
			ioOpts.BindFlags(fs)
			require.NoError(t, ioOpts.Validate())

			err := printResult(cmd, ioOpts, tt.server, tt.result)
			require.NoError(t, err)

			assert.Equal(t, tt.wantStdout, stdout.String(), "stdout mismatch")
			if tt.noStderr {
				assert.Empty(t, stderr.String(), "expected no stderr output")
			} else {
				for _, sub := range tt.wantStderrSubs {
					assert.Contains(t, stderr.String(), sub, "stderr should contain %q", sub)
				}
			}
		})
	}
}

// TestResolveSourceContext covers every branch of the context-selection
// switch. The regression case is "no context name + --server pointing at a
// different server than the current context" — that must NOT refresh the
// current context.
func TestResolveSourceContext(t *testing.T) {
	t.Parallel()

	current := &config.Context{
		Name:    "alpha-dev",
		Grafana: &config.GrafanaConfig{Server: "https://alpha.grafana-dev.net/"},
	}
	other := &config.Context{
		Name:    "beta-ops",
		Grafana: &config.GrafanaConfig{Server: "https://beta.grafana-ops.net/"},
	}
	cfg := config.Config{
		Contexts: map[string]*config.Context{
			"alpha-dev": current,
			"beta-ops":  other,
		},
		CurrentContext: "alpha-dev",
	}
	empty := config.Config{}

	tests := []struct {
		name        string
		cfg         config.Config
		contextName string
		server      string
		wantSource  *config.Context
		wantName    string
	}{
		{
			name:       "no_name_no_server_uses_current",
			cfg:        cfg,
			wantSource: current,
			wantName:   "alpha-dev",
		},
		{
			name:       "no_name_server_matches_existing_refreshes_it",
			cfg:        cfg,
			server:     "https://beta.grafana-ops.net/",
			wantSource: other,
			wantName:   "beta-ops",
		},
		{
			name:       "no_name_server_differs_from_current_creates_new",
			cfg:        cfg,
			server:     "https://example.test.local/",
			wantSource: nil,
			wantName:   "example-test-local",
		},
		{
			name:        "named_existing_refreshes_named",
			cfg:         cfg,
			contextName: "beta-ops",
			wantSource:  other,
			wantName:    "beta-ops",
		},
		{
			name:        "named_missing_creates_new",
			cfg:         cfg,
			contextName: "gamma",
			wantSource:  nil,
			wantName:    "gamma",
		},
		{
			name:        "named_takes_precedence_over_server",
			cfg:         cfg,
			contextName: "beta-ops",
			server:      "https://different.example.test/",
			wantSource:  other,
			wantName:    "beta-ops",
		},
		{
			name:       "empty_config_no_inputs_first_time_setup",
			cfg:        empty,
			wantSource: nil,
			wantName:   "",
		},
		{
			name:       "empty_config_with_server_derives_name",
			cfg:        empty,
			server:     "https://example.grafana.net/",
			wantSource: nil,
			wantName:   "example",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			gotSource, gotName := resolveSourceContext(tt.cfg, tt.contextName, tt.server)
			assert.Same(t, tt.wantSource, gotSource, "sourceCtx mismatch")
			assert.Equal(t, tt.wantName, gotName, "contextName mismatch")
		})
	}
}

// disableAgentMode forces agent.IsAgentMode() to return false for the
// duration of the test, regardless of the host env (e.g. `CLAUDECODE=1`
// during local agent sessions). Without this, cmdio.BindFlags would flip
// the default output format to "json" and break golden text comparisons.
//
// It unsets every known agent env var, sets GCX_AGENT_MODE=false (which
// takes priority over all others), and calls agent.ResetForTesting to
// re-derive the cached package-level state. On cleanup, it restores the
// original env and re-detects so subsequent tests in the process see the
// original agent-mode value.
func disableAgentMode(t *testing.T) {
	t.Helper()
	// t.Setenv handles both set-and-restore for us. Clearing every known
	// agent env var covers CLAUDECODE, CURSOR_AGENT, etc. in one pass.
	for _, v := range []string{
		"GCX_AGENT_MODE",
		"CLAUDECODE",
		"CLAUDE_CODE",
		"CURSOR_AGENT",
		"GITHUB_COPILOT",
		"AMAZON_Q",
		"OPENCODE",
	} {
		t.Setenv(v, "")
	}
	// GCX_AGENT_MODE=false is the authoritative override.
	t.Setenv("GCX_AGENT_MODE", "false")
	agent.ResetForTesting()
	t.Cleanup(agent.ResetForTesting)
}
