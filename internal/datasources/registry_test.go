package datasources_test

import (
	"testing"

	"github.com/grafana/gcx/internal/datasources"
	"github.com/grafana/gcx/internal/providers"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
)

type mockDSProvider struct{ kind string }

func (m *mockDSProvider) Kind() string      { return m.kind }
func (m *mockDSProvider) ShortDesc() string { return "Mock datasource" }
func (m *mockDSProvider) QueryCmd(_ *providers.ConfigLoader) *cobra.Command {
	return &cobra.Command{Use: "query", Short: "Mock query"}
}
func (m *mockDSProvider) ExtraCommands(_ *providers.ConfigLoader) []*cobra.Command { return nil }

func TestAllProviders_NonNil(t *testing.T) {
	got := datasources.AllProviders()
	assert.NotNil(t, got)
}

func TestRegisterProvider(t *testing.T) {
	before := len(datasources.AllProviders())
	datasources.RegisterProvider(&mockDSProvider{kind: "mock-test"})
	after := len(datasources.AllProviders())
	assert.Equal(t, before+1, after)

	last := datasources.AllProviders()[after-1]
	assert.Equal(t, "mock-test", last.Kind())
	assert.Equal(t, "Mock datasource", last.ShortDesc())
	assert.Equal(t, "query", last.QueryCmd(nil).Use)
}

func TestRegisterProvider_DuplicatePanics(t *testing.T) {
	datasources.RegisterProvider(&mockDSProvider{kind: "mock-dup"})
	assert.Panics(t, func() {
		datasources.RegisterProvider(&mockDSProvider{kind: "mock-dup"})
	})
}
