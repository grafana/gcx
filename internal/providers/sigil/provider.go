package sigil

import (
	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/providers/sigil/agents"
	"github.com/grafana/gcx/internal/providers/sigil/conversations"
	"github.com/grafana/gcx/internal/providers/sigil/eval/evaluators"
	"github.com/grafana/gcx/internal/providers/sigil/eval/rules"
	"github.com/grafana/gcx/internal/providers/sigil/eval/templates"
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

	agentsCmd := agents.Commands(loader)
	agentsCmd.Annotations = map[string]string{
		agent.AnnotationTokenCost: "medium",
		agent.AnnotationLLMHint:   `gcx sigil agents list --limit 10 -o json`,
	}
	sigilCmd.AddCommand(agentsCmd)

	evaluatorsCmd := evaluators.Commands(loader)
	evaluatorsCmd.Annotations = map[string]string{
		agent.AnnotationTokenCost: "low",
		agent.AnnotationLLMHint:   `gcx sigil evaluators list -o json; gcx sigil evaluators get <id> -o yaml; gcx sigil evaluators create -f def.yaml -o json; gcx sigil evaluators test -e <id> -g <gen-id> -o json; gcx sigil evaluators delete <id> --force`,
	}

	rulesCmd := rules.Commands()
	rulesCmd.Annotations = map[string]string{
		agent.AnnotationTokenCost: "low",
		agent.AnnotationLLMHint:   `gcx sigil rules list -o json; gcx sigil rules get <id> -o yaml; gcx sigil rules create -f rule.yaml -o json; gcx sigil rules update <id> -f patch.yaml -o json; gcx sigil rules delete <id> --force`,
	}

	templatesCmd := templates.Commands(loader)
	templatesCmd.Annotations = map[string]string{
		agent.AnnotationTokenCost: "low",
		agent.AnnotationLLMHint:   `gcx sigil templates list -o json; gcx sigil templates get <id> -o yaml; gcx sigil templates versions <id> -o json; gcx sigil templates list --scope global -o json`,
	}

	sigilCmd.AddCommand(evaluatorsCmd, rulesCmd, templatesCmd)

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
	evalDesc := evaluators.StaticDescriptor()
	ruleDesc := rules.StaticDescriptor()

	return []adapter.Registration{
		{
			Factory:    evaluators.NewLazyFactory(),
			Descriptor: evalDesc,
			GVK:        evalDesc.GroupVersionKind(),
			Schema:     evaluators.EvaluatorSchema(),
		},
		{
			Factory:    rules.NewLazyFactory(),
			Descriptor: ruleDesc,
			GVK:        ruleDesc.GroupVersionKind(),
			Schema:     rules.RuleSchema(),
		},
	}
}
