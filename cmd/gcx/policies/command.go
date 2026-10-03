// Package policies wires the experimental `gcx policies` commands, which read Grafana's CEL
// validation policies and evaluate them in-process.
package policies

import (
	cmdconfig "github.com/grafana/gcx/cmd/gcx/config"
	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/config"
	"github.com/spf13/cobra"
)

const experimentalNotice = `This command is experimental. It may be removed, or its subcommands, flags and
responses may change without following the normal semantic versioning conventions.`

func experimental(cmd *cobra.Command) *cobra.Command {
	if cmd.Annotations == nil {
		cmd.Annotations = map[string]string{}
	}
	cmd.Annotations[agent.AnnotationStability] = agent.StabilityExperimental
	return cmd
}

func withTokenCost(cmd *cobra.Command, cost string) *cobra.Command {
	cmd.Annotations[agent.AnnotationTokenCost] = cost
	return cmd
}

// Command returns the `policies` area.
func Command() *cobra.Command {
	configOpts := &cmdconfig.Options{}

	cmd := &cobra.Command{
		Use:   "policies",
		Short: "[experimental] Inspect and evaluate Grafana validation policies",
		Long: experimentalNotice + `

Inspect the CEL validation policies (policy.grafana.app) that Grafana evaluates when app
platform resources are written, and evaluate them locally against manifests or stored resources.`,
		// Cobra v1.x does not chain PersistentPreRun hooks, so the root hook is called explicitly to
		// keep terminal detection, agent mode and logging, then --context and --config are injected.
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			if root := cmd.Root(); root != nil && root.PersistentPreRun != nil {
				root.PersistentPreRun(cmd, args)
			}
			ctx := config.ContextWithName(cmd.Context(), configOpts.Context)
			ctx = config.ContextWithConfigFile(ctx, configOpts.ConfigFile)
			cmd.SetContext(ctx)
		},
	}
	configOpts.BindFlags(cmd.PersistentFlags())

	validationPolicies := &cobra.Command{
		Use:     "validation-policies",
		Aliases: []string{"validationpolicies"},
		Short:   "[experimental] Validation policies and their bindings",
		Long: experimentalNotice + `

Validation policies are written by Grafana apps, such as the rule policy and folder naming apps,
and enforced on writes through their bindings: Deny rejects a write, Warn admits it with a warning.`,
	}
	validationPolicies.AddCommand(withTokenCost(experimental(listCmd(configOpts)), "small"))
	// Evaluation reads every selected resource, but reports only violations.
	validationPolicies.AddCommand(withTokenCost(experimental(evaluateCmd(configOpts)), "medium"))

	cmd.AddCommand(experimental(validationPolicies))
	return experimental(cmd)
}
