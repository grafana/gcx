package appo11y

import (
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/resources/adapter"
	"github.com/spf13/cobra"
)

func init() { //nolint:gochecknoinits // Self-registration pattern (like database/sql drivers).
	providers.Register(&AppO11yProvider{})
}

// AppO11yProvider manages Grafana App Observability resources.
type AppO11yProvider struct{}

// Name returns the unique identifier for this provider.
func (p *AppO11yProvider) Name() string { return "appo11y" }

// ShortDesc returns a one-line description of the provider.
func (p *AppO11yProvider) ShortDesc() string {
	return "Manage Grafana App Observability settings"
}

// ConfigKeys returns the configuration keys used by this provider.
// App Observability uses the standard Grafana SA token; no additional keys are required.
func (p *AppO11yProvider) ConfigKeys() []providers.ConfigKey { return nil }

// Validate checks provider configuration.
// App Observability requires no provider-specific configuration.
func (p *AppO11yProvider) Validate(_ map[string]string) error { return nil }

// Commands returns the Cobra commands contributed by this provider.
// Subcommands (overrides, settings) are wired in T4.
func (p *AppO11yProvider) Commands() []*cobra.Command {
	return nil
}

// TypedRegistrations returns adapter registrations for App Observability resource types.
// Registrations are wired in T4.
func (p *AppO11yProvider) TypedRegistrations() []adapter.Registration {
	return nil
}
