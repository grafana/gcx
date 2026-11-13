package metrics

import (
	"fmt"
	"io"
	"os"
	"strconv"

	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

func (h *metricsHelper) metricsCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "metrics",
		Short: "Inspect analyzed metrics.",
	}
	cmd.AddCommand(
		h.metricsListCommand(),
		h.metricsGetCommand(),
	)
	return cmd
}

type metricsListOpts struct {
	cmdio.Options

	Segment string
	Actions []string
	Sort    string
	Limit   int
}

func (o *metricsListOpts) setup(flags *pflag.FlagSet) {
	table := metricsListTable()
	o.RegisterCustomCodec("table", table.Codec("table"))
	o.RegisterCustomCodec("wide", table.Codec("wide"))
	o.DefaultFormat("table")
	o.BindFlags(flags)
	o.BindListLimit(flags, &o.Limit, "metrics", 50)
	flags.StringVar(&o.Segment, "segment", "", "Segment ID")
	flags.StringArrayVar(&o.Actions, "action", nil, "Filter by action: add, update, remove, keep, none (repeatable)")
	flags.StringVar(&o.Sort, "sort", "projected-change", "Sort by: projected-change, raw-series, current-reduction, metric")
}

func (o *metricsListOpts) Validate() error {
	if err := o.Options.Validate(); err != nil {
		return err
	}
	if _, ok := metricsSorts[o.Sort]; !ok {
		return fmt.Errorf("invalid sort %q. Valid values are: projected-change, raw-series, current-reduction, metric", o.Sort)
	}
	for _, action := range o.Actions {
		if !metricsListActions[action] {
			return fmt.Errorf("invalid action %q. Valid values are: add, update, remove, keep, none", action)
		}
	}
	return nil
}

func (h *metricsHelper) metricsListCommand() *cobra.Command {
	opts := &metricsListOpts{}
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List analyzed metrics.",
		Long:  "List the segment's analyzed metrics, ranked by projected savings by default.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}

			ctx := cmd.Context()
			backend, err := newRulesBackend(ctx, h.loader, opts.Segment)
			if err != nil {
				return err
			}

			fetchLimit := opts.Limit
			if fetchLimit > 0 {
				fetchLimit++
			}
			metrics, err := backend.ListMetrics(ctx, metricsListOptions{
				Actions: opts.Actions,
				Sort:    opts.Sort,
				Limit:   fetchLimit,
			})
			if err != nil {
				return err
			}

			metrics, meta := cmdio.TruncatePagedList(metrics, opts.Limit)
			meta = cmdio.AttachListMeta(meta, os.Args)
			cmdio.EmitListTruncationHint(cmd.ErrOrStderr(), meta)
			fmt.Fprintf(cmd.ErrOrStderr(), "%d metric(s)\n", len(metrics))

			if metrics == nil {
				metrics = []AdaptiveMetric{}
			}
			return opts.Encode(cmd.OutOrStdout(), metricsListEnvelope{Items: metrics, ListMeta: meta})
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

type metricsListEnvelope struct {
	Items    []AdaptiveMetric `json:"items" yaml:"items"`
	ListMeta *cmdio.ListMeta  `json:"list_meta,omitempty" yaml:"list_meta,omitempty"`
}

func metricsListTable() cmdio.TableAdapter[metricsListEnvelope, AdaptiveMetric] {
	return cmdio.TableAdapter[metricsListEnvelope, AdaptiveMetric]{
		Table: metricsTable(),
		Rows:  func(e metricsListEnvelope) ([]AdaptiveMetric, error) { return e.Items, nil },
	}
}

func metricsTable() cmdio.Table[AdaptiveMetric] {
	return cmdio.Table[AdaptiveMetric]{
		Columns: []cmdio.Column[AdaptiveMetric]{
			{Header: "METRIC", Content: func(m AdaptiveMetric) string { return m.Metric }},
			{Header: "ACTION", Content: func(m AdaptiveMetric) string { return m.RecommendedAction }},
			{Header: "RAW SERIES", Content: func(m AdaptiveMetric) string { return formatSeriesCount(m.RawSeriesCount) }},
			{Header: "CURRENT REDUCTION", Content: func(m AdaptiveMetric) string { return formatSeriesCount(m.CurrentSeriesCountReduction) }},
			{Header: "PROJECTED CHANGE", Content: func(m AdaptiveMetric) string { return formatSeriesCount(m.ProjectedSeriesCountChange) }},
			{Header: "STATS UPDATED", Visible: cmdio.WideOnly, Content: func(m AdaptiveMetric) string { return derefString(m.StatsUpdatedAt) }},
		},
		Empty: func(io.Writer) error { return nil },
	}
}

func formatSeriesCount(v *int64) string {
	if v == nil {
		return "-"
	}
	return strconv.FormatInt(*v, 10)
}

func derefString(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}

type metricsGetOpts struct {
	cmdio.Options

	Segment string
}

func (o *metricsGetOpts) setup(flags *pflag.FlagSet) {
	o.DefaultFormat("yaml")
	o.BindFlags(flags)
	flags.StringVar(&o.Segment, "segment", "", "Segment ID")
}

func (h *metricsHelper) metricsGetCommand() *cobra.Command {
	opts := &metricsGetOpts{}
	cmd := &cobra.Command{
		Use:   "get <metric>",
		Short: "Get an analyzed metric's recommendation.",
		Long:  "Get one analyzed metric's recommendation, current rule, recommended rule, and usage stats.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}
			if err := validateRuleRef(opts.Segment, args[0], true); err != nil {
				return err
			}

			ctx := cmd.Context()
			backend, err := newRulesBackend(ctx, h.loader, opts.Segment)
			if err != nil {
				return err
			}

			detail, err := backend.GetMetricDetail(ctx, args[0])
			if err != nil {
				return err
			}
			return opts.Encode(cmd.OutOrStdout(), detail)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}
