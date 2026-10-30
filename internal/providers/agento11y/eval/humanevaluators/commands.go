package humanevaluators

import (
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/providers/agento11y/agento11yhttp"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

func newClient(cmd *cobra.Command, loader *providers.ConfigLoader) (*Client, error) {
	base, err := agento11yhttp.NewClientFromCommand(cmd, loader)
	if err != nil {
		return nil, err
	}
	return NewClient(base), nil
}

// Commands returns the human-evaluators command group.
func Commands(loader *providers.ConfigLoader) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "human-evaluators",
		Short: "List the human evaluators a review queue rubric can use.",
		Long: `List the human evaluators a review queue rubric can use.

A human evaluator is an evaluator whose answers come from a reviewer instead of a model. Use the IDs listed here as --rubric values for 'gcx agento11y collections create-review-queue' and 'gcx agento11y review-queues update'. Manage the evaluator definitions themselves with 'gcx agento11y evaluators'.`,
	}
	cmd.AddCommand(newListCommand(loader))
	return cmd
}

type listOpts struct {
	IO cmdio.Options
}

func (o *listOpts) setup(flags *pflag.FlagSet) {
	cmdio.RegisterTable(&o.IO, Table())
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)
}

func newListCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &listOpts{}
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List human evaluators at their latest version.",
		Long: `List human evaluators at their latest version, sorted by ID.

The command is only available for tenants with human review enabled; for other tenants the backend answers 404.`,
		Example: `  # List the questions a review rubric can be built from.
  gcx agento11y human-evaluators list -o json`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}

			client, err := newClient(cmd, loader)
			if err != nil {
				return err
			}

			items, err := client.List(cmd.Context())
			if err != nil {
				return err
			}
			return opts.IO.Encode(cmd.OutOrStdout(), items)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// Table returns the table codec for human evaluators.
func Table() cmdio.Table[RubricItem] {
	return cmdio.Table[RubricItem]{Columns: []cmdio.Column[RubricItem]{
		{Header: "ID", Content: func(r RubricItem) string { return r.EvaluatorID }},
		{Header: "VERSION", Content: func(r RubricItem) string { return r.EvaluatorVersion }},
		{Header: "OUTPUT KEY", Content: func(r RubricItem) string { return r.OutputKey.Key }},
		{Header: "TYPE", Content: func(r RubricItem) string { return r.OutputKey.Type }},
		{Header: "DESCRIPTION", Content: func(r RubricItem) string { return agento11yhttp.Truncate(r.Description, 40) }},
		{Header: "INSTRUCTIONS", Visible: cmdio.WideOnly, Content: func(r RubricItem) string { return agento11yhttp.Truncate(r.Instructions, 60) }},
	}}
}
