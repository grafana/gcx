package experiments

import (
	"errors"
	"fmt"
	"strings"

	"github.com/grafana/gcx/internal/agent"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"k8s.io/apimachinery/pkg/util/validation"
)

type updateOpts struct {
	IO   cmdio.Options
	File string
}

func (o *updateOpts) setup(flags *pflag.FlagSet) {
	flags.StringVarP(&o.File, "filename", "f", "", "Updated complete Experiment YAML or JSON manifest (use - for stdin)")
	o.IO.DefaultFormat("yaml")
	o.IO.BindFlags(flags)
}

func (o *updateOpts) Validate() error {
	if o.File == "" {
		return errors.New("--filename/-f is required (use - to read from stdin)")
	}
	return o.IO.Validate()
}

func newUpdateCommand(loader grafanaConfigLoader) *cobra.Command {
	opts := &updateOpts{}
	cmd := &cobra.Command{
		Use:   "update <name>",
		Short: "[experimental] Update an Odin experiment from a manifest.",
		Long: `This command is experimental. It may be removed, or its subcommands, flags and responses may change without following the normal semantic versioning conventions.

Replace an existing Experiment through the Odin app plugin on the selected
Grafana instance. Start with the complete object from experiments get, change
the desired fields, and retain metadata.name, namespace, uid, and resourceVersion.
Odin rejects a stale resourceVersion instead of overwriting a concurrent edit.`,
		Example: `  gcx experiments get checkout-conversion -o yaml > experiment.yaml
  gcx experiments update checkout-conversion -f experiment.yaml
  gcx experiments get checkout-conversion -o json | jq '.spec.title = "New title"' | gcx experiments update checkout-conversion -f -`,
		Args: cobra.ExactArgs(1),
		Annotations: map[string]string{
			agent.AnnotationStability: agent.StabilityExperimental,
			agent.AnnotationTokenCost: "medium",
			agent.AnnotationLLMHint:   "Get the full Experiment with -o json, modify its fields while retaining metadata.name, namespace, uid, and resourceVersion, then pipe it to update <name> -f -. A stale resourceVersion fails with HTTP 409.",
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}
			name := args[0]
			if problems := validation.IsDNS1123Subdomain(name); len(problems) > 0 {
				return fmt.Errorf("invalid experiment name %q: %s; use a name such as checkout-conversion", name, strings.Join(problems, "; "))
			}
			manifest, err := readUpdateManifest(opts.File, cmd.InOrStdin(), name)
			if err != nil {
				return err
			}
			cfg, err := loader.LoadGrafanaConfig(cmd.Context())
			if err != nil {
				return err
			}
			client, err := NewClient(cfg)
			if err != nil {
				return err
			}
			updated, err := client.Update(cmd.Context(), name, manifest)
			if err != nil {
				return err
			}
			return opts.IO.Encode(cmd.OutOrStdout(), updated)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}
