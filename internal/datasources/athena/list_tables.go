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

type listTablesOpts struct {
	IO         cmdio.Options
	Datasource string
	Region     string
	Catalog    string
	Database   string
	Filter     string
	Limit      int
}

func (opts *listTablesOpts) setup(flags *pflag.FlagSet) {
	cmdio.RegisterCSV(&opts.IO)
	dsquery.RegisterCodecs(&opts.IO, false)
	opts.IO.BindFlags(flags)
	flags.StringVarP(&opts.Datasource, "datasource", "d", "", "Datasource UID (required unless datasources.athena is configured)")
	flags.StringVar(&opts.Region, "region", "", "AWS region override")
	flags.StringVar(&opts.Catalog, "catalog", "", "Data catalog to query")
	flags.StringVar(&opts.Database, "database", "", "Database to query")
	flags.StringVar(&opts.Filter, "filter", "", "Only show tables whose name contains this text (case-insensitive)")
	opts.IO.BindListLimit(flags, &opts.Limit, "tables", athena.DiscoveryRowLimit)
}

func (opts *listTablesOpts) Validate() error {
	return opts.IO.Validate()
}

// ListTablesCmd returns the `list-tables` subcommand for an Athena datasource parent.
func ListTablesCmd(loader *providers.ConfigLoader) *cobra.Command {
	opts := &listTablesOpts{}

	cmd := &cobra.Command{
		Use:   "list-tables",
		Short: "List tables in an Athena database",
		Long: `List the tables in an Athena database.

--filter keeps only tables whose name contains the given text, matched literally
and case-insensitively. The Athena resource API has no name filter, so the
filter and --limit are applied by gcx after the full list is fetched. By
default every match is returned; with --limit N at most N are, and a warning
on stderr says when more matched.`,
		Example: `
  # List tables in a database
  gcx datasources athena list-tables -d UID --database mydb

  # Find tables by name
  gcx datasources athena list-tables -d UID --database mydb --filter events

  # With catalog and region
  gcx datasources athena list-tables --filter SUBSTRING -d UID --catalog AwsDataCatalog --database mydb --region us-east-1

  # Output as JSON
  gcx datasources athena list-tables -d UID --database mydb -o json`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}

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

			body := map[string]string{}
			if opts.Region != "" {
				body["region"] = opts.Region
			}
			if opts.Catalog != "" {
				body["catalog"] = opts.Catalog
			}
			if opts.Database != "" {
				body["database"] = opts.Database
			}

			data, err := client.Resource(ctx, datasourceUID, "/tables", body)
			if err != nil {
				return err
			}

			var tables []string
			if err := json.Unmarshal(data, &tables); err != nil {
				return fmt.Errorf("failed to parse response: %w", err)
			}

			tables = filterAndLimit(cmd.ErrOrStderr(), tables, opts.Filter, opts.Limit, "tables")

			return opts.IO.Encode(cmd.OutOrStdout(), athena.StringList{Items: tables, Header: "TABLE"})
		},
	}

	cmd.Annotations = map[string]string{
		agent.AnnotationTokenCost: "small",
		agent.AnnotationLLMHint:   `gcx datasources athena list-tables --filter SUBSTRING -d UID --catalog AwsDataCatalog --database mydb -o json`,
	}

	opts.setup(cmd.Flags())
	return cmd
}
