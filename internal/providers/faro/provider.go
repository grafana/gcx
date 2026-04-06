package faro

import (
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/resources/adapter"
	"github.com/spf13/cobra"
)

var _ providers.Provider = &FaroProvider{}

func init() { //nolint:gochecknoinits // Self-registration pattern (like database/sql drivers).
	providers.Register(&FaroProvider{})
}

// FaroProvider manages Grafana Frontend Observability (Faro) resources.
type FaroProvider struct{}

// Name returns the unique identifier for this provider.
func (p *FaroProvider) Name() string { return "faro" }

// ShortDesc returns a one-line description of the provider.
func (p *FaroProvider) ShortDesc() string {
	return "Manage Grafana Frontend Observability (Faro) resources"
}

// ConfigKeys returns the configuration keys used by this provider.
// Faro uses Grafana's built-in authentication and does not require
// additional provider-specific keys.
func (p *FaroProvider) ConfigKeys() []providers.ConfigKey {
	return []providers.ConfigKey{
		{Name: "faro-api-url", Secret: false},
	}
}

// Validate checks that the given provider configuration is valid.
func (p *FaroProvider) Validate(_ map[string]string) error { return nil }

// Commands returns the Cobra commands contributed by this provider.
func (p *FaroProvider) Commands() []*cobra.Command {
	loader := &providers.ConfigLoader{}

	faroCmd := &cobra.Command{
		Use:   "faro",
		Short: p.ShortDesc(),
	}
	loader.BindFlags(faroCmd.PersistentFlags())

	appsCmd := &cobra.Command{
		Use:     "apps",
		Short:   "Manage Faro apps.",
		Aliases: []string{"app"},
	}

	appsCmd.AddCommand(
		newListCommand(loader),
		newGetCommand(loader),
		newCreateCommand(loader),
		newUpdateCommand(loader),
		newDeleteCommand(loader),
		newShowSourcemapsCommand(loader),
		newApplySourcemapCommand(loader),
		newRemoveSourcemapCommand(loader),
	)

	faroCmd.AddCommand(appsCmd)
	return []*cobra.Command{faroCmd}
}

// TypedRegistrations returns adapter registrations for FaroApp resource types.
func (p *FaroProvider) TypedRegistrations() []adapter.Registration {
	loader := &providers.ConfigLoader{}
	return []adapter.Registration{
		{
			Factory:    NewAdapterFactory(loader),
			Descriptor: staticDescriptor,
			GVK:        staticDescriptor.GroupVersionKind(),
			Schema:     FaroAppSchema(),
			Example:    FaroAppExample(),
		},
	}
}
