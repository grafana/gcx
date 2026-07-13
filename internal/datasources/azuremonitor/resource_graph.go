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

type resourceGraphOpts struct {
	IO            cmdio.Options
	Datasource    string
	Subscriptions []string
}

func (opts *resourceGraphOpts) setup(flags *pflag.FlagSet) {
	dsquery.RegisterCodecs(&opts.IO, false)
	opts.IO.BindFlags(flags)

	flags.StringVarP(&opts.Datasource, "datasource", "d", "", "Datasource UID (required unless datasources.azuremonitor is configured)")
	flags.StringArrayVar(&opts.Subscriptions, "subscription", nil, "Azure subscription ID to query (repeatable; at least one required)")
}

func (opts *resourceGraphOpts) Validate() error {
	if err := opts.IO.Validate(); err != nil {
		return err
	}
	if len(opts.Subscriptions) == 0 {
		return errors.New("--subscription is required (repeatable)")
	}
	return nil
}

// ResourceGraphCmd returns the `resource-graph` subcommand for an Azure Monitor datasource.
func ResourceGraphCmd(loader *providers.ConfigLoader) *cobra.Command {
	opts := &resourceGraphOpts{}

	cmd := &cobra.Command{
		Use:   "resource-graph KQL",
		Short: "Query Azure Resource Graph with KQL",
		Long: `Execute a KQL (Kusto Query Language) query against Azure Resource Graph,
Azure's inventory of resources across subscriptions.

KQL is the query expression, e.g. 'Resources | project name, type | limit 10'.
Pass --subscription (repeatable) to scope the query.

Datasource is resolved from -d flag or datasources.azuremonitor in your context.`,
		Example: `
  # List resources by type
  gcx datasources azuremonitor resource-graph \
    'Resources | summarize count() by type | order by count_ desc' \
    -d UID --subscription SUB_ID

  # Query across multiple subscriptions, output as JSON
  gcx datasources azuremonitor resource-graph 'Resources | project name, type, location' \
    -d UID --subscription SUB_A --subscription SUB_B -o json`,
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

			client, err := azclient.NewClient(cfg)
			if err != nil {
				return fmt.Errorf("failed to create client: %w", err)
			}

			// Resource Graph results are not time-scoped; the range only
			// satisfies the query API envelope.
			now := time.Now()
			req := azclient.ResourceGraphRequest{
				Subscriptions: opts.Subscriptions,
				Query:         args[0],
				Start:         now.Add(-1 * time.Hour),
				End:           now,
			}

			resp, err := client.ResourceGraphQuery(ctx, datasourceUID, req)
			if err != nil {
				return fmt.Errorf("query failed: %w", err)
			}

			return opts.IO.Encode(cmd.OutOrStdout(), resp)
		},
	}

	cmd.Annotations = map[string]string{
		agent.AnnotationTokenCost: "medium",
		agent.AnnotationLLMHint:   "gcx datasources azuremonitor resource-graph 'Resources | project name, type | limit 20' -d UID --subscription SUB_ID",
	}

	opts.setup(cmd.Flags())

	return cmd
}
