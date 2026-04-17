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

// PermissionsProvider manages Grafana folder and dashboard permissions.
type PermissionsProvider struct{}

// Name returns the unique identifier for this provider.
func (p *PermissionsProvider) Name() string { return "permissions" }

// ShortDesc returns a one-line description of the provider.
func (p *PermissionsProvider) ShortDesc() string {
	return "Manage Grafana folder and dashboard permissions"
}

// Commands returns the Cobra commands contributed by this provider.
func (p *PermissionsProvider) Commands() []*cobra.Command {
	loader := &providers.ConfigLoader{}

	root := &cobra.Command{
		Use:   "permissions",
		Short: p.ShortDesc(),
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			if r := cmd.Root(); r.PersistentPreRun != nil {
				r.PersistentPreRun(cmd, args)
			}
		},
	}

	loader.BindFlags(root.PersistentFlags())

	root.AddCommand(folderCommands(loader))
	root.AddCommand(dashboardCommands(loader))

	return []*cobra.Command{root}
}

// Validate checks that the given provider configuration is valid.
func (p *PermissionsProvider) Validate(cfg map[string]string) error {
	return nil
}

// ConfigKeys returns the configuration keys used by this provider.
func (p *PermissionsProvider) ConfigKeys() []providers.ConfigKey {
	return nil
}

// TypedRegistrations returns adapter registrations. Permissions are
// exposed as imperative verbs only, so there are no typed registrations.
func (p *PermissionsProvider) TypedRegistrations() []adapter.Registration {
	return nil
}
