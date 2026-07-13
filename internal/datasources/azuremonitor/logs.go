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

type logsOpts struct {
	dsquery.TimeRangeOpts

	IO            cmdio.Options
	Datasource    string
	Subscription  string
	ResourceGroup string
	Workspace     string
}

func (opts *logsOpts) setup(flags *pflag.FlagSet) {
	dsquery.RegisterCodecs(&opts.IO, false)
	opts.IO.BindFlags(flags)
	opts.SetupTimeFlags(flags)

	flags.StringVarP(&opts.Datasource, "datasource", "d", "", "Datasource UID (required unless datasources.azuremonitor is configured)")
	flags.StringVar(&opts.Subscription, "subscription", "", "Azure subscription ID (required)")
	flags.StringVar(&opts.ResourceGroup, "resource-group", "", "Azure resource group of the workspace (required)")
	flags.StringVar(&opts.Workspace, "workspace", "", "Log Analytics workspace name (required)")
}

func (opts *logsOpts) Validate() error {
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
	if opts.Workspace == "" {
		return errors.New("--workspace is required")
	}
	return nil
}

// LogsCmd returns the `logs` subcommand for an Azure Monitor datasource.
func LogsCmd(loader *providers.ConfigLoader) *cobra.Command {
	opts := &logsOpts{}

	cmd := &cobra.Command{
		Use:   "logs KQL",
		Short: "Query a Log Analytics workspace with KQL",
		Long: `Execute a KQL (Kusto Query Language) query against an Azure Log Analytics
workspace.

KQL is the query expression, e.g. 'AppRequests | take 10'. The workspace is
identified by --subscription, --resource-group, and --workspace; use
list-resources to discover workspaces (type Microsoft.OperationalInsights/workspaces).

Datasource is resolved from -d flag or datasources.azuremonitor in your context.`,
		Example: `
  # Query a workspace
  gcx datasources azuremonitor logs 'AppRequests | take 10' -d UID \
    --subscription SUB_ID --resource-group my-rg --workspace my-workspace

  # With a time range
  gcx datasources azuremonitor logs 'AppRequests | summarize count() by bin(TimeGenerated, 5m)' \
    -d UID --subscription SUB_ID --resource-group my-rg --workspace my-workspace --since 1h

  # Output as JSON
  gcx datasources azuremonitor logs 'AppTraces | take 5' -d UID \
    --subscription SUB_ID --resource-group my-rg --workspace my-workspace -o json`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
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

			req := azclient.LogsQueryRequest{
				Subscription:  opts.Subscription,
				ResourceGroup: opts.ResourceGroup,
				Workspace:     opts.Workspace,
				Query:         args[0],
				Start:         start,
				End:           end,
			}

			resp, err := client.LogsQuery(ctx, datasourceUID, req)
			if err != nil {
				return fmt.Errorf("query failed: %w", err)
			}

			return opts.IO.Encode(cmd.OutOrStdout(), resp)
		},
	}

	cmd.Annotations = map[string]string{
		agent.AnnotationTokenCost: "large",
		agent.AnnotationLLMHint:   "gcx datasources azuremonitor logs 'AppRequests | take 10' -d UID --subscription SUB_ID --resource-group RG --workspace WS --since 1h",
	}

	opts.setup(cmd.Flags())

	return cmd
}
