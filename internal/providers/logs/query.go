package logs

import (
	"errors"
	"fmt"
	"time"

	internalconfig "github.com/grafana/gcx/internal/config"
	dsquery "github.com/grafana/gcx/internal/datasources/query"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/query/loki"
	"github.com/spf13/cobra"
)

// queryCmd returns the `query` subcommand for a Loki datasource parent.
func queryCmd(loader *providers.ConfigLoader) *cobra.Command {
	shared := &dsquery.SharedOpts{}
	var limit int

	cmd := &cobra.Command{
		Use:   "query [DATASOURCE_UID] EXPR",
		Short: "Execute a LogQL query against a Loki datasource",
		Long: `Execute a LogQL query against a Loki datasource.

DATASOURCE_UID is optional when datasources.loki is configured in your context.
EXPR is the LogQL expression to evaluate.`,
		Example: `
  # Log query using configured default datasource
  gcx logs query '{job="varlogs"}'

  # Query logs from the last hour with explicit datasource UID
  gcx logs query loki-001 '{job="varlogs"}' --since 1h

  # With custom limit
  gcx logs query loki-001 '{job="varlogs"}' --since 1h --limit 500

  # No limit (return all matching log lines)
  gcx logs query loki-001 '{job="varlogs"}' --limit 0

  # Output as JSON
  gcx logs query loki-001 '{job="varlogs"}' -o json`,
		Args: validateLokiQueryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := shared.Validate(); err != nil {
				return err
			}

			ctx := cmd.Context()

			// Resolve default UID from config.
			var defaultUID string
			fullCfg, err := loader.LoadFullConfig(ctx)
			if err == nil {
				defaultUID = internalconfig.DefaultDatasourceUID(*fullCfg.GetCurrentContext(), "loki")
			}

			datasourceUID, expr, err := dsquery.ResolveTypedArgs(args, defaultUID, "loki")
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
			if err := dsquery.ValidateDatasourceType(dsType, "loki"); err != nil {
				return err
			}

			now := time.Now()
			start, end, step, err := shared.ParseTimes(now)
			if err != nil {
				return err
			}

			client, err := loki.NewClient(cfg)
			if err != nil {
				return fmt.Errorf("failed to create client: %w", err)
			}

			req := loki.QueryRequest{
				Query: expr,
				Start: start,
				End:   end,
				Step:  step,
				Limit: limit,
			}

			resp, err := client.Query(ctx, datasourceUID, req)
			if err != nil {
				return fmt.Errorf("query failed: %w", err)
			}

			switch shared.IO.OutputFormat {
			case "table":
				return loki.FormatQueryTable(cmd.OutOrStdout(), resp)
			case "wide":
				return loki.FormatQueryTableWide(cmd.OutOrStdout(), resp)
			default:
				return shared.IO.Encode(cmd.OutOrStdout(), resp)
			}
		},
	}

	shared.Setup(cmd.Flags())
	cmd.Flags().IntVar(&limit, "limit", 1000, "Maximum number of log lines to return (0 means no limit)")

	return cmd
}

func validateLokiQueryArgs(_ *cobra.Command, args []string) error {
	switch len(args) {
	case 0:
		return errors.New("EXPR is required")
	case 1, 2:
		return nil
	default:
		return errors.New("too many arguments: expected [DATASOURCE_UID] EXPR")
	}
}
