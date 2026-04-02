package traces

import (
	"errors"
	"fmt"
	"time"

	internalconfig "github.com/grafana/gcx/internal/config"
	dsquery "github.com/grafana/gcx/internal/datasources/query"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/query/tempo"
	"github.com/spf13/cobra"
)

// searchCmd returns the `search` subcommand for a Tempo datasource parent.
func searchCmd(loader *providers.ConfigLoader) *cobra.Command {
	shared := &dsquery.SharedOpts{}
	var limit int

	cmd := &cobra.Command{
		Use:   "search [DATASOURCE_UID] TRACEQL",
		Short: "Search for traces using a TraceQL query",
		Long: `Search for traces using a TraceQL query against a Tempo datasource.

DATASOURCE_UID is optional when datasources.tempo is configured in your context.
TRACEQL is the TraceQL expression to evaluate.`,
		Example: `
  # Search traces using configured default datasource
  gcx traces search '{ span.http.status_code >= 500 }'

  # Search with explicit datasource UID and time range
  gcx traces search tempo-001 '{ span.http.status_code >= 500 }' --since 1h

  # With custom limit
  gcx traces search tempo-001 '{ span.http.status_code >= 500 }' --since 1h --limit 50

  # Output as JSON
  gcx traces search tempo-001 '{ span.http.status_code >= 500 }' -o json`,
		Args: validateSearchArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := shared.Validate(); err != nil {
				return err
			}

			ctx := cmd.Context()

			// Resolve default UID from config.
			var defaultUID string
			fullCfg, err := loader.LoadFullConfig(ctx)
			if err == nil {
				defaultUID = internalconfig.DefaultDatasourceUID(*fullCfg.GetCurrentContext(), "tempo")
			}

			datasourceUID, expr, err := dsquery.ResolveTypedArgs(args, defaultUID, "tempo")
			if err != nil {
				return err
			}

			cfg, err := loader.LoadGrafanaConfig(ctx)
			if err != nil {
				return err
			}

			dsType, err := dsquery.GetDatasourceType(ctx, cfg, datasourceUID)
			if err != nil {
				return err
			}
			if err := dsquery.ValidateDatasourceType(dsType, "tempo"); err != nil {
				return err
			}

			now := time.Now()
			start, end, _, err := shared.ParseTimes(now)
			if err != nil {
				return err
			}

			client, err := tempo.NewClient(cfg)
			if err != nil {
				return fmt.Errorf("failed to create client: %w", err)
			}

			req := tempo.SearchRequest{
				Query: expr,
				Start: start,
				End:   end,
				Limit: limit,
			}

			resp, err := client.Search(ctx, datasourceUID, req)
			if err != nil {
				return fmt.Errorf("search failed: %w", err)
			}

			switch shared.IO.OutputFormat {
			case "table":
				return tempo.FormatSearchTable(cmd.OutOrStdout(), resp)
			case "wide":
				return tempo.FormatSearchTable(cmd.OutOrStdout(), resp)
			default:
				return shared.IO.Encode(cmd.OutOrStdout(), resp)
			}
		},
	}

	shared.Setup(cmd.Flags())
	cmd.Flags().IntVar(&limit, "limit", 20, "Maximum number of traces to return (0 means no limit)")

	return cmd
}

func validateSearchArgs(_ *cobra.Command, args []string) error {
	switch len(args) {
	case 0:
		return errors.New("TRACEQL is required")
	case 1, 2:
		return nil
	default:
		return errors.New("too many arguments: expected [DATASOURCE_UID] TRACEQL")
	}
}
