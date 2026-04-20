package kg

import (
	"context"

	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/resources/adapter"
	"github.com/grafana/gcx/internal/setup/framework"
	"github.com/spf13/cobra"
)

// Note: package init() is only in provider.go (calls providers.Register).
// Resource adapter registrations are in TypedRegistrations().

var _ providers.Provider = &KGProvider{}

func init() { //nolint:gochecknoinits // Self-registration pattern (like database/sql drivers).
	providers.Register(&KGProvider{})
}

// KGProvider manages Grafana Knowledge Graph resources.
type KGProvider struct{}

// Name returns the unique identifier for this provider.
func (p *KGProvider) Name() string { return "kg" }

// ShortDesc returns a one-line description of the provider.
func (p *KGProvider) ShortDesc() string {
	return "Manage Grafana Knowledge Graph rules, entities, and insights"
}

// Commands returns the Cobra commands contributed by this provider.
func (p *KGProvider) Commands() []*cobra.Command {
	loader := &providers.ConfigLoader{}

	kgCmd := &cobra.Command{
		Use:     "kg",
		Short:   p.ShortDesc(),
		Aliases: []string{"knowledge-graph"},
	}

	loader.BindFlags(kgCmd.PersistentFlags())

	kgCmd.AddCommand(
		newStatusCommand(loader),
		// Configuration upload
		newRulesCommand(loader),
		newModelRulesCommand(loader),
		newSuppressionsCommand(loader),
		newRelabelRulesCommand(loader),
		// Entities
		newEntitiesCommand(loader),
		newScopesCommand(loader),
		// Assertions
		newAssertionsCommand(loader),
		// Search
		newSearchCommand(loader),
		// High-level
		newInspectCommand(loader),
		newHealthCommand(loader),
		newOpenCommand(loader),
		newSetupCommand(p),
	)

	return []*cobra.Command{kgCmd}
}

// ProductName implements framework.StatusDetectable.
func (p *KGProvider) ProductName() string { return p.Name() }

// Status implements framework.StatusDetectable using a config-key heuristic.
func (p *KGProvider) Status(ctx context.Context) (*framework.ProductStatus, error) {
	var loader providers.ConfigLoader
	cfg, _, _ := loader.LoadProviderConfig(ctx, p.Name())
	status := framework.ConfigKeysStatus(p, cfg)
	return &status, nil
}

// InfraCategories implements framework.Setupable.
func (p *KGProvider) InfraCategories() []framework.InfraCategory { return nil }

// ResolveChoices implements framework.Setupable.
func (p *KGProvider) ResolveChoices(_ context.Context, _ string) ([]string, error) {
	return nil, nil
}

// ValidateSetup implements framework.Setupable.
func (p *KGProvider) ValidateSetup(_ context.Context, _ map[string]string) error { return nil }

// Setup implements framework.Setupable.
func (p *KGProvider) Setup(_ context.Context, _ map[string]string) error {
	return framework.ErrSetupNotSupported
}

// Validate checks that the given provider configuration is valid.
func (p *KGProvider) Validate(_ map[string]string) error {
	return nil
}

// ConfigKeys returns the configuration keys used by this provider.
func (p *KGProvider) ConfigKeys() []providers.ConfigKey {
	return nil
}

// TypedRegistrations returns adapter registrations for KG resource types.
func (p *KGProvider) TypedRegistrations() []adapter.Registration {
	loader := &providers.ConfigLoader{}
	return []adapter.Registration{
		{
			Factory:    NewAdapterFactory(loader),
			Descriptor: staticDescriptor,
			GVK:        staticDescriptor.GroupVersionKind(),
			Schema:     RuleSchema(),
			Example:    RuleExample(),
		},
		{
			Factory:    NewScopeAdapterFactory(loader),
			Descriptor: scopeDescriptor,
			GVK:        scopeDescriptor.GroupVersionKind(),
			Schema:     ScopeSchema(),
		},
	}
}
