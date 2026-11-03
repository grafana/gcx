package org

import (
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/resources/adapter"
	"github.com/spf13/cobra"
)

func init() { //nolint:gochecknoinits // Providers register once at startup.
	providers.Register(&Provider{})
}

// Provider supplies commands for the selected Grafana organization.
type Provider struct{}

// Name returns the provider identifier.
func (*Provider) Name() string { return "org" }

// ShortDesc describes the provider.
func (*Provider) ShortDesc() string { return "Manage the selected Grafana instance organization." }

// ConfigKeys returns no extra authentication keys.
func (*Provider) ConfigKeys() []providers.ConfigKey { return nil }

// Validate accepts the selected Grafana configuration.
func (*Provider) Validate(map[string]string) error { return nil }

// TypedRegistrations returns no portable resource adapters.
func (*Provider) TypedRegistrations() []adapter.Registration { return nil }

// Commands returns the organization command tree.
func (p *Provider) Commands() []*cobra.Command {
	loader := &providers.ConfigLoader{}
	cmd := &cobra.Command{Use: "org", Short: p.ShortDesc(), Long: "Manage the organization on the Grafana server selected by the current context or --context. Use gcx cloud orgs to inspect Grafana Cloud account organizations."}
	loader.BindFlags(cmd.PersistentFlags())
	users := &cobra.Command{Use: "users", Short: "Manage users in the selected organization."}
	users.AddCommand(newListCommand(loader), newGetCommand(loader), newDeleteCommand(loader))
	cmd.AddCommand(users)
	return []*cobra.Command{cmd}
}
