package mysql

import (
	"fmt"

	"github.com/grafana/gcx/internal/agent"
	dsquery "github.com/grafana/gcx/internal/datasources/query"
	"github.com/grafana/gcx/internal/gcxerrors"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/query/mysql"
	querysql "github.com/grafana/gcx/internal/query/sql"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type describeConstraintsOpts struct {
	IO         cmdio.Options
	Datasource string
	Database   string
}

func (opts *describeConstraintsOpts) setup(flags *pflag.FlagSet) {
	dsquery.RegisterCodecs(&opts.IO, false)
	opts.IO.BindFlags(flags)
	flags.StringVarP(&opts.Datasource, "datasource", "d", "", "Datasource UID (required unless datasources.mysql is configured)")
	flags.StringVar(&opts.Database, "database", "", "Explicit database of the table (or use database.TABLE)")
}

func (opts *describeConstraintsOpts) Validate() error { return opts.IO.Validate() }

// DescribeConstraintsCmd returns declared keys and relationships for one table.
func DescribeConstraintsCmd(loader *providers.ConfigLoader) *cobra.Command {
	opts := &describeConstraintsOpts{}
	cmd := &cobra.Command{
		Use:     "describe-constraints TABLE",
		Short:   "Show the declared constraints of a MySQL table",
		Long:    "Show declared primary, unique, check, and foreign-key constraints.\n\nUse database.TABLE or --database to select one table explicitly. Local and\nreferenced columns retain their declared order. Only database-declared metadata\nis returned; relationships are never inferred from column names or values.\nTables with no declared constraints return an empty constraints array.",
		Example: "  gcx datasources mysql describe-constraints mydb.orders -d UID\n  gcx datasources mysql describe-constraints orders --database mydb -o json",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(); err != nil {
				return gcxerrors.NewCommandUsageError(cmd, err.Error(), err)
			}
			namespace, table, err := splitTableArg(args[0], opts.Database)
			if err != nil {
				return gcxerrors.NewCommandUsageError(cmd, err.Error(), err)
			}
			if cmd.Flags().Changed("database") && opts.Database == "" {
				return gcxerrors.NewCommandUsageError(cmd, "--database must not be empty", nil)
			}
			if namespace == "" {
				return gcxerrors.NewCommandUsageError(cmd, "describe-constraints requires an explicit database (use database.TABLE or --database)", nil)
			}
			if err := mysql.ValidateIdentifier(namespace, "database"); err != nil {
				return gcxerrors.NewCommandUsageError(cmd, err.Error(), err)
			}
			if err := mysql.ValidateIdentifier(table, "table"); err != nil {
				return gcxerrors.NewCommandUsageError(cmd, err.Error(), err)
			}

			ctx := cmd.Context()
			cfgCtx, cfg, err := dsquery.LoadContextAndConfig(ctx, loader)
			if err != nil {
				return err
			}
			uid, _, err := dsquery.ResolveValidateAndSaveDatasource(ctx, loader, opts.Datasource, cfgCtx, cfg, "mysql")
			if err != nil {
				return err
			}
			client, err := mysql.NewClient(cfg)
			if err != nil {
				return fmt.Errorf("failed to create client: %w", err)
			}

			// Distinguish an existing table without constraints from a missing table.
			existsSQL := fmt.Sprintf("SELECT 1 AS found FROM information_schema.tables WHERE table_schema = '%s' AND table_name = '%s'", mysql.EscapeSQLString(namespace), mysql.EscapeSQLString(table))
			exists, err := client.Query(ctx, uid, mysql.QueryRequest{RawSQL: existsSQL})
			if err != nil {
				return fmt.Errorf("query table failed: %w", err)
			}
			if len(exists.Rows) == 0 {
				return fmt.Errorf("table %q not found in database %q", table, namespace)
			}
			resp, err := client.Query(ctx, uid, mysql.QueryRequest{RawSQL: mysql.BuildDescribeConstraintsQuery(namespace, table)})
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
			agent.AnnotationLLMHint:   "gcx datasources mysql describe-constraints mydb.orders -d UID",
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}
