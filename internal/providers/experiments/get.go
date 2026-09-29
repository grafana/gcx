package experiments

import (
	"fmt"
	"strings"

	"github.com/grafana/gcx/internal/agent"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"k8s.io/apimachinery/pkg/util/validation"
)

type getOpts struct{ IO cmdio.Options }

func (o *getOpts) setup(flags *pflag.FlagSet) {
	o.IO.DefaultFormat("yaml")
	o.IO.BindFlags(flags)
}

func newGetCommand(loader grafanaConfigLoader) *cobra.Command {
	opts := &getOpts{}
	cmd := &cobra.Command{
		Use:   "get <name>",
		Short: "[experimental] Get one Odin experiment.",
		Long: `This command is experimental. It may be removed, or its subcommands, flags and responses may change without following the normal semantic versioning conventions.

Get the complete stored Experiment by metadata.name through the Odin app plugin
on the selected Grafana instance. Use list to discover experiment names.`,
		Example: `  gcx experiments get checkout-conversion
  gcx experiments get checkout-conversion -o json
  gcx experiments get checkout-conversion --jq '{name: .metadata.name, status: .spec.status, analytics: .spec.analyticsConfig}'`,
		Args: cobra.ExactArgs(1),
		Annotations: map[string]string{
			agent.AnnotationStability: agent.StabilityExperimental,
			agent.AnnotationTokenCost: "large",
			agent.AnnotationLLMHint:   "Get one complete Odin Experiment by metadata.name. Use --jq to select fields if the analytics query is large; use list to find names.",
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}
			name := args[0]
			if problems := validation.IsDNS1123Subdomain(name); len(problems) > 0 {
				return fmt.Errorf("invalid experiment name %q: %s; use a name such as checkout-conversion", name, strings.Join(problems, "; "))
			}
			cfg, err := loader.LoadGrafanaConfig(cmd.Context())
			if err != nil {
				return err
			}
			client, err := NewClient(cfg)
			if err != nil {
				return err
			}
			experiment, err := client.Get(cmd.Context(), name)
			if err != nil {
				return err
			}
			return opts.IO.Encode(cmd.OutOrStdout(), experiment)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}
