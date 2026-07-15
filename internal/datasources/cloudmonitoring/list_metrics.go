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

type listMetricsOpts struct {
	IO         cmdio.Options
	Datasource string
	Project    string
	Service    string
}

func (opts *listMetricsOpts) setup(flags *pflag.FlagSet) {
	opts.IO.RegisterCustomCodec("table", &listMetricsTableCodec{})
	opts.IO.DefaultFormat("table")
	opts.IO.BindFlags(flags)
	flags.StringVarP(&opts.Datasource, "datasource", "d", "", "Datasource UID (required unless datasources.cloudmonitoring is configured)")
	flags.StringVar(&opts.Project, "project", "", "GCP project ID (required)")
	flags.StringVar(&opts.Service, "service", "", "Restrict to metrics of this service prefix, e.g. compute.googleapis.com (recommended: unfiltered listings page through every metric and can be slow)")
}

func (opts *listMetricsOpts) Validate() error {
	if err := opts.IO.Validate(); err != nil {
		return err
	}
	if opts.Project == "" {
		return errors.New("--project is required")
	}
	return nil
}

// ListMetricsCmd returns the `list-metrics` subcommand for Google Cloud Monitoring.
func ListMetricsCmd(loader *providers.ConfigLoader) *cobra.Command {
	opts := &listMetricsOpts{}

	cmd := &cobra.Command{
		Use:   "list-metrics",
		Short: "List metric descriptors for a GCP project",
		Long: `List the Cloud Monitoring metric descriptors available in a project, with
kind, value type, and unit. Use --service to narrow the listing (recommended;
unfiltered listings page through every metric in the project and can be slow).`,
		Example: `
  gcx datasources cloudmonitoring list-metrics -d UID --project my-project --service compute.googleapis.com
  gcx datasources cloudmonitoring list-metrics -d UID --project my-project --service monitoring.googleapis.com -o json`,
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

			descriptors, err := client.ListMetricDescriptors(ctx, datasourceUID, opts.Project, opts.Service)
			if err != nil {
				return fmt.Errorf("failed to list metrics: %w", err)
			}

			return opts.IO.Encode(cmd.OutOrStdout(), descriptors)
		},
	}

	cmd.Annotations = map[string]string{
		agent.AnnotationTokenCost: "medium",
		agent.AnnotationLLMHint:   "gcx datasources cloudmonitoring list-metrics -d UID --project PROJECT --service compute.googleapis.com",
	}

	opts.setup(cmd.Flags())
	return cmd
}

type listMetricsTableCodec struct{}

func (c *listMetricsTableCodec) Format() format.Format { return "table" }

func (c *listMetricsTableCodec) Encode(w io.Writer, data any) error {
	descriptors, ok := data.([]gcmclient.MetricDescriptor)
	if !ok {
		return fmt.Errorf("listMetricsTableCodec: unexpected type %T", data)
	}
	return gcmclient.FormatMetricDescriptors(w, descriptors)
}

func (c *listMetricsTableCodec) Decode(io.Reader, any) error {
	return errors.New("listMetricsTableCodec does not support decoding")
}
