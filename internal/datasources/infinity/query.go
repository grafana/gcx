package infinity

import (
	"fmt"
	"log/slog"
	"time"

	"github.com/grafana/gcx/internal/agent"
	internalconfig "github.com/grafana/gcx/internal/config"
	dsquery "github.com/grafana/gcx/internal/datasources/query"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/query/infinity"
	"github.com/grafana/grafana-app-sdk/logging"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type queryOpts struct {
	dsquery.SharedOpts

	Datasource string
}

func (opts *queryOpts) setup(flags *pflag.FlagSet) {
	dsquery.RegisterCodecs(&opts.IO, false)
	opts.IO.BindFlags(flags)
	opts.SetupTimeFlags(flags)
	flags.StringVarP(&opts.Datasource, "datasource", "d", "", "Datasource UID (required unless datasources.infinity is configured)")
}

func (opts *queryOpts) Validate() error {
	return opts.SharedOpts.Validate()
}

// QueryCmd returns the `query` subcommand for an Infinity datasource parent.
func QueryCmd(loader *providers.ConfigLoader) *cobra.Command {
	opts := &queryOpts{}

	cmd := &cobra.Command{
		Use:   "query [EXPR]",
		Short: "Query a pre-configured Infinity datasource",
		Long: `Query a Grafana Infinity datasource using its saved configuration.

The datasource's URL, type, method, and headers are read from its saved configuration.
EXPR is an optional root selector expression (JSONPath for JSON, XPath for XML/HTML)
that narrows the returned data.

Datasource is resolved from -d flag or datasources.infinity in your context.`,
		Example: `
  # Query using datasource UID
  gcx datasources infinity query -d UID

  # Narrow results with a JSONPath expression
  gcx datasources infinity query -d UID '$.items'

  # Output as JSON
  gcx datasources infinity query -d UID '$.results' -o json

  # Query with a time range
  gcx datasources infinity query -d UID --from now-24h --to now`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}

			var expr string
			if len(args) == 1 {
				expr = args[0]
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

			datasourceUID, err := dsquery.ResolveAndSaveDatasource(ctx, loader, opts.Datasource, cfgCtx, cfg, "infinity")
			if err != nil {
				return err
			}

			dsType, err := dsquery.GetDatasourceType(ctx, cfg, datasourceUID)
			if err != nil {
				return err
			}
			if err := dsquery.ValidateDatasourceType(dsType, "infinity"); err != nil {
				return err
			}

			now := time.Now()
			start, end, err := opts.ParseTimeRange(now)
			if err != nil {
				return err
			}

			client, err := infinity.NewClient(cfg)
			if err != nil {
				return fmt.Errorf("failed to create client: %w", err)
			}

			req := infinity.QueryRequest{
				Expr:  expr,
				Start: start,
				End:   end,
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
		agent.AnnotationLLMHint:   `gcx datasources infinity query -d UID '$.items' -o json`,
	}

	opts.setup(cmd.Flags())

	return cmd
}
