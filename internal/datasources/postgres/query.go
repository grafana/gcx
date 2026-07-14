package postgres

import (
	"fmt"
	"time"

	"github.com/grafana/gcx/internal/agent"
	dsquery "github.com/grafana/gcx/internal/datasources/query"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/query/postgres"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

const (
	defaultLimit = 100
	maxLimit     = 1000
)

type queryOpts struct {
	dsquery.SharedOpts

	Datasource string
	Limit      int
}

func (opts *queryOpts) setup(flags *pflag.FlagSet) {
	opts.Setup(flags, false)
	flags.StringVarP(&opts.Datasource, "datasource", "d", "", "Datasource UID (required unless datasources.postgres is configured)")
	flags.IntVar(&opts.Limit, "limit", defaultLimit, "Max rows to return (0 disables enforcement)")
}

func (opts *queryOpts) Validate() error {
	return opts.SharedOpts.Validate()
}

// QueryCmd returns the `query` subcommand for a PostgreSQL datasource parent.
func QueryCmd(loader *providers.ConfigLoader) *cobra.Command {
	opts := &queryOpts{}

	cmd := &cobra.Command{
		Use:   "query [EXPR]",
		Short: "Execute a SQL query against a PostgreSQL datasource",
		Long: `Execute a SQL query against a PostgreSQL datasource.

EXPR is the SQL query to execute, passed as a positional argument or via --expr.
Datasource is resolved from -d flag or datasources.postgres in your context.
Server-side macros ($__timeFilter, $__timeGroup, etc.) are supported.`,
		Example: `
  # Simple query
  gcx datasources postgres query 'SELECT count(*) FROM orders'

  # With time macro and explicit datasource
  gcx datasources postgres query -d UID 'SELECT * FROM events WHERE $__timeFilter(created_at)' --since 1h

  # Output as JSON
  gcx datasources postgres query -d UID 'SELECT 1' -o json

  # Disable limit enforcement
  gcx datasources postgres query 'SELECT * FROM big_table' --limit 0`,
		Args: cobra.RangeArgs(0, 1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}

			expr, err := opts.ResolveExpr(args, 0)
			if err != nil {
				return err
			}

			ctx := cmd.Context()

			cfgCtx, cfg, err := dsquery.LoadContextAndConfig(ctx, loader)
			if err != nil {
				return err
			}

			datasourceUID, _, err := dsquery.ResolveValidateAndSaveDatasource(ctx, loader, opts.Datasource, cfgCtx, cfg, "postgres")
			if err != nil {
				return err
			}

			sql := postgres.EnforceLimit(expr, opts.Limit, maxLimit)

			now := time.Now()
			start, end, step, err := opts.ParseTimes(now)
			if err != nil {
				return err
			}

			var intervalMs int64
			if step > 0 {
				intervalMs = step.Milliseconds()
			}

			client, err := postgres.NewClient(cfg)
			if err != nil {
				return fmt.Errorf("failed to create client: %w", err)
			}

			resp, err := client.Query(ctx, datasourceUID, postgres.QueryRequest{
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
		agent.AnnotationLLMHint:   `gcx datasources postgres query -d UID 'SELECT count(*) FROM orders'`,
	}

	opts.setup(cmd.Flags())

	return cmd
}
