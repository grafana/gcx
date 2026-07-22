//nolint:testpackage // white-box: loginCmd is unexported
package cloud

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/grafana/gcx/internal/auth"
	"github.com/grafana/gcx/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type gcomOAuthFlowFunc func(context.Context) (*auth.GCOMResult, error)

func (f gcomOAuthFlowFunc) Run(ctx context.Context) (*auth.GCOMResult, error) {
	return f(ctx)
}

// `gcx cloud login` and the `gcx login` cloud followup must request the same
// scope set (auth.DefaultGCOMScopes) so tokens from either path are equivalent.
func TestLoginScopeFlagDefaultMatchesDefaultGCOMScopes(t *testing.T) {
	scopes, err := loginCmd().Flags().GetStringSlice("scope")
	require.NoError(t, err)
	assert.Equal(t, auth.DefaultGCOMScopes(), scopes)
}

func TestCloudLoginWritesExplicitlySelectedLocalConfig(t *testing.T) {
	userDir, workDir := isolateCloudConfigEnv(t)
	localPath := filepath.Join(workDir, ".gcx.yaml")
	require.NoError(t, os.WriteFile(localPath, []byte("version: 1\ncontexts:\n  default: {}\ncurrent-context: default\n"), 0o600))

	cmd := loginCmd()
	cmd.SilenceErrors = true
	cmd.SilenceUsage = true
	cmd.SetArgs([]string{
		"--config", localPath,
		"--cloud-token", "local-cap",
		"--oauth-url", "https://grafana.com",
		"--api-url", "https://grafana.com",
	})
	require.NoError(t, cmd.ExecuteContext(t.Context()))

	raw, err := os.ReadFile(localPath)
	require.NoError(t, err)
	assert.Contains(t, string(raw), "local-cap")
	_, err = os.Stat(filepath.Join(userDir, "gcx", "config.yaml"))
	assert.ErrorIs(t, err, os.ErrNotExist, "a shadowed user config must not be created")
}

func TestCloudLoginRejectsFreshTokenForAutoDiscoveredLocalConfig(t *testing.T) {
	userDir, workDir := isolateCloudConfigEnv(t)
	localPath := filepath.Join(workDir, ".gcx.yaml")
	original := []byte("version: 1\ncontexts:\n  default: {}\ncurrent-context: default\n")
	require.NoError(t, os.WriteFile(localPath, original, 0o600))

	cmd := loginCmd()
	cmd.SilenceErrors = true
	cmd.SilenceUsage = true
	cmd.SetArgs([]string{
		"--cloud-token", "must-not-be-written",
		"--oauth-url", "https://grafana.com",
		"--api-url", "https://grafana.com",
	})
	err := cmd.ExecuteContext(t.Context())
	require.ErrorContains(t, err, "auto-discovered repository config")
	require.ErrorContains(t, err, "--config "+localPath)

	raw, readErr := os.ReadFile(localPath)
	require.NoError(t, readErr)
	assert.Equal(t, original, raw)
	_, statErr := os.Stat(filepath.Join(userDir, "gcx", "config.yaml"))
	assert.ErrorIs(t, statErr, os.ErrNotExist)
}

func TestCloudLoginRejectsAutoLocalBeforeStartingOAuth(t *testing.T) {
	_, workDir := isolateCloudConfigEnv(t)
	localPath := filepath.Join(workDir, ".gcx.yaml")
	original := []byte(`version: 1
cloud:
  grafana-com:
    oauth-url: https://attacker.invalid
    api-url: https://attacker.invalid
contexts:
  default:
    cloud: grafana-com
current-context: default
`)
	require.NoError(t, os.WriteFile(localPath, original, 0o600))

	started := false
	previousFactory := newGCOMOAuthFlow
	newGCOMOAuthFlow = func(auth.GCOMOptions) gcomOAuthFlow {
		started = true
		return gcomOAuthFlowFunc(func(context.Context) (*auth.GCOMResult, error) {
			return nil, nil
		})
	}
	t.Cleanup(func() { newGCOMOAuthFlow = previousFactory })

	cmd := loginCmd()
	cmd.SilenceErrors = true
	cmd.SilenceUsage = true
	err := cmd.ExecuteContext(t.Context())
	require.ErrorContains(t, err, "auto-discovered repository config")
	assert.False(t, started, "the repository-controlled OAuth endpoint must not receive a fresh flow")
	raw, readErr := os.ReadFile(localPath)
	require.NoError(t, readErr)
	assert.Equal(t, original, raw)
}

func TestCloudLoginRejectsAmbiguousLayeredWrite(t *testing.T) {
	userDir, workDir := isolateCloudConfigEnv(t)
	userPath := filepath.Join(userDir, "gcx", "config.yaml")
	require.NoError(t, os.MkdirAll(filepath.Dir(userPath), 0o755))
	contents := []byte("version: 1\ncontexts:\n  default: {}\ncurrent-context: default\n")
	require.NoError(t, os.WriteFile(userPath, contents, 0o600))
	localPath := filepath.Join(workDir, ".gcx.yaml")
	require.NoError(t, os.WriteFile(localPath, contents, 0o600))

	cmd := loginCmd()
	cmd.SilenceErrors = true
	cmd.SilenceUsage = true
	cmd.SetArgs([]string{"--cloud-token", "must-not-be-written"})
	err := cmd.ExecuteContext(t.Context())
	require.ErrorContains(t, err, "write target is ambiguous")
	require.ErrorContains(t, err, "--config <path>")

	userRaw, readErr := os.ReadFile(userPath)
	require.NoError(t, readErr)
	localRaw, readErr := os.ReadFile(localPath)
	require.NoError(t, readErr)
	assert.Equal(t, contents, userRaw)
	assert.Equal(t, contents, localRaw)
}

func TestCloudLoginUsesEnvironmentTokenWithoutStartingOAuth(t *testing.T) {
	t.Setenv("GRAFANA_CLOUD_TOKEN", "environment-cap")
	path := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte("version: 1\ncontexts:\n  default: {}\ncurrent-context: default\n"), 0o600))

	cmd := loginCmd()
	cmd.SilenceErrors = true
	cmd.SilenceUsage = true
	cmd.SetArgs([]string{"--config", path})
	require.NoError(t, cmd.ExecuteContext(t.Context()))

	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(raw), "environment-cap")
	assert.Contains(t, string(raw), "oauth-url: https://grafana.com")
	assert.Contains(t, string(raw), "api-url: https://grafana.com")
}

func TestCloudTokenLoginCreatesMissingExplicitConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "new-config.yaml")

	cmd := loginCmd()
	cmd.SilenceErrors = true
	cmd.SilenceUsage = true
	cmd.SetArgs([]string{"--config", path, "--cloud-token", "new-cap"})
	require.NoError(t, cmd.ExecuteContext(t.Context()))

	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(raw), "new-cap")
	assert.Contains(t, string(raw), "api-url: https://grafana.com")
	assert.Contains(t, string(raw), "oauth-url: https://grafana.com")
}

func TestCloudLoginRejectsUnsupportedConfigBeforeStartingOAuth(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte("version: 999\ncontexts:\n  default: {}\ncurrent-context: default\n"), 0o600))

	started := false
	previousFactory := newGCOMOAuthFlow
	newGCOMOAuthFlow = func(auth.GCOMOptions) gcomOAuthFlow {
		started = true
		return gcomOAuthFlowFunc(func(context.Context) (*auth.GCOMResult, error) {
			return nil, nil
		})
	}
	t.Cleanup(func() { newGCOMOAuthFlow = previousFactory })

	cmd := loginCmd()
	cmd.SilenceErrors = true
	cmd.SilenceUsage = true
	cmd.SetArgs([]string{"--config", path})
	err := cmd.ExecuteContext(t.Context())
	require.ErrorContains(t, err, "unsupported config version 999")
	assert.False(t, started, "OAuth flow must not start before config preflight succeeds")
}

func isolateCloudConfigEnv(t *testing.T) (string, string) {
	t.Helper()
	userDir := t.TempDir()
	workDir := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", userDir)
	t.Setenv("XDG_CONFIG_DIRS", t.TempDir())
	t.Setenv("GCX_CONFIG", "")
	t.Setenv("GRAFANA_CLOUD_TOKEN", "")
	t.Chdir(workDir)
	return userDir, workDir
}

func TestCloudEntryFromOAuthResultPreservesMetadata(t *testing.T) {
	t.Parallel()

	entry := cloudEntryFromOAuthResult(&loginOpts{
		oauthURL: "https://grafana-ops.com",
		apiURL:   "https://grafana-ops.com",
	}, &auth.GCOMResult{
		AccessToken: "oauth-token",
		ExpiresAt:   "2030-01-01T00:00:00Z",
		Scope:       "stacks:read fleet-management:read",
	})

	assert.Equal(t, "oauth-token", entry.OAuthToken)
	assert.Equal(t, "2030-01-01T00:00:00Z", entry.OAuthTokenExpiresAt)
	assert.Equal(t, []string{"stacks:read", "fleet-management:read"}, entry.OAuthScopes)
	assert.Equal(t, "https://grafana-ops.com", entry.OAuthUrl)
	assert.Equal(t, "https://grafana-ops.com", entry.APIUrl)
}

func TestCloudLoginEntriesAlwaysPersistCompleteEndpointPair(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		oauthURL  string
		apiURL    string
		wantOAuth string
		wantAPI   string
	}{
		{
			name:      "both absent default to production",
			wantOAuth: "https://grafana.com",
			wantAPI:   "https://grafana.com",
		},
		{
			name:      "API endpoint selects the environment pair",
			apiURL:    "grafana-ops.com",
			wantOAuth: "https://grafana-ops.com",
			wantAPI:   "https://grafana-ops.com",
		},
		{
			name:      "OAuth endpoint selects the environment pair",
			oauthURL:  "grafana-dev.com",
			wantOAuth: "https://grafana-dev.com",
			wantAPI:   "https://grafana-dev.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			oauthURL, apiURL := resolveCloudLoginEndpoints(tt.oauthURL, tt.apiURL)
			assert.Equal(t, tt.wantOAuth, oauthURL)
			assert.Equal(t, tt.wantAPI, apiURL)
		})
	}
}

func TestSelectCloudLoginEndpointsKeepsOAuthAndAPICoherent(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		opts          loginOpts
		cur           *config.Context
		oauthSelected bool
		apiSelected   bool
		wantOAuth     string
		wantAPI       string
		wantErr       string
	}{
		{
			name:          "OAuth flag selects both",
			opts:          loginOpts{oauthURL: "grafana-dev.com", apiURL: "https://grafana.com"},
			oauthSelected: true,
			wantOAuth:     "https://grafana-dev.com",
			wantAPI:       "https://grafana-dev.com",
		},
		{
			name:        "API flag selects both",
			opts:        loginOpts{oauthURL: "https://grafana.com", apiURL: "grafana-ops.com"},
			apiSelected: true,
			wantOAuth:   "https://grafana-ops.com",
			wantAPI:     "https://grafana-ops.com",
		},
		{
			name: "legacy partial sticky entry becomes a pair",
			cur: &config.Context{CloudEntry: &config.CloudEntry{
				APIUrl: "grafana-ops.com",
			}},
			wantOAuth: "https://grafana-ops.com",
			wantAPI:   "https://grafana-ops.com",
		},
		{
			name: "different explicitly selected endpoints remain an exact pair",
			opts: loginOpts{oauthURL: "https://grafana-dev.com", apiURL: "https://grafana-ops.com"},
			cur: &config.Context{CloudEntry: &config.CloudEntry{
				OAuthUrl: "https://grafana.com",
				APIUrl:   "https://grafana.com",
			}},
			oauthSelected: true,
			apiSelected:   true,
			wantOAuth:     "https://grafana-dev.com",
			wantAPI:       "https://grafana-ops.com",
		},
		{
			name: "different sticky endpoints remain an exact pair",
			cur: &config.Context{CloudEntry: &config.CloudEntry{
				OAuthUrl: "https://grafana-dev.com",
				APIUrl:   "https://grafana-ops.com",
			}},
			wantOAuth: "https://grafana-dev.com",
			wantAPI:   "https://grafana-ops.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			opts := tt.opts
			err := selectCloudLoginEndpoints(&opts, tt.cur, tt.oauthSelected, tt.apiSelected)
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantOAuth, opts.oauthURL)
			assert.Equal(t, tt.wantAPI, opts.apiURL)
		})
	}
}
