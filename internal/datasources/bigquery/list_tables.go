package bigquery

import (
	"errors"
	"fmt"

	"github.com/grafana/gcx/internal/agent"
	dsquery "github.com/grafana/gcx/internal/datasources/query"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/query/bigquery"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type listTablesOpts struct {
	IO         cmdio.Options
	Datasource string
	Project    string
	Dataset    string
}

func (opts *listTablesOpts) setup(flags *pflag.FlagSet) {
	dsquery.RegisterCodecs(&opts.IO, false)
	opts.IO.BindFlags(flags)
	flags.StringVarP(&opts.Datasource, "datasource", "d", "", "Datasource UID (required unless datasources.bigquery is configured)")
	flags.StringVar(&opts.Project, "project", "", "GCP project ID (default: the datasource's default project)")
	flags.StringVar(&opts.Dataset, "dataset", "", "Dataset to list tables from (required)")
}

func (opts *listTablesOpts) Validate() error {
	return opts.IO.Validate()
}

// ListTablesCmd returns the `list-tables` subcommand for a BigQuery datasource parent.
func ListTablesCmd(loader *providers.ConfigLoader) *cobra.Command {
	opts := &listTablesOpts{}

	cmd := &cobra.Command{
		Use:   "list-tables",
		Short: "List tables in a BigQuery dataset",
		Long: `List tables in a BigQuery dataset via INFORMATION_SCHEMA.TABLES.

--dataset is required. When --project is omitted, the datasource's default
project is used. Run 'list-datasets' to discover available datasets.`,
		Example: `
  # List tables in a dataset (default project)
  gcx datasources bigquery list-tables --dataset my_dataset

  # List tables in a dataset in a specific project
  gcx datasources bigquery list-tables --project my-project --dataset my_dataset

  # Output as JSON
  gcx datasources bigquery list-tables --dataset my_dataset -o json`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}

			if opts.Dataset == "" {
				return errors.New("--dataset is required (run 'gcx datasources bigquery list-datasets' to discover datasets)")
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
			if err := bigquery.ValidateName(opts.Dataset, "dataset"); err != nil {
				return err
			}

			sql := fmt.Sprintf(
				"SELECT table_name, table_type FROM %s.TABLES ORDER BY table_name LIMIT 1000",
				bigquery.InfoSchemaPrefix(opts.Project, opts.Dataset),
			)

			client, err := bigquery.NewClient(cfg)
			if err != nil {
				return fmt.Errorf("failed to create client: %w", err)
			}

			resp, err := client.Query(ctx, datasourceUID, bigquery.QueryRequest{RawSQL: sql})
			if err != nil {
				return fmt.Errorf("query failed: %w", err)
			}

			tables := bigquery.ParseTableInfoRows(resp)
			return opts.IO.Encode(cmd.OutOrStdout(), tables)
		},
	}

	cmd.Annotations = map[string]string{
		agent.AnnotationTokenCost: "small",
		agent.AnnotationLLMHint:   "gcx datasources bigquery list-tables --dataset DATASET -d UID -o json",
	}

	opts.setup(cmd.Flags())

	return cmd
}
