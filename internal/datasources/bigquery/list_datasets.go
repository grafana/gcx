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
}

func (opts *listDatasetsOpts) setup(flags *pflag.FlagSet) {
	dsquery.RegisterCodecs(&opts.IO, false)
	opts.IO.BindFlags(flags)
	flags.StringVarP(&opts.Datasource, "datasource", "d", "", "Datasource UID (required unless datasources.bigquery is configured)")
	flags.StringVar(&opts.Project, "project", "", "GCP project ID (default: the datasource's default project)")
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

At most 1000 datasets are returned; additional datasets are not listed.`,
		Example: `
  # List datasets in the default project
  gcx datasources bigquery list-datasets

  # List datasets in a specific project
  gcx datasources bigquery list-datasets --project my-project

  # Output as JSON
  gcx datasources bigquery list-datasets -o json`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(); err != nil {
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

			if err := bigquery.ValidateProject(opts.Project); err != nil {
				return err
			}

			sql := fmt.Sprintf(
				"SELECT schema_name FROM %s.SCHEMATA ORDER BY schema_name LIMIT %d",
				bigquery.InfoSchemaPrefix(opts.Project, ""),
				bigquery.MetadataRowLimit,
			)

			client, err := bigquery.NewClient(cfg)
			if err != nil {
				return fmt.Errorf("failed to create client: %w", err)
			}

			resp, err := client.Query(ctx, datasourceUID, bigquery.QueryRequest{RawSQL: sql})
			if err != nil {
				return fmt.Errorf("query failed: %w", err)
			}

			datasets := bigquery.ParseStringColumn(resp)
			return opts.IO.Encode(cmd.OutOrStdout(), bigquery.StringList{Items: datasets, Header: "DATASET"})
		},
	}

	cmd.Annotations = map[string]string{
		agent.AnnotationTokenCost: "small",
		agent.AnnotationLLMHint:   "gcx datasources bigquery list-datasets -d UID -o json",
	}

	opts.setup(cmd.Flags())

	return cmd
}
