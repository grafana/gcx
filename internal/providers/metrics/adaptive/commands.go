package metrics

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"sort"
	"strings"
	"text/tabwriter"

	auth "github.com/grafana/gcx/internal/auth/adaptive"
	"github.com/grafana/gcx/internal/format"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/resources/adapter"
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
	cmd.AddCommand(
		h.recommendationsCommand(),
		h.rulesCommand(),
	)

	return cmd
}

// ---------------------------------------------------------------------------
// recommendations
// ---------------------------------------------------------------------------

func (h *metricsHelper) recommendationsCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "recommendations",
		Short: "Manage metric recommendations.",
	}
	cmd.AddCommand(
		h.recommendationsListCommand(),
		h.recommendationsDiffCommand(),
		h.recommendationsApplyCommand(),
	)
	return cmd
}

// recommendations list

type recommendationsListOpts struct {
	cmdio.Options

	Actions []string
	Segment string
	Sort    string
	Top     int
	Reverse bool
}

func (o *recommendationsListOpts) setup(flags *pflag.FlagSet) {
	o.DefaultFormat("table")
	o.RegisterCustomCodec("table", &recommendationsTableCodec{wide: false})
	o.RegisterCustomCodec("wide", &recommendationsTableCodec{wide: true})
	o.BindFlags(flags)
	flags.StringArrayVar(&o.Actions, "action", nil, "Filter by action: add, update, remove, keep (repeatable)")
	flags.StringVar(&o.Segment, "segment", "", "Segment ID")
	flags.StringVar(&o.Sort, "sort", "metric", "Sort by: metric, savings, series-before, series-after, action")
	flags.IntVar(&o.Top, "top", 0, "Limit to top N results (0 = all)")
	flags.BoolVar(&o.Reverse, "reverse", false, "Reverse the default sort order")
}

func (h *metricsHelper) recommendationsListCommand() *cobra.Command {
	opts := &recommendationsListOpts{}
	cmd := &cobra.Command{
		Use:   "show",
		Short: "Show metric recommendations.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}

			if opts.Top < 0 {
				return errors.New("--top must be 0 or greater")
			}

			sortFields := []string{"metric", "savings", "series-before", "series-after", "action"}
			if !slices.Contains(sortFields, opts.Sort) {
				return fmt.Errorf("invalid sort field %q. Valid values are: %s", opts.Sort, strings.Join(sortFields, ", "))
			}

			ctx := cmd.Context()
			signalAuth, err := auth.ResolveSignalAuth(ctx, h.loader, "metrics")
			if err != nil {
				return err
			}
			client := NewClient(ctx, signalAuth.BaseURL, signalAuth.TenantID, signalAuth.APIToken, signalAuth.HTTPClient)

			recs, err := client.ListRecommendations(ctx, opts.Segment, opts.Actions)
			if err != nil {
				return err
			}

			sortRecommendations(recs, opts.Sort, opts.Reverse)

			total := len(recs)
			if opts.Top > 0 && opts.Top < total {
				recs = recs[:opts.Top]
				fmt.Fprintf(cmd.ErrOrStderr(), "%d of %d recommendation(s)\n", opts.Top, total)
			} else {
				fmt.Fprintf(cmd.ErrOrStderr(), "%d recommendation(s)\n", total)
			}

			if len(recs) == 0 {
				return nil
			}

			return opts.Encode(cmd.OutOrStdout(), recs)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// recommendations diff

type recommendationsDiffOpts struct {
	cmdio.Options

	Segment string
}

func (o *recommendationsDiffOpts) setup(flags *pflag.FlagSet) {
	o.DefaultFormat("table")
	o.RegisterCustomCodec("table", &recommendationsDiffTableCodec{})
	o.BindFlags(flags)
	flags.StringVar(&o.Segment, "segment", "", "Segment ID")
}

// diffEntry holds current vs recommended state for one metric.
type diffEntry struct {
	Metric            string      `json:"metric" yaml:"metric"`
	Action            string      `json:"action" yaml:"action"`
	CurrentRule       *MetricRule `json:"current_rule,omitempty" yaml:"current_rule,omitempty"`
	RecommendedRule   *MetricRule `json:"recommended_rule,omitempty" yaml:"recommended_rule,omitempty"`
	CurrentSeries     int         `json:"current_series" yaml:"current_series"`
	RecommendedSeries int         `json:"recommended_series" yaml:"recommended_series"`
}

func (h *metricsHelper) recommendationsDiffCommand() *cobra.Command {
	opts := &recommendationsDiffOpts{}
	cmd := &cobra.Command{
		Use:   "diff <metric>...",
		Short: "Show what applying recommendation(s) would change.",
		Args: func(_ *cobra.Command, args []string) error {
			if len(args) == 0 {
				return errors.New("at least one metric name is required")
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}

			ctx := cmd.Context()
			signalAuth, err := auth.ResolveSignalAuth(ctx, h.loader, "metrics")
			if err != nil {
				return err
			}
			client := NewClient(ctx, signalAuth.BaseURL, signalAuth.TenantID, signalAuth.APIToken, signalAuth.HTTPClient)

			allRecs, err := client.ListRecommendations(ctx, opts.Segment, nil)
			if err != nil {
				return err
			}
			recsByMetric := make(map[string]MetricRecommendation, len(allRecs))
			for _, r := range allRecs {
				recsByMetric[r.Metric] = r
			}

			var entries []diffEntry
			for _, metric := range args {
				rec, ok := recsByMetric[metric]
				if !ok {
					return fmt.Errorf("no recommendation found for %s. Use 'recommendations show' to see available recommendations", metric)
				}

				entry := diffEntry{
					Metric:            metric,
					Action:            rec.RecommendedAction,
					CurrentSeries:     rec.CurrentSeriesCount,
					RecommendedSeries: rec.RecommendedSeriesCount,
				}

				if rec.RecommendedAction != "add" {
					rule, err := client.GetRule(ctx, metric, opts.Segment)
					if err != nil && !errors.Is(err, ErrRuleNotFound) {
						return fmt.Errorf("get rule for %s: %w", metric, err)
					}
					if err == nil {
						entry.CurrentRule = &rule
					}
				}

				recommended := rec.ToRule()
				if rec.RecommendedAction != "remove" {
					entry.RecommendedRule = &recommended
				}

				entries = append(entries, entry)
			}

			return opts.Encode(cmd.OutOrStdout(), entries)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// recommendations apply

type recommendationsApplyOpts struct {
	All     bool
	DryRun  bool
	Yes     bool
	Segment string
}

func (o *recommendationsApplyOpts) setup(flags *pflag.FlagSet) {
	flags.BoolVar(&o.All, "all", false, "Apply all recommendations (bulk)")
	flags.BoolVar(&o.DryRun, "dry-run", false, "Preview without applying")
	flags.BoolVar(&o.Yes, "yes", false, "Skip confirmation prompt")
	flags.StringVar(&o.Segment, "segment", "", "Segment ID")
}

func (o *recommendationsApplyOpts) Validate() error {
	return nil
}

func (h *metricsHelper) recommendationsApplyCommand() *cobra.Command {
	opts := &recommendationsApplyOpts{}
	cmd := &cobra.Command{
		Use:   "apply [<metric>...|--all]",
		Short: "Apply specific or all recommendations as rules.",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}
			if !opts.All && len(args) == 0 {
				return errors.New("specify one or more metric names, or use --all to apply all recommendations")
			}
			if opts.All && len(args) > 0 {
				return errors.New("--all and metric names are mutually exclusive")
			}

			ctx := cmd.Context()
			signalAuth, err := auth.ResolveSignalAuth(ctx, h.loader, "metrics")
			if err != nil {
				return err
			}
			client := NewClient(ctx, signalAuth.BaseURL, signalAuth.TenantID, signalAuth.APIToken, signalAuth.HTTPClient)

			if opts.All {
				return applyAllRecommendations(cmd, client, opts)
			}
			return applySelectiveRecommendations(cmd, client, opts, args)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

func applyAllRecommendations(cmd *cobra.Command, client *Client, opts *recommendationsApplyOpts) error {
	ctx := cmd.Context()

	rules, rulesVersion, err := client.ListRecommendedRules(ctx, opts.Segment)
	if err != nil {
		return err
	}

	_, currentEtag, err := client.ListRules(ctx, opts.Segment)
	if err != nil {
		return fmt.Errorf("fetch current rules for ETag: %w", err)
	}

	recs, err := client.ListRecommendations(ctx, opts.Segment, nil)
	if err != nil {
		return fmt.Errorf("fetch recommendations for summary: %w", err)
	}
	counts := map[string]int{"add": 0, "update": 0, "remove": 0, "keep": 0}
	for _, r := range recs {
		counts[r.RecommendedAction]++
	}

	stderr := cmd.ErrOrStderr()

	if opts.DryRun {
		cmdio.Info(stderr, "Dry run — would apply all recommendations (%d rules): %d add, %d update, %d remove, %d keep.",
			len(rules), counts["add"], counts["update"], counts["remove"], counts["keep"])
		return nil
	}

	if !opts.Yes {
		fmt.Fprintf(stderr, "Apply all recommendations (%d rules)? [y/N] ", len(rules))
		reader := bufio.NewReader(cmd.InOrStdin())
		answer, err := reader.ReadString('\n')
		if err != nil {
			return fmt.Errorf("read confirmation: %w", err)
		}
		answer = strings.TrimSpace(strings.ToLower(answer))
		if answer != "y" && answer != "yes" {
			cmdio.Info(stderr, "Aborted.")
			return nil
		}
	}

	// Use rulesVersion if available, otherwise fall back to currentEtag.
	etag := rulesVersion
	if etag == "" {
		etag = currentEtag
	}

	if err := client.SyncRules(ctx, rules, etag, opts.Segment); err != nil {
		return err
	}

	cmdio.Success(stderr, "Applied all recommendations (%d rules): %d add, %d update, %d remove, %d keep.",
		len(rules), counts["add"], counts["update"], counts["remove"], counts["keep"])
	return nil
}

type applyItem struct {
	rec    MetricRecommendation
	action string
}

func applySelectiveRecommendations(cmd *cobra.Command, client *Client, opts *recommendationsApplyOpts, metrics []string) error {
	ctx := cmd.Context()

	allRecs, err := client.ListRecommendations(ctx, opts.Segment, nil)
	if err != nil {
		return err
	}
	recsByMetric := make(map[string]MetricRecommendation, len(allRecs))
	for _, r := range allRecs {
		recsByMetric[r.Metric] = r
	}

	var items []applyItem
	for _, m := range metrics {
		rec, ok := recsByMetric[m]
		if !ok {
			return fmt.Errorf("no recommendation found for %s. Use 'recommendations show' to see available recommendations", m)
		}
		items = append(items, applyItem{rec: rec, action: rec.RecommendedAction})
	}

	// Validate add/update rules before any mutations.
	var toValidate []MetricRule
	for _, item := range items {
		if item.action == "add" || item.action == "update" {
			toValidate = append(toValidate, item.rec.ToRule())
		}
	}
	if len(toValidate) > 0 {
		errs, err := client.ValidateRules(ctx, toValidate, opts.Segment)
		if err != nil {
			return fmt.Errorf("validate rules: %w", err)
		}
		if len(errs) > 0 {
			return formatValidationErrors(errs)
		}
	}

	// Count non-keep actions.
	actionCount := 0
	for _, item := range items {
		if item.action != "keep" {
			actionCount++
		}
	}

	stderr := cmd.ErrOrStderr()

	if opts.DryRun {
		for _, item := range items {
			if item.action == "keep" {
				fmt.Fprintf(stderr, "  %s: keep (no change)\n", item.rec.Metric)
			} else {
				fmt.Fprintf(stderr, "  %s: %s (%d → %d series)\n",
					item.rec.Metric, item.action,
					item.rec.CurrentSeriesCount, item.rec.RecommendedSeriesCount)
			}
		}
		return nil
	}

	if !opts.Yes && actionCount > 0 {
		fmt.Fprintf(stderr, "Apply %d recommendation(s)? [y/N] ", actionCount)
		reader := bufio.NewReader(cmd.InOrStdin())
		answer, err := reader.ReadString('\n')
		if err != nil {
			return fmt.Errorf("read confirmation: %w", err)
		}
		answer = strings.TrimSpace(strings.ToLower(answer))
		if answer != "y" && answer != "yes" {
			cmdio.Info(stderr, "Aborted.")
			return nil
		}
	}

	// Fetch global rules ETag — the API requires If-Match on all mutations.
	_, rulesEtag, err := client.ListRules(ctx, opts.Segment)
	if err != nil {
		return fmt.Errorf("fetch rules ETag: %w", err)
	}

	var failed []string
	for _, item := range items {
		if !applyOneItem(ctx, cmd, client, item, opts.Segment, &rulesEtag) {
			failed = append(failed, item.rec.Metric)
		}
	}

	if len(failed) > 0 {
		return fmt.Errorf("failed to apply %d of %d recommendation(s): %s",
			len(failed), len(items), strings.Join(failed, ", "))
	}
	return nil
}

func applyOneItem(ctx context.Context, cmd *cobra.Command, client *Client, item applyItem, segment string, rulesEtag *string) bool {
	stderr := cmd.ErrOrStderr()
	switch item.action {
	case "keep":
		cmdio.Info(stderr, "Skipping %s: keep (no change).", item.rec.Metric)

	case "add":
		newEtag, err := client.CreateRule(ctx, item.rec.ToRule(), *rulesEtag, segment)
		if err != nil {
			cmdio.Error(stderr, "Failed to create rule for %s: %v", item.rec.Metric, err)
			return false
		}
		cmdio.Success(stderr, "Created rule for %s.", item.rec.Metric)
		*rulesEtag = newEtag

	case "update":
		newEtag, err := client.UpdateRule(ctx, item.rec.ToRule(), *rulesEtag, segment)
		if err != nil {
			cmdio.Error(stderr, "Failed to update rule for %s: %v", item.rec.Metric, err)
			return false
		}
		cmdio.Success(stderr, "Updated rule for %s.", item.rec.Metric)
		*rulesEtag = newEtag

	case "remove":
		if err := client.DeleteRule(ctx, item.rec.Metric, *rulesEtag, segment); err != nil {
			cmdio.Error(stderr, "Failed to delete rule for %s: %v", item.rec.Metric, err)
			return false
		}
		cmdio.Success(stderr, "Deleted rule for %s.", item.rec.Metric)
		// Refetch ETag after delete since delete doesn't return one.
		_, newEtag, err := client.ListRules(ctx, segment)
		if err != nil {
			cmdio.Error(stderr, "Failed to refresh ETag after delete: %v. Subsequent mutations may fail.", err)
			return false
		}
		*rulesEtag = newEtag
	}
	return true
}

// ---------------------------------------------------------------------------
// rules
// ---------------------------------------------------------------------------

func (h *metricsHelper) rulesCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "rules",
		Short: "Manage aggregation rules.",
	}
	cmd.AddCommand(
		h.rulesListCommand(),
		h.rulesGetCommand(),
		h.rulesCreateCommand(),
		h.rulesUpdateCommand(),
		h.rulesDeleteCommand(),
	)
	return cmd
}

// rules list

type rulesListOpts struct {
	cmdio.Options

	Segment string
	Limit   int64
}

func (o *rulesListOpts) setup(flags *pflag.FlagSet) {
	o.DefaultFormat("table")
	o.RegisterCustomCodec("table", &rulesTableCodec{wide: false})
	o.RegisterCustomCodec("wide", &rulesTableCodec{wide: true})
	o.BindFlags(flags)
	flags.StringVar(&o.Segment, "segment", "", "Segment ID")
	flags.Int64Var(&o.Limit, "limit", 50, "Maximum number of rules to return (0 for no limit)")
}

func (h *metricsHelper) rulesListCommand() *cobra.Command {
	opts := &rulesListOpts{}
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List aggregation rules.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}

			ctx := cmd.Context()
			crud, err := NewRuleTypedCRUD(ctx, h.loader, opts.Segment)
			if err != nil {
				return err
			}

			typedObjs, err := crud.List(ctx, opts.Limit)
			if err != nil {
				return err
			}
			rules := make([]MetricRule, len(typedObjs))
			for i := range typedObjs {
				rules[i] = typedObjs[i].Spec
			}

			fmt.Fprintf(cmd.ErrOrStderr(), "%d rule(s)\n", len(rules))
			if len(rules) == 0 {
				return nil
			}

			return opts.Encode(cmd.OutOrStdout(), rules)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// rules get

type rulesGetOpts struct {
	cmdio.Options

	Segment string
}

func (o *rulesGetOpts) setup(flags *pflag.FlagSet) {
	o.DefaultFormat("json")
	o.BindFlags(flags)
	flags.StringVar(&o.Segment, "segment", "", "Segment ID")
}

func (h *metricsHelper) rulesGetCommand() *cobra.Command {
	opts := &rulesGetOpts{}
	cmd := &cobra.Command{
		Use:   "get <metric>",
		Short: "Get an aggregation rule by metric name.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}

			ctx := cmd.Context()
			crud, err := NewRuleTypedCRUD(ctx, h.loader, opts.Segment)
			if err != nil {
				return err
			}

			obj, err := crud.Get(ctx, args[0])
			if err != nil {
				return err
			}

			return opts.Encode(cmd.OutOrStdout(), obj.Spec)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// rules create

type rulesCreateOpts struct {
	cmdio.Options

	Metric              string
	MatchType           string
	DropLabels          []string
	KeepLabels          []string
	Aggregations        []string
	Drop                bool
	AggregationInterval string
	AggregationDelay    string
	Segment             string
}

func (o *rulesCreateOpts) setup(cmd *cobra.Command) {
	cmd.Flags().StringVar(&o.Metric, "metric", "", "Metric name (required)")
	cmd.Flags().StringVar(&o.MatchType, "match-type", "exact", "Match type: exact, prefix, or suffix")
	cmd.Flags().StringSliceVar(&o.DropLabels, "drop-labels", nil, "Labels to drop (comma-separated)")
	cmd.Flags().StringSliceVar(&o.KeepLabels, "keep-labels", nil, "Labels to keep (comma-separated)")
	cmd.Flags().StringSliceVar(&o.Aggregations, "aggregations", nil, "Aggregation types: sum, count, min, max, sum:counter (comma-separated)")
	cmd.Flags().BoolVar(&o.Drop, "drop", false, "Drop the metric entirely")
	cmd.Flags().StringVar(&o.AggregationInterval, "aggregation-interval", "", "Aggregation interval (e.g. 1m)")
	cmd.Flags().StringVar(&o.AggregationDelay, "aggregation-delay", "", "Aggregation delay (e.g. 5m)")
	cmd.Flags().StringVar(&o.Segment, "segment", "", "Segment ID")
	_ = cmd.MarkFlagRequired("metric")
	o.DefaultFormat("json")
	o.BindFlags(cmd.Flags())
}

func (h *metricsHelper) rulesCreateCommand() *cobra.Command {
	opts := &rulesCreateOpts{}
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create an aggregation rule.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}

			ctx := cmd.Context()
			crud, err := NewRuleTypedCRUD(ctx, h.loader, opts.Segment)
			if err != nil {
				return err
			}

			rule := MetricRule{
				Metric:              opts.Metric,
				MatchType:           opts.MatchType,
				DropLabels:          opts.DropLabels,
				KeepLabels:          opts.KeepLabels,
				Aggregations:        opts.Aggregations,
				Drop:                opts.Drop,
				AggregationInterval: opts.AggregationInterval,
				AggregationDelay:    opts.AggregationDelay,
			}

			created, err := crud.Create(ctx, &adapter.TypedObject[MetricRule]{Spec: rule})
			if err != nil {
				return err
			}

			cmdio.Success(cmd.ErrOrStderr(), "Created rule for %s.", opts.Metric)
			return opts.Encode(cmd.OutOrStdout(), created.Spec)
		},
	}
	opts.setup(cmd)
	return cmd
}

// rules update

type rulesUpdateOpts struct {
	cmdio.Options

	MatchType           string
	DropLabels          []string
	KeepLabels          []string
	Aggregations        []string
	Drop                bool
	AggregationInterval string
	AggregationDelay    string
	Segment             string
}

func (o *rulesUpdateOpts) setup(cmd *cobra.Command) {
	cmd.Flags().StringVar(&o.MatchType, "match-type", "", "Match type: exact, prefix, or suffix")
	cmd.Flags().StringSliceVar(&o.DropLabels, "drop-labels", nil, "Labels to drop (comma-separated)")
	cmd.Flags().StringSliceVar(&o.KeepLabels, "keep-labels", nil, "Labels to keep (comma-separated)")
	cmd.Flags().StringSliceVar(&o.Aggregations, "aggregations", nil, "Aggregation types: sum, count, min, max, sum:counter (comma-separated)")
	cmd.Flags().BoolVar(&o.Drop, "drop", false, "Drop the metric entirely")
	cmd.Flags().StringVar(&o.AggregationInterval, "aggregation-interval", "", "Aggregation interval (e.g. 1m)")
	cmd.Flags().StringVar(&o.AggregationDelay, "aggregation-delay", "", "Aggregation delay (e.g. 5m)")
	cmd.Flags().StringVar(&o.Segment, "segment", "", "Segment ID")
	o.DefaultFormat("json")
	o.BindFlags(cmd.Flags())
}

func (h *metricsHelper) rulesUpdateCommand() *cobra.Command {
	opts := &rulesUpdateOpts{}
	cmd := &cobra.Command{
		Use:   "update <metric>",
		Short: "Update an aggregation rule.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}

			flags := cmd.Flags()
			if !flags.Changed("match-type") && !flags.Changed("drop-labels") && !flags.Changed("keep-labels") &&
				!flags.Changed("aggregations") && !flags.Changed("drop") &&
				!flags.Changed("aggregation-interval") && !flags.Changed("aggregation-delay") {
				return errors.New("specify at least one flag to update")
			}

			ctx := cmd.Context()
			crud, err := NewRuleTypedCRUD(ctx, h.loader, opts.Segment)
			if err != nil {
				return err
			}

			existing, err := crud.Get(ctx, args[0])
			if err != nil {
				return fmt.Errorf("failed to fetch existing rule for merge: %w", err)
			}

			if flags.Changed("match-type") {
				existing.Spec.MatchType = opts.MatchType
			}
			if flags.Changed("drop-labels") {
				existing.Spec.DropLabels = opts.DropLabels
			}
			if flags.Changed("keep-labels") {
				existing.Spec.KeepLabels = opts.KeepLabels
			}
			if flags.Changed("aggregations") {
				existing.Spec.Aggregations = opts.Aggregations
			}
			if flags.Changed("drop") {
				existing.Spec.Drop = opts.Drop
			}
			if flags.Changed("aggregation-interval") {
				existing.Spec.AggregationInterval = opts.AggregationInterval
			}
			if flags.Changed("aggregation-delay") {
				existing.Spec.AggregationDelay = opts.AggregationDelay
			}

			updated, err := crud.Update(ctx, args[0], existing)
			if err != nil {
				return err
			}

			cmdio.Success(cmd.ErrOrStderr(), "Updated rule for %s.", args[0])
			return opts.Encode(cmd.OutOrStdout(), updated.Spec)
		},
	}
	opts.setup(cmd)
	return cmd
}

// rules delete

type rulesDeleteOpts struct {
	Segment string
	Yes     bool
}

func (o *rulesDeleteOpts) setup(flags *pflag.FlagSet) {
	flags.StringVar(&o.Segment, "segment", "", "Segment ID")
	flags.BoolVar(&o.Yes, "yes", false, "Skip confirmation prompt")
}

func (h *metricsHelper) rulesDeleteCommand() *cobra.Command {
	opts := &rulesDeleteOpts{}
	cmd := &cobra.Command{
		Use:   "delete <metric>",
		Short: "Delete an aggregation rule.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			metric := args[0]
			stderr := cmd.ErrOrStderr()

			if !opts.Yes {
				fmt.Fprintf(stderr, "Delete rule for %s? [y/N] ", metric)
				reader := bufio.NewReader(cmd.InOrStdin())
				answer, err := reader.ReadString('\n')
				if err != nil {
					return fmt.Errorf("read confirmation: %w", err)
				}
				answer = strings.TrimSpace(strings.ToLower(answer))
				if answer != "y" && answer != "yes" {
					cmdio.Info(stderr, "Aborted.")
					return nil
				}
			}

			ctx := cmd.Context()
			crud, err := NewRuleTypedCRUD(ctx, h.loader, opts.Segment)
			if err != nil {
				return err
			}

			if err := crud.Delete(ctx, metric); err != nil {
				return err
			}

			cmdio.Success(stderr, "Deleted rule for %s.", metric)
			return nil
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// ---------------------------------------------------------------------------
// Rules table codecs
// ---------------------------------------------------------------------------

type rulesTableCodec struct {
	wide bool
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

	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	if c.wide {
		fmt.Fprintln(tw, "METRIC\tMATCH TYPE\tDROP LABELS\tKEEP LABELS\tAGGREGATIONS\tDROP\tINTERVAL\tDELAY\tMANAGED BY")
	} else {
		fmt.Fprintln(tw, "METRIC\tMATCH TYPE\tDROP LABELS\tKEEP LABELS\tAGGREGATIONS")
	}
	for _, r := range rules {
		if c.wide {
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%v\t%s\t%s\t%s\n",
				r.Metric,
				defaultStr(r.MatchType, "exact"),
				strings.Join(r.DropLabels, ","),
				strings.Join(r.KeepLabels, ","),
				strings.Join(r.Aggregations, ","),
				r.Drop,
				r.AggregationInterval,
				r.AggregationDelay,
				r.ManagedBy,
			)
		} else {
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n",
				r.Metric,
				defaultStr(r.MatchType, "exact"),
				strings.Join(r.DropLabels, ","),
				strings.Join(r.KeepLabels, ","),
				strings.Join(r.Aggregations, ","),
			)
		}
	}
	return tw.Flush()
}

func (c *rulesTableCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("table format does not support decoding")
}

func defaultStr(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func formatValidationErrors(errs []string) error {
	var sb strings.Builder
	sb.WriteString("Rule validation failed:")
	for _, e := range errs {
		sb.WriteString("\n  - ")
		sb.WriteString(e)
	}
	return errors.New(sb.String())
}

// ---------------------------------------------------------------------------
// Table codecs
// ---------------------------------------------------------------------------

type recommendationsTableCodec struct {
	wide bool
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

	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	if c.wide {
		fmt.Fprintln(tw, "METRIC\tACTION\tCURRENT SERIES\tRECOMMENDED SERIES\tSAVINGS%\tDROP LABELS\tKEEP LABELS\tAGGREGATIONS\tRULES\tQUERIES\tDASHBOARDS")
	} else {
		fmt.Fprintln(tw, "METRIC\tACTION\tCURRENT SERIES\tRECOMMENDED SERIES\tSAVINGS%")
	}
	for _, r := range recs {
		savings := savings(r.CurrentSeriesCount, r.RecommendedSeriesCount)
		if c.wide {
			fmt.Fprintf(tw, "%s\t%s\t%d\t%d\t%.1f%%\t%s\t%s\t%s\t%d\t%d\t%d\n",
				r.Metric,
				r.RecommendedAction,
				r.CurrentSeriesCount,
				r.RecommendedSeriesCount,
				savings,
				strings.Join(r.DropLabels, ","),
				strings.Join(r.KeptLabels, ","),
				strings.Join(r.Aggregations, ","),
				r.UsagesInRules,
				r.UsagesInQueries,
				r.UsagesInDashboards,
			)
		} else {
			fmt.Fprintf(tw, "%s\t%s\t%d\t%d\t%.1f%%\n",
				r.Metric,
				r.RecommendedAction,
				r.CurrentSeriesCount,
				r.RecommendedSeriesCount,
				savings,
			)
		}
	}
	return tw.Flush()
}

func (c *recommendationsTableCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("table format does not support decoding")
}

type recommendationsDiffTableCodec struct{}

func (c *recommendationsDiffTableCodec) Format() format.Format { return "table" }

func (c *recommendationsDiffTableCodec) Encode(w io.Writer, v any) error {
	entries, ok := v.([]diffEntry)
	if !ok {
		return fmt.Errorf("metrics: diff table codec: expected []diffEntry, got %T", v)
	}

	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	for _, e := range entries {
		fmt.Fprintf(tw, "--- %s (current)\n", e.Metric)
		fmt.Fprintf(tw, "+++ %s (recommended action: %s)\n", e.Metric, e.Action)
		fmt.Fprintf(tw, "Series:\t%d → %d\n", e.CurrentSeries, e.RecommendedSeries)

		if e.CurrentRule != nil {
			fmt.Fprintf(tw, "Current rule:\n")
			fmt.Fprintf(tw, "  drop_labels:\t%s\n", strings.Join(e.CurrentRule.DropLabels, ","))
			fmt.Fprintf(tw, "  keep_labels:\t%s\n", strings.Join(e.CurrentRule.KeepLabels, ","))
			fmt.Fprintf(tw, "  aggregations:\t%s\n", strings.Join(e.CurrentRule.Aggregations, ","))
			fmt.Fprintf(tw, "  drop:\t%v\n", e.CurrentRule.Drop)
		} else {
			fmt.Fprintf(tw, "Current rule:\t(none)\n")
		}

		if e.RecommendedRule != nil {
			fmt.Fprintf(tw, "Recommended rule:\n")
			fmt.Fprintf(tw, "  drop_labels:\t%s\n", strings.Join(e.RecommendedRule.DropLabels, ","))
			fmt.Fprintf(tw, "  keep_labels:\t%s\n", strings.Join(e.RecommendedRule.KeepLabels, ","))
			fmt.Fprintf(tw, "  aggregations:\t%s\n", strings.Join(e.RecommendedRule.Aggregations, ","))
			fmt.Fprintf(tw, "  drop:\t%v\n", e.RecommendedRule.Drop)
		} else {
			fmt.Fprintf(tw, "Recommended rule:\t(remove)\n")
		}
		fmt.Fprintln(tw)
	}
	return tw.Flush()
}

func (c *recommendationsDiffTableCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("table format does not support decoding")
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func savings(current, recommended int) float64 {
	if current == 0 {
		return 0.0
	}
	return float64(current-recommended) / float64(current) * 100
}

func sortRecommendations(recs []MetricRecommendation, by string, reverse bool) {
	sort.SliceStable(recs, func(i, j int) bool {
		var less bool
		switch by {
		case "savings":
			si := savings(recs[i].CurrentSeriesCount, recs[i].RecommendedSeriesCount)
			sj := savings(recs[j].CurrentSeriesCount, recs[j].RecommendedSeriesCount)
			less = si > sj // default: highest savings first
		case "series-before":
			less = recs[i].CurrentSeriesCount > recs[j].CurrentSeriesCount // default: highest first
		case "series-after":
			less = recs[i].RecommendedSeriesCount > recs[j].RecommendedSeriesCount // default: highest first
		case "action":
			less = recs[i].RecommendedAction < recs[j].RecommendedAction // default: A-Z
		default: // "metric"
			less = recs[i].Metric < recs[j].Metric // default: A-Z
		}
		if reverse {
			return !less
		}
		return less
	})
}
