package evaluators

import (
	"errors"
	"fmt"
	"io"
	"text/tabwriter"

	"github.com/grafana/gcx/internal/format"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/providers/sigil/eval"
	"github.com/grafana/gcx/internal/providers/sigil/sigilhttp"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

func newClient(cmd *cobra.Command, loader *providers.ConfigLoader) (*Client, error) {
	base, err := sigilhttp.NewClientFromCommand(cmd, loader)
	if err != nil {
		return nil, err
	}
	return NewClient(base), nil
}

// Commands returns the evaluators command group.
func Commands(loader *providers.ConfigLoader) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "evaluators",
		Short: "Query Sigil evaluators.",
	}
	cmd.AddCommand(
		newShowCommand(loader),
	)
	return cmd
}

// --- show (list + get) ---

type showOpts struct {
	IO cmdio.Options
}

func (o *showOpts) setup(flags *pflag.FlagSet) {
	o.IO.RegisterCustomCodec("table", &TableCodec{})
	o.IO.RegisterCustomCodec("wide", &TableCodec{Wide: true})
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)
}

func newShowCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &showOpts{}
	cmd := &cobra.Command{
		Use:   "show [evaluator-id]",
		Short: "Show evaluators or a single evaluator detail.",
		Long: `Show evaluators. Without an ID, lists all evaluators.
With an ID, shows the full evaluator definition.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}
			client, err := newClient(cmd, loader)
			if err != nil {
				return err
			}

			if len(args) == 1 {
				if !cmd.Flags().Changed("output") && !cmd.Flags().Changed("json") {
					opts.IO.OutputFormat = "yaml"
				}
				evaluator, err := client.Get(cmd.Context(), args[0])
				if err != nil {
					return err
				}
				return opts.IO.Encode(cmd.OutOrStdout(), evaluator)
			}

			evaluators, err := client.List(cmd.Context())
			if err != nil {
				return err
			}
			return opts.IO.Encode(cmd.OutOrStdout(), evaluators)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// --- table codec ---

type TableCodec struct {
	Wide bool
}

func (c *TableCodec) Format() format.Format {
	if c.Wide {
		return "wide"
	}
	return "table"
}

func (c *TableCodec) Encode(w io.Writer, v any) error {
	evaluators, ok := v.([]eval.EvaluatorDefinition)
	if !ok {
		return errors.New("invalid data type for table codec: expected []EvaluatorDefinition")
	}

	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	if c.Wide {
		fmt.Fprintln(tw, "ID\tVERSION\tKIND\tDESCRIPTION\tOUTPUTS\tCREATED BY\tCREATED AT")
	} else {
		fmt.Fprintln(tw, "ID\tVERSION\tKIND\tDESCRIPTION")
	}

	for _, e := range evaluators {
		desc := sigilhttp.Truncate(e.Description, 40)

		if c.Wide {
			createdBy := e.CreatedBy
			if createdBy == "" {
				createdBy = "-"
			}
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%d\t%s\t%s\n",
				e.EvaluatorID, e.Version, e.Kind, desc, len(e.OutputKeys), createdBy, sigilhttp.FormatTime(e.CreatedAt))
		} else {
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n",
				e.EvaluatorID, e.Version, e.Kind, desc)
		}
	}
	return tw.Flush()
}

func (c *TableCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("table format does not support decoding")
}
