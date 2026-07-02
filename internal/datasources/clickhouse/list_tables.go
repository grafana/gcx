package clickhouse

import (
	"fmt"

	"github.com/grafana/gcx/internal/agent"
	dsquery "github.com/grafana/gcx/internal/datasources/query"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/query/clickhouse"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type listTablesOpts struct {
	IO         cmdio.Options
	Datasource string
	Database   string
}

func (opts *listTablesOpts) setup(flags *pflag.FlagSet) {
	dsquery.RegisterCodecs(&opts.IO, false)
	opts.IO.BindFlags(flags)
	flags.StringVarP(&opts.Datasource, "datasource", "d", "", "Datasource UID (required unless datasources.clickhouse is configured)")
	flags.StringVar(&opts.Database, "database", "", "Filter tables to this database")
}

func (opts *listTablesOpts) Validate() error {
	return opts.IO.Validate()
}

// ListTablesCmd returns the `list-tables` subcommand for a ClickHouse datasource parent.
func ListTablesCmd(loader *providers.ConfigLoader) *cobra.Command {
	opts := &listTablesOpts{}

	cmd := &cobra.Command{
		Use:   "list-tables",
		Short: "List tables from a ClickHouse datasource",
		Long: `List tables from all non-system databases, or filter to a specific database.

Shows database, name, engine, total_rows, and total_bytes for each table.`,
		Example: `
  # List all tables
  gcx datasources clickhouse list-tables

  # Filter to a specific database
  gcx datasources clickhouse list-tables --database otel

  # Output as JSON
  gcx datasources clickhouse list-tables -o json`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}

			ctx := cmd.Context()

			// Resolve datasource UID from -d flag, config, or Grafana auto-discovery.
			cfgCtx, cfg, err := dsquery.LoadContextAndConfig(ctx, loader)
			if err != nil {
				return err
			}

			datasourceUID, _, err := dsquery.ResolveValidateAndSaveDatasource(ctx, loader, opts.Datasource, cfgCtx, cfg, "clickhouse")
			if err != nil {
				return err
			}

			if err := clickhouse.ValidateIdentifier(opts.Database, "database"); err != nil {
				return err
			}

			sql := "SELECT database, name, engine, total_rows, total_bytes FROM system.tables WHERE database NOT IN ('system', 'INFORMATION_SCHEMA', 'information_schema')"
			if opts.Database != "" {
				sql += fmt.Sprintf(" AND database = '%s'", clickhouse.EscapeSQLString(opts.Database))
			}
			sql += " ORDER BY database, name LIMIT 500"

			client, err := clickhouse.NewClient(cfg)
			if err != nil {
				return fmt.Errorf("failed to create client: %w", err)
			}

			resp, err := client.Query(ctx, datasourceUID, clickhouse.QueryRequest{RawSQL: sql})
			if err != nil {
				return fmt.Errorf("query failed: %w", err)
			}

			tables := clickhouse.ParseTableInfoRows(resp)
			return opts.IO.Encode(cmd.OutOrStdout(), tables)
		},
	}

	cmd.Annotations = map[string]string{
		agent.AnnotationTokenCost: "small",
		agent.AnnotationLLMHint:   `gcx datasources clickhouse list-tables -d UID -o json`,
	}

	opts.setup(cmd.Flags())

	return cmd
}
