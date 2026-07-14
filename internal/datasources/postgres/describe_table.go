package postgres

import (
	"fmt"

	"github.com/grafana/gcx/internal/agent"
	dsquery "github.com/grafana/gcx/internal/datasources/query"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/query/postgres"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type describeTableOpts struct {
	IO         cmdio.Options
	Datasource string
	Schema     string
}

func (opts *describeTableOpts) setup(flags *pflag.FlagSet) {
	dsquery.RegisterCodecs(&opts.IO, false)
	opts.IO.BindFlags(flags)
	flags.StringVarP(&opts.Datasource, "datasource", "d", "", "Datasource UID (required unless datasources.postgres is configured)")
	flags.StringVar(&opts.Schema, "schema", "", "Schema of the table (defaults to all schemas)")
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

Use --schema to disambiguate when the same table name exists in multiple schemas.`,
		Example: `
  # Describe a table
  gcx datasources postgres describe-table orders

  # Disambiguate by schema
  gcx datasources postgres describe-table orders --schema public

  # Output as JSON
  gcx datasources postgres describe-table orders -o json`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}

			table := args[0]
			if err := postgres.ValidateIdentifier(table, "table"); err != nil {
				return err
			}
			if err := postgres.ValidateIdentifier(opts.Schema, "schema"); err != nil {
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

			sql := fmt.Sprintf(
				"SELECT column_name AS name, data_type AS type, is_nullable AS nullable, column_default AS default FROM information_schema.columns WHERE table_name = '%s'",
				postgres.EscapeSQLString(table),
			)
			if opts.Schema != "" {
				sql += fmt.Sprintf(" AND table_schema = '%s'", postgres.EscapeSQLString(opts.Schema))
			}
			sql += " ORDER BY ordinal_position"

			client, err := postgres.NewClient(cfg)
			if err != nil {
				return fmt.Errorf("failed to create client: %w", err)
			}

			resp, err := client.Query(ctx, datasourceUID, postgres.QueryRequest{RawSQL: sql})
			if err != nil {
				return fmt.Errorf("query failed: %w", err)
			}

			if len(resp.Rows) == 0 {
				return fmt.Errorf("table %q not found", table)
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
