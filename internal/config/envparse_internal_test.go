package config

import (
	"context"
	"testing"

	"github.com/grafana/gcx/internal/credentials"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseEnvIntoContextDetachesSharedStackRuntimeView(t *testing.T) {
	t.Setenv("GRAFANA_SERVER", "https://runtime.grafana.invalid")
	t.Setenv("GRAFANA_TOKEN", "runtime-token")
	t.Setenv("GRAFANA_TLS_CA_FILE", "/runtime/ca.pem")

	stack := &StackConfig{
		Name:           "shared",
		sourceIdentity: "/trusted/config.yaml",
		sourceLayer:    "user",
		Slug:           "persisted-slug",
		Grafana: &GrafanaConfig{
			Server:     "https://persisted.grafana.invalid",
			APIToken:   "persisted-token",
			AuthMethod: "token",
			OrgID:      1,
			TLS: &TLS{
				CAFile:                  "/persisted/ca.pem",
				CAData:                  []byte("persisted-ca-data"),
				NextProtos:              []string{"h2", "http/1.1"},
				credentialFilesCaptured: true,
				credentialCAFile: tlsFileSnapshot{
					path:        "/persisted/ca.pem",
					fingerprint: "persisted-ca-fingerprint",
					contents:    []byte("persisted-ca-snapshot"),
				},
			},
		},
		Providers: map[string]map[string]string{
			"synth": {
				"sm-url":   "https://persisted.sm.invalid",
				"sm-token": "persisted-sm-token",
			},
		},
		Resources: &ResourcesConfig{AssumeServerDryRun: []string{"dashboards.grafana.app"}},
	}
	stack.rejectCredential(credentials.FieldGrafanaToken, "persisted rejection evidence")

	cfg := Config{
		Stacks: map[string]*StackConfig{"shared": stack},
		Contexts: map[string]*Context{
			"selected": {Stack: "shared"},
			"sibling":  {Stack: "shared"},
		},
		CurrentContext: "selected",
	}
	cfg.Resolve()

	originalStack := cfg.Stacks["shared"]
	originalGrafana := originalStack.Grafana
	originalTLS := originalGrafana.TLS
	sibling := cfg.Contexts["sibling"]
	selected := cfg.Contexts["selected"]
	require.Same(t, originalStack, sibling.StackEntry)
	require.Same(t, originalStack, selected.StackEntry)

	require.NoError(t, ParseEnvIntoContext(selected))

	assert.NotSame(t, originalStack, selected.StackEntry)
	assert.NotSame(t, originalGrafana, selected.Grafana)
	assert.NotSame(t, originalTLS, selected.Grafana.TLS)
	assert.Same(t, selected.Grafana, selected.StackEntry.Grafana)
	assert.Equal(t, "https://runtime.grafana.invalid", selected.Grafana.Server)
	assert.Equal(t, "runtime-token", selected.Grafana.APIToken)
	assert.Equal(t, "/runtime/ca.pem", selected.Grafana.TLS.CAFile)

	// Provider overlays run immediately after ParseEnvIntoContext. Mutating both
	// an existing nested map and a newly added provider must remain selected-only.
	selected.Providers["synth"]["sm-url"] = "https://runtime.sm.invalid"
	selected.Providers["synth"]["sm-token"] = "runtime-sm-token"
	selected.Providers["new-provider"] = map[string]string{"url": "https://runtime.invalid"}
	assert.Equal(t, "runtime-sm-token", selected.StackEntry.Providers["synth"]["sm-token"])
	assert.Equal(t, "persisted-sm-token", originalStack.Providers["synth"]["sm-token"])
	assert.NotContains(t, originalStack.Providers, "new-provider")

	// All mutable nested TLS and resource state is detached too, including the
	// captured file bytes used to bind credentials to their transport identity.
	selected.Grafana.TLS.CAData[0] = 'R'
	selected.Grafana.TLS.NextProtos[0] = "runtime-proto"
	selected.Grafana.TLS.credentialCAFile.contents[0] = 'R'
	selected.StackEntry.Resources.AssumeServerDryRun[0] = "runtime.grafana.app"
	assert.Equal(t, []byte("persisted-ca-data"), originalTLS.CAData)
	assert.Equal(t, []string{"h2", "http/1.1"}, originalTLS.NextProtos)
	assert.Equal(t, []byte("persisted-ca-snapshot"), originalTLS.credentialCAFile.contents)
	assert.Equal(t, []string{"dashboards.grafana.app"}, originalStack.Resources.AssumeServerDryRun)

	// Owner provenance and rejection evidence survive on the runtime clone, but
	// its map is independent so later binding enforcement cannot mutate disk state.
	assert.Equal(t, originalStack.Name, selected.StackEntry.Name)
	assert.Equal(t, originalStack.sourceIdentity, selected.StackEntry.sourceIdentity)
	assert.Equal(t, originalStack.sourceLayer, selected.StackEntry.sourceLayer)
	require.Error(t, selected.StackEntry.credentialRejection(credentials.FieldGrafanaToken))
	selected.StackEntry.clearCredentialRejection(credentials.FieldGrafanaToken)
	require.NoError(t, selected.StackEntry.credentialRejection(credentials.FieldGrafanaToken))
	require.Error(t, originalStack.credentialRejection(credentials.FieldGrafanaToken))

	// The sibling and the named stack retain the exact persisted view.
	assert.Same(t, originalStack, cfg.Stacks["shared"])
	assert.Same(t, originalStack, sibling.StackEntry)
	assert.Same(t, originalGrafana, sibling.Grafana)
	assert.Equal(t, "https://persisted.grafana.invalid", sibling.Grafana.Server)
	assert.Equal(t, "persisted-token", sibling.Grafana.APIToken)
	assert.Equal(t, "/persisted/ca.pem", sibling.Grafana.TLS.CAFile)
	assert.Equal(t, "persisted-sm-token", sibling.Providers["synth"]["sm-token"])
}

func TestRuntimeBindingEnforcementUsesDetachedStackIdentity(t *testing.T) {
	t.Setenv("GRAFANA_SERVER", "https://runtime.grafana.invalid")
	t.Setenv("GRAFANA_TOKEN", "runtime-token")

	stack := &StackConfig{
		sourceIdentity: "/trusted/config.yaml",
		sourceLayer:    "user",
		Grafana: &GrafanaConfig{
			Server:     "https://persisted.grafana.invalid",
			APIToken:   "persisted-token",
			AuthMethod: "token",
			OrgID:      1,
		},
	}
	stack.rejectCredential(credentials.FieldGrafanaToken, "previously rejected")
	cfg := Config{
		Stacks: map[string]*StackConfig{"shared": stack},
		Contexts: map[string]*Context{
			"selected": {Stack: "shared"},
			"sibling":  {Stack: "shared"},
		},
		CurrentContext: "selected",
	}
	cfg.Resolve()
	cfg.capturePlaintextCredentialOrigins()

	originalBinding := stackOwner("shared", stack).binding(credentials.FieldGrafanaToken)
	selected := cfg.Contexts["selected"]
	require.NoError(t, ParseEnvIntoContext(selected))
	runtimeBinding := stackOwner("shared", selected.StackEntry).binding(credentials.FieldGrafanaToken)
	assert.Equal(t, originalBinding.Source, runtimeBinding.Source)
	assert.Equal(t, originalBinding.Owner, runtimeBinding.Owner)
	assert.Equal(t, originalBinding.Field, runtimeBinding.Field)
	assert.NotEqual(t, originalBinding.Destination, runtimeBinding.Destination)
	assert.True(t, runtimeBinding.Valid())

	require.NoError(t, enforceRuntimeCredentialBindings(&cfg))
	assert.Equal(t, "runtime-token", selected.Grafana.APIToken)
	require.NoError(t, selected.StackEntry.credentialRejection(credentials.FieldGrafanaToken))
	assert.Equal(t, "persisted-token", cfg.Stacks["shared"].Grafana.APIToken)
	require.Error(t, cfg.Stacks["shared"].credentialRejection(credentials.FieldGrafanaToken))
	assert.Equal(t, "persisted-token", cfg.Contexts["sibling"].Grafana.APIToken)
	require.NoError(t, selected.Validate(context.Background()))
}

func TestParseEnvIntoContextLinksSynthesizedRuntimeFieldsToDetachedStack(t *testing.T) {
	t.Setenv("GRAFANA_SERVER", "https://runtime.grafana.invalid")

	stack := &StackConfig{sourceIdentity: "/trusted/config.yaml", sourceLayer: "user"}
	cfg := Config{
		Stacks: map[string]*StackConfig{"shared": stack},
		Contexts: map[string]*Context{
			"selected": {Stack: "shared"},
			"sibling":  {Stack: "shared"},
		},
		CurrentContext: "selected",
	}
	cfg.Resolve()
	selected := cfg.Contexts["selected"]

	require.NoError(t, ParseEnvIntoContext(selected))
	require.NotNil(t, selected.Grafana)
	require.NotNil(t, selected.Providers)
	assert.Same(t, selected.Grafana, selected.StackEntry.Grafana)
	selected.Providers["synth"] = map[string]string{"sm-token": "runtime-sm-token"}
	assert.Equal(t, "runtime-sm-token", selected.StackEntry.Providers["synth"]["sm-token"])

	assert.Nil(t, cfg.Stacks["shared"].Grafana)
	assert.Nil(t, cfg.Stacks["shared"].Providers)
	assert.Nil(t, cfg.Contexts["sibling"].Grafana)
	assert.Nil(t, cfg.Contexts["sibling"].Providers)
}
