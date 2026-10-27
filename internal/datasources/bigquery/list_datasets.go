package bigquery

import (
	"fmt"

	"github.com/grafana/gcx/internal/agent"
	dsquery "github.com/grafana/gcx/internal/datasources/query"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/query/bigquery"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type listDatasetsOpts struct {
	IO         cmdio.Options
	Datasource string
	Project    string
	Filter     string
	Limit      int
}

func (opts *listDatasetsOpts) setup(flags *pflag.FlagSet) {
	cmdio.RegisterCSV(&opts.IO)
	dsquery.RegisterCodecs(&opts.IO, false)
	opts.IO.BindFlags(flags)
	flags.StringVarP(&opts.Datasource, "datasource", "d", "", "Datasource UID (required unless datasources.bigquery is configured)")
	flags.StringVar(&opts.Project, "project", "", "GCP project ID (default: the datasource's default project)")
	flags.StringVar(&opts.Filter, "filter", "", "Only list datasets whose name contains this text (case-insensitive)")
	flags.IntVar(&opts.Limit, "limit", bigquery.MetadataRowLimit, "Max datasets to return; more matches are dropped with a warning (0 for no cap)")
}

func (opts *listDatasetsOpts) Validate() error {
	return opts.IO.Validate()
}

// ListDatasetsCmd returns the `list-datasets` subcommand for a BigQuery datasource parent.
func ListDatasetsCmd(loader *providers.ConfigLoader) *cobra.Command {
	opts := &listDatasetsOpts{}

	cmd := &cobra.Command{
		Use:   "list-datasets",
		Short: "List datasets in a BigQuery project",
		Long: `List datasets (schemas) in a BigQuery project via INFORMATION_SCHEMA.SCHEMATA.

When --project is omitted, the datasource's default project is queried.

At most --limit datasets are returned (1000 by default); if more match, a
warning is printed on stderr rather than silently truncating. Narrow a large
project with --filter, or pass --limit 0 to return every dataset.`,
		Example: `
  # List datasets in the default project
  gcx datasources bigquery list-datasets

  # List datasets in a specific project
  gcx datasources bigquery list-datasets --project my-project

  # Find datasets by name in a project with thousands of them
  gcx datasources bigquery list-datasets --filter analytics

  # Return every dataset, however many match
  gcx datasources bigquery list-datasets --limit 0

  # Output as JSON
  gcx datasources bigquery list-datasets -o json`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}

			if err := bigquery.ValidateProject(opts.Project); err != nil {
				return err
			}
			if err := validateMetadataLimit(opts.Limit); err != nil {
				return err
			}

			ctx := cmd.Context()

			// Resolve datasource UID from -d flag, config, or Grafana auto-discovery.
			cfgCtx, cfg, err := dsquery.LoadContextAndConfig(ctx, loader)
			if err != nil {
				return err
			}

			datasourceUID, _, err := dsquery.ResolveValidateAndSaveDatasource(ctx, loader, opts.Datasource, cfgCtx, cfg, "bigquery")
			if err != nil {
				return err
			}

			sql := bigquery.MetadataQuery(
				fmt.Sprintf("SELECT schema_name FROM %s.SCHEMATA", bigquery.InfoSchemaPrefix(opts.Project, "")),
				[]string{bigquery.MetadataFilterClause("schema_name", opts.Filter)},
				"schema_name",
				opts.Limit,
			)

			client, err := bigquery.NewClient(cfg)
			if err != nil {
				return fmt.Errorf("failed to create client: %w", err)
			}

			resp, err := client.Query(ctx, datasourceUID, bigquery.QueryRequest{RawSQL: sql})
			if err != nil {
				return fmt.Errorf("query failed: %w", err)
			}

			warnIfMetadataTruncated(cmd.ErrOrStderr(), resp, "datasets", opts.Limit)

			datasets := bigquery.ParseStringColumn(resp)
			return opts.IO.Encode(cmd.OutOrStdout(), bigquery.StringList{Items: datasets, Header: "DATASET"})
		},
	}

	cmd.Annotations = map[string]string{
		agent.AnnotationTokenCost: "small",
		agent.AnnotationLLMHint:   "gcx datasources bigquery list-datasets --filter SUBSTRING -d UID -o json",
	}

	opts.setup(cmd.Flags())

	return cmd
}
