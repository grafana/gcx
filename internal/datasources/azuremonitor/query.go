package azuremonitor

import (
	"errors"
	"fmt"
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

	flags.StringVarP(&opts.Datasource, "datasource", "d", "", "Datasource UID (required unless datasources.azuremonitor is configured)")
	flags.StringVar(&opts.Subscription, "subscription", "", "Azure subscription ID (required)")
	flags.StringVar(&opts.ResourceGroup, "resource-group", "", "Azure resource group name (required)")
	flags.StringVar(&opts.Resource, "resource", "", "Azure resource name (required)")
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
	if opts.Subscription == "" {
		return errors.New("--subscription is required")
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
	if opts.Aggregation == "" {
		return errors.New("--aggregation must not be empty")
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
authentication cannot be queried with API tokens or service accounts.`,
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
    --resource my-vm --metric 'Percentage CPU' -o json`,
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

			datasourceUID, _, err := dsquery.ResolveValidateAndSaveDatasource(ctx, loader, opts.Datasource, cfgCtx, cfg, "azuremonitor")
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

			client, err := azclient.NewClient(cfg)
			if err != nil {
				return fmt.Errorf("failed to create client: %w", err)
			}

			req := azclient.QueryRequest{
				Subscription:     opts.Subscription,
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

			return opts.IO.Encode(cmd.OutOrStdout(), resp)
		},
	}

	cmd.Annotations = map[string]string{
		agent.AnnotationTokenCost: "large",
		agent.AnnotationLLMHint:   "gcx datasources azuremonitor query -d UID --subscription SUB_ID --resource-group RG --namespace Microsoft.Storage/storageAccounts --resource NAME --metric Transactions --aggregation Total --since 1h",
	}

	opts.setup(cmd.Flags())

	return cmd
}
