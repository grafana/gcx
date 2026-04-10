package auth

import (
	"bytes"
	"context"
	"testing"
	"time"

	configcmd "github.com/grafana/gcx/cmd/gcx/config"
	internalauth "github.com/grafana/gcx/internal/auth"
	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/testutils"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeFlow struct {
	result *internalauth.Result
	err    error
}

func (f fakeFlow) Run(context.Context) (*internalauth.Result, error) {
	return f.result, f.err
}

func TestRunLogin_serverDerivesContextFromURL(t *testing.T) {
	configFile := testutils.CreateTempFile(t, "contexts:\n")

	newFlow := func(server string, _ internalauth.Options) authFlow {
		require.Equal(t, "https://ops.grafana.net", server)
		return fakeFlow{result: &internalauth.Result{
			Email:            "user@example.com",
			APIEndpoint:      "https://proxy.grafana.net",
			Token:            "gat_test_token",
			RefreshToken:     "gar_test_refresh",
			ExpiresAt:        time.Now().Add(time.Hour).Format(time.RFC3339),
			RefreshExpiresAt: time.Now().Add(24 * time.Hour).Format(time.RFC3339),
		}}
	}

	cmd := &cobra.Command{}
	cmd.SetContext(t.Context())
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)

	opts := &loginOpts{
		Config:  configcmd.Options{ConfigFile: configFile},
		Server:  "https://ops.grafana.net",
		NewFlow: newFlow,
	}

	require.NoError(t, runLogin(cmd, opts))
	assert.Contains(t, stderr.String(), "WARNING: OAuth login is experimental")
	assert.Contains(t, stdout.String(), "Authenticated as user@example.com. Tokens saved to context \"ops\".")

	saved, err := config.Load(t.Context(), config.ExplicitConfigFile(configFile))
	require.NoError(t, err)
	assert.Equal(t, "ops", saved.CurrentContext)
	require.Contains(t, saved.Contexts, "ops")
	require.NotNil(t, saved.Contexts["ops"])
	require.NotNil(t, saved.Contexts["ops"].Grafana)
	assert.Equal(t, "https://ops.grafana.net", saved.Contexts["ops"].Grafana.Server)
	assert.Equal(t, "https://proxy.grafana.net", saved.Contexts["ops"].Grafana.ProxyEndpoint)
	assert.Equal(t, "gat_test_token", saved.Contexts["ops"].Grafana.OAuthToken)
	assert.Equal(t, "gar_test_refresh", saved.Contexts["ops"].Grafana.OAuthRefreshToken)
}

func TestRunLogin_serverNonGrafanaNetUsesHostname(t *testing.T) {
	configFile := testutils.CreateTempFile(t, "contexts:\n")

	newFlow := func(server string, _ internalauth.Options) authFlow {
		require.Equal(t, "https://grafana.mycompany.com", server)
		return fakeFlow{result: &internalauth.Result{
			Email:            "user@example.com",
			APIEndpoint:      "https://proxy.grafana.net",
			Token:            "gat_test_token",
			RefreshToken:     "gar_test_refresh",
			ExpiresAt:        time.Now().Add(time.Hour).Format(time.RFC3339),
			RefreshExpiresAt: time.Now().Add(24 * time.Hour).Format(time.RFC3339),
		}}
	}

	cmd := &cobra.Command{}
	cmd.SetContext(t.Context())
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)

	opts := &loginOpts{
		Config:  configcmd.Options{ConfigFile: configFile},
		Server:  "https://grafana.mycompany.com",
		NewFlow: newFlow,
	}

	require.NoError(t, runLogin(cmd, opts))
	assert.Contains(t, stdout.String(), `Tokens saved to context "grafana.mycompany.com".`)

	saved, err := config.Load(t.Context(), config.ExplicitConfigFile(configFile))
	require.NoError(t, err)
	assert.Equal(t, "grafana.mycompany.com", saved.CurrentContext)
	require.Contains(t, saved.Contexts, "grafana.mycompany.com")
	require.NotNil(t, saved.Contexts["grafana.mycompany.com"].Grafana)
	assert.Equal(t, "https://grafana.mycompany.com", saved.Contexts["grafana.mycompany.com"].Grafana.Server)
}

func TestRunLogin_serverCreatesMissingExplicitContext(t *testing.T) {
	configFile := testutils.CreateTempFile(t, "contexts:\n")

	newFlow := func(server string, _ internalauth.Options) authFlow {
		require.Equal(t, "https://ops.grafana.net", server)
		return fakeFlow{result: &internalauth.Result{
			Email:            "user@example.com",
			APIEndpoint:      "https://proxy.grafana.net",
			Token:            "gat_test_token",
			RefreshToken:     "gar_test_refresh",
			ExpiresAt:        time.Now().Add(time.Hour).Format(time.RFC3339),
			RefreshExpiresAt: time.Now().Add(24 * time.Hour).Format(time.RFC3339),
		}}
	}

	cmd := &cobra.Command{}
	cmd.SetContext(t.Context())
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})

	opts := &loginOpts{
		Config:  configcmd.Options{ConfigFile: configFile, Context: "ops"},
		Server:  "https://ops.grafana.net",
		NewFlow: newFlow,
	}

	require.NoError(t, runLogin(cmd, opts))

	saved, err := config.Load(t.Context(), config.ExplicitConfigFile(configFile))
	require.NoError(t, err)
	assert.Equal(t, "ops", saved.CurrentContext)
	require.Contains(t, saved.Contexts, "ops")
	require.NotNil(t, saved.Contexts["ops"])
	require.NotNil(t, saved.Contexts["ops"].Grafana)
	assert.Equal(t, "https://ops.grafana.net", saved.Contexts["ops"].Grafana.Server)
	assert.Equal(t, "gat_test_token", saved.Contexts["ops"].Grafana.OAuthToken)
}
