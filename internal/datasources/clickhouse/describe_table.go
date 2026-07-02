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

type describeTableOpts struct {
	IO         cmdio.Options
	Datasource string
	Database   string
}

func (opts *describeTableOpts) setup(flags *pflag.FlagSet) {
	dsquery.RegisterCodecs(&opts.IO, false)
	opts.IO.BindFlags(flags)
	flags.StringVarP(&opts.Datasource, "datasource", "d", "", "Datasource UID (required unless datasources.clickhouse is configured)")
	flags.StringVar(&opts.Database, "database", "", "Database name (default: \"default\")")
}

func (opts *describeTableOpts) Validate() error {
	return opts.IO.Validate()
}

// DescribeTableCmd returns the `describe-table` subcommand for a ClickHouse datasource parent.
func DescribeTableCmd(loader *providers.ConfigLoader) *cobra.Command {
	opts := &describeTableOpts{}

	cmd := &cobra.Command{
		Use:   "describe-table TABLE",
		Short: "Show column schema for a ClickHouse table",
		Long:  `Show column details including name, type, default, and comment for each column in the specified table.`,
		Example: `
  # Describe a table in the default database
  gcx datasources clickhouse describe-table otel_logs

  # Describe a table in a specific database
  gcx datasources clickhouse describe-table otel_logs --database otel

  # Output as JSON
  gcx datasources clickhouse describe-table otel_logs -o json`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}

			table := args[0]
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

			db := opts.Database
			if db == "" {
				db = "default"
			}

			if err := clickhouse.ValidateIdentifier(db, "database"); err != nil {
				return err
			}
			if err := clickhouse.ValidateIdentifier(table, "table"); err != nil {
				return err
			}

			sql := fmt.Sprintf(
				"SELECT name, type, default_kind AS default_type, default_expression, comment FROM system.columns WHERE database = '%s' AND table = '%s' ORDER BY position",
				clickhouse.EscapeSQLString(db),
				clickhouse.EscapeSQLString(table),
			)

			client, err := clickhouse.NewClient(cfg)
			if err != nil {
				return fmt.Errorf("failed to create client: %w", err)
			}

			resp, err := client.Query(ctx, datasourceUID, clickhouse.QueryRequest{RawSQL: sql})
			if err != nil {
				return fmt.Errorf("query failed: %w", err)
			}

			cols := clickhouse.ParseColumnInfoRows(resp)
			return opts.IO.Encode(cmd.OutOrStdout(), cols)
		},
	}

	cmd.Annotations = map[string]string{
		agent.AnnotationTokenCost: "small",
		agent.AnnotationLLMHint:   `gcx datasources clickhouse describe-table TABLE -d UID --database default -o json`,
	}

	opts.setup(cmd.Flags())

	return cmd
}
