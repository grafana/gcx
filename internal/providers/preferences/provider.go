package preferences

import (
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/resources/adapter"
	"github.com/spf13/cobra"
)

var _ providers.Provider = &PreferencesProvider{}

func init() { //nolint:gochecknoinits // Self-registration pattern (like database/sql drivers).
	providers.Register(&PreferencesProvider{})
}

// PreferencesProvider manages Grafana organization preferences.
type PreferencesProvider struct{}

// Name returns the unique identifier for this provider.
func (p *PreferencesProvider) Name() string { return "preferences" }

// ShortDesc returns a one-line description of the provider.
func (p *PreferencesProvider) ShortDesc() string { return "Manage Grafana org preferences" }

// Commands returns the Cobra commands contributed by this provider.
func (p *PreferencesProvider) Commands() []*cobra.Command {
	loader := &providers.ConfigLoader{}

	cmd := &cobra.Command{
		Use:   "preferences",
		Short: p.ShortDesc(),
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			if root := cmd.Root(); root.PersistentPreRun != nil {
				root.PersistentPreRun(cmd, args)
			}
		},
	}

	loader.BindFlags(cmd.PersistentFlags())

	cmd.AddCommand(newGetCommand(loader))
	cmd.AddCommand(newUpdateCommand(loader))

	return []*cobra.Command{cmd}
}

// Validate checks that the given provider configuration is valid.
func (p *PreferencesProvider) Validate(_ map[string]string) error { return nil }

// ConfigKeys returns the configuration keys used by this provider.
// Preferences uses the standard Grafana SA token; no additional keys are required.
func (p *PreferencesProvider) ConfigKeys() []providers.ConfigKey { return nil }

// TypedRegistrations returns adapter registrations for Preferences resource types.
// Preferences are a singleton settings resource exposed only through imperative commands.
func (p *PreferencesProvider) TypedRegistrations() []adapter.Registration { return nil }
