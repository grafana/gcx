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

type describeTableOpts struct {
	IO         cmdio.Options
	Datasource string
	Project    string
	Dataset    string
}

func (opts *describeTableOpts) setup(flags *pflag.FlagSet) {
	dsquery.RegisterCodecs(&opts.IO, false)
	opts.IO.BindFlags(flags)
	flags.StringVarP(&opts.Datasource, "datasource", "d", "", "Datasource UID (required unless datasources.bigquery is configured)")
	flags.StringVar(&opts.Project, "project", "", "GCP project ID (default: the datasource's default project)")
	flags.StringVar(&opts.Dataset, "dataset", "", "Dataset containing the table (required)")
}

func (opts *describeTableOpts) Validate() error {
	return opts.IO.Validate()
}

// DescribeTableCmd returns the `describe-table` subcommand for a BigQuery datasource parent.
func DescribeTableCmd(loader *providers.ConfigLoader) *cobra.Command {
	opts := &describeTableOpts{}

	cmd := &cobra.Command{
		Use:   "describe-table TABLE",
		Short: "Show column schema for a BigQuery table",
		Long: `Show column name, data type, and nullability for each column in a table via
INFORMATION_SCHEMA.COLUMNS.

--dataset is required. When --project is omitted, the datasource's default
project is used.`,
		Example: `
  # Describe a table in a dataset (default project)
  gcx datasources bigquery describe-table events --dataset my_dataset

  # Describe a table in a specific project
  gcx datasources bigquery describe-table events --project my-project --dataset my_dataset

  # Output as JSON
  gcx datasources bigquery describe-table events --dataset my_dataset -o json`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}

			if opts.Dataset == "" {
				return errors.New("--dataset is required (run 'gcx datasources bigquery list-datasets' to discover datasets)")
			}

			table := args[0]
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
			if err := bigquery.ValidateName(table, "table"); err != nil {
				return err
			}

			sql := fmt.Sprintf(
				"SELECT column_name, data_type, is_nullable FROM %s.COLUMNS WHERE table_name = '%s' ORDER BY ordinal_position LIMIT 1000",
				bigquery.InfoSchemaPrefix(opts.Project, opts.Dataset),
				bigquery.EscapeSQLString(table),
			)

			client, err := bigquery.NewClient(cfg)
			if err != nil {
				return fmt.Errorf("failed to create client: %w", err)
			}

			resp, err := client.Query(ctx, datasourceUID, bigquery.QueryRequest{RawSQL: sql})
			if err != nil {
				return fmt.Errorf("query failed: %w", err)
			}

			cols := bigquery.ParseColumnInfoRows(resp)
			return opts.IO.Encode(cmd.OutOrStdout(), cols)
		},
	}

	cmd.Annotations = map[string]string{
		agent.AnnotationTokenCost: "small",
		agent.AnnotationLLMHint:   "gcx datasources bigquery describe-table TABLE --dataset DATASET -d UID -o json",
	}

	opts.setup(cmd.Flags())

	return cmd
}
