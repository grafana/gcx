// Package scim manages Grafana SCIM provisioning.
package scim

import (
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/providers/native"
	"github.com/grafana/gcx/internal/resources/adapter"
	"github.com/spf13/cobra"
)

const scimGroup = "scim.grafana.app"

func init() { //nolint:gochecknoinits // Provider self-registration.
	providers.Register(&SCIMProvider{})
}

// SCIMProvider manages SCIM provisioning settings.
type SCIMProvider struct{}

func (*SCIMProvider) Name() string                               { return "scim" }
func (*SCIMProvider) ShortDesc() string                          { return "Manage SCIM provisioning" }
func (*SCIMProvider) Validate(_ map[string]string) error         { return nil }
func (*SCIMProvider) ConfigKeys() []providers.ConfigKey          { return nil }
func (*SCIMProvider) TypedRegistrations() []adapter.Registration { return nil }
func (p *SCIMProvider) Commands() []*cobra.Command {
	loader := &providers.ConfigLoader{}
	cmd := &cobra.Command{Use: "scim", Short: p.ShortDesc()}
	loader.BindFlags(cmd.PersistentFlags())
	binding := native.Bind(loader, native.Config{Group: scimGroup, Resource: "config"})
	cmd.AddCommand(newConfigCommand(binding), newEndpointCommand(binding))
	return []*cobra.Command{cmd}
}
