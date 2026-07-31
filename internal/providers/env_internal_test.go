package providers

import (
	"testing"

	"github.com/grafana/gcx/internal/resources/adapter"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
)

type providerEnvTestProvider struct {
	name string
	keys []ConfigKey
}

func (p providerEnvTestProvider) Name() string                               { return p.name }
func (p providerEnvTestProvider) ShortDesc() string                          { return "test provider" }
func (p providerEnvTestProvider) Commands() []*cobra.Command                 { return nil }
func (p providerEnvTestProvider) Validate(map[string]string) error           { return nil }
func (p providerEnvTestProvider) ConfigKeys() []ConfigKey                    { return p.keys }
func (p providerEnvTestProvider) TypedRegistrations() []adapter.Registration { return nil }

func TestBlankProviderCredentialEnvironmentOverride(t *testing.T) {
	registered := []Provider{providerEnvTestProvider{
		name: "example",
		keys: []ConfigKey{
			{Name: "api-token", Secret: true},
			{Name: "api-url"},
		},
	}}

	tests := map[string]struct {
		envKey string
		value  string
		want   bool
	}{
		"trust-bound synth credential": {
			envKey: "GRAFANA_PROVIDER_SYNTH_SM_TOKEN",
			value:  " \t\n ",
			want:   true,
		},
		"registered generic credential": {
			envKey: "GRAFANA_PROVIDER_EXAMPLE_API_TOKEN",
			value:  "\t",
			want:   true,
		},
		"registered non-secret field": {
			envKey: "GRAFANA_PROVIDER_EXAMPLE_API_URL",
			value:  "",
			want:   false,
		},
		"nonblank credential": {
			envKey: "GRAFANA_PROVIDER_EXAMPLE_API_TOKEN",
			value:  "token",
			want:   false,
		},
		"unknown provider field": {
			envKey: "GRAFANA_PROVIDER_UNKNOWN_API_URL",
			value:  "",
			want:   false,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, test.want,
				isBlankProviderCredentialEnvironmentOverride(test.envKey, test.value, registered))
		})
	}
}
