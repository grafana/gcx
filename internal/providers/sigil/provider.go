package sigil

import (
	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/providers/sigil/conversations"
	"github.com/grafana/gcx/internal/resources/adapter"
	"github.com/spf13/cobra"
)

func init() { //nolint:gochecknoinits // Self-registration pattern (like database/sql drivers).
	providers.Register(&SigilProvider{})
}

// SigilProvider manages Grafana Sigil AI observability resources.
type SigilProvider struct{}

// Name returns the unique identifier for this provider.
func (p *SigilProvider) Name() string { return "sigil" }

// ShortDesc returns a one-line description of the provider.
func (p *SigilProvider) ShortDesc() string {
	return "Manage Sigil AI observability resources"
}

// Commands returns the Cobra commands contributed by this provider.
func (p *SigilProvider) Commands() []*cobra.Command {
	loader := &providers.ConfigLoader{}

	sigilCmd := &cobra.Command{
		Use:   "sigil",
		Short: p.ShortDesc(),
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			if root := cmd.Root(); root.PersistentPreRun != nil {
				root.PersistentPreRun(cmd, args)
			}
		},
	}

	loader.BindFlags(sigilCmd.PersistentFlags())

	convsCmd := conversations.Commands(loader)
	convsCmd.Annotations = map[string]string{
		agent.AnnotationTokenCost: "medium",
		agent.AnnotationLLMHint:   `gcx sigil conversations list --limit 10 -o json`,
	}
	sigilCmd.AddCommand(convsCmd)

	return []*cobra.Command{sigilCmd}
}

// Validate checks that the given provider configuration is valid.
// The Sigil provider uses Grafana's built-in authentication via the plugin API,
// so no extra keys are required.
func (p *SigilProvider) Validate(cfg map[string]string) error {
	return nil
}

// ConfigKeys returns the configuration keys used by this provider.
// The Sigil provider uses Grafana's built-in authentication and does not require
// additional provider-specific keys.
func (p *SigilProvider) ConfigKeys() []providers.ConfigKey {
	return nil
}

// TypedRegistrations returns adapter registrations for Sigil resource types.
func (p *SigilProvider) TypedRegistrations() []adapter.Registration {
	return nil
}
