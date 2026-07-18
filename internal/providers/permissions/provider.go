package permissions

import (
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/resources/adapter"
	"github.com/spf13/cobra"
)

var _ providers.Provider = &PermissionsProvider{}

func init() { //nolint:gochecknoinits // Self-registration pattern (like database/sql drivers).
	providers.Register(&PermissionsProvider{})
}

// PermissionsProvider manages Grafana resource permissions via the granular
// access-control (RBAC) API.
type PermissionsProvider struct{}

// Name returns the unique identifier for this provider.
func (p *PermissionsProvider) Name() string { return "permissions" }

// ShortDesc returns a one-line description of the provider.
func (p *PermissionsProvider) ShortDesc() string {
	return "Manage Grafana resource permissions (RBAC)"
}

// Commands returns the Cobra commands contributed by this provider.
func (p *PermissionsProvider) Commands() []*cobra.Command {
	loader := &providers.ConfigLoader{}

	root := &cobra.Command{
		Use:   "permissions",
		Short: p.ShortDesc(),
		Long: "Manage Grafana resource permissions via the granular access-control (RBAC) API.\n\n" +
			"Supported resources: folders, dashboards, datasources, teams, serviceaccounts.\n" +
			"Reads work on all editions; writes require RBAC (Grafana Enterprise or Cloud).",
	}
	root.PersistentPreRun = func(cmd *cobra.Command, args []string) {
		if r := cmd.Root(); r != root && r.PersistentPreRun != nil {
			r.PersistentPreRun(cmd, args)
		}
	}

	loader.BindFlags(root.PersistentFlags())
	root.AddCommand(
		newGetCommand(loader),
		newSetCommand(loader),
		newGrantCommand(loader),
		newLevelsCommand(loader),
	)

	return []*cobra.Command{root}
}

// Validate checks that the given provider configuration is valid.
func (p *PermissionsProvider) Validate(map[string]string) error { return nil }

// ConfigKeys returns the configuration keys used by this provider.
func (p *PermissionsProvider) ConfigKeys() []providers.ConfigKey { return nil }

// TypedRegistrations returns no adapter registrations: RBAC permissions are a
// per-resource attribute across five resource kinds, not standalone resources,
// so this provider is command-only.
func (p *PermissionsProvider) TypedRegistrations() []adapter.Registration { return nil }
