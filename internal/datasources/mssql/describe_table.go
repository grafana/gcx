package mssql

import (
	"fmt"

	"github.com/grafana/gcx/internal/agent"
	dsquery "github.com/grafana/gcx/internal/datasources/query"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/query/mssql"
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
	flags.StringVarP(&opts.Datasource, "datasource", "d", "", "Datasource UID (required unless datasources.mssql is configured)")
	flags.StringVar(&opts.Schema, "schema", "", "Schema the table belongs to (e.g. dbo)")
}

func (opts *describeTableOpts) Validate() error {
	return opts.IO.Validate()
}

// DescribeTableCmd returns the `describe-table` subcommand for an MSSQL datasource parent.
func DescribeTableCmd(loader *providers.ConfigLoader) *cobra.Command {
	opts := &describeTableOpts{}

	cmd := &cobra.Command{
		Use:   "describe-table TABLE",
		Short: "List columns for an MSSQL table",
		Long: `List the columns of the specified table from INFORMATION_SCHEMA.COLUMNS,
reporting name, data type, nullability, max length, and default. Pass --schema
to disambiguate a table that exists in multiple schemas.`,
		Example: `
  # Describe a table
  gcx datasources mssql describe-table WORLD_DATA

  # Restrict to a schema
  gcx datasources mssql describe-table WORLD_DATA --schema dbo

  # Output as JSON
  gcx datasources mssql describe-table WORLD_DATA -o json`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}

			table := args[0]
			if err := mssql.ValidateIdentifier(table, "table"); err != nil {
				return err
			}
			if err := mssql.ValidateIdentifier(opts.Schema, "schema"); err != nil {
				return err
			}

			ctx := cmd.Context()
			cfgCtx, cfg, err := dsquery.LoadContextAndConfig(ctx, loader)
			if err != nil {
				return err
			}

			// Resolve datasource UID from -d flag, config, or Grafana auto-discovery.
			datasourceUID, _, err := dsquery.ResolveValidateAndSaveDatasource(ctx, loader, opts.Datasource, cfgCtx, cfg, "mssql")
			if err != nil {
				return err
			}

			sql := fmt.Sprintf(
				"SELECT COLUMN_NAME, DATA_TYPE, IS_NULLABLE, CHARACTER_MAXIMUM_LENGTH, COLUMN_DEFAULT FROM INFORMATION_SCHEMA.COLUMNS WHERE TABLE_NAME = '%s'",
				mssql.EscapeSQLString(table),
			)
			if opts.Schema != "" {
				sql += fmt.Sprintf(" AND TABLE_SCHEMA = '%s'", mssql.EscapeSQLString(opts.Schema))
			}
			sql += " ORDER BY ORDINAL_POSITION"

			client, err := mssql.NewClient(cfg)
			if err != nil {
				return fmt.Errorf("failed to create client: %w", err)
			}

			resp, err := client.Query(ctx, datasourceUID, mssql.QueryRequest{RawSQL: sql})
			if err != nil {
				return fmt.Errorf("query failed: %w", err)
			}

			return opts.IO.Encode(cmd.OutOrStdout(), resp)
		},
	}

	cmd.Annotations = map[string]string{
		agent.AnnotationTokenCost: "small",
		agent.AnnotationLLMHint:   `gcx datasources mssql describe-table WORLD_DATA --schema dbo -o json`,
	}

	opts.setup(cmd.Flags())
	return cmd
}
