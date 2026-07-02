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

type listCatalogsOpts struct {
	IO         cmdio.Options
	Datasource string
	Region     string
}

func (opts *listCatalogsOpts) setup(flags *pflag.FlagSet) {
	dsquery.RegisterCodecs(&opts.IO, false)
	opts.IO.BindFlags(flags)
	flags.StringVarP(&opts.Datasource, "datasource", "d", "", "Datasource UID (required unless datasources.athena is configured)")
	flags.StringVar(&opts.Region, "region", "", "AWS region override")
}

func (opts *listCatalogsOpts) Validate() error {
	return opts.IO.Validate()
}

// ListCatalogsCmd returns the `list-catalogs` subcommand for an Athena datasource parent.
func ListCatalogsCmd(loader *providers.ConfigLoader) *cobra.Command {
	opts := &listCatalogsOpts{}

	cmd := &cobra.Command{
		Use:   "list-catalogs",
		Short: "List available Athena data catalogs",
		Example: `
  # List all catalogs
  gcx datasources athena list-catalogs

  # With explicit datasource and region
  gcx datasources athena list-catalogs -d UID --region us-east-1`,
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

			data, err := client.Resource(ctx, datasourceUID, "/catalogs", body)
			if err != nil {
				return err
			}

			var catalogs []string
			if err := json.Unmarshal(data, &catalogs); err != nil {
				return fmt.Errorf("failed to parse response: %w", err)
			}

			return opts.IO.Encode(cmd.OutOrStdout(), athena.StringList{Items: catalogs, Header: "CATALOG"})
		},
	}

	cmd.Annotations = map[string]string{
		agent.AnnotationTokenCost: "small",
		agent.AnnotationLLMHint:   `gcx datasources athena list-catalogs -d UID -o json`,
	}

	opts.setup(cmd.Flags())
	return cmd
}
