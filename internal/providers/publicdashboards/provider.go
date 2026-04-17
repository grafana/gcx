package publicdashboards

import (
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/resources/adapter"
	"github.com/spf13/cobra"
)

var _ providers.Provider = &PublicDashboardsProvider{}

func init() { //nolint:gochecknoinits // Self-registration pattern (like database/sql drivers).
	providers.Register(&PublicDashboardsProvider{})
}

// PublicDashboardsProvider manages Grafana public dashboard resources.
type PublicDashboardsProvider struct{}

// Name returns the unique identifier for this provider.
func (p *PublicDashboardsProvider) Name() string { return "public-dashboards" }

// ShortDesc returns a one-line description of the provider.
func (p *PublicDashboardsProvider) ShortDesc() string { return "Manage public dashboards" }

// Commands returns the Cobra commands contributed by this provider.
func (p *PublicDashboardsProvider) Commands() []*cobra.Command {
	loader := &providers.ConfigLoader{}

	cmd := &cobra.Command{
		Use:   "public-dashboards",
		Short: p.ShortDesc(),
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			if root := cmd.Root(); root.PersistentPreRun != nil {
				root.PersistentPreRun(cmd, args)
			}
		},
	}

	loader.BindFlags(cmd.PersistentFlags())

	cmd.AddCommand(
		newListCommand(loader),
		newGetCommand(loader),
		newCreateCommand(loader),
		newUpdateCommand(loader),
		newDeleteCommand(loader),
	)

	return []*cobra.Command{cmd}
}

// Validate checks that the given provider configuration is valid.
func (p *PublicDashboardsProvider) Validate(cfg map[string]string) error {
	return nil
}

// ConfigKeys returns the configuration keys used by this provider.
func (p *PublicDashboardsProvider) ConfigKeys() []providers.ConfigKey {
	return nil
}

// TypedRegistrations returns adapter registrations for typed resources.
// Public dashboards are not exposed through the unified resources pipeline.
func (p *PublicDashboardsProvider) TypedRegistrations() []adapter.Registration {
	return nil
}
