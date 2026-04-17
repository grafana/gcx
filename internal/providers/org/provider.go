package org

import (
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/resources/adapter"
	"github.com/spf13/cobra"
)

var _ providers.Provider = &OrgProvider{}

func init() { //nolint:gochecknoinits // Self-registration pattern (like database/sql drivers).
	providers.Register(&OrgProvider{})
}

// OrgProvider manages Grafana organization resources.
type OrgProvider struct{}

// Name returns the unique identifier for this provider.
func (p *OrgProvider) Name() string { return "org" }

// ShortDesc returns a one-line description of the provider.
func (p *OrgProvider) ShortDesc() string { return "Manage Grafana organization resources" }

// Commands returns the Cobra commands contributed by this provider.
func (p *OrgProvider) Commands() []*cobra.Command {
	loader := &providers.ConfigLoader{}

	orgCmd := &cobra.Command{
		Use:   "org",
		Short: p.ShortDesc(),
	}
	// Bubble parent PersistentPreRun when attached to a real CLI root; guard
	// against self-recursion when orgCmd itself is used as the root (e.g. in
	// isolated tests where cmd.Root() == orgCmd).
	orgCmd.PersistentPreRun = func(cmd *cobra.Command, args []string) {
		if root := cmd.Root(); root != orgCmd && root.PersistentPreRun != nil {
			root.PersistentPreRun(cmd, args)
		}
	}

	loader.BindFlags(orgCmd.PersistentFlags())
	orgCmd.AddCommand(usersCommands(loader))

	return []*cobra.Command{orgCmd}
}

// Validate checks that the given provider configuration is valid.
func (p *OrgProvider) Validate(_ map[string]string) error { return nil }

// ConfigKeys returns the configuration keys used by this provider.
func (p *OrgProvider) ConfigKeys() []providers.ConfigKey { return nil }

// TypedRegistrations returns adapter registrations for OrgUser resource types.
func (p *OrgProvider) TypedRegistrations() []adapter.Registration {
	loader := &providers.ConfigLoader{}
	return []adapter.Registration{
		{
			Factory:    NewUsersAdapterFactory(loader),
			Descriptor: staticUsersDescriptor,
			GVK:        staticUsersDescriptor.GroupVersionKind(),
			Schema:     OrgUserSchema(),
			Example:    OrgUserExample(),
		},
	}
}
