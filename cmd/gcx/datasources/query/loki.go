package query

import (
	"fmt"
	"time"

	cmdconfig "github.com/grafana/gcx/cmd/gcx/config"
	"github.com/grafana/gcx/cmd/gcx/fail"
	"github.com/grafana/gcx/internal/query/loki"
	"github.com/spf13/cobra"
)

// LokiCmd returns the `query` subcommand for a Loki datasource parent.
func LokiCmd(configOpts *cmdconfig.Options) *cobra.Command {
	shared := &sharedQueryOpts{}
	var limit int

	cmd := &cobra.Command{
		Use:   "query [DATASOURCE_UID] EXPR",
		Short: "Execute a LogQL query against a Loki datasource",
		Long: `Execute a LogQL query against a Loki datasource.

DATASOURCE_UID is optional when datasources.loki is configured in your context.
EXPR is the LogQL expression to evaluate.`,
		Example: `
  # Log query using configured default datasource
  gcx datasources loki query '{job="varlogs"}'

  # Query logs from the last hour with explicit datasource UID
  gcx datasources loki query loki-001 '{job="varlogs"}' --since 1h

  # With custom limit
  gcx datasources loki query loki-001 '{job="varlogs"}' --since 1h --limit 500

  # No limit (return all matching log lines)
  gcx datasources loki query loki-001 '{job="varlogs"}' --limit 0

  # Output as JSON
  gcx datasources loki query loki-001 '{job="varlogs"}' -o json`,
		Args: validateLokiQueryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := shared.Validate(); err != nil {
				return err
			}

			ctx := cmd.Context()

			datasourceUID, expr, err := resolveTypedArgs(args, configOpts, ctx, "loki")
			if err != nil {
				return err
			}

			if err := validateDatasourceType(ctx, configOpts, datasourceUID, "loki"); err != nil {
				return err
			}

			cfg, err := configOpts.LoadGrafanaConfig(ctx)
			if err != nil {
				return err
			}

			now := time.Now()
			start, end, step, err := shared.parseTimes(now)
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

	shared.setup(cmd.Flags())
	cmd.Flags().IntVar(&limit, "limit", 1000, "Maximum number of log lines to return (0 means no limit)")

	return cmd
}

func validateLokiQueryArgs(cmd *cobra.Command, args []string) error {
	switch len(args) {
	case 0:
		return fail.NewCommandUsageError(cmd, "EXPR is required", nil)
	case 1, 2:
		return nil
	default:
		return fail.NewCommandUsageError(cmd, "too many arguments: expected [DATASOURCE_UID] EXPR", nil)
	}
}
