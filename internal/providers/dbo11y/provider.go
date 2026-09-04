// Package dbo11y implements the `gcx dbo11y` provider, exposing Grafana
// Database Observability data (postgres_exporter + pg_stat_statements metrics
// collected via the `database_observability.postgres` Alloy component /
// `integrations/db-o11y` job convention) as CLI commands.
package dbo11y

import (
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/providers/dbo11y/instances"
	"github.com/grafana/gcx/internal/resources/adapter"
	"github.com/spf13/cobra"
)

func init() { //nolint:gochecknoinits // Self-registration pattern (like database/sql drivers).
	providers.Register(&DBO11yProvider{})
}

// DBO11yProvider manages Grafana Database Observability resources.
type DBO11yProvider struct{}

// Name returns the unique identifier for this provider.
func (p *DBO11yProvider) Name() string { return "dbo11y" }

// ShortDesc returns a one-line description of the provider.
func (p *DBO11yProvider) ShortDesc() string {
	return "Inspect Grafana Database Observability instances and query performance"
}

// ConfigKeys returns the configuration keys used by this provider.
// Database Observability uses the standard Grafana SA token; no additional keys are required.
func (p *DBO11yProvider) ConfigKeys() []providers.ConfigKey { return nil }

// Validate checks provider configuration.
// Database Observability requires no provider-specific configuration.
func (p *DBO11yProvider) Validate(_ map[string]string) error { return nil }

// Commands returns the Cobra commands contributed by this provider.
func (p *DBO11yProvider) Commands() []*cobra.Command {
	loader := &providers.ConfigLoader{}

	cmd := &cobra.Command{
		Use:   "dbo11y",
		Short: p.ShortDesc(),
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			if root := cmd.Root(); root.PersistentPreRun != nil {
				root.PersistentPreRun(cmd, args)
			}
		},
	}

	// Bind config flags on the parent — all subcommands inherit these.
	loader.BindFlags(cmd.PersistentFlags())

	cmd.AddCommand(instances.Commands(loader))
	return []*cobra.Command{cmd}
}

// TypedRegistrations returns adapter registrations for Database Observability
// resource types. Instances are a query/discovery result derived from live
// telemetry, not a CRUD resource, so there is nothing to register.
func (p *DBO11yProvider) TypedRegistrations() []adapter.Registration { return nil }
