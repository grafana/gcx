package mysql

import (
	"errors"
	"fmt"
	"io"

	"github.com/grafana/gcx/internal/agent"
	dsquery "github.com/grafana/gcx/internal/datasources/query"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/query/mysql"
	querysql "github.com/grafana/gcx/internal/query/sql"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// listTablesRowCap is the maximum number of tables returned in one call.
const listTablesRowCap = 500

type listTablesOpts struct {
	IO         cmdio.Options
	Datasource string
	Database   string
}

func (opts *listTablesOpts) setup(flags *pflag.FlagSet) {
	dsquery.RegisterCodecs(&opts.IO, false)
	opts.IO.BindFlags(flags)
	flags.StringVarP(&opts.Datasource, "datasource", "d", "", "Datasource UID (required unless datasources.mysql is configured)")
	flags.StringVar(&opts.Database, "database", "", "Filter tables to this database (exact match, case-sensitive)")
}

func (opts *listTablesOpts) Validate() error {
	return opts.IO.Validate()
}

// buildListTablesSQL builds the information_schema query for list-tables,
// optionally filtered to database. It requests rowCap+1 rows so the caller
// can detect and warn on truncation rather than reporting a capped result
// as complete.
func buildListTablesSQL(database string, rowCap int) string {
	sql := "SELECT table_schema AS `database`, table_name AS name, table_type AS type FROM information_schema.tables WHERE table_schema NOT IN ('mysql', 'information_schema', 'performance_schema', 'sys')"
	if database != "" {
		sql += fmt.Sprintf(" AND table_schema = '%s'", mysql.EscapeSQLString(database))
	}
	sql += fmt.Sprintf(" ORDER BY table_schema, table_name LIMIT %d", rowCap+1)
	return sql
}

// warnIfTruncated drops rows beyond rowCap (fetched via the rowCap+1 request
// in buildListTablesSQL) and warns on warnw that more tables matched, so a
// capped result never reads as the complete inventory.
func warnIfTruncated(warnw io.Writer, resp *querysql.QueryResponse, rowCap int) {
	if len(resp.Rows) <= rowCap {
		return
	}
	resp.Rows = resp.Rows[:rowCap]
	cmdio.Warning(warnw, "showing the first %d tables; more tables match — use --database to narrow the results", rowCap)
}

// ListTablesCmd returns the `list-tables` subcommand for a MySQL datasource parent.
func ListTablesCmd(loader *providers.ConfigLoader) *cobra.Command {
	opts := &listTablesOpts{}

	cmd := &cobra.Command{
		Use:   "list-tables",
		Short: "List tables from a MySQL datasource",
		Long: `List tables and views from all non-system databases, or filter to a specific database.

Shows database, name, and type for each table. --database matches exactly and
is case-sensitive.`,
		Example: `
  # List all tables
  gcx datasources mysql list-tables -d UID

  # Filter to a specific database
  gcx datasources mysql list-tables --database mydb

  # Output as JSON
  gcx datasources mysql list-tables -o json`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}

			if cmd.Flags().Changed("database") && opts.Database == "" {
				return errors.New("--database must not be empty")
			}
			if err := mysql.ValidateIdentifier(opts.Database, "database"); err != nil {
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

			sql := buildListTablesSQL(opts.Database, listTablesRowCap)

			client, err := mysql.NewClient(cfg)
			if err != nil {
				return fmt.Errorf("failed to create client: %w", err)
			}

			resp, err := client.Query(ctx, datasourceUID, mysql.QueryRequest{RawSQL: sql})
			if err != nil {
				return fmt.Errorf("query failed: %w", err)
			}

			warnIfTruncated(cmd.ErrOrStderr(), resp, listTablesRowCap)

			return opts.IO.Encode(cmd.OutOrStdout(), resp)
		},
	}

	cmd.Annotations = map[string]string{
		agent.AnnotationTokenCost: "small",
		agent.AnnotationLLMHint:   `gcx datasources mysql list-tables -d UID`,
	}

	opts.setup(cmd.Flags())

	return cmd
}
