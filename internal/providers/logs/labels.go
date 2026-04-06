package logs

import (
	"errors"
	"fmt"
	"io"
	"log/slog"

	internalconfig "github.com/grafana/gcx/internal/config"
	dsquery "github.com/grafana/gcx/internal/datasources/query"
	"github.com/grafana/gcx/internal/format"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/query/loki"
	"github.com/grafana/grafana-app-sdk/logging"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type lokiLabelsOpts struct {
	IO         cmdio.Options
	Datasource string
	Label      string
}

func (opts *lokiLabelsOpts) setup(flags *pflag.FlagSet) {
	opts.IO.RegisterCustomCodec("table", &lokiLabelsTableCodec{})
	opts.IO.DefaultFormat("table")
	opts.IO.BindFlags(flags)

	flags.StringVarP(&opts.Datasource, "datasource", "d", "", "Datasource UID (required unless default-loki-datasource is configured)")
	flags.StringVarP(&opts.Label, "label", "l", "", "Get values for this label (omit to list all labels)")
}

func (opts *lokiLabelsOpts) Validate() error {
	return opts.IO.Validate()
}

func labelsCmd(loader *providers.ConfigLoader) *cobra.Command {
	opts := &lokiLabelsOpts{}

	cmd := &cobra.Command{
		Use:   "labels",
		Short: "List labels or label values",
		Long:  "List all labels or get values for a specific label from a Loki datasource.",
		Example: `
	# List all labels (use datasource UID, not name)
	gcx logs labels -d <datasource-uid>

	# Get values for a specific label
	gcx logs labels -d <datasource-uid> --label job

	# Output as JSON
	gcx logs labels -d <datasource-uid> -o json`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}

			ctx := cmd.Context()

			cfg, err := loader.LoadGrafanaConfig(ctx)
			if err != nil {
				return err
			}

			var cfgCtx *internalconfig.Context
			fullCfg, err := loader.LoadFullConfig(ctx)
			if err != nil {
				logging.FromContext(ctx).Warn("could not load config; falling back to auto-discovery", slog.String("error", err.Error()))
			} else {
				cfgCtx = fullCfg.GetCurrentContext()
			}

			datasourceUID, err := dsquery.ResolveAndSaveDatasource(ctx, loader, opts.Datasource, cfgCtx, cfg, "loki")
			if err != nil {
				return err
			}

			client, err := loki.NewClient(cfg)
			if err != nil {
				return fmt.Errorf("failed to create client: %w", err)
			}

			if opts.Label != "" {
				resp, err := client.LabelValues(ctx, datasourceUID, opts.Label)
				if err != nil {
					return fmt.Errorf("failed to get label values: %w", err)
				}

				if opts.IO.OutputFormat == "table" {
					return loki.FormatLabelsTable(cmd.OutOrStdout(), resp)
				}

				return opts.IO.Encode(cmd.OutOrStdout(), resp)
			}

			resp, err := client.Labels(ctx, datasourceUID)
			if err != nil {
				return fmt.Errorf("failed to get labels: %w", err)
			}

			if opts.IO.OutputFormat == "table" {
				return loki.FormatLabelsTable(cmd.OutOrStdout(), resp)
			}

			return opts.IO.Encode(cmd.OutOrStdout(), resp)
		},
	}

	opts.setup(cmd.Flags())

	return cmd
}

type lokiLabelsTableCodec struct{}

func (c *lokiLabelsTableCodec) Format() format.Format {
	return "table"
}

func (c *lokiLabelsTableCodec) Encode(w io.Writer, data any) error {
	resp, ok := data.(*loki.LabelsResponse)
	if !ok {
		return errors.New("invalid data type for loki labels table codec")
	}

	return loki.FormatLabelsTable(w, resp)
}

func (c *lokiLabelsTableCodec) Decode(io.Reader, any) error {
	return errors.New("loki labels table codec does not support decoding")
}
