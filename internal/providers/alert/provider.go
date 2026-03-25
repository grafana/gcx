package alert

import (
	"github.com/grafana/grafanactl/internal/providers"
	"github.com/grafana/grafanactl/internal/resources/adapter"
	"github.com/spf13/cobra"
)

var _ providers.Provider = &AlertProvider{}

func init() { //nolint:gochecknoinits // Self-registration pattern (like database/sql drivers).
	providers.Register(&AlertProvider{})
}

// AlertProvider manages Grafana alerting resources.
type AlertProvider struct{}

// Name returns the unique identifier for this provider.
func (p *AlertProvider) Name() string { return "alert" }

// ShortDesc returns a one-line description of the provider.
func (p *AlertProvider) ShortDesc() string { return "Manage Grafana alerting resources." }

// Commands returns the Cobra commands contributed by this provider.
func (p *AlertProvider) Commands() []*cobra.Command {
	loader := &providers.ConfigLoader{}

	alertCmd := &cobra.Command{
		Use:   "alert",
		Short: p.ShortDesc(),
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			if root := cmd.Root(); root.PersistentPreRun != nil {
				root.PersistentPreRun(cmd, args)
			}
		},
	}

	loader.BindFlags(alertCmd.PersistentFlags())

	alertCmd.AddCommand(rulesCommands(loader))
	alertCmd.AddCommand(groupsCommands(loader))

	return []*cobra.Command{alertCmd}
}

// Validate checks that the given provider configuration is valid.
func (p *AlertProvider) Validate(cfg map[string]string) error {
	return nil
}

// ConfigKeys returns the configuration keys used by this provider.
func (p *AlertProvider) ConfigKeys() []providers.ConfigKey {
	return nil
}

// ResourceAdapters returns adapter factories for Alert resource types.
// Factories are registered globally via adapter.Register() in resource_adapter.go init().
func (p *AlertProvider) ResourceAdapters() []adapter.Factory {
	return nil
}
