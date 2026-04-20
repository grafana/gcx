package appo11y

import (
	"context"

	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/providers/appo11y/overrides"
	"github.com/grafana/gcx/internal/providers/appo11y/settings"
	"github.com/grafana/gcx/internal/resources/adapter"
	"github.com/grafana/gcx/internal/setup/framework"
	"github.com/spf13/cobra"
)

func init() { //nolint:gochecknoinits // Self-registration pattern (like database/sql drivers).
	providers.Register(&AppO11yProvider{})
}

// AppO11yProvider manages Grafana App Observability resources.
type AppO11yProvider struct{}

// Name returns the unique identifier for this provider.
func (p *AppO11yProvider) Name() string { return "appo11y" }

// ShortDesc returns a one-line description of the provider.
func (p *AppO11yProvider) ShortDesc() string {
	return "Manage Grafana App Observability settings"
}

// ConfigKeys returns the configuration keys used by this provider.
// App Observability uses the standard Grafana SA token; no additional keys are required.
func (p *AppO11yProvider) ConfigKeys() []providers.ConfigKey { return nil }

// Validate checks provider configuration.
// App Observability requires no provider-specific configuration.
func (p *AppO11yProvider) Validate(_ map[string]string) error { return nil }

// ProductName implements framework.StatusDetectable.
func (p *AppO11yProvider) ProductName() string { return p.Name() }

// Status implements framework.StatusDetectable using a config-key heuristic.
func (p *AppO11yProvider) Status(ctx context.Context) (*framework.ProductStatus, error) {
	var loader providers.ConfigLoader
	cfg, _, _ := loader.LoadProviderConfig(ctx, p.Name())
	status := framework.ConfigKeysStatus(p, cfg)
	return &status, nil
}

// InfraCategories implements framework.Setupable.
func (p *AppO11yProvider) InfraCategories() []framework.InfraCategory { return nil }

// ResolveChoices implements framework.Setupable.
func (p *AppO11yProvider) ResolveChoices(_ context.Context, _ string) ([]string, error) {
	return nil, nil
}

// ValidateSetup implements framework.Setupable.
func (p *AppO11yProvider) ValidateSetup(_ context.Context, _ map[string]string) error { return nil }

// Setup implements framework.Setupable.
func (p *AppO11yProvider) Setup(_ context.Context, _ map[string]string) error {
	return framework.ErrSetupNotSupported
}

// Commands returns the Cobra commands contributed by this provider.
func (p *AppO11yProvider) Commands() []*cobra.Command {
	loader := &providers.ConfigLoader{}

	cmd := &cobra.Command{
		Use:   "appo11y",
		Short: p.ShortDesc(),
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			if root := cmd.Root(); root.PersistentPreRun != nil {
				root.PersistentPreRun(cmd, args)
			}
		},
	}

	// Bind config flags on the parent — all subcommands inherit these.
	loader.BindFlags(cmd.PersistentFlags())

	cmd.AddCommand(overrides.Commands())
	cmd.AddCommand(settings.Commands())
	cmd.AddCommand(newSetupCommand(p))
	return []*cobra.Command{cmd}
}

// TypedRegistrations returns adapter registrations for App Observability resource types.
func (p *AppO11yProvider) TypedRegistrations() []adapter.Registration {
	overridesDesc := overrides.StaticDescriptor()
	settingsDesc := settings.StaticDescriptor()
	return []adapter.Registration{
		{
			Factory:    overrides.NewLazyFactory(),
			Descriptor: overridesDesc,
			GVK:        overridesDesc.GroupVersionKind(),
			Schema:     overrides.OverridesSchema(),
			Example:    overrides.OverridesExample(),
		},
		{
			Factory:    settings.NewLazyFactory(),
			Descriptor: settingsDesc,
			GVK:        settingsDesc.GroupVersionKind(),
			Schema:     settings.SettingsSchema(),
			Example:    settings.SettingsExample(),
		},
	}
}
