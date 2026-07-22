package aio11y

import (
	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/providers/aio11y/agents"
	"github.com/grafana/gcx/internal/providers/aio11y/conversations"
	"github.com/grafana/gcx/internal/providers/aio11y/eval/collections"
	"github.com/grafana/gcx/internal/providers/aio11y/eval/evaluators"
	"github.com/grafana/gcx/internal/providers/aio11y/eval/experiments"
	"github.com/grafana/gcx/internal/providers/aio11y/eval/guards"
	"github.com/grafana/gcx/internal/providers/aio11y/eval/judge"
	"github.com/grafana/gcx/internal/providers/aio11y/eval/rules"
	"github.com/grafana/gcx/internal/providers/aio11y/eval/savedconversations"
	"github.com/grafana/gcx/internal/providers/aio11y/eval/templates"
	"github.com/grafana/gcx/internal/providers/aio11y/generations"
	"github.com/grafana/gcx/internal/resources/adapter"
	"github.com/spf13/cobra"
)

func init() { //nolint:gochecknoinits // Self-registration pattern (like database/sql drivers).
	providers.Register(&AIO11yProvider{})
}

// AIO11yProvider manages Grafana Agent Observability resources
// (backed by the upstream `grafana-sigil-app` plugin). The CLI command is
// `agento11y`; the Go package name is retained for internal stability.
type AIO11yProvider struct{}

// Name returns the unique identifier for this provider.
func (p *AIO11yProvider) Name() string { return "agento11y" }

// ShortDesc returns a one-line description of the provider.
func (p *AIO11yProvider) ShortDesc() string {
	return "Manage Grafana Agent Observability resources"
}

// Commands returns the Cobra commands contributed by this provider.
func (p *AIO11yProvider) Commands() []*cobra.Command {
	loader := &providers.ConfigLoader{}

	aio11yCmd := &cobra.Command{
		Use:   "agento11y",
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
		agent.AnnotationLLMHint:   `gcx agento11y conversations list --limit 10 -o json`,
	}
	agentsCmd := agents.Commands(loader)
	agentsCmd.Annotations = map[string]string{
		agent.AnnotationTokenCost: "medium",
		agent.AnnotationLLMHint:   `gcx agento11y agents list --limit 10 -o json`,
	}

	evaluatorsCmd := evaluators.Commands(loader)
	evaluatorsCmd.Annotations = map[string]string{
		agent.AnnotationTokenCost: "low",
		agent.AnnotationLLMHint:   `gcx agento11y evaluators list -o json; gcx agento11y evaluators get <id> -o yaml; gcx agento11y evaluators upsert -f def.yaml -o json; gcx agento11y evaluators test -e <id> -g <gen-id> -o json; gcx agento11y evaluators delete <id> --force`,
	}

	rulesCmd := rules.Commands()
	rulesCmd.Annotations = map[string]string{
		agent.AnnotationTokenCost: "low",
		agent.AnnotationLLMHint:   `gcx agento11y rules list -o json; gcx agento11y rules get <id> -o yaml; gcx agento11y rules create -f rule.yaml -o json; gcx agento11y rules update <id> -f patch.yaml -o json; gcx agento11y rules delete <id> --force`,
	}

	guardsCmd := guards.Commands()
	guardsCmd.Annotations = map[string]string{
		agent.AnnotationTokenCost: "low",
		agent.AnnotationLLMHint:   `gcx agento11y guards list -o json; gcx agento11y guards get <id> -o yaml; gcx agento11y guards create -f guard.yaml -o json; gcx agento11y guards update <id> -f guard.yaml -o json; gcx agento11y guards delete <id> --force`,
	}

	templatesCmd := templates.Commands(loader)
	templatesCmd.Annotations = map[string]string{
		agent.AnnotationTokenCost: "low",
		agent.AnnotationLLMHint:   `gcx agento11y templates list -o json; gcx agento11y templates get <id> -o yaml; gcx agento11y templates list-versions <id> -o json; gcx agento11y templates list --scope global -o json`,
	}

	generationsCmd := generations.Commands(loader)
	generationsCmd.Annotations = map[string]string{
		agent.AnnotationTokenCost: "medium",
		agent.AnnotationLLMHint:   `gcx agento11y generations get <generation-id> -o json; gcx agento11y generations list-scores <generation-id> -o json; gcx agento11y generations list-scores <generation-id> -o wide`,
	}

	judgeCmd := judge.Commands(loader)
	judgeCmd.Annotations = map[string]string{
		agent.AnnotationTokenCost: "low",
		agent.AnnotationLLMHint:   `gcx agento11y judge list-providers -o json; gcx agento11y judge list-models --provider openai -o json`,
	}

	savedConvsCmd := savedconversations.Commands(loader)
	savedConvsCmd.Annotations = map[string]string{
		agent.AnnotationTokenCost: "medium",
		agent.AnnotationLLMHint:   `gcx agento11y saved-conversations list -o json; gcx agento11y saved-conversations get <id> -o yaml; gcx agento11y saved-conversations save <conv-id> --name '...' -o json; gcx agento11y saved-conversations collections <saved-id> -o json`,
	}

	collectionsCmd := collections.Commands(loader)
	collectionsCmd.Annotations = map[string]string{
		agent.AnnotationTokenCost: "low",
		agent.AnnotationLLMHint:   `gcx agento11y collections list -o json; gcx agento11y collections get <id> -o yaml; gcx agento11y collections create --name '...' -o json; gcx agento11y collections update <id> --name '...' -o json; gcx agento11y collections delete <id> --force; gcx agento11y collections list-conversations <id> -o json; gcx agento11y collections add-conversations <id> <saved-id>; gcx agento11y collections remove-conversation <id> <saved-id>`,
	}

	experimentsCmd := experiments.Commands(loader)
	experimentsCmd.Annotations = map[string]string{
		agent.AnnotationTokenCost: "medium",
		agent.AnnotationLLMHint:   `gcx agento11y experiments list -o json; gcx agento11y experiments get <run-id> -o yaml; gcx agento11y experiments update <run-id> --description '...' --tag nightly --tag support -o json; gcx agento11y experiments list-scores <run-id> -o json; gcx agento11y experiments report <run-id> -o json`,
	}

	aio11yCmd.AddCommand(convsCmd, agentsCmd, evaluatorsCmd, rulesCmd, guardsCmd, templatesCmd, generationsCmd, judgeCmd, savedConvsCmd, collectionsCmd, experimentsCmd)

	return []*cobra.Command{aio11yCmd}
}

// Validate checks that the given provider configuration is valid.
// Agent Observability uses Grafana's built-in authentication via the plugin API,
// so no extra keys are required.
func (p *AIO11yProvider) Validate(cfg map[string]string) error {
	return nil
}

// ConfigKeys returns the configuration keys used by this provider.
// Agent Observability uses Grafana's built-in authentication and does not require
// additional provider-specific keys.
func (p *AIO11yProvider) ConfigKeys() []providers.ConfigKey {
	return nil
}

// TypedRegistrations returns adapter registrations for Agent Observability resource types.
//
// Saved-conversations are intentionally absent: `save` bookmarks a specific
// live conversation (not an idempotent upsert) and the resource is shaped
// like an event record rather than declarative config.
//
// Experiments are also absent: they are runs (cancel, status, terminal states)
// — operational records, not declarative configuration.
func (p *AIO11yProvider) TypedRegistrations() []adapter.Registration {
	evalDesc := evaluators.StaticDescriptor()
	ruleDesc := rules.StaticDescriptor()
	guardDesc := guards.StaticDescriptor()
	collectionDesc := collections.StaticDescriptor()

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
		{
			Factory:     guards.NewLazyFactory(),
			Descriptor:  guardDesc,
			GVK:         guardDesc.GroupVersionKind(),
			Schema:      guards.HookRuleSchema(),
			URLTemplate: "/a/grafana-sigil-app/guards/{name}",
		},
		{
			Factory:     collections.NewLazyFactory(),
			Descriptor:  collectionDesc,
			GVK:         collectionDesc.GroupVersionKind(),
			Schema:      collections.CollectionSchema(),
			URLTemplate: "/a/grafana-sigil-app/collections/{name}",
		},
	}
}
