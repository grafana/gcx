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

type listDatabasesOpts struct {
	IO         cmdio.Options
	Datasource string
	Region     string
	Catalog    string
}

func (opts *listDatabasesOpts) setup(flags *pflag.FlagSet) {
	dsquery.RegisterCodecs(&opts.IO, false)
	opts.IO.BindFlags(flags)
	flags.StringVarP(&opts.Datasource, "datasource", "d", "", "Datasource UID (required unless datasources.athena is configured)")
	flags.StringVar(&opts.Region, "region", "", "AWS region override")
	flags.StringVar(&opts.Catalog, "catalog", "", "Data catalog to query")
}

func (opts *listDatabasesOpts) Validate() error {
	return opts.IO.Validate()
}

// ListDatabasesCmd returns the `list-databases` subcommand for an Athena datasource parent.
func ListDatabasesCmd(loader *providers.ConfigLoader) *cobra.Command {
	opts := &listDatabasesOpts{}

	cmd := &cobra.Command{
		Use:   "list-databases",
		Short: "List databases in an Athena data catalog",
		Example: `
  # List databases in the default catalog
  gcx datasources athena list-databases -d UID --catalog AwsDataCatalog

  # With region override
  gcx datasources athena list-databases -d UID --catalog AwsDataCatalog --region us-east-1`,
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

			data, err := client.Resource(ctx, datasourceUID, "/databases", body)
			if err != nil {
				return err
			}

			var databases []string
			if err := json.Unmarshal(data, &databases); err != nil {
				return fmt.Errorf("failed to parse response: %w", err)
			}

			return opts.IO.Encode(cmd.OutOrStdout(), athena.StringList{Items: databases, Header: "DATABASE"})
		},
	}

	cmd.Annotations = map[string]string{
		agent.AnnotationTokenCost: "small",
		agent.AnnotationLLMHint:   `gcx datasources athena list-databases -d UID --catalog AwsDataCatalog -o json`,
	}

	opts.setup(cmd.Flags())
	return cmd
}
