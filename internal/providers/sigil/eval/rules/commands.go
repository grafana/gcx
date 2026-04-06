package rules

import (
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/grafana/gcx/internal/format"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/providers/sigil/commandutil"
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

// Commands returns the rules command group.
func Commands(loader *providers.ConfigLoader) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "rules",
		Short: "Query Sigil evaluation rules.",
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
		Use:   "show [rule-id]",
		Short: "Show evaluation rules or a single rule detail.",
		Long: `Show evaluation rules. Without an ID, lists all rules.
With an ID, shows the full rule definition.`,
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
				if commandutil.ShouldDefaultDetailToYAML(cmd) {
					opts.IO.OutputFormat = "yaml"
				}
				if err := commandutil.ValidateDetailOutputFormat(cmd, opts.IO.OutputFormat, "rule", args[0]); err != nil {
					return err
				}
				rule, err := client.Get(cmd.Context(), args[0])
				if err != nil {
					return err
				}
				return opts.IO.Encode(cmd.OutOrStdout(), rule)
			}

			rules, err := client.List(cmd.Context())
			if err != nil {
				return err
			}
			return opts.IO.Encode(cmd.OutOrStdout(), rules)
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
	rules, ok := v.([]eval.RuleDefinition)
	if !ok {
		return errors.New("invalid data type for table codec: expected []RuleDefinition")
	}

	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	if c.Wide {
		fmt.Fprintln(tw, "ID\tENABLED\tSELECTOR\tSAMPLE RATE\tEVALUATORS\tCREATED BY\tCREATED AT")
	} else {
		fmt.Fprintln(tw, "ID\tENABLED\tSELECTOR\tSAMPLE RATE\tEVALUATORS")
	}

	for _, r := range rules {
		enabled := "no"
		if r.Enabled {
			enabled = "yes"
		}
		evalIDs := strings.Join(r.EvaluatorIDs, ", ")
		if evalIDs == "" {
			evalIDs = "-"
		}
		sampleRate := strconv.FormatFloat(r.SampleRate, 'f', -1, 64)

		if c.Wide {
			createdBy := r.CreatedBy
			if createdBy == "" {
				createdBy = "-"
			}
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
				r.RuleID, enabled, r.Selector, sampleRate, evalIDs, createdBy, sigilhttp.FormatTime(r.CreatedAt))
		} else {
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n",
				r.RuleID, enabled, r.Selector, sampleRate, evalIDs)
		}
	}
	return tw.Flush()
}

func (c *TableCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("table format does not support decoding")
}
