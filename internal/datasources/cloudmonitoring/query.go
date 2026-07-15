package cloudmonitoring

import (
	"errors"
	"fmt"
	"time"

	"github.com/grafana/gcx/internal/agent"
	dsquery "github.com/grafana/gcx/internal/datasources/query"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	gcmclient "github.com/grafana/gcx/internal/query/cloudmonitoring"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type queryOpts struct {
	dsquery.TimeRangeOpts

	IO              cmdio.Options
	Datasource      string
	Project         string
	Metric          string
	Reducer         string
	Aligner         string
	AlignmentPeriod string
	GroupBys        []string
	Filters         map[string]string
}

func (opts *queryOpts) setup(flags *pflag.FlagSet) {
	dsquery.RegisterCodecs(&opts.IO, true)
	opts.IO.BindFlags(flags)
	opts.SetupTimeFlags(flags)

	flags.StringVarP(&opts.Datasource, "datasource", "d", "", "Datasource UID (required unless datasources.cloudmonitoring is configured)")
	flags.StringVar(&opts.Project, "project", "", "GCP project ID (required)")
	flags.StringVar(&opts.Metric, "metric", "", "Metric type, e.g. compute.googleapis.com/instance/cpu/utilization (required)")
	flags.StringVar(&opts.Reducer, "reducer", "REDUCE_NONE", "Cross-series reducer: REDUCE_NONE, REDUCE_MEAN, REDUCE_SUM, REDUCE_MIN, REDUCE_MAX, REDUCE_COUNT, ...")
	flags.StringVar(&opts.Aligner, "aligner", "ALIGN_MEAN", "Per-series aligner: ALIGN_MEAN, ALIGN_SUM, ALIGN_MIN, ALIGN_MAX, ALIGN_RATE, ALIGN_DELTA, ...")
	flags.StringVar(&opts.AlignmentPeriod, "alignment-period", "", `Alignment period, e.g. +60s (default: auto-fit the time range)`)
	flags.StringArrayVar(&opts.GroupBys, "group-by", nil, "Label to split series by, e.g. resource.label.instance_name (repeatable)")
	flags.StringToStringVar(&opts.Filters, "filter", nil, "Label filter key=value (repeatable, e.g. --filter resource.label.zone=us-east1-b)")
}

func (opts *queryOpts) Validate() error {
	if err := opts.IO.Validate(); err != nil {
		return err
	}
	if err := opts.ValidateTimeRange(); err != nil {
		return err
	}
	if opts.Project == "" {
		return errors.New("--project is required")
	}
	if opts.Metric == "" {
		return errors.New("--metric is required")
	}
	return nil
}

// QueryCmd returns the `query` subcommand for a Google Cloud Monitoring datasource.
func QueryCmd(loader *providers.ConfigLoader) *cobra.Command {
	opts := &queryOpts{}

	cmd := &cobra.Command{
		Use:   "query",
		Short: "Execute a Google Cloud Monitoring metrics query",
		Long: `Execute a Google Cloud Monitoring (formerly Stackdriver) metrics query.

Queries are structured (project, metric type, reducer, aligner) — there is no
expression language. Use --group-by to split the result into one series per
label value, and --filter to narrow by labels.

Use list-projects and list-metrics to discover valid flag values.
Datasource is resolved from -d flag or datasources.cloudmonitoring in your context.`,
		Example: `
  # CPU utilization across a project
  gcx datasources cloudmonitoring query -d UID --project my-project \
    --metric compute.googleapis.com/instance/cpu/utilization --since 1h

  # Split by instance, mean-reduced
  gcx datasources cloudmonitoring query -d UID --project my-project \
    --metric compute.googleapis.com/instance/cpu/utilization \
    --reducer REDUCE_MEAN --group-by resource.label.instance_name --since 1h

  # Chart it
  gcx datasources cloudmonitoring query -d UID --project my-project \
    --metric compute.googleapis.com/instance/cpu/utilization --since 6h -o graph`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}

			ctx := cmd.Context()

			cfgCtx, cfg, err := dsquery.LoadContextAndConfig(ctx, loader)
			if err != nil {
				return err
			}

			datasourceUID, _, err := dsquery.ResolveValidateAndSaveDatasource(ctx, loader, opts.Datasource, cfgCtx, cfg, "cloudmonitoring")
			if err != nil {
				return err
			}

			now := time.Now()
			start, end, err := opts.ParseTimeRange(now)
			if err != nil {
				return err
			}
			if start.IsZero() && end.IsZero() && opts.Since == "" {
				end = now
				start = now.Add(-1 * time.Hour)
			}

			client, err := gcmclient.NewClient(cfg)
			if err != nil {
				return fmt.Errorf("failed to create client: %w", err)
			}

			resp, err := client.Query(ctx, datasourceUID, gcmclient.QueryRequest{
				Project:         opts.Project,
				MetricType:      opts.Metric,
				Reducer:         opts.Reducer,
				Aligner:         opts.Aligner,
				AlignmentPeriod: opts.AlignmentPeriod,
				GroupBys:        opts.GroupBys,
				Filters:         opts.Filters,
				Start:           start,
				End:             end,
			})
			if err != nil {
				return fmt.Errorf("query failed: %w", err)
			}

			return opts.IO.Encode(cmd.OutOrStdout(), resp)
		},
	}

	cmd.Annotations = map[string]string{
		agent.AnnotationTokenCost: "large",
		agent.AnnotationLLMHint:   "gcx datasources cloudmonitoring query -d UID --project PROJECT --metric compute.googleapis.com/instance/cpu/utilization --since 1h",
	}

	opts.setup(cmd.Flags())

	return cmd
}
