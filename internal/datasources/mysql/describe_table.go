package mysql

import (
	"errors"
	"fmt"
	"strings"

	"github.com/grafana/gcx/internal/agent"
	dsquery "github.com/grafana/gcx/internal/datasources/query"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/query/mysql"
	querysql "github.com/grafana/gcx/internal/query/sql"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type describeTableOpts struct {
	IO                 cmdio.Options
	Datasource         string
	Database           string
	IncludeConstraints bool
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
	flags.StringVar(&opts.Database, "database", "", "Database of the table (exact match, case-sensitive; defaults to all databases)")
	flags.BoolVar(&opts.IncludeConstraints, "include-constraints", false, "Include ordered constraint and foreign-key metadata (requires an explicit database and JSON/YAML output)")
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
disambiguate when the same table name exists in multiple databases. TABLE and
--database both match exactly and are case-sensitive, which can differ from
how information_schema itself compares names depending on the server's
platform and lower_case_table_names setting.

Use --include-constraints with a database-qualified table (or --database) to
also return a structured table identity, columns, and ordered constraint
metadata. Constraint metadata requires -o json or -o yaml.`,
		Example: `
  # Describe a table
  gcx datasources mysql describe-table orders -d UID

  # Disambiguate by database
  gcx datasources mysql describe-table mydb.orders
  gcx datasources mysql describe-table orders --database mydb

	  # Output as JSON
	  gcx datasources mysql describe-table orders -o json

	  # Include ordered keys and foreign-key relationships
	  gcx datasources mysql describe-table mydb.orders --include-constraints -o json`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}

			if cmd.Flags().Changed("database") && opts.Database == "" {
				return errors.New("--database must not be empty")
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
			if opts.IncludeConstraints {
				if database == "" {
					return errors.New("--include-constraints requires an explicit database (use DATABASE.TABLE or --database)")
				}
				if opts.IO.OutputFormat != "json" && opts.IO.OutputFormat != "yaml" && opts.IO.OutputFormat != "agents" {
					return errors.New("--include-constraints requires JSON or YAML output (use -o json or -o yaml)")
				}
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

			if opts.IncludeConstraints {
				constraintsResp, err := client.Query(ctx, datasourceUID, mysql.QueryRequest{RawSQL: mysql.BuildDescribeConstraintsQuery(database, table)})
				if err != nil {
					return fmt.Errorf("query constraints failed: %w", err)
				}
				description, err := querysql.ParseTableDescription(database, table, resp, constraintsResp)
				if err != nil {
					return fmt.Errorf("parse table description: %w", err)
				}
				return opts.IO.Encode(cmd.OutOrStdout(), description)
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
