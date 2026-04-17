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
	orgCmd.PersistentPreRun = func(cmd *cobra.Command, args []string) {
		root := cmd.Root()
		// Guard against self-recursion when the provider's orgCmd is used as
		// the root directly (for example in isolated tests) — in that case
		// root == orgCmd and we'd re-enter this same function forever.
		if root != nil && root != orgCmd && root.PersistentPreRun != nil {
			root.PersistentPreRun(cmd, args)
		}
	}

	loader.BindFlags(orgCmd.PersistentFlags())

	orgCmd.AddCommand(usersCommands(loader))

	return []*cobra.Command{orgCmd}
}

// Validate checks that the given provider configuration is valid.
func (p *OrgProvider) Validate(cfg map[string]string) error {
	return nil
}

// ConfigKeys returns the configuration keys used by this provider.
func (p *OrgProvider) ConfigKeys() []providers.ConfigKey {
	return nil
}

// TypedRegistrations returns adapter registrations for org resource types.
// The org provider does not yet expose typed resources through the unified
// resources pipeline.
func (p *OrgProvider) TypedRegistrations() []adapter.Registration {
	return nil
}
