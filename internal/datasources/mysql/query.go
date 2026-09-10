package mysql

import (
	"fmt"
	"time"

	"github.com/grafana/gcx/internal/agent"
	dsquery "github.com/grafana/gcx/internal/datasources/query"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/query/mysql"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

const (
	defaultLimit = 100
	maxLimit     = 1000
)

type queryOpts struct {
	dsquery.SQLQueryOpts

	Datasource string
	Limit      int
}

func (opts *queryOpts) setup(flags *pflag.FlagSet) {
	opts.Setup(flags, false)
	flags.StringVarP(&opts.Datasource, "datasource", "d", "", "Datasource UID (required unless datasources.mysql is configured)")
	flags.IntVar(&opts.Limit, "limit", defaultLimit, fmt.Sprintf("Max rows to return; requests above %d are capped, with a warning (0 disables enforcement)", maxLimit))
}

func (opts *queryOpts) Validate() error {
	if opts.Limit < 0 {
		return fmt.Errorf("--limit must be >= 0, got %d", opts.Limit)
	}
	return opts.SQLQueryOpts.Validate()
}

// QueryCmd returns the `query` subcommand for a MySQL datasource parent.
func QueryCmd(loader *providers.ConfigLoader) *cobra.Command {
	opts := &queryOpts{}

	cmd := &cobra.Command{
		Use:   "query [EXPR]",
		Short: "Execute a SQL query against a MySQL datasource",
		Long: `Execute a SQL query against a MySQL datasource.

EXPR is the SQL query to execute, passed as a positional argument, via --expr,
or via --query-file. Use --query-file - to read SQL from stdin.
Datasource is resolved from -d flag or datasources.mysql in your context.
Server-side macros ($__timeFilter, $__timeGroup, etc.) are supported.`,
		Example: `
  # Simple query
  gcx datasources mysql query 'SELECT count(*) FROM orders'

  # With time macro and explicit datasource
  gcx datasources mysql query -d UID 'SELECT * FROM events WHERE $__timeFilter(created_at)' --since 1h

  # Output as JSON
  gcx datasources mysql query -d UID 'SELECT 1' -o json

  # Read a long query from a file
  gcx datasources mysql query -d UID --query-file ./query.sql -o json

  # Disable limit enforcement
  gcx datasources mysql query 'SELECT * FROM big_table' --limit 0`,
		Args: cobra.RangeArgs(0, 1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}

			expr, err := opts.ResolveExpr(args, 0, cmd.InOrStdin(), cmd.Flags().Changed("query-file"))
			if err != nil {
				return err
			}

			ctx := cmd.Context()

			cfgCtx, cfg, err := dsquery.LoadContextAndConfig(ctx, loader)
			if err != nil {
				return err
			}

			datasourceUID, _, err := dsquery.ResolveValidateAndSaveDatasource(ctx, loader, opts.Datasource, cfgCtx, cfg, "mysql")
			if err != nil {
				return err
			}

			sql, capped := mysql.EnforceLimit(expr, opts.Limit, maxLimit)
			if capped {
				cmdio.Warning(cmd.ErrOrStderr(), "LIMIT in query exceeds the maximum of %d and was capped; use --limit 0 to disable enforcement", maxLimit)
			}

			now := time.Now()
			start, end, step, err := opts.ParseTimes(now)
			if err != nil {
				return err
			}

			var intervalMs int64
			if step > 0 {
				intervalMs = step.Milliseconds()
			}

			client, err := mysql.NewClient(cfg)
			if err != nil {
				return fmt.Errorf("failed to create client: %w", err)
			}

			resp, err := client.Query(ctx, datasourceUID, mysql.QueryRequest{
				RawSQL:     sql,
				Start:      start,
				End:        end,
				IntervalMs: intervalMs,
			})
			if err != nil {
				return fmt.Errorf("query failed: %w", err)
			}

			return opts.IO.Encode(cmd.OutOrStdout(), resp)
		},
	}

	cmd.Annotations = map[string]string{
		agent.AnnotationTokenCost: "medium",
		agent.AnnotationLLMHint:   `gcx datasources mysql query -d UID 'SELECT count(*) FROM orders'`,
	}

	opts.setup(cmd.Flags())

	return cmd
}
