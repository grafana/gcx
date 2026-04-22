package faro

import (
	"context"

	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/resources/adapter"
	"github.com/grafana/gcx/internal/setup/framework"
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
	return "Manage Grafana Frontend Observability resources"
}

// ConfigKeys returns the configuration keys used by this provider.
// Faro uses Grafana's built-in authentication and does not require
// additional provider-specific keys.
func (p *FaroProvider) ConfigKeys() []providers.ConfigKey {
	return []providers.ConfigKey{
		{Name: "faro-api-url", Secret: false},
	}
}

// ProductName implements framework.StatusDetectable.
func (p *FaroProvider) ProductName() string { return p.Name() }

// Status implements framework.StatusDetectable using a config-key heuristic.
func (p *FaroProvider) Status(ctx context.Context) (*framework.ProductStatus, error) {
	var loader providers.ConfigLoader
	// TODO: add proper error handling once provider setup is implemented
	cfg, _, _ := loader.LoadProviderConfig(ctx, p.Name())
	status := framework.ConfigKeysStatus(p, cfg)
	return &status, nil
}

// InfraCategories implements framework.Setupable.
func (p *FaroProvider) InfraCategories() []framework.InfraCategory { return nil }

// ResolveChoices implements framework.Setupable.
func (p *FaroProvider) ResolveChoices(_ context.Context, _ string) ([]string, error) {
	return nil, nil
}

// ValidateSetup implements framework.Setupable.
func (p *FaroProvider) ValidateSetup(_ context.Context, _ map[string]string) error { return nil }

// Setup implements framework.Setupable.
func (p *FaroProvider) Setup(_ context.Context, _ map[string]string) error {
	return framework.ErrSetupNotSupported
}

// Validate checks that the given provider configuration is valid.
func (p *FaroProvider) Validate(_ map[string]string) error { return nil }

// Commands returns the Cobra commands contributed by this provider.
func (p *FaroProvider) Commands() []*cobra.Command {
	loader := &providers.ConfigLoader{}

	faroCmd := &cobra.Command{
		Use:   "frontend",
		Short: p.ShortDesc(),
	}
	loader.BindFlags(faroCmd.PersistentFlags())

	appsCmd := &cobra.Command{
		Use:     "apps",
		Short:   "Manage Frontend Observability apps.",
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
	faroCmd.AddCommand(newSetupCommand(p))
	return []*cobra.Command{faroCmd}
}

// TypedRegistrations returns adapter registrations for FaroApp resource types.
func (p *FaroProvider) TypedRegistrations() []adapter.Registration {
	loader := &providers.ConfigLoader{}
	return []adapter.Registration{
		{
			Factory:     NewAdapterFactory(loader),
			Descriptor:  staticDescriptor,
			GVK:         staticDescriptor.GroupVersionKind(),
			Schema:      FaroAppSchema(),
			Example:     FaroAppExample(),
			URLTemplate: "/a/grafana-faro-app/apps/{name}",
		},
	}
}
