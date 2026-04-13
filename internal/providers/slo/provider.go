package slo

import (
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/providers/slo/definitions"
	"github.com/grafana/gcx/internal/providers/slo/reports"
	"github.com/grafana/gcx/internal/resources/adapter"
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

	return []*cobra.Command{sloCmd}
}

// Validate checks that the given provider configuration is valid.
// The SLO provider uses Grafana's built-in authentication, so no extra keys
// are required.
func (p *SLOProvider) Validate(cfg map[string]string) error {
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
