// Package modelcards reads the model catalog (provider, limits and catalog
// pricing) behind Agent Observability.
package modelcards

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

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

// Commands returns the model-cards command group: read-only catalog lookup,
// separate from tenant pricing in model-rates.
func Commands(loader *providers.ConfigLoader) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "model-cards",
		Short: "Read model catalog metadata, not tenant-negotiated prices.",
		Long: `Read the public model catalog: provider, context limits, modalities and catalog pricing.

Catalog prices are not your negotiated rates. Use 'gcx agento11y model-rates get-effective' for the price that applies to your tenant.`,
	}
	cmd.AddCommand(
		newGetCommand(loader),
		newListCommand(loader),
	)
	return cmd
}

// --- get ---

type getOpts struct {
	IO            cmdio.Options
	Source        string
	SourceModelID string
}

func (o *getOpts) setup(flags *pflag.FlagSet) {
	o.IO.DefaultFormat("json")
	o.IO.BindFlags(flags)
	flags.StringVar(&o.Source, "source", "", "Catalog source, used with --source-model-id instead of a model key")
	flags.StringVar(&o.SourceModelID, "source-model-id", "", "The source's own model ID, used with --source instead of a model key")
}

// query resolves the lookup the flags and argument ask for. A model key and a
// source pair are alternatives, so exactly one is required.
func (o *getOpts) query(args []string) (LookupQuery, error) {
	if err := o.IO.Validate(); err != nil {
		return LookupQuery{}, err
	}
	key := ""
	if len(args) > 0 {
		key = strings.TrimSpace(args[0])
	}
	source := strings.TrimSpace(o.Source)
	sourceModelID := strings.TrimSpace(o.SourceModelID)

	switch {
	case key != "" && (source != "" || sourceModelID != ""):
		return LookupQuery{}, errors.New("give either a model key or --source with --source-model-id, not both")
	case key != "":
		return LookupQuery{ModelKey: key}, nil
	case source != "" && sourceModelID != "":
		return LookupQuery{Source: source, SourceModelID: sourceModelID}, nil
	case source != "" || sourceModelID != "":
		return LookupQuery{}, errors.New("--source and --source-model-id must be given together")
	default:
		return LookupQuery{}, errors.New("a model key, or --source with --source-model-id, is required")
	}
}

func newGetCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &getOpts{}
	cmd := &cobra.Command{
		Use:   "get [model-key]",
		Args:  cobra.MaximumNArgs(1),
		Short: "Look up a canonical model key, including provider and available limits.",
		Long: `Read the model card for a canonical model key such as openai/gpt-4o-mini, or for a catalog source and that source's own model ID.

The result is the backend envelope: the card under "data" and catalog freshness under "freshness". Missing limits mean unknown, not zero. A failed lookup does not prove the model does not exist.`,
		Example: `  # Look up a model card by canonical key.
  gcx agento11y model-cards get openai/gpt-4o-mini -o json

  # Look up by a catalog source's own model ID.
  gcx agento11y model-cards get --source openrouter --source-model-id openai/gpt-4o-mini`,
		RunE: func(cmd *cobra.Command, args []string) error {
			query, err := opts.query(args)
			if err != nil {
				return err
			}

			client, err := newClient(cmd, loader)
			if err != nil {
				return err
			}

			result, err := client.Lookup(cmd.Context(), query)
			if err != nil {
				return err
			}
			return opts.IO.Encode(cmd.OutOrStdout(), result)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// --- list ---

type listOpts struct {
	IO               cmdio.Options
	Query            string
	Source           string
	Provider         string
	Regex            string
	Sort             string
	Order            string
	FreeOnly         bool
	MinContextLength int
	Limit            int64
}

func (o *listOpts) setup(flags *pflag.FlagSet) {
	cmdio.RegisterTable(&o.IO, Table())
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)
	flags.StringVar(&o.Query, "query", "", "Free-text filter on model name and key")
	flags.StringVar(&o.Source, "source", "", "Only cards from this catalog source")
	flags.StringVar(&o.Provider, "provider", "", "Only cards for this provider")
	flags.StringVar(&o.Regex, "regex", "", "Only cards whose model key matches this regular expression")
	flags.BoolVar(&o.FreeOnly, "free-only", false, "Only free models (no effect unless set to true; it does not select paid models)")
	flags.IntVar(&o.MinContextLength, "min-context-length", 0, "Only models with at least this many context tokens")
	flags.StringVar(&o.Sort, "sort", "", "Sort by name (default), provider, prompt_price, context_length or last_seen_at")
	flags.StringVar(&o.Order, "order", "", "Sort order: asc (default) or desc")
	flags.Int64Var(&o.Limit, "limit", 50, "Maximum number of cards to return (0 for no limit)")
}

func (o *listOpts) Validate() error {
	if err := o.IO.Validate(); err != nil {
		return err
	}
	if o.Limit < 0 {
		return errors.New("--limit cannot be negative")
	}
	if o.MinContextLength < 0 {
		return errors.New("--min-context-length cannot be negative")
	}
	switch strings.ToLower(o.Sort) {
	case "", "name", "provider", "prompt_price", "context_length", "last_seen_at":
	default:
		return fmt.Errorf("--sort must be one of name, provider, prompt_price, context_length or last_seen_at, got %q", o.Sort)
	}
	switch strings.ToLower(o.Order) {
	case "", "asc", "desc":
	default:
		return fmt.Errorf("--order must be asc or desc, got %q", o.Order)
	}
	return nil
}

func (o *listOpts) listQuery(flags *pflag.FlagSet) ListQuery {
	q := ListQuery{
		Q:        strings.TrimSpace(o.Query),
		Source:   strings.TrimSpace(o.Source),
		Provider: strings.TrimSpace(o.Provider),
		Regex:    o.Regex,
		Sort:     strings.ToLower(o.Sort),
		Order:    strings.ToLower(o.Order),
	}
	if flags.Changed("free-only") {
		q.FreeOnly = &o.FreeOnly
	}
	if flags.Changed("min-context-length") {
		q.MinContextLength = &o.MinContextLength
	}
	return q
}

func newListCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &listOpts{}
	cmd := &cobra.Command{
		Use:   "list",
		Args:  cobra.NoArgs,
		Short: "Search the model catalog.",
		Long: `Search the model catalog by text, provider, source, context length or price.

Prices in the table are catalog prices in USD per million tokens, shown as given by the catalog and not as your negotiated rates. A dash means the catalog has no price for that dimension, which is not the same as free.`,
		Example: `  # The first 50 cards.
  gcx agento11y model-cards list

  # OpenAI models with at least a 100k context window, cheapest prompt first.
  gcx agento11y model-cards list --provider openai --min-context-length 100000 --sort prompt_price

  # Every card matching a pattern, with all columns.
  gcx agento11y model-cards list --regex '^openai/gpt-5' --limit 0 -o wide`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}

			client, err := newClient(cmd, loader)
			if err != nil {
				return err
			}

			result, err := client.List(cmd.Context(), opts.listQuery(cmd.Flags()), int(opts.Limit))
			if err != nil {
				return err
			}
			if result.Freshness.Stale {
				cmdio.Warning(cmd.ErrOrStderr(), "The model catalog is stale; prices and limits may be out of date.")
			}
			if err := opts.IO.Encode(cmd.OutOrStdout(), result.Cards); err != nil {
				return err
			}
			if result.HasMore {
				// Typed diagnostic, like conversations search: it keeps stderr
				// machine-parseable in agent mode.
				cmdio.EmitWarn(cmd.ErrOrStderr(),
					fmt.Sprintf("Results truncated. %d shown, more available. Raise --limit (0 for no limit) or narrow with --query, --provider, --source or --regex.", len(result.Cards)))
			}
			return nil
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// --- table codec ---

// Table renders catalog cards. Prices are converted from the catalog's USD per
// token to USD per million tokens, the unit model-rates uses.
func Table() cmdio.Table[Card] {
	return cmdio.Table[Card]{Columns: []cmdio.Column[Card]{
		{Header: "MODEL KEY", Content: func(c Card) string { return cmdio.OrDash(c.str("model_key")) }},
		{Header: "PROVIDER", Content: func(c Card) string { return cmdio.OrDash(c.str("provider")) }},
		{Header: "NAME", Content: func(c Card) string { return cmdio.OrDash(c.str("name")) }},
		{Header: "CONTEXT", Content: func(c Card) string { return c.number("context_length", 1) }},
		{Header: "INPUT /1M", Content: func(c Card) string { return c.price("prompt_usd_per_token") }},
		{Header: "OUTPUT /1M", Content: func(c Card) string { return c.price("completion_usd_per_token") }},
		{Header: "SOURCE", Visible: cmdio.WideOnly, Content: func(c Card) string { return cmdio.OrDash(c.str("source")) }},
		{Header: "FREE", Visible: cmdio.WideOnly, Content: func(c Card) string {
			if free, ok := c["is_free"].(bool); ok && free {
				return "yes"
			}
			return "no"
		}},
		{Header: "MODALITY", Visible: cmdio.WideOnly, Content: func(c Card) string { return cmdio.OrDash(c.str("modality")) }},
		{Header: "CACHE READ /1M", Visible: cmdio.WideOnly, Content: func(c Card) string { return c.price("input_cache_read_usd_per_token") }},
		{Header: "CACHE WRITE /1M", Visible: cmdio.WideOnly, Content: func(c Card) string { return c.price("input_cache_write_usd_per_token") }},
	}}
}

func (c Card) str(key string) string {
	s, _ := c[key].(string)
	return s
}

// number renders a numeric top-level field scaled by 1/divisor, or a dash when
// the field is absent.
func (c Card) number(key string, divisor float64) string {
	n, ok := rawNumber(c[key])
	if !ok {
		return "-"
	}
	return strconv.FormatFloat(n/divisor, 'g', 10, 64)
}

// price renders a catalog per-token price as USD per million tokens. An absent
// price is a dash, and a catalog zero prints as zero.
func (c Card) price(key string) string {
	pricing, _ := c["pricing"].(map[string]any)
	n, ok := rawNumber(pricing[key])
	if !ok {
		return "-"
	}
	return strconv.FormatFloat(n*1e6, 'g', 10, 64)
}
