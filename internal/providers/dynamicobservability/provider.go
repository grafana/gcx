package dynamicobservability

import (
	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/resources/adapter"
	"github.com/spf13/cobra"
)

const experimentalNotice = "This command is experimental. It may be removed, or its subcommands, flags and responses may change without following the normal semantic versioning conventions."

func init() { //nolint:gochecknoinits // Provider self-registration.
	providers.Register(&provider{})
}

type provider struct{}

func (*provider) Name() string { return "dynamic-observability" }
func (*provider) ShortDesc() string {
	return "Inspect and control Dynamic Observability probes and agents."
}
func (*provider) Validate(map[string]string) error           { return nil }
func (*provider) ConfigKeys() []providers.ConfigKey          { return nil }
func (*provider) TypedRegistrations() []adapter.Registration { return nil }

func (*provider) Commands() []*cobra.Command {
	loader := &providers.ConfigLoader{}
	root := experimental("dynamic-observability", "Inspect Dynamic Observability probes and agents")
	root.PersistentPreRun = func(cmd *cobra.Command, args []string) {
		if parent := cmd.Root(); parent != cmd && parent.PersistentPreRun != nil {
			parent.PersistentPreRun(cmd, args)
		}
	}
	loader.BindFlags(root.PersistentFlags())

	rulesets := experimental("rulesets", "Inspect and control probe rulesets")
	rulesets.AddCommand(newRulesetListCommand(loader), newRulesetStatusCommand(loader), newPauseCommand(loader, true), newPauseCommand(loader, false))
	agents := experimental("agents", "Inspect node agent health")
	agents.AddCommand(newAgentListCommand(loader), newAgentStatusCommand(loader))
	root.AddCommand(rulesets, agents)
	return []*cobra.Command{root}
}

func experimental(use, short string) *cobra.Command {
	return &cobra.Command{
		Use:         use,
		Short:       "[experimental] " + short,
		Long:        experimentalNotice + "\n\n" + short + ".",
		Annotations: map[string]string{agent.AnnotationStability: agent.StabilityExperimental},
	}
}
