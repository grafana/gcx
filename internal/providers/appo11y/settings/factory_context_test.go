package settings_test

import (
	"testing"

	"github.com/grafana/gcx/internal/providers/appo11y/settings"
	"github.com/grafana/gcx/internal/testutils"
)

// Guards the resources-tier half of the #1048 contract: the lazy adapter
// factory constructs a zero-value ConfigLoader, so an explicitly selected
// config file must reach it through ctx threading (what the generic
// `gcx resources ... --config` path sets up via config.ContextWithConfigFile).
func TestNewLazyFactory_HonorsContextThreadedConfigFile(t *testing.T) {
	testutils.AssertFactoryHonorsThreadedConfigFile(t, settings.NewLazyFactory(), `{}`, nil)
}
