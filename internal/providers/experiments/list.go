package experiments

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/config"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type grafanaConfigLoader interface {
	LoadGrafanaConfig(ctx context.Context) (config.NamespacedRESTConfig, error)
}

type listOpts struct{ IO cmdio.Options }

func (o *listOpts) setup(flags *pflag.FlagSet) {
	cmdio.RegisterTable(&o.IO, experimentTable())
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)
}

func newListCommand(loader grafanaConfigLoader) *cobra.Command {
	opts := &listOpts{}
	cmd := &cobra.Command{
		Use:   "list",
		Short: "[experimental] List Odin experiments.",
		Long: `This command is experimental. It may be removed, or its subcommands, flags and responses may change without following the normal semantic versioning conventions.

List experiments through the Odin app plugin on the selected Grafana instance.
The plugin returns at most 500 experiments in one page; if 500 are returned,
the list may be incomplete.`,
		Example: `  gcx experiments list
  gcx experiments list -o json
  gcx experiments list --jq '[.[] | {name: .metadata.name, title: .spec.title, status: .spec.status}]'`,
		Args: cobra.NoArgs,
		Annotations: map[string]string{
			agent.AnnotationStability: agent.StabilityExperimental,
			agent.AnnotationTokenCost: "medium",
			agent.AnnotationLLMHint:   "Odin experiment inventory; up to 500 full resources. Use the default table or --jq to select fields for agents.",
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.IO.Validate(); err != nil {
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
			items, err := client.List(cmd.Context())
			if err != nil {
				return err
			}
			if len(items) >= pluginPageSize {
				cmdio.EmitWarn(cmd.ErrOrStderr(), "Odin returned 500 experiments, its page cap; this list may be incomplete")
			}
			return opts.IO.Encode(cmd.OutOrStdout(), items)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

func experimentTable() cmdio.Table[Experiment] {
	return cmdio.Table[Experiment]{
		Columns: []cmdio.Column[Experiment]{
			{Header: "NAME", Content: func(e Experiment) string { return nestedString(e, "metadata", "name") }},
			{Header: "TITLE", Content: func(e Experiment) string { return nestedString(e, "spec", "title") }},
			{Header: "STATUS", Content: func(e Experiment) string { return nestedString(e, "spec", "status") }},
			{Header: "NAMESPACE", Visible: cmdio.WideOnly, Content: func(e Experiment) string { return nestedString(e, "metadata", "namespace") }},
		},
		Empty: func(w io.Writer) error {
			_, err := fmt.Fprintln(w, "No experiments found.")
			return err
		},
	}
}

func nestedString(e Experiment, parent, key string) string {
	fields, ok := e[parent].(map[string]any)
	if !ok {
		return "-"
	}
	value, ok := fields[key].(string)
	if !ok || value == "" {
		return "-"
	}
	return strings.ReplaceAll(value, "\n", " ")
}
