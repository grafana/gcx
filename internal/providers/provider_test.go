package providers_test

import (
	"testing"

	"github.com/grafana/grafanactl/internal/providers"
	"github.com/grafana/grafanactl/internal/resources/adapter"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

var _ providers.Provider = &mockProvider{}

type mockProvider struct {
	name       string
	shortDesc  string
	commands   []*cobra.Command
	validateFn func(cfg map[string]string) error
	configKeys []providers.ConfigKey
}

func (m *mockProvider) Name() string                         { return m.name }
func (m *mockProvider) ShortDesc() string                    { return m.shortDesc }
func (m *mockProvider) Commands() []*cobra.Command           { return m.commands }
func (m *mockProvider) Validate(cfg map[string]string) error { return m.validateFn(cfg) }
func (m *mockProvider) ConfigKeys() []providers.ConfigKey    { return m.configKeys }
func (m *mockProvider) TypedRegistrations() []adapter.Registration { return nil }

func TestAll(t *testing.T) {
	t.Run("returns empty slice when no providers are registered at the internal layer", func(t *testing.T) {
		got := providers.All()
		require.NotNil(t, got)
		require.Empty(t, got)
	})
}

func TestMockProviderSatisfiesInterface(t *testing.T) {
	tests := []struct {
		name     string
		provider providers.Provider
		wantName string
		wantDesc string
		wantKeys []providers.ConfigKey
	}{
		{
			name: "mock provider returns expected values",
			provider: &mockProvider{
				name:      "test-provider",
				shortDesc: "A test provider.",
				commands:  []*cobra.Command{{Use: "test"}},
				validateFn: func(_ map[string]string) error {
					return nil
				},
				configKeys: []providers.ConfigKey{
					{Name: "token", Secret: true},
					{Name: "url", Secret: false},
				},
			},
			wantName: "test-provider",
			wantDesc: "A test provider.",
			wantKeys: []providers.ConfigKey{
				{Name: "token", Secret: true},
				{Name: "url", Secret: false},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.wantName, tc.provider.Name())
			require.Equal(t, tc.wantDesc, tc.provider.ShortDesc())
			require.Len(t, tc.provider.Commands(), 1)
			require.NoError(t, tc.provider.Validate(map[string]string{}))
			require.Equal(t, tc.wantKeys, tc.provider.ConfigKeys())
		})
	}
}
