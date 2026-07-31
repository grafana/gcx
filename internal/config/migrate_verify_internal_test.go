package config

import (
	"bytes"
	"testing"

	"github.com/grafana/gcx/internal/credentials"
	"github.com/grafana/gcx/internal/format"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const legacyVerificationConfig = `
contexts:
  prod:
    grafana:
      server: https://prod.example
      token: keychain:gcx:prod:grafana-token
    providers:
      slo:
        org-id: "42"
      synth:
        sm-token: keychain:gcx:prod:sm-token
    resources:
      assume-server-dry-run:
        - receivers.notifications.alerting.grafana.app
current-context: prod
`

func decodeLegacyVerificationConfig(t *testing.T) legacyConfig {
	t.Helper()
	var decoded legacyConfig
	codec := &format.YAMLCodec{BytesAsBase64: true}
	require.NoError(t, codec.Decode(bytes.NewBufferString(legacyVerificationConfig), &decoded))
	return decoded
}

func TestConvertLegacyConfigDoesNotMutateOrAliasInput(t *testing.T) {
	input := decodeLegacyVerificationConfig(t)
	baseline := decodeLegacyVerificationConfig(t)
	secrets := map[legacySecretKey]string{
		{context: "prod", field: credentials.FieldGrafanaToken}: "resolved-grafana-token",
		{context: "prod", field: credentials.FieldSMToken}:      "resolved-sm-token",
	}

	converted := convertLegacyConfig(&input, "user", secrets)
	assert.Equal(t, baseline, input, "conversion must leave its legacy input immutable")
	require.NotNil(t, converted.Stacks["prod"])
	assert.Equal(t, "resolved-grafana-token", converted.Stacks["prod"].Grafana.APIToken)
	assert.Equal(t, "resolved-sm-token", converted.Stacks["prod"].Providers["synth"]["sm-token"])

	// Mutating the converted tree must not mutate the independently decoded
	// legacy baseline used by the migration verifier.
	converted.Stacks["prod"].Grafana.Server = "https://corrupt.example"
	converted.Stacks["prod"].Providers["slo"]["org-id"] = "999"
	converted.Stacks["prod"].Resources.AssumeServerDryRun[0] = "corrupt.example"
	assert.Equal(t, baseline, input)
}

func TestVerifyLegacyMigrationDetectsCredentialRoutingCorruption(t *testing.T) {
	tests := []struct {
		name      string
		corrupt   func(*Config)
		wantError string
	}{
		{
			name: "grafana server",
			corrupt: func(cfg *Config) {
				cfg.Stacks["prod"].Grafana.Server = "https://wrong.example"
			},
			wantError: "grafana config differs",
		},
		{
			name: "grafana token",
			corrupt: func(cfg *Config) {
				cfg.Stacks["prod"].Grafana.APIToken = "wrong-token"
			},
			wantError: "grafana config differs",
		},
		{
			name: "provider value",
			corrupt: func(cfg *Config) {
				cfg.Stacks["prod"].Providers["slo"]["org-id"] = "999"
			},
			wantError: "provider config differs",
		},
		{
			name: "provider credential",
			corrupt: func(cfg *Config) {
				cfg.Stacks["prod"].Providers["synth"]["sm-token"] = "wrong-token"
			},
			wantError: "provider config differs",
		},
		{
			name: "resource dry run set",
			corrupt: func(cfg *Config) {
				cfg.Contexts["prod"].assumeServerDryRun = []string{"wrong.example"}
			},
			wantError: "assume-server-dry-run differs",
		},
	}

	secrets := map[legacySecretKey]string{
		{context: "prod", field: credentials.FieldGrafanaToken}: "resolved-grafana-token",
		{context: "prod", field: credentials.FieldSMToken}:      "resolved-sm-token",
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			legacy := decodeLegacyVerificationConfig(t)
			converted := convertLegacyConfig(&legacy, "user", secrets)
			require.NoError(t, verifyLegacyMigration(&legacy, converted, secrets))

			tc.corrupt(converted)
			err := verifyLegacyMigration(&legacy, converted, secrets)
			require.ErrorContains(t, err, tc.wantError)
		})
	}
}

func TestSameStringSetTreatsInputsAsIndependentUniqueSets(t *testing.T) {
	tests := []struct {
		name string
		a    []string
		b    []string
		want bool
	}{
		{name: "same", a: []string{"a", "b"}, b: []string{"b", "a"}, want: true},
		{name: "duplicates on left", a: []string{"a", "a"}, b: []string{"a"}, want: true},
		{name: "duplicates on right", a: []string{"a"}, b: []string{"a", "a"}, want: true},
		{name: "duplicates on both", a: []string{"a", "b", "a"}, b: []string{"b", "b", "a"}, want: true},
		{name: "different unique values", a: []string{"a", "a"}, b: []string{"a", "b"}, want: false},
		{name: "empty", a: nil, b: []string{}, want: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, sameStringSet(tc.a, tc.b))
			assert.Equal(t, tc.want, sameStringSet(tc.b, tc.a), "set comparison must be symmetric")
		})
	}
}
