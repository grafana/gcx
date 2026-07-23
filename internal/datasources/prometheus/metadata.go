package prometheus

import (
	"errors"
	"fmt"
	"io"
	"sort"

	"github.com/grafana/gcx/internal/agent"
	dsquery "github.com/grafana/gcx/internal/datasources/query"
	"github.com/grafana/gcx/internal/format"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/query/prometheus"
	"github.com/grafana/gcx/internal/style"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type metadataOpts struct {
	IO         cmdio.Options
	Datasource string
	Metric     string
}

func (opts *metadataOpts) setup(flags *pflag.FlagSet) {
	opts.IO.RegisterCustomCodec("table", &metadataTableCodec{})
	opts.IO.DefaultFormat("table")
	opts.IO.BindFlags(flags)

	flags.StringVarP(&opts.Datasource, "datasource", "d", "", "Datasource UID (required unless datasources.prometheus is configured)")
	flags.StringVarP(&opts.Metric, "metric", "m", "", "Filter by metric name")
}

func (opts *metadataOpts) Validate() error {
	return opts.IO.Validate()
}

func MetadataCmd(loader *providers.ConfigLoader) *cobra.Command {
	opts := &metadataOpts{}

	cmd := &cobra.Command{
		Use:   "metadata",
		Short: "Get metric metadata",
		Long:  "Get metadata (type, help text) for metrics from a Prometheus datasource.",
		Example: `
	# Get all metric metadata (use datasource UID, not name)
	gcx datasources prometheus metadata -d UID

	# Get metadata for a specific metric
	gcx datasources prometheus metadata -d UID --metric http_requests_total

	# Output as JSON
	gcx datasources prometheus metadata -d UID -o json`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}

			ctx := cmd.Context()

			cfgCtx, cfg, err := dsquery.LoadContextAndConfig(ctx, loader)
			if err != nil {
				return err
			}

			datasourceUID, err := dsquery.ResolveAndSaveDatasource(ctx, loader, opts.Datasource, cfgCtx, cfg, "prometheus")
			if err != nil {
				return err
			}

			client, err := prometheus.NewClient(cfg)
			if err != nil {
				return fmt.Errorf("failed to create client: %w", err)
			}

			resp, err := client.Metadata(ctx, datasourceUID, opts.Metric)
			if err != nil {
				return fmt.Errorf("failed to get metadata: %w", err)
			}

			if opts.IO.OutputFormat == "table" {
				return prometheus.FormatMetadataTable(cmd.OutOrStdout(), resp)
			}

			return opts.IO.Encode(cmd.OutOrStdout(), resp)
		},
	}

	cmd.Annotations = map[string]string{
		agent.AnnotationTokenCost: "small",
		agent.AnnotationLLMHint:   "gcx datasources prometheus metadata -d UID -o json",
	}

	opts.setup(cmd.Flags())
	return cmd
}

type metadataTableCodec struct{}

func (c *metadataTableCodec) Format() format.Format {
	return "table"
}

func (c *metadataTableCodec) Encode(w io.Writer, data any) error {
	resp, ok := data.(*prometheus.MetadataResponse)
	if !ok {
		return errors.New("invalid data type for metadata table codec")
	}

	t := style.NewTable("METRIC", "TYPE", "HELP")

	metrics := make([]string, 0, len(resp.Data))
	for m := range resp.Data {
		metrics = append(metrics, m)
	}
	sort.Strings(metrics)
	for _, metric := range metrics {
		entries := resp.Data[metric]
		for _, entry := range entries {
			t.Row(metric, entry.Type, entry.Help)
		}
	}

	return t.Render(w)
}

func (c *metadataTableCodec) Decode(io.Reader, any) error {
	return errors.New("metadata table codec does not support decoding")
}
