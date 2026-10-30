package postgres

import (
	"fmt"

	"github.com/grafana/gcx/internal/agent"
	dsquery "github.com/grafana/gcx/internal/datasources/query"
	"github.com/grafana/gcx/internal/gcxerrors"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/query/postgres"
	querysql "github.com/grafana/gcx/internal/query/sql"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type describeConstraintsOpts struct {
	IO         cmdio.Options
	Datasource string
	Schema     string
}

func (opts *describeConstraintsOpts) setup(flags *pflag.FlagSet) {
	dsquery.RegisterCodecs(&opts.IO, false)
	opts.IO.BindFlags(flags)
	flags.StringVarP(&opts.Datasource, "datasource", "d", "", "Datasource UID (required unless datasources.postgres is configured)")
	flags.StringVar(&opts.Schema, "schema", "", "Explicit schema of the table (or use schema.TABLE)")
}

func (opts *describeConstraintsOpts) Validate() error { return opts.IO.Validate() }

// DescribeConstraintsCmd returns declared keys and relationships for one table.
func DescribeConstraintsCmd(loader *providers.ConfigLoader) *cobra.Command {
	opts := &describeConstraintsOpts{}
	cmd := &cobra.Command{
		Use:     "describe-constraints TABLE",
		Short:   "Show the declared constraints of a PostgreSQL table",
		Long:    "Show declared primary, unique, check, and foreign-key constraints.\n\nUse schema.TABLE or --schema to select one table explicitly. Local and\nreferenced columns retain their declared order. Only database-declared metadata\nis returned; relationships are never inferred from column names or values.\nTables with no declared constraints return an empty constraints array.",
		Example: "  gcx datasources postgres describe-constraints public.orders -d UID\n  gcx datasources postgres describe-constraints orders --schema public -o json",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(); err != nil {
				return gcxerrors.NewCommandUsageError(cmd, err.Error(), err)
			}
			namespace, table, err := splitTableArg(args[0], opts.Schema)
			if err != nil {
				return gcxerrors.NewCommandUsageError(cmd, err.Error(), err)
			}
			if cmd.Flags().Changed("schema") && opts.Schema == "" {
				return gcxerrors.NewCommandUsageError(cmd, "--schema must not be empty", nil)
			}
			if namespace == "" {
				return gcxerrors.NewCommandUsageError(cmd, "describe-constraints requires an explicit schema (use schema.TABLE or --schema)", nil)
			}
			if err := postgres.ValidateIdentifier(namespace, "schema"); err != nil {
				return gcxerrors.NewCommandUsageError(cmd, err.Error(), err)
			}
			if err := postgres.ValidateIdentifier(table, "table"); err != nil {
				return gcxerrors.NewCommandUsageError(cmd, err.Error(), err)
			}

			ctx := cmd.Context()
			cfgCtx, cfg, err := dsquery.LoadContextAndConfig(ctx, loader)
			if err != nil {
				return err
			}
			uid, _, err := dsquery.ResolveValidateAndSaveDatasource(ctx, loader, opts.Datasource, cfgCtx, cfg, "postgres")
			if err != nil {
				return err
			}
			client, err := postgres.NewClient(cfg)
			if err != nil {
				return fmt.Errorf("failed to create client: %w", err)
			}

			// Distinguish an existing table without constraints from a missing table.
			existsSQL := fmt.Sprintf("SELECT 1 AS found FROM information_schema.tables WHERE table_schema = '%s' AND table_name = '%s'", postgres.EscapeSQLString(namespace), postgres.EscapeSQLString(table))
			exists, err := client.Query(ctx, uid, postgres.QueryRequest{RawSQL: existsSQL})
			if err != nil {
				return fmt.Errorf("query table failed: %w", err)
			}
			if len(exists.Rows) == 0 {
				return fmt.Errorf("table %q not found in schema %q", table, namespace)
			}
			resp, err := client.Query(ctx, uid, postgres.QueryRequest{RawSQL: postgres.BuildDescribeConstraintsQuery(namespace, table)})
			if err != nil {
				return fmt.Errorf("query constraints failed: %w", err)
			}
			result, err := querysql.ParseTableConstraints(namespace, table, resp)
			if err != nil {
				return fmt.Errorf("parse constraints: %w", err)
			}
			return opts.IO.Encode(cmd.OutOrStdout(), result)
		},
		Annotations: map[string]string{
			agent.AnnotationTokenCost: "small",
			agent.AnnotationLLMHint:   "gcx datasources postgres describe-constraints public.orders -d UID",
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}
