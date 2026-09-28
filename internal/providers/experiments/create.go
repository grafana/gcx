package experiments

import (
	"encoding/json"
	"errors"

	"github.com/grafana/gcx/internal/agent"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type createOpts struct {
	IO      cmdio.Options
	File    string
	Example bool
}

func (o *createOpts) setup(flags *pflag.FlagSet) {
	flags.StringVarP(&o.File, "filename", "f", "", "Complete Experiment YAML or JSON manifest (use - for stdin)")
	flags.BoolVar(&o.Example, "example", false, "Print a complete example manifest without creating an experiment")
	o.IO.DefaultFormat("yaml")
	o.IO.BindFlags(flags)
}

func (o *createOpts) Validate() error {
	if o.Example {
		if o.File != "" {
			return errors.New("--example and --filename/-f cannot be used together")
		}
		return o.IO.Validate()
	}
	if o.File == "" {
		return errors.New("--filename/-f is required (use - to read from stdin)")
	}
	return o.IO.Validate()
}

func newCreateCommand(loader grafanaConfigLoader) *cobra.Command {
	opts := &createOpts{}
	cmd := &cobra.Command{
		Use:   "create",
		Short: "[experimental] Create an Odin experiment from a manifest.",
		Long: `This command is experimental. It may be removed, or its subcommands, flags and responses may change without following the normal semantic versioning conventions.

Create one Experiment through the Odin app plugin on the selected Grafana instance.
Supply the full resource as YAML or JSON. The plugin chooses the namespace and
validates the resource. New experiments should use an existing Grafana feature
toggle and include analyticsConfig with a query, variant mapping, and metrics.
Use spec.status: draft until the experiment is ready. This command never updates
an existing experiment.`,
		Example: `  gcx experiments create --example -o yaml
  gcx experiments create -f experiment.yaml
  gcx experiments create -f - < experiment.json
  gcx experiments create -f experiment.yaml -o json`,
		Args: cobra.NoArgs,
		Annotations: map[string]string{
			agent.AnnotationStability: agent.StabilityExperimental,
			agent.AnnotationTokenCost: "medium",
			agent.AnnotationLLMHint:   "Run --example to discover a complete manifest, then create with -f FILE or -f - for stdin. No prompts. Include analyticsConfig and metrics for an analyzable experiment.",
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}
			if opts.Example {
				var example Experiment
				if err := json.Unmarshal([]byte(exampleManifest), &example); err != nil {
					return err
				}
				return opts.IO.Encode(cmd.OutOrStdout(), example)
			}
			manifest, err := readManifest(opts.File, cmd.InOrStdin())
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
			created, err := client.Create(cmd.Context(), manifest)
			if err != nil {
				return err
			}
			return opts.IO.Encode(cmd.OutOrStdout(), created)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// This example uses a PostgreSQL query object. Other datasource types have
// different query fields, which should be taken from that datasource's editor.
const exampleManifest = `{
  "apiVersion": "odin.ext.grafana.com/v1alpha1",
  "kind": "Experiment",
  "metadata": {"name": "checkout-conversion"},
  "spec": {
    "title": "Checkout conversion",
    "description": "Measure whether the new checkout flow improves conversion.",
    "status": "draft",
    "grafanaFeatureToggle": {"name": "checkoutConversion"},
    "analyticsConfig": {
      "mode": "single",
      "timeRange": {"from": "now-14d", "to": "now"},
      "unifiedQuery": {
        "datasourceUid": "replace-with-datasource-uid",
        "datasourceType": "postgres",
        "query": {"rawSql": "SELECT user_id, variant, conversion FROM experiment_events"}
      },
      "entityField": "user_id",
      "variantConfig": {
        "field": "variant",
        "controlValues": ["control"],
        "treatmentValues": ["treatment"]
      },
      "kpiConfig": {"valueField": "conversion", "aggregation": "sum"},
      "metrics": [{"name": "Conversion", "goodDirection": "up", "valueField": "conversion", "aggregation": "sum"}]
    }
  }
}`
