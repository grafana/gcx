package postgres

import (
	"errors"
	"fmt"
	"strings"

	"github.com/grafana/gcx/internal/agent"
	dsquery "github.com/grafana/gcx/internal/datasources/query"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/query/postgres"
	querysql "github.com/grafana/gcx/internal/query/sql"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type describeTableOpts struct {
	IO                 cmdio.Options
	Datasource         string
	Schema             string
	IncludeConstraints bool
}

// splitTableArg resolves a possibly schema-qualified TABLE argument
// (schema.table, the Postgres idiom) against the --schema flag, returning
// the effective schema and bare table name.
func splitTableArg(arg, schemaFlag string) (string, string, error) {
	parts := strings.Split(arg, ".")
	switch {
	case len(parts) == 1:
		return schemaFlag, arg, nil
	case len(parts) == 2 && parts[0] != "" && parts[1] != "":
		if schemaFlag != "" && schemaFlag != parts[0] {
			return "", "", fmt.Errorf("schema %q from the table argument conflicts with --schema %q", parts[0], schemaFlag)
		}
		return parts[0], parts[1], nil
	default:
		return "", "", fmt.Errorf("invalid table %q: use TABLE or SCHEMA.TABLE", arg)
	}
}

func (opts *describeTableOpts) setup(flags *pflag.FlagSet) {
	dsquery.RegisterCodecs(&opts.IO, false)
	opts.IO.BindFlags(flags)
	flags.StringVarP(&opts.Datasource, "datasource", "d", "", "Datasource UID (required unless datasources.postgres is configured)")
	flags.StringVar(&opts.Schema, "schema", "", "Schema of the table (exact match, case-sensitive; defaults to all schemas)")
	flags.BoolVar(&opts.IncludeConstraints, "include-constraints", false, "Include ordered constraint and foreign-key metadata (requires an explicit schema and JSON/YAML output)")
}

func (opts *describeTableOpts) Validate() error {
	return opts.IO.Validate()
}

// DescribeTableCmd returns the `describe-table` subcommand for a PostgreSQL datasource parent.
func DescribeTableCmd(loader *providers.ConfigLoader) *cobra.Command {
	opts := &describeTableOpts{}

	cmd := &cobra.Command{
		Use:   "describe-table TABLE",
		Short: "Show the columns of a PostgreSQL table",
		Long: `Show the columns of a PostgreSQL table: name, data type, nullability, and default.

The table can be schema-qualified (schema.table); otherwise use --schema to
disambiguate when the same table name exists in multiple schemas.

Use --include-constraints with a schema-qualified table (or --schema) to also
return a structured table identity, columns, and ordered constraint metadata.
Constraint metadata requires -o json or -o yaml.`,
		Example: `
  # Describe a table
  gcx datasources postgres describe-table orders -d UID

  # Disambiguate by schema
  gcx datasources postgres describe-table public.orders
  gcx datasources postgres describe-table orders --schema public

  # Output as JSON
  gcx datasources postgres describe-table orders -o json

  # Include ordered keys and foreign-key relationships
  gcx datasources postgres describe-table public.orders --include-constraints -o json`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}

			if cmd.Flags().Changed("schema") && opts.Schema == "" {
				return errors.New("--schema must not be empty")
			}

			schema, table, err := splitTableArg(args[0], opts.Schema)
			if err != nil {
				return err
			}
			if err := postgres.ValidateIdentifier(table, "table"); err != nil {
				return err
			}
			if err := postgres.ValidateIdentifier(schema, "schema"); err != nil {
				return err
			}
			if opts.IncludeConstraints {
				if schema == "" {
					return errors.New("--include-constraints requires an explicit schema (use SCHEMA.TABLE or --schema)")
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

			datasourceUID, _, err := dsquery.ResolveValidateAndSaveDatasource(ctx, loader, opts.Datasource, cfgCtx, cfg, "postgres")
			if err != nil {
				return err
			}

			sql := fmt.Sprintf(
				"SELECT table_schema AS schema, column_name AS name, data_type AS type, is_nullable AS nullable, column_default AS default FROM information_schema.columns WHERE table_name = '%s'",
				postgres.EscapeSQLString(table),
			)
			if schema != "" {
				sql += fmt.Sprintf(" AND table_schema = '%s'", postgres.EscapeSQLString(schema))
			} else {
				// Same system-schema exclusion as list-tables, so an unscoped
				// describe never returns pg_catalog/information_schema columns.
				sql += " AND table_schema NOT IN ('pg_catalog', 'information_schema')"
			}
			sql += " ORDER BY table_schema, ordinal_position"

			client, err := postgres.NewClient(cfg)
			if err != nil {
				return fmt.Errorf("failed to create client: %w", err)
			}

			resp, err := client.Query(ctx, datasourceUID, postgres.QueryRequest{RawSQL: sql})
			if err != nil {
				return fmt.Errorf("query failed: %w", err)
			}

			if len(resp.Rows) == 0 {
				if schema != "" {
					return fmt.Errorf("table %q not found in schema %q", table, schema)
				}
				return fmt.Errorf("table %q not found", table)
			}

			if opts.IncludeConstraints {
				constraintsResp, err := client.Query(ctx, datasourceUID, postgres.QueryRequest{RawSQL: postgres.BuildDescribeConstraintsQuery(schema, table)})
				if err != nil {
					return fmt.Errorf("query constraints failed: %w", err)
				}
				description, err := querysql.ParseTableDescription(schema, table, resp, constraintsResp)
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
		agent.AnnotationLLMHint:   `gcx datasources postgres describe-table orders -d UID`,
	}

	opts.setup(cmd.Flags())

	return cmd
}
