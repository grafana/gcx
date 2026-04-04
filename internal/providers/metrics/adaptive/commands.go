package metrics

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	auth "github.com/grafana/gcx/internal/auth/adaptive"
	"github.com/grafana/gcx/internal/format"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/style"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type metricsHelper struct {
	loader *providers.ConfigLoader
}

// Commands returns the Cobra command tree for adaptive metrics management.
func Commands(loader *providers.ConfigLoader) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "metrics",
		Short: "Manage Adaptive Metrics resources.",
	}

	h := &metricsHelper{loader: loader}
	cmd.AddCommand(h.rulesCommand(), h.recommendationsCommand())

	return cmd
}

// ---------------------------------------------------------------------------
// Rules commands
// ---------------------------------------------------------------------------

func (h *metricsHelper) rulesCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "rules",
		Short: "Manage aggregation rules.",
	}

	cmd.AddCommand(h.rulesShowCommand(), h.rulesSyncCommand())

	return cmd
}

type rulesShowOpts struct {
	cmdio.Options
}

func (o *rulesShowOpts) setup(flags *pflag.FlagSet) {
	o.DefaultFormat("table")
	o.RegisterCustomCodec("table", newRulesTableCodec(false))
	o.RegisterCustomCodec("wide", newRulesTableCodec(true))
	o.BindFlags(flags)
}

func (h *metricsHelper) rulesShowCommand() *cobra.Command {
	opts := &rulesShowOpts{}
	cmd := &cobra.Command{
		Use:   "show",
		Short: "Show current aggregation rules.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}

			ctx := cmd.Context()
			signalAuth, err := auth.ResolveSignalAuth(ctx, h.loader, "metrics")
			if err != nil {
				return err
			}

			client := NewClient(signalAuth.BaseURL, signalAuth.TenantID, signalAuth.APIToken, signalAuth.HTTPClient)
			rules, _, err := client.ListRules(ctx)
			if err != nil {
				return err
			}

			if len(rules) == 0 {
				fmt.Fprintln(cmd.ErrOrStderr(), "No rules found.")
				return nil
			}

			return opts.Encode(cmd.OutOrStdout(), rules)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

type rulesSyncOpts struct {
	cmdio.Options

	File                string
	FromRecommendations bool
	DryRun              bool
}

func (o *rulesSyncOpts) setup(flags *pflag.FlagSet) {
	o.DefaultFormat("table")
	o.RegisterCustomCodec("table", newRulesTableCodec(false))
	o.RegisterCustomCodec("wide", newRulesTableCodec(true))
	o.BindFlags(flags)
	flags.StringVarP(&o.File, "file", "f", "", "File containing rules to sync (JSON or YAML)")
	flags.BoolVar(&o.FromRecommendations, "from-recommendations", false, "Sync rules from current recommendations")
	flags.BoolVar(&o.DryRun, "dry-run", false, "Print what would be synced without making changes")
}

func (o *rulesSyncOpts) Validate() error {
	if err := o.Options.Validate(); err != nil {
		return err
	}
	if o.File == "" && !o.FromRecommendations {
		return errors.New("one of --file or --from-recommendations is required")
	}
	if o.File != "" && o.FromRecommendations {
		return errors.New("--file and --from-recommendations are mutually exclusive")
	}
	return nil
}

func (h *metricsHelper) rulesSyncCommand() *cobra.Command {
	opts := &rulesSyncOpts{}
	cmd := &cobra.Command{
		Use:   "sync",
		Short: "Sync aggregation rules from a file or recommendations.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}

			ctx := cmd.Context()
			signalAuth, err := auth.ResolveSignalAuth(ctx, h.loader, "metrics")
			if err != nil {
				return err
			}

			client := NewClient(signalAuth.BaseURL, signalAuth.TenantID, signalAuth.APIToken, signalAuth.HTTPClient)

			// Fetch current ETag BEFORE computing new rules to avoid TOCTOU race.
			// The ETag represents the baseline state we're replacing.
			_, etag, err := client.ListRules(ctx)
			if err != nil {
				return fmt.Errorf("failed to fetch current rules for ETag: %w", err)
			}

			var rules []MetricRule
			if opts.File != "" {
				rules, err = readRulesFromFile(opts.File)
				if err != nil {
					return err
				}
			} else {
				recs, err := client.ListRecommendations(ctx)
				if err != nil {
					return err
				}
				rules = recommendationsToRules(recs)
			}

			if opts.DryRun {
				cmdio.Info(cmd.OutOrStdout(), "Dry run — would sync %d rule(s):", len(rules))
				return opts.Encode(cmd.OutOrStdout(), rules)
			}

			if err := client.SyncRules(ctx, rules, etag); err != nil {
				return err
			}

			cmdio.Success(cmd.OutOrStdout(), "Synced %d rule(s).", len(rules))
			return nil
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// ---------------------------------------------------------------------------
// Recommendations commands
// ---------------------------------------------------------------------------

func (h *metricsHelper) recommendationsCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "recommendations",
		Short: "Manage metric recommendations.",
	}

	cmd.AddCommand(h.recommendationsShowCommand(), h.recommendationsApplyCommand())

	return cmd
}

type recommendationsShowOpts struct {
	cmdio.Options
}

func (o *recommendationsShowOpts) setup(flags *pflag.FlagSet) {
	o.DefaultFormat("table")
	o.RegisterCustomCodec("table", newRecommendationsTableCodec(false))
	o.RegisterCustomCodec("wide", newRecommendationsTableCodec(true))
	o.BindFlags(flags)
}

func (h *metricsHelper) recommendationsShowCommand() *cobra.Command {
	opts := &recommendationsShowOpts{}
	cmd := &cobra.Command{
		Use:   "show",
		Short: "Show current metric recommendations.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}

			ctx := cmd.Context()
			signalAuth, err := auth.ResolveSignalAuth(ctx, h.loader, "metrics")
			if err != nil {
				return err
			}

			client := NewClient(signalAuth.BaseURL, signalAuth.TenantID, signalAuth.APIToken, signalAuth.HTTPClient)
			recs, err := client.ListRecommendations(ctx)
			if err != nil {
				return err
			}

			if len(recs) == 0 {
				fmt.Fprintln(cmd.ErrOrStderr(), "No recommendations found.")
				return nil
			}

			return opts.Encode(cmd.OutOrStdout(), recs)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

type recommendationsApplyOpts struct {
	DryRun bool
}

func (o *recommendationsApplyOpts) setup(flags *pflag.FlagSet) {
	flags.BoolVar(&o.DryRun, "dry-run", false, "Print what would be synced without making changes")
}

func (o *recommendationsApplyOpts) Validate() error {
	return nil
}

func (h *metricsHelper) recommendationsApplyCommand() *cobra.Command {
	opts := &recommendationsApplyOpts{}
	cmd := &cobra.Command{
		Use:   "apply",
		Short: "Apply recommendations as aggregation rules.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}

			ctx := cmd.Context()
			signalAuth, err := auth.ResolveSignalAuth(ctx, h.loader, "metrics")
			if err != nil {
				return err
			}

			client := NewClient(signalAuth.BaseURL, signalAuth.TenantID, signalAuth.APIToken, signalAuth.HTTPClient)

			// Fetch current ETag BEFORE computing new rules to avoid TOCTOU race.
			_, etag, err := client.ListRules(ctx)
			if err != nil {
				return fmt.Errorf("failed to fetch current rules for ETag: %w", err)
			}

			recs, err := client.ListRecommendations(ctx)
			if err != nil {
				return err
			}

			rules := recommendationsToRules(recs)

			if opts.DryRun {
				cmdio.Info(cmd.OutOrStdout(), "Dry run — would apply %d recommendation(s) as rules.", len(rules))
				return nil
			}

			if err := client.SyncRules(ctx, rules, etag); err != nil {
				return err
			}

			cmdio.Success(cmd.OutOrStdout(), "Applied %d recommendation(s) as rules.", len(rules))
			return nil
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// ---------------------------------------------------------------------------
// Table codecs
// ---------------------------------------------------------------------------

type rulesTableCodec struct {
	wide bool
}

func newRulesTableCodec(wide bool) *rulesTableCodec {
	return &rulesTableCodec{wide: wide}
}

func (c *rulesTableCodec) Format() format.Format {
	if c.wide {
		return "wide"
	}
	return "table"
}

func (c *rulesTableCodec) Encode(w io.Writer, v any) error {
	rules, ok := v.([]MetricRule)
	if !ok {
		return fmt.Errorf("metrics: rules table codec: expected []MetricRule, got %T", v)
	}

	t := style.NewTable("METRIC", "DROP LABELS", "AGGREGATIONS")
	for _, r := range rules {
		t.Row(r.MetricName, strings.Join(r.DropLabels, ","), strings.Join(r.Aggregations, ","))
	}
	return t.Render(w)
}

func (c *rulesTableCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("table format does not support decoding")
}

type recommendationsTableCodec struct {
	wide bool
}

func newRecommendationsTableCodec(wide bool) *recommendationsTableCodec {
	return &recommendationsTableCodec{wide: wide}
}

func (c *recommendationsTableCodec) Format() format.Format {
	if c.wide {
		return "wide"
	}
	return "table"
}

func (c *recommendationsTableCodec) Encode(w io.Writer, v any) error {
	recs, ok := v.([]MetricRecommendation)
	if !ok {
		return fmt.Errorf("metrics: recommendations table codec: expected []MetricRecommendation, got %T", v)
	}

	t := style.NewTable("METRIC", "DROP LABELS", "AGGREGATIONS")
	for _, r := range recs {
		t.Row(r.MetricName, strings.Join(r.DropLabels, ","), strings.Join(r.Aggregations, ","))
	}
	return t.Render(w)
}

func (c *recommendationsTableCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("table format does not support decoding")
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// readRulesFromFile reads MetricRules from a JSON or YAML file.
func readRulesFromFile(filename string) ([]MetricRule, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("metrics: read rules file: %w", err)
	}

	var codec interface {
		Decode(src io.Reader, value any) error
	}

	switch strings.ToLower(filepath.Ext(filename)) {
	case ".json":
		codec = format.NewJSONCodec()
	default:
		codec = format.NewYAMLCodec()
	}

	var rules []MetricRule
	if err := codec.Decode(bytes.NewReader(data), &rules); err != nil {
		return nil, fmt.Errorf("metrics: parse rules file: %w", err)
	}

	return rules, nil
}

// recommendationsToRules converts MetricRecommendations to MetricRules.
func recommendationsToRules(recs []MetricRecommendation) []MetricRule {
	rules := make([]MetricRule, len(recs))
	for i, r := range recs {
		rules[i] = MetricRule(r)
	}
	return rules
}
