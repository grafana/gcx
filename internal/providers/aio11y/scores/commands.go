package scores

import (
	"errors"
	"io"

	"github.com/grafana/gcx/internal/format"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/providers/aio11y/aio11yhttp"
	"github.com/grafana/gcx/internal/style"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

func newClient(cmd *cobra.Command, loader *providers.ConfigLoader) (*Client, error) {
	base, err := aio11yhttp.NewClientFromCommand(cmd, loader)
	if err != nil {
		return nil, err
	}
	return NewClient(base), nil
}

// --- list-scores ---

type listOpts struct {
	IO    cmdio.Options
	Limit int
}

func (o *listOpts) setup(flags *pflag.FlagSet) {
	o.IO.RegisterCustomCodec("table", &TableCodec{})
	o.IO.RegisterCustomCodec("wide", &TableCodec{Wide: true})
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)
	flags.IntVar(&o.Limit, "limit", 50, "Maximum number of scores to return")
}

// NewListScoresCommand returns the `list-scores` leaf command, mounted under
// `gcx agento11y generations`. Scores are addressed by the parent generation's
// ID, so the command is an operation-subject compound under generations; the
// command lives in this package alongside the scores client and table codec.
func NewListScoresCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &listOpts{}
	cmd := &cobra.Command{
		Use:   "list-scores <generation-id>",
		Short: "List evaluation scores for a generation.",
		Long:  `List evaluation scores produced by online rules for a generation.`,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}
			client, err := newClient(cmd, loader)
			if err != nil {
				return err
			}
			scores, err := client.ListByGeneration(cmd.Context(), args[0], opts.Limit)
			if err != nil {
				return err
			}
			return opts.IO.Encode(cmd.OutOrStdout(), scores)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// --- table codec ---

// TableCodec renders scores as a text table.
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
	items, ok := v.([]Score)
	if !ok {
		return errors.New("invalid data type for table codec: expected []Score")
	}

	var t *style.TableBuilder
	if c.Wide {
		t = style.NewTable("SCORE KEY", "TYPE", "VALUE", "PASSED", "EVALUATOR", "VERSION", "RULE", "EXPLANATION", "CREATED AT")
	} else {
		t = style.NewTable("SCORE KEY", "VALUE", "PASSED", "EVALUATOR", "CREATED AT")
	}

	for _, s := range items {
		passed := "-"
		if s.Passed != nil {
			if *s.Passed {
				passed = "yes"
			} else {
				passed = "no"
			}
		}

		if c.Wide {
			ruleID := s.RuleID
			if ruleID == "" {
				ruleID = "-"
			}
			explanation := aio11yhttp.Truncate(s.Explanation, 80)
			t.Row(s.ScoreKey, s.ScoreType, s.Value.Display(), passed,
				s.EvaluatorID, s.EvaluatorVersion, ruleID, explanation,
				aio11yhttp.FormatTime(s.CreatedAt))
		} else {
			t.Row(s.ScoreKey, s.Value.Display(), passed,
				s.EvaluatorID, aio11yhttp.FormatTime(s.CreatedAt))
		}
	}
	return t.Render(w)
}

func (c *TableCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("table format does not support decoding")
}
