package athena

import (
	"encoding/json"
	"fmt"

	"github.com/grafana/gcx/internal/agent"
	dsquery "github.com/grafana/gcx/internal/datasources/query"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/query/athena"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type describeTableOpts struct {
	IO         cmdio.Options
	Datasource string
	Region     string
	Catalog    string
	Database   string
	Filter     string
	Limit      int
}

func (opts *describeTableOpts) setup(flags *pflag.FlagSet) {
	cmdio.RegisterCSV(&opts.IO)
	dsquery.RegisterCodecs(&opts.IO, false)
	opts.IO.BindFlags(flags)
	flags.StringVarP(&opts.Datasource, "datasource", "d", "", "Datasource UID (required unless datasources.athena is configured)")
	flags.StringVar(&opts.Region, "region", "", "AWS region override")
	flags.StringVar(&opts.Catalog, "catalog", "", "Data catalog")
	flags.StringVar(&opts.Database, "database", "", "Database name")
	flags.StringVar(&opts.Filter, "filter", "", "Only show columns whose name contains this text (case-insensitive)")
	opts.IO.BindListLimit(flags, &opts.Limit, "columns", athena.DiscoveryRowLimit)
}

func (opts *describeTableOpts) Validate() error {
	return opts.IO.Validate()
}

// DescribeTableCmd returns the `describe-table` subcommand for an Athena datasource parent.
func DescribeTableCmd(loader *providers.ConfigLoader) *cobra.Command {
	opts := &describeTableOpts{}

	cmd := &cobra.Command{
		Use:   "describe-table TABLE",
		Short: "List column names for an Athena table",
		Long: `List the column names of the specified table. Only names are returned, not types or other schema details.

--filter keeps only columns whose name contains the given text, matched
literally and case-insensitively. The Athena resource API has no name filter,
so the filter and --limit are applied by gcx after the full list is fetched.
By default every match is returned; with --limit N at most N are, and a
warning on stderr says when more matched.`,
		Example: `
  # List columns for a table
  gcx datasources athena describe-table my_table -d UID --database mydb

  # Find columns by name in a wide table
  gcx datasources athena describe-table my_table -d UID --database mydb --filter created

  # With catalog and region
  gcx datasources athena describe-table my_table -d UID --catalog AwsDataCatalog --database mydb --region us-east-1

  # Output as JSON
  gcx datasources athena describe-table my_table -d UID --database mydb -o json`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}

			table := args[0]
			ctx := cmd.Context()

			cfgCtx, cfg, err := dsquery.LoadContextAndConfig(ctx, loader)
			if err != nil {
				return err
			}

			// Resolve datasource UID from -d flag, config, or Grafana auto-discovery.
			datasourceUID, _, err := dsquery.ResolveValidateAndSaveDatasource(ctx, loader, opts.Datasource, cfgCtx, cfg, "athena")
			if err != nil {
				return err
			}

			client, err := athena.NewClient(cfg)
			if err != nil {
				return fmt.Errorf("failed to create client: %w", err)
			}

			body := map[string]string{
				"table": table,
			}
			if opts.Region != "" {
				body["region"] = opts.Region
			}
			if opts.Catalog != "" {
				body["catalog"] = opts.Catalog
			}
			if opts.Database != "" {
				body["database"] = opts.Database
			}

			data, err := client.Resource(ctx, datasourceUID, "/columns", body)
			if err != nil {
				return err
			}

			var columns []string
			if err := json.Unmarshal(data, &columns); err != nil {
				return fmt.Errorf("failed to parse response: %w", err)
			}

			columns = filterAndLimit(cmd.ErrOrStderr(), columns, opts.Filter, opts.Limit, "columns")

			return opts.IO.Encode(cmd.OutOrStdout(), athena.StringList{Items: columns, Header: "COLUMN"})
		},
	}

	cmd.Annotations = map[string]string{
		agent.AnnotationTokenCost: "small",
		agent.AnnotationLLMHint:   `gcx datasources athena describe-table TABLE --filter SUBSTRING -d UID --catalog AwsDataCatalog --database mydb -o json`,
	}

	opts.setup(cmd.Flags())
	return cmd
}
