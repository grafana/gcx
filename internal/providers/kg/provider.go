package kg

import (
	"github.com/grafana/grafanactl/internal/providers"
	"github.com/grafana/grafanactl/internal/resources/adapter"
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
	return "Manage Grafana Knowledge Graph (Asserts) resources."
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
		// Lifecycle
		newSetupCommand(loader),
		newEnableCommand(loader),
		newStatusCommand(loader),
		// Datasets
		newDatasetsCommand(loader),
		newVendorsCommand(loader),
		// Configuration upload
		newRulesCommand(loader),
		newModelRulesCommand(loader),
		newSuppressionsCommand(loader),
		newRelabelRulesCommand(loader),
		newServiceDashboardCommand(loader),
		newKPIDisplayCommand(loader),
		newFrontendRulesCommand(loader),
		// Environment
		newEnvCommand(loader),
		// Entities
		newEntitiesCommand(loader),
		newEntityTypesCommand(loader),
		newScopesCommand(loader),
		// Assertions
		newAssertionsCommand(loader),
		// Search
		newSearchCommand(loader),
		// Graph
		newGraphConfigCommand(loader),
		// High-level
		newInspectCommand(loader),
		newHealthCommand(loader),
		newOpenCommand(loader),
	)

	return []*cobra.Command{kgCmd}
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
			Aliases:    staticAliases,
			GVK:        staticDescriptor.GroupVersionKind(),
			Schema:     RuleSchema(),
			Example:    RuleExample(),
		},
		{
			Factory:    NewDatasetAdapterFactory(loader),
			Descriptor: datasetDescriptor,
			Aliases:    datasetAliases,
			GVK:        datasetDescriptor.GroupVersionKind(),
			Schema:     DatasetSchema(),
		},
		{
			Factory:    NewVendorAdapterFactory(loader),
			Descriptor: vendorDescriptor,
			Aliases:    vendorAliases,
			GVK:        vendorDescriptor.GroupVersionKind(),
			Schema:     VendorSchema(),
		},
		{
			Factory:    NewEntityTypeAdapterFactory(loader),
			Descriptor: entityTypeDescriptor,
			Aliases:    entityTypeAliases,
			GVK:        entityTypeDescriptor.GroupVersionKind(),
			Schema:     EntityTypeSchema(),
		},
		{
			Factory:    NewScopeAdapterFactory(loader),
			Descriptor: scopeDescriptor,
			Aliases:    scopeAliases,
			GVK:        scopeDescriptor.GroupVersionKind(),
			Schema:     ScopeSchema(),
		},
	}
}
