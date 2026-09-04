package providers_test

import (
	"testing"

	"github.com/grafana/gcx/internal/datasources"
	_ "github.com/grafana/gcx/internal/datasources/providers"
	"github.com/grafana/gcx/internal/providers"
)

func TestPyroscopeProviderIncludesSeries(t *testing.T) {
	t.Helper()

	for _, provider := range datasources.AllProviders() {
		if provider.Kind() != "pyroscope" {
			continue
		}

		for _, cmd := range provider.ExtraCommands(&providers.ConfigLoader{}) {
			if cmd.Name() == "series" {
				return
			}
		}
		t.Error("Pyroscope datasource provider is missing series command")
		return
	}

	t.Error("Pyroscope datasource provider is not registered")
}
