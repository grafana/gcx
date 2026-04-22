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

func (p *PreferencesProvider) Name() string { return "preferences" }

func (p *PreferencesProvider) ShortDesc() string { return "Manage Grafana org preferences" }

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

func (p *PreferencesProvider) Validate(_ map[string]string) error { return nil }

// ConfigKeys returns no keys; preferences uses the standard Grafana SA token.
func (p *PreferencesProvider) ConfigKeys() []providers.ConfigKey { return nil }

// TypedRegistrations returns adapter registrations for organization preferences.
func (p *PreferencesProvider) TypedRegistrations() []adapter.Registration {
	desc := StaticDescriptor()
	return []adapter.Registration{
		{
			Factory:    NewLazyFactory(),
			Descriptor: desc,
			GVK:        desc.GroupVersionKind(),
			Schema:     PreferencesSchema(),
			Example:    PreferencesExample(),
		},
	}
}
