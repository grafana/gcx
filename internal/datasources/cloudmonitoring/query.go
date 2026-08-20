package cloudmonitoring

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
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
	flags.StringToStringVar(&opts.Filters, "filter", nil, "Label filter key=value (repeatable, AND-combined, exact-match and case-sensitive only — no regex/wildcard; e.g. --filter resource.label.zone=us-east1-b)")
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
	if err := validateReducer(opts.Reducer); err != nil {
		return err
	}
	if err := validateAligner(opts.Aligner); err != nil {
		return err
	}
	if err := validateAlignmentPeriod(opts.AlignmentPeriod); err != nil {
		return err
	}
	if err := validateGroupBys(opts.GroupBys); err != nil {
		return err
	}
	if err := validateFilters(opts.Filters); err != nil {
		return err
	}
	return nil
}

// validateReducer checks r against the google.monitoring.v3.Aggregation.Reducer
// enum values the Google Cloud Monitoring API accepts for --reducer
// (cross-series reduction).
// https://cloud.google.com/monitoring/api/ref_v3/rpc/google.monitoring.v3#aggregation-reducer
func validateReducer(r string) error {
	switch r {
	case "REDUCE_NONE", "REDUCE_MEAN", "REDUCE_MIN", "REDUCE_MAX", "REDUCE_SUM",
		"REDUCE_STDDEV", "REDUCE_COUNT", "REDUCE_COUNT_TRUE", "REDUCE_COUNT_FALSE",
		"REDUCE_FRACTION_TRUE", "REDUCE_PERCENTILE_99", "REDUCE_PERCENTILE_95",
		"REDUCE_PERCENTILE_50", "REDUCE_PERCENTILE_05":
		return nil
	default:
		return fmt.Errorf("--reducer %q is not a valid cross-series reducer", r)
	}
}

// validateAligner checks a against the google.monitoring.v3.Aggregation.Aligner
// enum values the Google Cloud Monitoring API accepts for --aligner
// (per-series alignment).
// https://cloud.google.com/monitoring/api/ref_v3/rpc/google.monitoring.v3#aggregation-aligner
func validateAligner(a string) error {
	switch a {
	case "ALIGN_NONE", "ALIGN_DELTA", "ALIGN_RATE", "ALIGN_INTERPOLATE", "ALIGN_NEXT_OLDER",
		"ALIGN_MIN", "ALIGN_MAX", "ALIGN_MEAN", "ALIGN_COUNT", "ALIGN_SUM", "ALIGN_STDDEV",
		"ALIGN_COUNT_TRUE", "ALIGN_COUNT_FALSE", "ALIGN_FRACTION_TRUE",
		"ALIGN_PERCENTILE_99", "ALIGN_PERCENTILE_95", "ALIGN_PERCENTILE_50", "ALIGN_PERCENTILE_05",
		"ALIGN_PERCENT_CHANGE":
		return nil
	default:
		return fmt.Errorf("--aligner %q is not a valid per-series aligner", a)
	}
}

// alignmentPeriodRe matches the Grafana Cloud Monitoring plugin's alignment
// period syntax: a leading "+", digits, then "s" (e.g. +60s, +3600s).
var alignmentPeriodRe = regexp.MustCompile(`^\+\d+s$`)

func validateAlignmentPeriod(p string) error {
	if p == "" {
		return nil // auto-fit the time range
	}
	if !alignmentPeriodRe.MatchString(p) {
		return fmt.Errorf(`--alignment-period %q must be empty (auto-fit) or match "+<seconds>s", e.g. +60s`, p)
	}
	return nil
}

func validateGroupBys(groupBys []string) error {
	for _, g := range groupBys {
		if strings.TrimSpace(g) == "" {
			return errors.New("--group-by entries must not be empty")
		}
	}
	return nil
}

// validateFilters rejects an empty key or empty value in --filter entries.
// pflag's key=value parsing already rejects a bare "foo" (no "=") and an
// entirely empty "" with "must be formatted as key=value", but "=value" and
// "key=" both parse successfully to a map entry with one side empty and
// reach the request unchecked.
func validateFilters(filters map[string]string) error {
	for k, v := range filters {
		if k == "" {
			return fmt.Errorf("--filter %s: key must not be empty", "="+v)
		}
		if v == "" {
			return fmt.Errorf("--filter %s=: value must not be empty", k)
		}
	}
	return nil
}

// QueryCmd returns the `query` subcommand for a Google Cloud Monitoring datasource.
func QueryCmd(loader *providers.ConfigLoader) *cobra.Command {
	opts := &queryOpts{}
	share := &dsquery.ExploreLinkOpts{}

	cmd := &cobra.Command{
		Use:   "query",
		Short: "Execute a Google Cloud Monitoring metrics query",
		Long: `Execute a Google Cloud Monitoring (formerly Stackdriver) metrics query.

Queries are structured (project, metric type, reducer, aligner) — there is no
expression language. Use --group-by to split the result into one series per
label value, and --filter to narrow by labels.

--filter matches are exact and case-sensitive, not regex or wildcard — GCM has
no equivalent of "=~". Repeated --filter flags are AND-combined. A filter like
"zone=~us-east.*" is not a regex match; it is a literal equality comparison
against that exact string, which will not match real zone values and returns
"No data" with no error.

Use list-projects and list-metrics to discover valid flag values.
Datasource is resolved from -d flag or datasources.cloudmonitoring in your context.

Use --share-link to print the equivalent Grafana Explore URL after the query,
or --open to open it in your browser. Note: when no --from/--to/--since flags
are provided, the link encodes "now-1h"/"now" (relative), not the absolute
window the CLI just queried.`,
		Example: `
  # CPU utilization across a project
  gcx datasources cloudmonitoring query -d UID --project my-project \
    --metric compute.googleapis.com/instance/cpu/utilization --since 1h

  # Print a Grafana Explore share link for the executed query
  gcx datasources cloudmonitoring query -d UID --project my-project \
    --metric compute.googleapis.com/instance/cpu/utilization --since 1h --share-link

  # Run the query and continue in Grafana Explore
  gcx datasources cloudmonitoring query -d UID --project my-project \
    --metric compute.googleapis.com/instance/cpu/utilization --since 1h --open

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

			datasourceUID, dsType, err := dsquery.ResolveValidateAndSaveDatasource(ctx, loader, opts.Datasource, cfgCtx, cfg, "cloudmonitoring")
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

			req := gcmclient.QueryRequest{
				Project:         opts.Project,
				MetricType:      opts.Metric,
				Reducer:         opts.Reducer,
				Aligner:         opts.Aligner,
				AlignmentPeriod: opts.AlignmentPeriod,
				GroupBys:        opts.GroupBys,
				Filters:         opts.Filters,
				Start:           start,
				End:             end,
			}

			resp, err := client.Query(ctx, datasourceUID, req)
			if err != nil {
				return fmt.Errorf("query failed: %w", err)
			}

			exploreURL := QueryExploreURL(cfg.GrafanaURL, dsquery.ExploreQuery{
				DatasourceUID:  datasourceUID,
				DatasourceType: dsType,
				From:           opts.From,
				To:             opts.To,
				OrgID:          dsquery.OrgID(cfgCtx),
			}, req)
			unavailableMsg, failedOpenMsg := dsquery.ExploreMessages("query")

			return dsquery.EncodeAndHandleExplore(cmd, func() error {
				return opts.IO.Encode(cmd.OutOrStdout(), resp)
			}, *share, dsquery.ExploreLink{
				URL:            exploreURL,
				UnavailableMsg: unavailableMsg,
				FailedOpenMsg:  failedOpenMsg,
			})
		},
	}

	cmd.Annotations = map[string]string{
		agent.AnnotationTokenCost: "large",
		agent.AnnotationLLMHint:   "gcx datasources cloudmonitoring query -d UID --project PROJECT --metric compute.googleapis.com/instance/cpu/utilization --since 1h",
	}

	opts.setup(cmd.Flags())
	share.Setup(cmd.Flags(), "executed query")

	return cmd
}
