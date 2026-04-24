package aio11y

import (
	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/providers/aio11y/agents"
	"github.com/grafana/gcx/internal/providers/aio11y/conversations"
	"github.com/grafana/gcx/internal/providers/aio11y/eval/evaluators"
	"github.com/grafana/gcx/internal/providers/aio11y/eval/judge"
	"github.com/grafana/gcx/internal/providers/aio11y/eval/rules"
	"github.com/grafana/gcx/internal/providers/aio11y/eval/templates"
	"github.com/grafana/gcx/internal/providers/aio11y/generations"
	"github.com/grafana/gcx/internal/providers/aio11y/scores"
	"github.com/grafana/gcx/internal/resources/adapter"
	"github.com/spf13/cobra"
)

func init() { //nolint:gochecknoinits // Self-registration pattern (like database/sql drivers).
	providers.Register(&AIO11yProvider{})
}

// AIO11yProvider manages Grafana AI Observability resources
// (backed by the upstream `grafana-sigil-app` plugin).
type AIO11yProvider struct{}

// Name returns the unique identifier for this provider.
func (p *AIO11yProvider) Name() string { return "aio11y" }

// ShortDesc returns a one-line description of the provider.
func (p *AIO11yProvider) ShortDesc() string {
	return "Manage Grafana AI Observability resources"
}

// Commands returns the Cobra commands contributed by this provider.
func (p *AIO11yProvider) Commands() []*cobra.Command {
	loader := &providers.ConfigLoader{}

	aio11yCmd := &cobra.Command{
		Use:   "aio11y",
		Short: p.ShortDesc(),
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			if root := cmd.Root(); root.PersistentPreRun != nil {
				root.PersistentPreRun(cmd, args)
			}
		},
	}

	loader.BindFlags(aio11yCmd.PersistentFlags())

	convsCmd := conversations.Commands(loader)
	convsCmd.Annotations = map[string]string{
		agent.AnnotationTokenCost: "medium",
		agent.AnnotationLLMHint:   `gcx aio11y conversations list --limit 10 -o json`,
	}
	agentsCmd := agents.Commands(loader)
	agentsCmd.Annotations = map[string]string{
		agent.AnnotationTokenCost: "medium",
		agent.AnnotationLLMHint:   `gcx aio11y agents list --limit 10 -o json`,
	}

	evaluatorsCmd := evaluators.Commands(loader)
	evaluatorsCmd.Annotations = map[string]string{
		agent.AnnotationTokenCost: "low",
		agent.AnnotationLLMHint:   `gcx aio11y evaluators list -o json; gcx aio11y evaluators get <id> -o yaml; gcx aio11y evaluators create -f def.yaml -o json; gcx aio11y evaluators test -e <id> -g <gen-id> -o json; gcx aio11y evaluators delete <id> --force`,
	}

	rulesCmd := rules.Commands()
	rulesCmd.Annotations = map[string]string{
		agent.AnnotationTokenCost: "low",
		agent.AnnotationLLMHint:   `gcx aio11y rules list -o json; gcx aio11y rules get <id> -o yaml; gcx aio11y rules create -f rule.yaml -o json; gcx aio11y rules update <id> -f patch.yaml -o json; gcx aio11y rules delete <id> --force`,
	}

	templatesCmd := templates.Commands(loader)
	templatesCmd.Annotations = map[string]string{
		agent.AnnotationTokenCost: "low",
		agent.AnnotationLLMHint:   `gcx aio11y templates list -o json; gcx aio11y templates get <id> -o yaml; gcx aio11y templates versions <id> -o json; gcx aio11y templates list --scope global -o json`,
	}

	generationsCmd := generations.Commands(loader)
	generationsCmd.Annotations = map[string]string{
		agent.AnnotationTokenCost: "medium",
		agent.AnnotationLLMHint:   `gcx aio11y generations get <generation-id> -o json`,
	}

	scoresCmd := scores.Commands(loader)
	scoresCmd.Annotations = map[string]string{
		agent.AnnotationTokenCost: "low",
		agent.AnnotationLLMHint:   `gcx aio11y scores list <generation-id> -o json; gcx aio11y scores list <generation-id> -o wide`,
	}

	judgeCmd := judge.Commands(loader)
	judgeCmd.Annotations = map[string]string{
		agent.AnnotationTokenCost: "low",
		agent.AnnotationLLMHint:   `gcx aio11y judge providers -o json; gcx aio11y judge models --provider openai -o json`,
	}

	aio11yCmd.AddCommand(convsCmd, agentsCmd, evaluatorsCmd, rulesCmd, templatesCmd, generationsCmd, scoresCmd, judgeCmd)

	return []*cobra.Command{aio11yCmd}
}

// Validate checks that the given provider configuration is valid.
// AI Observability uses Grafana's built-in authentication via the plugin API,
// so no extra keys are required.
func (p *AIO11yProvider) Validate(cfg map[string]string) error {
	return nil
}

// ConfigKeys returns the configuration keys used by this provider.
// AI Observability uses Grafana's built-in authentication and does not require
// additional provider-specific keys.
func (p *AIO11yProvider) ConfigKeys() []providers.ConfigKey {
	return nil
}

// TypedRegistrations returns adapter registrations for AI Observability resource types.
func (p *AIO11yProvider) TypedRegistrations() []adapter.Registration {
	evalDesc := evaluators.StaticDescriptor()
	ruleDesc := rules.StaticDescriptor()

	return []adapter.Registration{
		{
			Factory:     evaluators.NewLazyFactory(),
			Descriptor:  evalDesc,
			GVK:         evalDesc.GroupVersionKind(),
			Schema:      evaluators.EvaluatorSchema(),
			URLTemplate: "/a/grafana-sigil-app/evaluators/{name}",
		},
		{
			Factory:     rules.NewLazyFactory(),
			Descriptor:  ruleDesc,
			GVK:         ruleDesc.GroupVersionKind(),
			Schema:      rules.RuleSchema(),
			URLTemplate: "/a/grafana-sigil-app/rules/{name}",
		},
	}
}
