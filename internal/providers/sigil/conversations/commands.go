package conversations

import (
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/grafana/gcx/internal/format"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
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

// Commands returns the conversations command group.
func Commands(loader *providers.ConfigLoader) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "conversations",
		Short: "Query Sigil conversations.",
	}

	cmd.AddCommand(
		newShowCommand(loader),
		newSearchCommand(loader),
	)
	return cmd
}

// --- show (list + get) ---

type showOpts struct {
	IO    cmdio.Options
	Limit int
}

func (o *showOpts) setup(flags *pflag.FlagSet) {
	o.IO.RegisterCustomCodec("table", &TableCodec{})
	o.IO.RegisterCustomCodec("wide", &TableCodec{Wide: true})
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)
	flags.IntVar(&o.Limit, "limit", 100, "Maximum number of conversations to return (0 for no limit)")
}

func newShowCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &showOpts{}
	cmd := &cobra.Command{
		Use:   "show [conversation-id]",
		Short: "Show conversations or a single conversation detail.",
		Long: `Show conversations. Without an ID, lists conversations (use --limit to control count).
With an ID, shows the full conversation detail including all generations.`,
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
				// Detail mode: get a single conversation by ID.
				// Switch to yaml unless the user explicitly set --output.
				if !cmd.Flags().Changed("output") {
					opts.IO.OutputFormat = "yaml"
				}
				detail, err := client.Get(cmd.Context(), args[0])
				if err != nil {
					return err
				}
				return opts.IO.Encode(cmd.OutOrStdout(), detail)
			}

			// List mode: return conversations with limit.
			convs, err := client.List(cmd.Context(), opts.Limit)
			if err != nil {
				return err
			}
			return opts.IO.Encode(cmd.OutOrStdout(), convs)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// --- search ---

type searchOpts struct {
	IO       cmdio.Options
	Filters  string
	From     string
	To       string
	PageSize int
}

func (o *searchOpts) setup(flags *pflag.FlagSet) {
	o.IO.RegisterCustomCodec("table", &SearchTableCodec{})
	o.IO.RegisterCustomCodec("wide", &SearchTableCodec{Wide: true})
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)
	flags.StringVar(&o.Filters, "filters", "", "Filter expression for conversation search")
	flags.StringVar(&o.From, "from", "", "Start of time range (RFC3339, e.g. 2026-01-01T00:00:00Z)")
	flags.StringVar(&o.To, "to", "", "End of time range (RFC3339, e.g. 2026-12-31T23:59:59Z)")
	flags.IntVar(&o.PageSize, "page-size", 50, "Number of results per page")
}

func newSearchCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &searchOpts{}
	cmd := &cobra.Command{
		Use:   "search",
		Short: "Search conversations with filters.",
		Long: `Search conversations using filter expressions and time ranges.

Defaults to the last 24 hours. Use --from and --to for custom ranges (both required).

Filter syntax: key operator "value" (multiple filters separated by spaces).

Filter keys (trace): model, provider, agent, agent.version, status,
  error.type, error.category, duration, tool.name, operation, namespace, cluster, service
Filter keys (metadata): generation_count, eval.passed, eval.evaluator_id, eval.score_key, eval.score
Operators: =, !=, >, <, >=, <=, =~ (regex)

Returns a single page of results (controlled by --page-size). A warning is
shown when more results are available.`,
		Example: `  gcx sigil conversations search --filters 'agent = "claude-code"'
  gcx sigil conversations search --filters 'agent = "claude-code" model = "claude-opus-4-6"'
  gcx sigil conversations search --filters 'status = "error"' --from 2026-04-01T00:00:00Z --to 2026-04-02T00:00:00Z`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}
			client, err := newClient(cmd, loader)
			if err != nil {
				return err
			}

			tr, err := parseTimeRange(opts.From, opts.To)
			if err != nil {
				return err
			}

			req := SearchRequest{
				Filters:   opts.Filters,
				PageSize:  opts.PageSize,
				TimeRange: tr,
			}

			resp, err := client.Search(cmd.Context(), req)
			if err != nil {
				return err
			}
			if err := opts.IO.Encode(cmd.OutOrStdout(), resp.Conversations); err != nil {
				return err
			}
			if resp.HasMore {
				cmdio.Warning(cmd.ErrOrStderr(), "Results truncated. %d shown, more available. Use --page-size to adjust.", len(resp.Conversations))
			}
			return nil
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// --- list table codec (Conversation — upstream fields) ---

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
	convs, ok := v.([]Conversation)
	if !ok {
		return errors.New("invalid data type for table codec: expected []Conversation")
	}

	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	if c.Wide {
		fmt.Fprintln(tw, "ID\tTITLE\tGENERATIONS\tCREATED\tLAST ACTIVITY")
	} else {
		fmt.Fprintln(tw, "ID\tTITLE\tGENERATIONS\tLAST ACTIVITY")
	}

	for _, conv := range convs {
		title := truncate(conv.Title, 40)
		lastActivity := sigilhttp.FormatTime(conv.LastGenerationAt)

		if c.Wide {
			created := sigilhttp.FormatTime(conv.CreatedAt)
			fmt.Fprintf(tw, "%s\t%s\t%d\t%s\t%s\n",
				conv.ID, title, conv.GenerationCount, created, lastActivity)
		} else {
			fmt.Fprintf(tw, "%s\t%s\t%d\t%s\n",
				conv.ID, title, conv.GenerationCount, lastActivity)
		}
	}
	return tw.Flush()
}

func (c *TableCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("table format does not support decoding")
}

// --- search table codec (SearchResult — plugin fields) ---

type SearchTableCodec struct {
	Wide bool
}

func (c *SearchTableCodec) Format() format.Format {
	if c.Wide {
		return "wide"
	}
	return "table"
}

func (c *SearchTableCodec) Encode(w io.Writer, v any) error {
	results, ok := v.([]SearchResult)
	if !ok {
		return errors.New("invalid data type for table codec: expected []SearchResult")
	}

	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	if c.Wide {
		fmt.Fprintln(tw, "ID\tTITLE\tGENERATIONS\tMODELS\tAGENTS\tERRORS\tLAST ACTIVITY")
	} else {
		fmt.Fprintln(tw, "ID\tTITLE\tGENERATIONS\tMODELS\tLAST ACTIVITY")
	}

	for _, r := range results {
		title := truncate(r.ConversationTitle, 40)
		models := strings.Join(r.Models, ", ")
		if models == "" {
			models = "-"
		}
		lastActivity := sigilhttp.FormatTime(r.LastGenerationAt)

		if c.Wide {
			agents := strings.Join(r.Agents, ", ")
			if agents == "" {
				agents = "-"
			}
			errCount := "-"
			if r.ErrorCount > 0 {
				errCount = strconv.Itoa(r.ErrorCount)
			}
			fmt.Fprintf(tw, "%s\t%s\t%d\t%s\t%s\t%s\t%s\n",
				r.ConversationID, title, r.GenerationCount, models, agents, errCount, lastActivity)
		} else {
			fmt.Fprintf(tw, "%s\t%s\t%d\t%s\t%s\n",
				r.ConversationID, title, r.GenerationCount, models, lastActivity)
		}
	}
	return tw.Flush()
}

func (c *SearchTableCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("table format does not support decoding")
}

func parseTimeRange(from, to string) (*SearchTimeRange, error) {
	if from == "" && to == "" {
		// Default to last 24 hours — Sigil requires both bounds.
		now := time.Now().UTC()
		return &SearchTimeRange{From: now.Add(-24 * time.Hour), To: now}, nil
	}
	if from == "" || to == "" {
		return nil, errors.New("both --from and --to are required (Sigil requires a complete time range)")
	}
	fromT, err := time.Parse(time.RFC3339, from)
	if err != nil {
		return nil, fmt.Errorf("invalid --from value: %w", err)
	}
	toT, err := time.Parse(time.RFC3339, to)
	if err != nil {
		return nil, fmt.Errorf("invalid --to value: %w", err)
	}
	if !fromT.Before(toT) {
		return nil, errors.New("--from must be before --to")
	}
	return &SearchTimeRange{From: fromT, To: toT}, nil
}

func truncate(s string, maxLen int) string {
	if s == "" {
		return "-"
	}
	r := []rune(s)
	if len(r) > maxLen {
		return string(r[:maxLen-3]) + "..."
	}
	return s
}
