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

func (p *PermissionsProvider) Name() string { return "permissions" }

func (p *PermissionsProvider) ShortDesc() string {
	return "Manage Grafana folder and dashboard permissions"
}

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
	root.AddCommand(
		resourceCommands(loader, folderKind),
		resourceCommands(loader, dashboardKind),
	)

	return []*cobra.Command{root}
}

func (p *PermissionsProvider) Validate(map[string]string) error { return nil }

func (p *PermissionsProvider) ConfigKeys() []providers.ConfigKey { return nil }

// TypedRegistrations returns adapter registrations for folder and dashboard
// permission resource types.
func (p *PermissionsProvider) TypedRegistrations() []adapter.Registration {
	loader := &providers.ConfigLoader{}
	return []adapter.Registration{
		{
			Factory:    NewFolderAdapterFactory(loader),
			Descriptor: staticFolderDescriptor,
			GVK:        staticFolderDescriptor.GroupVersionKind(),
			Schema:     FolderPermissionsSchema(),
			Example:    FolderPermissionsExample(),
		},
		{
			Factory:    NewDashboardAdapterFactory(loader),
			Descriptor: staticDashboardDescriptor,
			GVK:        staticDashboardDescriptor.GroupVersionKind(),
			Schema:     DashboardPermissionsSchema(),
			Example:    DashboardPermissionsExample(),
		},
	}
}
