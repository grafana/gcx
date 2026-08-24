package mssql

import (
	"errors"
	"fmt"

	"github.com/grafana/gcx/internal/agent"
	dsquery "github.com/grafana/gcx/internal/datasources/query"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/query/mssql"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type listTablesOpts struct {
	IO         cmdio.Options
	Datasource string
	Schema     string
}

func (opts *listTablesOpts) setup(flags *pflag.FlagSet) {
	dsquery.RegisterCodecs(&opts.IO, false)
	opts.IO.BindFlags(flags)
	flags.StringVarP(&opts.Datasource, "datasource", "d", "", "Datasource UID (required unless datasources.mssql is configured)")
	flags.StringVar(&opts.Schema, "schema", "", "Filter tables to this schema (e.g. dbo)")
}

func (opts *listTablesOpts) Validate() error {
	return opts.IO.Validate()
}

// ListTablesCmd returns the `list-tables` subcommand for an MSSQL datasource parent.
func ListTablesCmd(loader *providers.ConfigLoader) *cobra.Command {
	opts := &listTablesOpts{}

	cmd := &cobra.Command{
		Use:   "list-tables",
		Short: "List tables and views in an MSSQL database",
		Long: `List base tables and views from INFORMATION_SCHEMA.TABLES, optionally
filtered to a single schema. Reports schema, name, and type for each.`,
		Example: `
  # List all tables and views
  gcx datasources mssql list-tables

  # Filter to the dbo schema
  gcx datasources mssql list-tables --schema dbo

  # Output as JSON
  gcx datasources mssql list-tables -o json`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}
			if cmd.Flags().Changed("schema") && opts.Schema == "" {
				return errors.New("--schema must not be empty")
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

			sql := "SELECT TABLE_SCHEMA, TABLE_NAME, TABLE_TYPE FROM INFORMATION_SCHEMA.TABLES"
			if opts.Schema != "" {
				sql += fmt.Sprintf(" WHERE TABLE_SCHEMA = '%s'", mssql.EscapeSQLString(opts.Schema))
			}
			sql += " ORDER BY TABLE_SCHEMA, TABLE_NAME"

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
		agent.AnnotationLLMHint:   `gcx datasources mssql list-tables --schema dbo -o json`,
	}

	opts.setup(cmd.Flags())
	return cmd
}
