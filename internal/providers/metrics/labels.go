package metrics

import (
	"errors"
	"fmt"
	"io"

	internalconfig "github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/format"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/query/prometheus"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type labelsOpts struct {
	IO         cmdio.Options
	Datasource string
	Label      string
}

func (opts *labelsOpts) setup(flags *pflag.FlagSet) {
	opts.IO.RegisterCustomCodec("table", &labelsTableCodec{})
	opts.IO.DefaultFormat("table")
	opts.IO.BindFlags(flags)

	flags.StringVarP(&opts.Datasource, "datasource", "d", "", "Datasource UID (required unless default-prometheus-datasource is configured)")
	flags.StringVarP(&opts.Label, "label", "l", "", "Get values for this label (omit to list all labels)")
}

func (opts *labelsOpts) Validate() error {
	return opts.IO.Validate()
}

func labelsCmd(loader *providers.ConfigLoader) *cobra.Command {
	opts := &labelsOpts{}

	cmd := &cobra.Command{
		Use:   "labels",
		Short: "List labels or label values",
		Long:  "List all labels or get values for a specific label from a Prometheus datasource.",
		Example: `
	# List all labels (use datasource UID, not name)
	gcx datasources prometheus labels -d <datasource-uid>

	# Get values for a specific label
	gcx datasources prometheus labels -d <datasource-uid> --label job

	# Output as JSON
	gcx datasources prometheus labels -d <datasource-uid> -o json`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}

			ctx := cmd.Context()

			cfg, err := loader.LoadGrafanaConfig(ctx)
			if err != nil {
				return err
			}

			// Resolve datasource
			datasourceUID := opts.Datasource
			if datasourceUID == "" {
				fullCfg, err := loader.LoadFullConfig(ctx)
				if err != nil {
					return err
				}
				datasourceUID = internalconfig.DefaultDatasourceUID(*fullCfg.GetCurrentContext(), "prometheus")
			}
			if datasourceUID == "" {
				return errors.New("datasource UID is required: use -d flag or set default-prometheus-datasource in config")
			}

			client, err := prometheus.NewClient(cfg)
			if err != nil {
				return fmt.Errorf("failed to create client: %w", err)
			}

			if opts.Label != "" {
				resp, err := client.LabelValues(ctx, datasourceUID, opts.Label)
				if err != nil {
					return fmt.Errorf("failed to get label values: %w", err)
				}

				if opts.IO.OutputFormat == "table" {
					return prometheus.FormatLabelsTable(cmd.OutOrStdout(), resp)
				}

				return opts.IO.Encode(cmd.OutOrStdout(), resp)
			}

			resp, err := client.Labels(ctx, datasourceUID)
			if err != nil {
				return fmt.Errorf("failed to get labels: %w", err)
			}

			if opts.IO.OutputFormat == "table" {
				return prometheus.FormatLabelsTable(cmd.OutOrStdout(), resp)
			}

			return opts.IO.Encode(cmd.OutOrStdout(), resp)
		},
	}

	opts.setup(cmd.Flags())

	return cmd
}

type labelsTableCodec struct{}

func (c *labelsTableCodec) Format() format.Format {
	return "table"
}

func (c *labelsTableCodec) Encode(w io.Writer, data any) error {
	resp, ok := data.(*prometheus.LabelsResponse)
	if !ok {
		return errors.New("invalid data type for labels table codec")
	}

	return prometheus.FormatLabelsTable(w, resp)
}

func (c *labelsTableCodec) Decode(io.Reader, any) error {
	return errors.New("labels table codec does not support decoding")
}
