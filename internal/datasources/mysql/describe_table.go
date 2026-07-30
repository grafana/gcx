package mysql

import (
	"fmt"
	"strings"

	"github.com/grafana/gcx/internal/agent"
	dsquery "github.com/grafana/gcx/internal/datasources/query"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/query/mysql"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type describeTableOpts struct {
	IO         cmdio.Options
	Datasource string
	Database   string
}

// splitTableArg resolves a possibly database-qualified TABLE argument
// (db.table, the MySQL idiom) against the --database flag, returning the
// effective database and bare table name.
func splitTableArg(arg, databaseFlag string) (string, string, error) {
	parts := strings.Split(arg, ".")
	switch {
	case len(parts) == 1:
		return databaseFlag, arg, nil
	case len(parts) == 2 && parts[0] != "" && parts[1] != "":
		if databaseFlag != "" && databaseFlag != parts[0] {
			return "", "", fmt.Errorf("database %q from the table argument conflicts with --database %q", parts[0], databaseFlag)
		}
		return parts[0], parts[1], nil
	default:
		return "", "", fmt.Errorf("invalid table %q: use TABLE or DATABASE.TABLE", arg)
	}
}

func (opts *describeTableOpts) setup(flags *pflag.FlagSet) {
	dsquery.RegisterCodecs(&opts.IO, false)
	opts.IO.BindFlags(flags)
	flags.StringVarP(&opts.Datasource, "datasource", "d", "", "Datasource UID (required unless datasources.mysql is configured)")
	flags.StringVar(&opts.Database, "database", "", "Database of the table (defaults to all databases)")
}

func (opts *describeTableOpts) Validate() error {
	return opts.IO.Validate()
}

// DescribeTableCmd returns the `describe-table` subcommand for a MySQL datasource parent.
func DescribeTableCmd(loader *providers.ConfigLoader) *cobra.Command {
	opts := &describeTableOpts{}

	cmd := &cobra.Command{
		Use:   "describe-table TABLE",
		Short: "Show the columns of a MySQL table",
		Long: `Show the columns of a MySQL table: name, column type, nullability, and default.

The table can be database-qualified (db.table); otherwise use --database to
disambiguate when the same table name exists in multiple databases.`,
		Example: `
  # Describe a table
  gcx datasources mysql describe-table orders -d UID

  # Disambiguate by database
  gcx datasources mysql describe-table mydb.orders
  gcx datasources mysql describe-table orders --database mydb

  # Output as JSON
  gcx datasources mysql describe-table orders -o json`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}

			database, table, err := splitTableArg(args[0], opts.Database)
			if err != nil {
				return err
			}
			if err := mysql.ValidateIdentifier(table, "table"); err != nil {
				return err
			}
			if err := mysql.ValidateIdentifier(database, "database"); err != nil {
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

			sql := fmt.Sprintf(
				"SELECT table_schema AS `database`, column_name AS name, column_type AS type, is_nullable AS nullable, column_default AS `default` FROM information_schema.columns WHERE table_name = '%s'",
				mysql.EscapeSQLString(table),
			)
			if database != "" {
				sql += fmt.Sprintf(" AND table_schema = '%s'", mysql.EscapeSQLString(database))
			} else {
				// Same system-schema exclusion as list-tables, so an unscoped
				// describe never returns system-catalog columns.
				sql += " AND table_schema NOT IN ('mysql', 'information_schema', 'performance_schema', 'sys')"
			}
			sql += " ORDER BY table_schema, ordinal_position"

			client, err := mysql.NewClient(cfg)
			if err != nil {
				return fmt.Errorf("failed to create client: %w", err)
			}

			resp, err := client.Query(ctx, datasourceUID, mysql.QueryRequest{RawSQL: sql})
			if err != nil {
				return fmt.Errorf("query failed: %w", err)
			}

			if len(resp.Rows) == 0 {
				if database != "" {
					return fmt.Errorf("table %q not found in database %q", table, database)
				}
				return fmt.Errorf("table %q not found", table)
			}

			return opts.IO.Encode(cmd.OutOrStdout(), resp)
		},
	}

	cmd.Annotations = map[string]string{
		agent.AnnotationTokenCost: "small",
		agent.AnnotationLLMHint:   `gcx datasources mysql describe-table orders -d UID`,
	}

	opts.setup(cmd.Flags())

	return cmd
}
