package slo

import (
	"context"

	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/providers/slo/definitions"
	"github.com/grafana/gcx/internal/providers/slo/reports"
	"github.com/grafana/gcx/internal/resources/adapter"
	"github.com/grafana/gcx/internal/setup/framework"
	"github.com/spf13/cobra"
)

func init() { //nolint:gochecknoinits // Self-registration pattern (like database/sql drivers).
	providers.Register(&SLOProvider{})
}

// SLOProvider manages Grafana SLO resources.
type SLOProvider struct{}

// Name returns the unique identifier for this provider.
func (p *SLOProvider) Name() string { return "slo" }

// ShortDesc returns a one-line description of the provider.
func (p *SLOProvider) ShortDesc() string { return "Manage Grafana SLO definitions and reports" }

// Commands returns the Cobra commands contributed by this provider.
func (p *SLOProvider) Commands() []*cobra.Command {
	loader := &providers.ConfigLoader{}

	sloCmd := &cobra.Command{
		Use:   "slo",
		Short: p.ShortDesc(),
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			if root := cmd.Root(); root.PersistentPreRun != nil {
				root.PersistentPreRun(cmd, args)
			}
		},
	}

	// Bind config flags on the parent — all subcommands inherit these.
	loader.BindFlags(sloCmd.PersistentFlags())

	sloCmd.AddCommand(definitions.Commands(loader))
	sloCmd.AddCommand(reports.Commands(loader))
	sloCmd.AddCommand(newSetupCommand(p))

	return []*cobra.Command{sloCmd}
}

// ProductName implements framework.StatusDetectable.
func (p *SLOProvider) ProductName() string { return p.Name() }

// Status implements framework.StatusDetectable using a config-key heuristic.
func (p *SLOProvider) Status(ctx context.Context) (*framework.ProductStatus, error) {
	var loader providers.ConfigLoader
	// TODO: add proper error handling once provider setup is implemented
	cfg, _, _ := loader.LoadProviderConfig(ctx, p.Name())
	status := framework.ConfigKeysStatus(p, cfg)
	return &status, nil
}

// InfraCategories implements framework.Setupable.
func (p *SLOProvider) InfraCategories() []framework.InfraCategory { return nil }

// ResolveChoices implements framework.Setupable.
func (p *SLOProvider) ResolveChoices(_ context.Context, _ string) ([]string, error) {
	return nil, nil
}

// ValidateSetup implements framework.Setupable.
func (p *SLOProvider) ValidateSetup(_ context.Context, _ map[string]string) error { return nil }

// Setup implements framework.Setupable.
func (p *SLOProvider) Setup(_ context.Context, _ map[string]string) error {
	return framework.ErrSetupNotSupported
}

// Validate checks that the given provider configuration is valid.
// The SLO provider uses Grafana's built-in authentication, so no extra keys
// are required.
func (p *SLOProvider) Validate(_ map[string]string) error {
	return nil
}

// ConfigKeys returns the configuration keys used by this provider.
// The SLO provider uses Grafana's built-in authentication and does not require
// additional provider-specific keys.
func (p *SLOProvider) ConfigKeys() []providers.ConfigKey {
	return nil
}

// TypedRegistrations returns adapter registrations for SLO resource types.
func (p *SLOProvider) TypedRegistrations() []adapter.Registration {
	desc := definitions.StaticDescriptor()
	return []adapter.Registration{
		{
			Factory:     definitions.NewLazyFactory(),
			Descriptor:  desc,
			GVK:         desc.GroupVersionKind(),
			Schema:      definitions.SloSchema(),
			Example:     definitions.SloExample(),
			URLTemplate: "/a/grafana-slo-app/slo/{name}",
		},
	}
}
