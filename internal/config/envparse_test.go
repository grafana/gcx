package config_test

import (
	"testing"

	"github.com/grafana/gcx/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseEnvIntoContext_StringFields(t *testing.T) {
	t.Setenv("GRAFANA_SERVER", "https://example.com")

	var ctx config.Context
	require.NoError(t, config.ParseEnvIntoContext(&ctx))
	assert.Equal(t, "https://example.com", ctx.Grafana.Server)
}

func TestParseEnvIntoContext_CloudFields(t *testing.T) {
	t.Setenv("GRAFANA_CLOUD_STACK", "mystack")
	t.Setenv("GRAFANA_CLOUD_TOKEN", "env-token")

	var ctx config.Context
	require.NoError(t, config.ParseEnvIntoContext(&ctx))
	assert.Equal(t, "mystack", ctx.ResolveStackSlug())
	require.NotNil(t, ctx.CloudEntry)
	assert.Equal(t, "env-token", ctx.CloudEntry.Token)
}

func TestParseEnvIntoContext_Int64Fields(t *testing.T) {
	t.Setenv("GRAFANA_STACK_ID", "12345")

	var ctx config.Context
	require.NoError(t, config.ParseEnvIntoContext(&ctx))
	assert.Equal(t, int64(12345), ctx.Grafana.StackID)
}

func TestParseEnvIntoContext_EmptyBoolSkipped(t *testing.T) {
	t.Setenv("GCX_AUTO_APPROVE", "")

	opts, err := config.LoadCLIOptions()
	require.NoError(t, err)
	assert.False(t, opts.AutoApprove)
}

func TestParseEnvIntoContext_EmptyInt64Skipped(t *testing.T) {
	t.Setenv("GRAFANA_STACK_ID", "")

	var ctx config.Context
	require.NoError(t, config.ParseEnvIntoContext(&ctx))
	assert.Equal(t, int64(0), ctx.Grafana.StackID)
}

func TestParseEnvIntoContext_EmptyStringIsSet(t *testing.T) {
	t.Setenv("GRAFANA_SERVER", "")

	var ctx config.Context
	require.NoError(t, config.ParseEnvIntoContext(&ctx))
	assert.Empty(t, ctx.Grafana.Server)
}

func TestParseEnvIntoContext_NestedTLS(t *testing.T) {
	t.Setenv("GRAFANA_TLS_CERT_FILE", "/path/to/cert.pem")

	var ctx config.Context
	require.NoError(t, config.ParseEnvIntoContext(&ctx))
	require.NotNil(t, ctx.Grafana.TLS)
	assert.Equal(t, "/path/to/cert.pem", ctx.Grafana.TLS.CertFile)
}

func TestParseEnvIntoContext_CleansUpEmptyTLS(t *testing.T) {
	// No TLS env vars set - TLS struct should be nil after cleanup.
	var ctx config.Context
	require.NoError(t, config.ParseEnvIntoContext(&ctx))
	assert.Nil(t, ctx.Grafana.TLS)
}

func TestLoadCLIOptions_BoolTrue(t *testing.T) {
	t.Setenv("GCX_AUTO_APPROVE", "true")

	opts, err := config.LoadCLIOptions()
	require.NoError(t, err)
	assert.True(t, opts.AutoApprove)
}

func TestLoadCLIOptions_InvalidBoolErrors(t *testing.T) {
	t.Setenv("GCX_AUTO_APPROVE", "notabool")

	_, err := config.LoadCLIOptions()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "GCX_AUTO_APPROVE")
}
