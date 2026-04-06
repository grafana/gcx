package profiles

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
	"github.com/grafana/gcx/internal/query/pyroscope"
	"github.com/grafana/grafana-app-sdk/logging"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type pyroscopeLabelsOpts struct {
	IO         cmdio.Options
	Datasource string
	Label      string
}

func (opts *pyroscopeLabelsOpts) setup(flags *pflag.FlagSet) {
	opts.IO.RegisterCustomCodec("table", &pyroscopeLabelsTableCodec{})
	opts.IO.DefaultFormat("table")
	opts.IO.BindFlags(flags)

	flags.StringVarP(&opts.Datasource, "datasource", "d", "", "Datasource UID (required unless default-pyroscope-datasource is configured)")
	flags.StringVarP(&opts.Label, "label", "l", "", "Get values for this label (omit to list all labels)")
}

func (opts *pyroscopeLabelsOpts) Validate() error {
	return opts.IO.Validate()
}

func labelsCmd(loader *providers.ConfigLoader) *cobra.Command {
	opts := &pyroscopeLabelsOpts{}

	cmd := &cobra.Command{
		Use:   "labels",
		Short: "List labels or label values",
		Long:  "List all labels or get values for a specific label from a Pyroscope datasource.",
		Example: `
	# List all labels (use datasource UID, not name)
	gcx profiles labels -d <datasource-uid>

	# Get values for a specific label
	gcx profiles labels -d <datasource-uid> --label service_name

	# Output as JSON
	gcx profiles labels -d <datasource-uid> -o json`,
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

			datasourceUID, err := dsquery.ResolveAndSaveDatasource(ctx, loader, opts.Datasource, cfgCtx, cfg, "pyroscope")
			if err != nil {
				return err
			}

			client, err := pyroscope.NewClient(cfg)
			if err != nil {
				return fmt.Errorf("failed to create client: %w", err)
			}

			if opts.Label != "" {
				resp, err := client.LabelValues(ctx, datasourceUID, pyroscope.LabelValuesRequest{
					Name: opts.Label,
				})
				if err != nil {
					return fmt.Errorf("failed to get label values: %w", err)
				}

				if opts.IO.OutputFormat == "table" {
					return pyroscope.FormatLabelsTable(cmd.OutOrStdout(), resp.Names)
				}
				return opts.IO.Encode(cmd.OutOrStdout(), resp)
			}

			resp, err := client.LabelNames(ctx, datasourceUID, pyroscope.LabelNamesRequest{})
			if err != nil {
				return fmt.Errorf("failed to get labels: %w", err)
			}

			if opts.IO.OutputFormat == "table" {
				return pyroscope.FormatLabelsTable(cmd.OutOrStdout(), resp.Names)
			}
			return opts.IO.Encode(cmd.OutOrStdout(), resp)
		},
	}

	opts.setup(cmd.Flags())
	return cmd
}

type pyroscopeLabelsTableCodec struct{}

func (c *pyroscopeLabelsTableCodec) Format() format.Format {
	return "table"
}

func (c *pyroscopeLabelsTableCodec) Encode(w io.Writer, data any) error {
	switch v := data.(type) {
	case *pyroscope.LabelNamesResponse:
		return pyroscope.FormatLabelsTable(w, v.Names)
	case *pyroscope.LabelValuesResponse:
		return pyroscope.FormatLabelsTable(w, v.Names)
	default:
		return errors.New("invalid data type for pyroscope labels table codec")
	}
}

func (c *pyroscopeLabelsTableCodec) Decode(io.Reader, any) error {
	return errors.New("pyroscope labels table codec does not support decoding")
}
