package cloudwatch

import (
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/grafana/gcx/internal/agent"
	dsquery "github.com/grafana/gcx/internal/datasources/query"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	cwclient "github.com/grafana/gcx/internal/query/cloudwatch"
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
	Period     string
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
	flags.StringVar(&opts.Statistic, "statistic", "Average", "Statistic: Average, Sum, Maximum, Minimum, SampleCount, or a percentile/trimmed-mean (e.g. p95, p99, tm99)")
	flags.StringVar(&opts.Period, "period", "auto", `Period in seconds (e.g. 60, 300) or "auto" to let CloudWatch pick a period that fits the time range`)
	flags.StringToStringVar(&opts.Dimensions, "dimensions", nil, "Dimension key=value pairs (repeatable, e.g. --dimensions InstanceId=i-abc)")
	flags.StringVar(&opts.AccountID, "account-id", "", "AWS account ID for cross-account monitoring; pass 'all' to query all linked accounts, or a specific ID from list-accounts. Required to surface dimensions from source accounts on monitoring datasources.")
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
	if opts.Statistic == "" {
		return errors.New("--statistic must not be empty")
	}
	if err := validatePeriod(opts.Period); err != nil {
		return err
	}
	return nil
}

// validatePeriod mirrors the Grafana CloudWatch plugin's getPeriod logic in
// pkg/cloudwatch/models/cloudwatch_query.go: "auto" (or empty) means the
// plugin picks a period from CloudWatch's retention ladder, an integer is
// taken as seconds, otherwise the value must parse as a Go duration
// (e.g. "5m", "1h").
func validatePeriod(p string) error {
	if p == "" || strings.EqualFold(p, "auto") {
		return nil
	}
	if _, err := strconv.Atoi(p); err == nil {
		return nil
	}
	if _, err := time.ParseDuration(p); err == nil {
		return nil
	}
	return errors.New(`--period must be "auto", an integer (seconds), or a Go duration like "5m", "1h"`)
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
"now-1h"/"now" (relative), not the absolute window the CLI just queried.

Cross-account monitoring datasources: if your datasource is configured for
cross-account monitoring (a "monitoring account"), --dimensions filters scope
to the datasource's own account by default. To surface dimensions from source
accounts, pass --account-id <id> (run list-accounts to discover IDs) or
--account-id all to query all linked accounts.`,
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

			cfgCtx, cfg, err := dsquery.LoadContextAndConfig(ctx, loader)
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

			// Translate the CLI's single-valued --dimensions flag map into
			// the multi-valued shape Grafana expects on the wire. Leave nil
			// when empty; the wire-level helper substitutes an empty map.
			var dimensions map[string][]string
			if len(opts.Dimensions) > 0 {
				dimensions = make(map[string][]string, len(opts.Dimensions))
				for k, v := range opts.Dimensions {
					dimensions[k] = []string{v}
				}
			}
			matchExact := len(dimensions) > 0

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
				Dimensions: dimensions,
				MatchExact: matchExact,
				AccountID:  opts.AccountID,
				Start:      start,
				End:        end,
			}

			resp, err := client.Query(ctx, datasourceUID, req)
			if err != nil {
				return fmt.Errorf("query failed: %w", err)
			}

			maybeEmitCrossAccountHint(cmd.ErrOrStderr(), opts.Dimensions, opts.AccountID, resp)

			exploreURL := QueryExploreURL(cfg.GrafanaURL, dsquery.ExploreQuery{
				DatasourceUID:  datasourceUID,
				DatasourceType: "cloudwatch",
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
		agent.AnnotationLLMHint:   "gcx datasources cloudwatch query -d UID --region us-east-1 --namespace AWS/EC2 --metric CPUUtilization --since 1h -o json",
	}

	opts.setup(cmd.Flags())
	share.Setup(cmd.Flags(), "executed query")

	return cmd
}

func allFramesEmpty(resp *cwclient.QueryResponse) bool {
	if resp == nil || len(resp.Frames) == 0 {
		return true
	}
	for _, f := range resp.Frames {
		if len(f.Timestamps) > 0 {
			return false
		}
	}
	return true
}

func maybeEmitCrossAccountHint(w io.Writer, dimensions map[string]string, accountID string, resp *cwclient.QueryResponse) {
	if len(dimensions) == 0 || accountID != "" || !allFramesEmpty(resp) {
		return
	}
	fmt.Fprintln(w,
		"Hint: no data returned. If this is a cross-account monitoring datasource and you intended to query a linked account, try --account-id all (or pass a specific ID; use `list-accounts` to discover them).")
}
