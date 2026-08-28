package cloudmonitoring

import (
	"errors"
	"fmt"
	"io"

	"github.com/grafana/gcx/internal/agent"
	dsquery "github.com/grafana/gcx/internal/datasources/query"
	"github.com/grafana/gcx/internal/format"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	gcmclient "github.com/grafana/gcx/internal/query/cloudmonitoring"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type listProjectsOpts struct {
	IO         cmdio.Options
	Datasource string
}

func (opts *listProjectsOpts) setup(flags *pflag.FlagSet) {
	opts.IO.RegisterCustomCodec("table", &listProjectsTableCodec{})
	opts.IO.DefaultFormat("table")
	opts.IO.BindFlags(flags)
	flags.StringVarP(&opts.Datasource, "datasource", "d", "", "Datasource UID (required unless datasources.cloudmonitoring is configured)")
}

func (opts *listProjectsOpts) Validate() error {
	return opts.IO.Validate()
}

// ListProjectsCmd returns the `list-projects` subcommand for Google Cloud Monitoring.
func ListProjectsCmd(loader *providers.ConfigLoader) *cobra.Command {
	opts := &listProjectsOpts{}

	cmd := &cobra.Command{
		Use:   "list-projects",
		Short: "List GCP projects visible to the datasource",
		Long:  "List the Google Cloud projects the datasource's credentials can access.",
		Example: `
  gcx datasources cloudmonitoring list-projects -d UID
  gcx datasources cloudmonitoring list-projects -d UID -o json`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}

			ctx := cmd.Context()

			cfgCtx, cfg, err := dsquery.LoadContextAndConfig(ctx, loader)
			if err != nil {
				return err
			}

			datasourceUID, _, err := dsquery.ResolveValidateAndSaveDatasource(ctx, loader, opts.Datasource, cfgCtx, cfg, "cloudmonitoring")
			if err != nil {
				return err
			}

			client, err := gcmclient.NewClient(cfg)
			if err != nil {
				return fmt.Errorf("failed to create client: %w", err)
			}

			projects, err := client.ListProjects(ctx, datasourceUID)
			if err != nil {
				return fmt.Errorf("failed to list projects: %w", err)
			}

			return opts.IO.Encode(cmd.OutOrStdout(), projects)
		},
	}

	cmd.Annotations = map[string]string{
		agent.AnnotationTokenCost: "small",
		agent.AnnotationLLMHint:   "gcx datasources cloudmonitoring list-projects -d UID",
	}

	opts.setup(cmd.Flags())
	return cmd
}

type listProjectsTableCodec struct{}

func (c *listProjectsTableCodec) Format() format.Format { return "table" }

func (c *listProjectsTableCodec) Encode(w io.Writer, data any) error {
	projects, ok := data.([]gcmclient.Project)
	if !ok {
		return fmt.Errorf("listProjectsTableCodec: unexpected type %T", data)
	}
	return gcmclient.FormatProjects(w, projects)
}

func (c *listProjectsTableCodec) Decode(io.Reader, any) error {
	return errors.New("listProjectsTableCodec does not support decoding")
}
