package cloudwatch

import (
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/grafana/gcx/internal/agent"
	internalconfig "github.com/grafana/gcx/internal/config"
	dsquery "github.com/grafana/gcx/internal/datasources/query"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	cwclient "github.com/grafana/gcx/internal/query/cloudwatch"
	"github.com/grafana/grafana-app-sdk/logging"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type queryOpts struct {
	dsquery.TimeRangeOpts

	IO         cmdio.Options
	Datasource string
	Namespace  string
	Metric     string
	Region     string
	Statistic  string
	Period     int
	Dimensions map[string]string
	AccountID  string
}

func (opts *queryOpts) setup(flags *pflag.FlagSet) {
	dsquery.RegisterCodecs(&opts.IO, true)
	opts.IO.BindFlags(flags)
	opts.SetupTimeFlags(flags)

	flags.StringVarP(&opts.Datasource, "datasource", "d", "", "Datasource UID (required unless datasources.cloudwatch is configured)")
	flags.StringVar(&opts.Namespace, "namespace", "", "CloudWatch namespace, e.g. AWS/EC2 (required)")
	flags.StringVar(&opts.Metric, "metric", "", "CloudWatch metric name, e.g. CPUUtilization (required)")
	flags.StringVar(&opts.Region, "region", "", "AWS region, e.g. us-east-1 (required)")
	flags.StringVar(&opts.Statistic, "statistic", "Average", "Statistic: Average, Sum, Maximum, Minimum, or SampleCount")
	flags.IntVar(&opts.Period, "period", 300, "Period in seconds (must be > 0)")
	flags.StringToStringVar(&opts.Dimensions, "dimensions", nil, "Dimension key=value pairs (repeatable, e.g. --dimensions InstanceId=i-abc)")
	flags.StringVar(&opts.AccountID, "account-id", "", "AWS account ID for cross-account monitoring (or 'all')")
}

func (opts *queryOpts) Validate() error {
	if err := opts.IO.Validate(); err != nil {
		return err
	}
	if err := opts.ValidateTimeRange(); err != nil {
		return err
	}
	if opts.Namespace == "" {
		return errors.New("--namespace is required")
	}
	if opts.Metric == "" {
		return errors.New("--metric is required")
	}
	if opts.Region == "" {
		return errors.New("--region is required")
	}
	if !cwclient.IsValidStatistic(opts.Statistic) {
		return fmt.Errorf("invalid --statistic %q: must be one of Average, Sum, Maximum, Minimum, SampleCount", opts.Statistic)
	}
	if opts.Period <= 0 {
		return errors.New("--period must be > 0")
	}
	return nil
}

// QueryCmd returns the `query` subcommand for a CloudWatch datasource.
func QueryCmd(loader *providers.ConfigLoader) *cobra.Command {
	opts := &queryOpts{}
	share := &dsquery.ExploreLinkOpts{}

	cmd := &cobra.Command{
		Use:   "query",
		Short: "Execute a CloudWatch metric query",
		Long: `Execute a CloudWatch metric query.

Queries are structured (namespace, metric, region, statistic, period, dimensions) —
there is no expression language for CloudWatch. Use --dimensions (repeatable) for
dimension filters, or omit them to aggregate across all combinations.

Use --share-link to print the equivalent Grafana Explore URL after the query.
Note: when no --from/--to/--since flags are provided, the share link encodes
"now-1h"/"now" (relative), not the absolute window the CLI just queried.`,
		Example: `
  # Query with required flags
  gcx datasources cloudwatch query -d UID --region us-east-1 --namespace AWS/EC2 --metric CPUUtilization

  # With time range
  gcx datasources cloudwatch query -d UID --region us-east-1 --namespace AWS/EC2 --metric CPUUtilization --since 1h

  # With dimension filter
  gcx datasources cloudwatch query -d UID --region us-east-1 --namespace AWS/EC2 --metric CPUUtilization \
    --dimensions InstanceId=i-0123456789abcdef0 --since 1h

  # Print as JSON
  gcx datasources cloudwatch query -d UID --region us-east-1 --namespace AWS/EC2 --metric CPUUtilization -o json`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}

			ctx := cmd.Context()

			var cfgCtx *internalconfig.Context
			fullCfg, err := loader.LoadFullConfig(ctx)
			if err != nil {
				logging.FromContext(ctx).Warn("could not load config; falling back to auto-discovery", slog.String("error", err.Error()))
			} else {
				cfgCtx = fullCfg.GetCurrentContext()
			}

			cfg, err := loader.LoadGrafanaConfig(ctx)
			if err != nil {
				return err
			}

			datasourceUID, _, err := dsquery.ResolveValidateAndSaveDatasource(ctx, loader, opts.Datasource, cfgCtx, cfg, "cloudwatch")
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

			matchExact := len(opts.Dimensions) > 0

			client, err := cwclient.NewClient(cfg)
			if err != nil {
				return fmt.Errorf("failed to create client: %w", err)
			}

			req := cwclient.QueryRequest{
				Namespace:  opts.Namespace,
				MetricName: opts.Metric,
				Region:     opts.Region,
				Statistic:  opts.Statistic,
				Period:     opts.Period,
				Dimensions: opts.Dimensions,
				MatchExact: matchExact,
				AccountID:  opts.AccountID,
				Start:      start,
				End:        end,
			}

			resp, err := client.Query(ctx, datasourceUID, req)
			if err != nil {
				return fmt.Errorf("query failed: %w", err)
			}

			exploreURL := QueryExploreURL(cfg.GrafanaURL, dsquery.ExploreQuery{
				DatasourceUID:  datasourceUID,
				DatasourceType: "cloudwatch",
				From:           opts.From,
				To:             opts.To,
				OrgID:          dsquery.OrgID(cfgCtx),
			}, CloudWatchQuery{
				Namespace:  opts.Namespace,
				MetricName: opts.Metric,
				Region:     opts.Region,
				Statistic:  opts.Statistic,
				Period:     opts.Period,
				Dimensions: opts.Dimensions,
				MatchExact: matchExact,
				AccountID:  opts.AccountID,
			})
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
		agent.AnnotationTokenCost: "medium",
		agent.AnnotationLLMHint:   "-d UID --region us-east-1 --namespace AWS/EC2 --metric CPUUtilization --since 1h -o json",
	}

	opts.setup(cmd.Flags())
	share.Setup(cmd.Flags(), "executed query")

	return cmd
}
