package testhelpers

import (
	"testing"

	"github.com/grafana/gcx/internal/providers"
)

// SetupTestRegistry replaces the global provider registry with ps for the
// duration of t and automatically restores it via t.Cleanup. It returns the
// slice passed in, allowing inline construction patterns such as:
//
//	ps := testhelpers.SetupTestRegistry(t, []providers.Provider{fake1, fake2})
func SetupTestRegistry(t *testing.T, ps []providers.Provider) []providers.Provider {
	t.Helper()
	restore := providers.SetRegistryForTest(ps)
	t.Cleanup(restore)
	return ps
}
