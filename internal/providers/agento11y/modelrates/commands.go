package modelrates

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/providers/agento11y/agento11yhttp"
	"github.com/grafana/gcx/internal/providers/agento11y/commandutil"
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

// Commands returns the model-rates command group.
func Commands(loader *providers.ConfigLoader) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "model-rates",
		Short: "Configure your own negotiated model prices.",
		Long: `Configure your own negotiated model prices.

Cost figures are otherwise computed from a public catalog, so a negotiated
rate, a committed-use discount or a reseller agreement shows a price that is
not yours — and a model the catalog does not carry shows no price at all.

Rates are given in USD per million tokens, the same way providers publish
them, so what you type is what you can check against your contract.

Setting a rate applies it from that moment on. Generations already recorded
keep the price they were given, and a later change records a new rate rather
than replacing the old one, so history stays readable.`,
	}
	cmd.AddCommand(
		newCreateCommand(loader),
		newListCommand(loader),
		newDeleteCommand(loader),
	)
	return cmd
}

// --- create ---

type createOpts struct {
	IO       cmdio.Options
	Provider string
	Model    string

	Input      float64
	Output     float64
	Request    float64
	CacheRead  float64
	CacheWrite float64

	LongContextThreshold  int64
	LongContextInput      float64
	LongContextOutput     float64
	LongContextCacheRead  float64
	LongContextCacheWrite float64
}

func (o *createOpts) setup(flags *pflag.FlagSet) {
	o.IO.DefaultFormat("yaml")
	o.IO.BindFlags(flags)

	flags.StringVar(&o.Provider, "provider", "", "Provider the model belongs to, as your telemetry reports it (required)")
	flags.StringVar(&o.Model, "model", "", "Model the rate applies to, as your telemetry reports it (required)")

	flags.Float64Var(&o.Input, "price-input", 0, "USD per million input tokens")
	flags.Float64Var(&o.Output, "price-output", 0, "USD per million output tokens")
	flags.Float64Var(&o.Request, "price-request", 0, "USD charged per call, for models that have a flat fee")
	flags.Float64Var(&o.CacheRead, "price-cache-read", 0, "USD per million tokens read from the prompt cache")
	flags.Float64Var(&o.CacheWrite, "price-cache-write", 0, "USD per million tokens written to the prompt cache")

	flags.Int64Var(&o.LongContextThreshold, "long-context-threshold", 0, "Input tokens above which the long-context rates apply")
	flags.Float64Var(&o.LongContextInput, "long-context-price-input", 0, "USD per million input tokens above the threshold")
	flags.Float64Var(&o.LongContextOutput, "long-context-price-output", 0, "USD per million output tokens above the threshold")
	flags.Float64Var(&o.LongContextCacheRead, "long-context-price-cache-read", 0, "USD per million cache-read tokens above the threshold")
	flags.Float64Var(&o.LongContextCacheWrite, "long-context-price-cache-write", 0, "USD per million cache-write tokens above the threshold")
}

// toWrite builds the request from the flags the caller actually set. An unset
// rate is left out rather than sent as zero, because the two mean different
// things: zero says the model does not charge for that bucket, and absent says
// nothing is configured for it.
func (o *createOpts) toWrite(flags *pflag.FlagSet) (*RateWrite, error) {
	provider := strings.TrimSpace(o.Provider)
	model := strings.TrimSpace(o.Model)
	if provider == "" {
		return nil, errors.New("--provider is required")
	}
	if model == "" {
		return nil, errors.New("--model is required")
	}

	write := &RateWrite{
		Provider:                provider,
		Model:                   model,
		InputUSDPerMillion:      setFloat(flags, "price-input", o.Input),
		OutputUSDPerMillion:     setFloat(flags, "price-output", o.Output),
		RequestUSD:              setFloat(flags, "price-request", o.Request),
		CacheReadUSDPerMillion:  setFloat(flags, "price-cache-read", o.CacheRead),
		CacheWriteUSDPerMillion: setFloat(flags, "price-cache-write", o.CacheWrite),
	}

	// A row is the whole price for a model: nothing is completed from the
	// catalog, so a row that prices nothing would silently mean the model is
	// free. The API rejects that too; failing here names the flags instead.
	if write.InputUSDPerMillion == nil && write.OutputUSDPerMillion == nil && write.RequestUSD == nil &&
		write.CacheReadUSDPerMillion == nil && write.CacheWriteUSDPerMillion == nil {
		return nil, errors.New("set at least one of --price-input, --price-output, --price-request, --price-cache-read or --price-cache-write")
	}

	tierSet := flags.Changed("long-context-price-input") || flags.Changed("long-context-price-output") ||
		flags.Changed("long-context-price-cache-read") || flags.Changed("long-context-price-cache-write")
	if flags.Changed("long-context-threshold") || tierSet {
		if o.LongContextThreshold <= 0 {
			return nil, errors.New("--long-context-threshold is required, and must be positive, to price a long-context tier")
		}
		write.LongContext = &LongContextRate{
			ThresholdInputTokens:    o.LongContextThreshold,
			InputUSDPerMillion:      setFloat(flags, "long-context-price-input", o.LongContextInput),
			OutputUSDPerMillion:     setFloat(flags, "long-context-price-output", o.LongContextOutput),
			CacheReadUSDPerMillion:  setFloat(flags, "long-context-price-cache-read", o.LongContextCacheRead),
			CacheWriteUSDPerMillion: setFloat(flags, "long-context-price-cache-write", o.LongContextCacheWrite),
		}
	}
	return write, nil
}

// setFloat returns a pointer only when the caller passed the flag, so a rate
// nobody mentioned is absent rather than zero.
func setFloat(flags *pflag.FlagSet, name string, value float64) *float64 {
	if !flags.Changed(name) {
		return nil
	}
	return &value
}

func newCreateCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &createOpts{}
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Record your price for one model, in force from now.",
		Long: `Record your price for one model, in force from now.

The rate replaces the public catalog price for this provider and model
completely: nothing is filled in from the catalog card, so state every rate
your contract covers.

A bucket you leave unset is not charged, and passing 0 charges the same. The
difference is what it records: 0 says your contract prices that bucket at
nothing, while leaving the flag off says nothing about it at all. At least one
rate has to be set, and an explicit 0 counts.

Generations already recorded keep the price they were given. A later call
records a new rate rather than overwriting this one.`,
		Example: `  # A flat negotiated rate.
  gcx agento11y model-rates create --provider openai --model gpt-5.5 \
      --price-input 2.00 --price-output 8.00

  # A model the catalog does not carry.
  gcx agento11y model-rates create --provider acme --model in-house-7b \
      --price-input 1.00 --price-output 4.00

  # A contract that keeps the provider's long-context tier.
  gcx agento11y model-rates create --provider openai --model gpt-5.5 \
      --price-input 2.00 --price-output 8.00 \
      --long-context-threshold 272000 \
      --long-context-price-input 4.00 --long-context-price-output 12.00`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}
			write, err := opts.toWrite(cmd.Flags())
			if err != nil {
				return err
			}
			client, err := newClient(cmd, loader)
			if err != nil {
				return err
			}
			stored, err := client.Set(cmd.Context(), write)
			if err != nil {
				return err
			}
			return opts.IO.Encode(cmd.OutOrStdout(), stored)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// --- list ---

type listOpts struct {
	IO    cmdio.Options
	Limit int64
}

func (o *listOpts) setup(flags *pflag.FlagSet) {
	cmdio.RegisterTable(&o.IO, Table())
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)
	flags.Int64Var(&o.Limit, "limit", 50, "Maximum number of rates to return (0 for no limit)")
}

func newListCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &listOpts{}
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List the rates you have configured.",
		Long: `List the rates you have configured.

Superseded rates are listed too. A change records a new rate rather than
replacing the old one, so a model can appear more than once — the newest
effective-from is the one in force, and the others are what priced the
generations that arrived while they applied.`,
		Example: `  # Everything configured, newest first per model.
  gcx agento11y model-rates list

  # With every rate column.
  gcx agento11y model-rates list -o wide`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}
			client, err := newClient(cmd, loader)
			if err != nil {
				return err
			}
			rates, err := client.List(cmd.Context(), int(opts.Limit))
			if err != nil {
				return err
			}
			return opts.IO.Encode(cmd.OutOrStdout(), rates)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// --- delete ---

type deleteOpts struct {
	IO            cmdio.Options
	Provider      string
	Model         string
	EffectiveFrom string
	Force         bool
}

func (o *deleteOpts) setup(flags *pflag.FlagSet) {
	// The receipt goes to stderr and the result document to stdout, so the
	// human default prints nothing extra — the same silent codec the other
	// delete verbs register.
	o.IO.RegisterCustomCodec("text", commandutil.SilentTextCodec{})
	o.IO.DefaultFormat("text")
	o.IO.BindFlags(flags)
	flags.BoolVar(&o.Force, "force", false, "Skip confirmation prompt")
	flags.StringVar(&o.Provider, "provider", "", "Provider of the rate to delete (required)")
	flags.StringVar(&o.Model, "model", "", "Model of the rate to delete (required)")
	flags.StringVar(&o.EffectiveFrom, "effective-from", "", "effective-from of the rate to delete, as 'list' reports it (required)")
}

func newDeleteCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &deleteOpts{}
	cmd := &cobra.Command{
		Use:   "delete",
		Short: "Delete one configured rate.",
		Long: `Delete one configured rate.

A model can carry several rates, one per change, so a delete names which by
its effective-from. Take that value from 'list'.

Deleting does not restore the catalog price for generations this rate already
priced: their cost was computed when they arrived and is not recalculated.
What changes is the price applied from now on.`,
		Example: `  # Take effective-from from list, then delete that rate.
  gcx agento11y model-rates list
  gcx agento11y model-rates delete --provider openai --model gpt-5.5 \
      --effective-from 2026-09-23T09:14:22.481739Z`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}
			provider := strings.TrimSpace(opts.Provider)
			model := strings.TrimSpace(opts.Model)
			raw := strings.TrimSpace(opts.EffectiveFrom)
			if provider == "" || model == "" || raw == "" {
				return errors.New("--provider, --model and --effective-from are all required")
			}
			effectiveFrom, err := time.Parse(time.RFC3339Nano, raw)
			if err != nil {
				return fmt.Errorf("--effective-from must be an RFC 3339 timestamp as 'list' reports it: %w", err)
			}
			proceed, err := providers.ConfirmDestructive(cmd.InOrStdin(), cmd.ErrOrStderr(), opts.Force,
				fmt.Sprintf("Delete the %s/%s rate effective from %s?", provider, model, raw))
			if err != nil {
				return err
			}
			if !proceed {
				return nil
			}

			client, err := newClient(cmd, loader)
			if err != nil {
				return err
			}
			// Through the shared batch helper even for one rate, so the
			// result reaches stdout as the document the finite output class
			// requires. Writing a sentence here instead made -o json print
			// English and put prose on an agent's stdout.
			target := fmt.Sprintf("%s/%s@%s", provider, model, raw)
			return commandutil.RunBatchDelete(cmd.OutOrStdout(), cmd.ErrOrStderr(), &opts.IO,
				"rate", "Deleted rate %s", "deleting rate %s", []string{target},
				func(string) error {
					return client.Delete(cmd.Context(), provider, model, effectiveFrom)
				})
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// Table renders configured rates. Per-million figures are what the caller
// typed, so they are shown as given rather than reformatted as currency.
func Table() cmdio.Table[Rate] {
	return cmdio.Table[Rate]{Columns: []cmdio.Column[Rate]{
		{Header: "PROVIDER", Content: func(r Rate) string { return r.Provider }},
		{Header: "MODEL", Content: func(r Rate) string { return r.Model }},
		{Header: "INPUT /1M", Content: func(r Rate) string { return perMillion(r.InputUSDPerMillion) }},
		{Header: "OUTPUT /1M", Content: func(r Rate) string { return perMillion(r.OutputUSDPerMillion) }},
		{Header: "TIER", Content: func(r Rate) string {
			if r.LongContext == nil {
				return "-"
			}
			return "above " + strconv.FormatInt(r.LongContext.ThresholdInputTokens, 10)
		}},
		{Header: "EFFECTIVE FROM", Content: func(r Rate) string { return cmdio.OrDash(r.EffectiveFrom) }},
		{Header: "REQUEST", Visible: cmdio.WideOnly, Content: func(r Rate) string { return perMillion(r.RequestUSD) }},
		{Header: "CACHE READ /1M", Visible: cmdio.WideOnly, Content: func(r Rate) string { return perMillion(r.CacheReadUSDPerMillion) }},
		{Header: "CACHE WRITE /1M", Visible: cmdio.WideOnly, Content: func(r Rate) string { return perMillion(r.CacheWriteUSDPerMillion) }},
		{Header: "CREATED BY", Visible: cmdio.WideOnly, Content: func(r Rate) string { return cmdio.OrDash(r.CreatedBy) }},
	}}
}

// perMillion distinguishes "not charged" from "free": an absent rate is a
// dash, and a configured zero prints as zero.
func perMillion(value *float64) string {
	if value == nil {
		return "-"
	}
	return strconv.FormatFloat(*value, 'f', -1, 64)
}
