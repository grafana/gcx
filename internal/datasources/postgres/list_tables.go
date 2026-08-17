package postgres

import (
	"errors"
	"fmt"
	"io"

	"github.com/grafana/gcx/internal/agent"
	dsquery "github.com/grafana/gcx/internal/datasources/query"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/query/postgres"
	querysql "github.com/grafana/gcx/internal/query/sql"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// listTablesRowCap is the maximum number of tables returned in one call.
const listTablesRowCap = 500

type listTablesOpts struct {
	IO         cmdio.Options
	Datasource string
	Schema     string
}

func (opts *listTablesOpts) setup(flags *pflag.FlagSet) {
	dsquery.RegisterCodecs(&opts.IO, false)
	opts.IO.BindFlags(flags)
	flags.StringVarP(&opts.Datasource, "datasource", "d", "", "Datasource UID (required unless datasources.postgres is configured)")
	flags.StringVar(&opts.Schema, "schema", "", "Filter tables to this schema")
}

func (opts *listTablesOpts) Validate() error {
	return opts.IO.Validate()
}

// buildListTablesSQL builds the information_schema query for list-tables,
// optionally filtered to schema. It requests rowCap+1 rows so the caller can
// detect and warn on truncation rather than reporting a capped result as complete.
func buildListTablesSQL(schema string, rowCap int) string {
	sql := "SELECT table_schema AS schema, table_name AS name, table_type AS type FROM information_schema.tables WHERE table_schema NOT IN ('pg_catalog', 'information_schema')"
	if schema != "" {
		sql += fmt.Sprintf(" AND table_schema = '%s'", postgres.EscapeSQLString(schema))
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
	cmdio.Warning(warnw, "showing the first %d tables; more tables match — use --schema to narrow the results", rowCap)
}

// ListTablesCmd returns the `list-tables` subcommand for a PostgreSQL datasource parent.
func ListTablesCmd(loader *providers.ConfigLoader) *cobra.Command {
	opts := &listTablesOpts{}

	cmd := &cobra.Command{
		Use:   "list-tables",
		Short: "List tables from a PostgreSQL datasource",
		Long: `List tables and views from all non-system schemas, or filter to a specific schema.

Shows schema, name, and type for each table.`,
		Example: `
  # List all tables
  gcx datasources postgres list-tables -d UID

  # Filter to a specific schema
  gcx datasources postgres list-tables --schema public

  # Output as JSON
  gcx datasources postgres list-tables -o json`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.Validate(); err != nil {
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

			if cmd.Flags().Changed("schema") && opts.Schema == "" {
				return errors.New("--schema must not be empty")
			}
			if err := postgres.ValidateIdentifier(opts.Schema, "schema"); err != nil {
				return err
			}

			sql := buildListTablesSQL(opts.Schema, listTablesRowCap)

			client, err := postgres.NewClient(cfg)
			if err != nil {
				return fmt.Errorf("failed to create client: %w", err)
			}

			resp, err := client.Query(ctx, datasourceUID, postgres.QueryRequest{RawSQL: sql})
			if err != nil {
				return fmt.Errorf("query failed: %w", err)
			}

			warnIfTruncated(cmd.ErrOrStderr(), resp, listTablesRowCap)

			return opts.IO.Encode(cmd.OutOrStdout(), resp)
		},
	}

	cmd.Annotations = map[string]string{
		agent.AnnotationTokenCost: "small",
		agent.AnnotationLLMHint:   `gcx datasources postgres list-tables -d UID`,
	}

	opts.setup(cmd.Flags())

	return cmd
}
