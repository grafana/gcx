package azuremonitor

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/grafana/gcx/internal/agent"
	dsquery "github.com/grafana/gcx/internal/datasources/query"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	azclient "github.com/grafana/gcx/internal/query/azuremonitor"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type queryOpts struct {
	dsquery.TimeRangeOpts

	IO            cmdio.Options
	Share         dsquery.ExploreLinkOpts
	Datasource    string
	Subscription  string
	ResourceGroup string
	Resource      string
	Namespace     string
	Metric        string
	Aggregation   string
	TimeGrain     string
	Region        string
	Top           string
	Dimensions    map[string]string
}

func (opts *queryOpts) setup(flags *pflag.FlagSet) {
	dsquery.RegisterCodecs(&opts.IO, true)
	opts.IO.BindFlags(flags)
	opts.SetupTimeFlags(flags)
	opts.Share.Setup(flags, "executed query")

	flags.StringVarP(&opts.Datasource, "datasource", "d", "", "Datasource UID (required unless datasources.azuremonitor is configured)")
	flags.StringVar(&opts.Subscription, "subscription", "", "Azure subscription ID (defaults to the datasource's default subscription)")
	flags.StringVar(&opts.ResourceGroup, "resource-group", "", "Azure resource group name (required)")
	flags.StringVar(&opts.Resource, "resource", "", "Azure resource name; use the slash form for sub-resources, e.g. mystorage/blobServices/default (required)")
	flags.StringVar(&opts.Namespace, "namespace", "", "Metric namespace, e.g. Microsoft.Storage/storageAccounts (required)")
	flags.StringVar(&opts.Metric, "metric", "", "Metric name, e.g. Transactions (required)")
	flags.StringVar(&opts.Aggregation, "aggregation", "Average", "Aggregation: Average, Total, Maximum, Minimum, or Count (must be supported by the metric; see list-metrics)")
	flags.StringVar(&opts.TimeGrain, "time-grain", "auto", `Time grain as an ISO 8601 duration (e.g. PT1M, PT1H) or "auto" to fit the time range`)
	flags.StringVar(&opts.Region, "region", "", "Azure region, e.g. uksouth (optional; used for multi-resource queries)")
	flags.StringVar(&opts.Top, "top", "", "Maximum number of dimension value series to return (only with --dimensions)")
	flags.StringToStringVar(&opts.Dimensions, "dimensions", nil, `Dimension key=value filters (repeatable, e.g. --dimensions ApiName=GetBlob); use "*" as the value to split the result by that dimension`)
}

func (opts *queryOpts) Validate() error {
	if err := opts.IO.Validate(); err != nil {
		return err
	}
	if err := opts.ValidateTimeRange(); err != nil {
		return err
	}
	if opts.ResourceGroup == "" {
		return errors.New("--resource-group is required")
	}
	if opts.Resource == "" {
		return errors.New("--resource is required")
	}
	if opts.Namespace == "" {
		return errors.New("--namespace is required")
	}
	if opts.Metric == "" {
		return errors.New("--metric is required")
	}
	if err := validateAggregation(opts.Aggregation); err != nil {
		return err
	}
	if err := validateTimeGrain(opts.TimeGrain); err != nil {
		return err
	}
	if err := validateTop(opts.Top, opts.Dimensions); err != nil {
		return err
	}
	return nil
}

// validateAggregation checks agg against the aggregation types the Azure
// Monitor Metrics API accepts. Not all metrics support every aggregation;
// list-metrics reports which ones a given metric actually supports.
func validateAggregation(agg string) error {
	if agg == "" {
		return errors.New("--aggregation must not be empty")
	}
	switch agg {
	case "Average", "Total", "Maximum", "Minimum", "Count":
		return nil
	default:
		return fmt.Errorf("--aggregation must be one of Average, Total, Maximum, Minimum, or Count, got %q", agg)
	}
}

// isoDurationRe matches an ISO 8601 duration with at least one date or time
// component (bare "P" or "PT" are rejected below since every group here is
// optional). Azure's time grains are always positive, whole-unit durations
// (e.g. PT1M, PT1H, P1D), so fractional seconds aren't accepted.
var isoDurationRe = regexp.MustCompile(`^P(\d+Y)?(\d+M)?(\d+W)?(\d+D)?(T(\d+H)?(\d+M)?(\d+S)?)?$`)

func validateTimeGrain(tg string) error {
	if strings.EqualFold(tg, "auto") {
		return nil
	}
	if tg != "" && tg != "P" && tg != "PT" && isoDurationRe.MatchString(tg) {
		return nil
	}
	return fmt.Errorf(`--time-grain must be "auto" or an ISO 8601 duration (e.g. PT1M, PT1H, P1D), got %q`, tg)
}

func validateTop(top string, dimensions map[string]string) error {
	if top == "" {
		return nil
	}
	n, err := strconv.Atoi(top)
	if err != nil || n <= 0 {
		return fmt.Errorf("--top must be a positive integer, got %q", top)
	}
	if len(dimensions) == 0 {
		return errors.New("--top is only meaningful together with --dimensions")
	}
	return nil
}

// QueryCmd returns the `query` subcommand for an Azure Monitor datasource.
func QueryCmd(loader *providers.ConfigLoader) *cobra.Command {
	opts := &queryOpts{}

	cmd := &cobra.Command{
		Use:   "query",
		Short: "Execute an Azure Monitor metrics query",
		Long: `Execute an Azure Monitor metrics query.

Queries are structured (subscription, resource group, resource, metric namespace,
metric, aggregation) — there is no expression language for Azure Monitor metrics.
Use --dimensions (repeatable) to filter or split by dimension values: a specific
value filters the series, "*" splits the result into one series per value.

Use the list-subscriptions, list-resource-groups, list-resources, and
list-metrics subcommands to discover valid flag values.

Datasource is resolved from -d flag or datasources.azuremonitor in your context.
Note: datasources configured with "Current User" (Azure AD passthrough)
authentication cannot be queried with API tokens or service accounts.

Use --share-link to print the equivalent Grafana Explore URL, or --open to
open it in your browser after the query succeeds.`,
		Example: `
  # Query a storage account metric
  gcx datasources azuremonitor query -d UID --subscription SUB_ID \
    --resource-group my-rg --namespace Microsoft.Storage/storageAccounts \
    --resource mystorage --metric Transactions --aggregation Total --since 1h

  # Split the series by a dimension
  gcx datasources azuremonitor query -d UID --subscription SUB_ID \
    --resource-group my-rg --namespace Microsoft.Storage/storageAccounts \
    --resource mystorage --metric Transactions --aggregation Total \
    --dimensions ApiName='*' --since 1h

  # Output as JSON
  gcx datasources azuremonitor query -d UID --subscription SUB_ID \
    --resource-group my-rg --namespace Microsoft.Compute/virtualMachines \
    --resource my-vm --metric 'Percentage CPU' -o json

  # Print a Grafana Explore share link for the executed query
  gcx datasources azuremonitor query -d UID --subscription SUB_ID \
    --resource-group my-rg --namespace Microsoft.Compute/virtualMachines \
    --resource my-vm --metric 'Percentage CPU' --since 1h --share-link

  # Open the executed query in Grafana Explore
  gcx datasources azuremonitor query -d UID --subscription SUB_ID \
    --resource-group my-rg --namespace Microsoft.Compute/virtualMachines \
    --resource my-vm --metric 'Percentage CPU' --since 1h --open`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}
			if err := rejectExplicitEmptyFlag(cmd, "subscription", opts.Subscription); err != nil {
				return err
			}

			ctx := cmd.Context()

			cfgCtx, cfg, err := dsquery.LoadContextAndConfig(ctx, loader)
			if err != nil {
				return err
			}

			datasourceUID, dsType, err := dsquery.ResolveValidateAndSaveDatasource(ctx, loader, opts.Datasource, cfgCtx, cfg, "azuremonitor")
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

			subscription, err := resolveSubscription(ctx, cfg, datasourceUID, opts.Subscription)
			if err != nil {
				return err
			}

			client, err := azclient.NewClient(cfg)
			if err != nil {
				return fmt.Errorf("failed to create client: %w", err)
			}

			req := azclient.QueryRequest{
				Subscription:     subscription,
				ResourceGroup:    opts.ResourceGroup,
				ResourceName:     opts.Resource,
				MetricNamespace:  opts.Namespace,
				MetricName:       opts.Metric,
				Aggregation:      opts.Aggregation,
				TimeGrain:        opts.TimeGrain,
				Region:           opts.Region,
				Top:              opts.Top,
				DimensionFilters: opts.Dimensions,
				Start:            start,
				End:              end,
			}

			resp, err := client.Query(ctx, datasourceUID, req)
			if err != nil {
				return fmt.Errorf("query failed: %w", err)
			}

			// req carries the resolved subscription, so the Explore link
			// always names the subscription the query actually used.
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
			}, opts.Share, dsquery.ExploreLink{
				URL:            exploreURL,
				UnavailableMsg: unavailableMsg,
				FailedOpenMsg:  failedOpenMsg,
			})
		},
	}

	cmd.Annotations = map[string]string{
		agent.AnnotationTokenCost: "large",
		agent.AnnotationLLMHint:   "gcx datasources azuremonitor query -d UID --subscription SUB_ID --resource-group RG --namespace Microsoft.Storage/storageAccounts --resource NAME --metric Transactions --aggregation Total --since 1h",
	}

	opts.setup(cmd.Flags())

	return cmd
}
