package influxdb

import (
	"errors"
	"fmt"
	"io"
	"log/slog"

	"github.com/grafana/gcx/internal/agent"
	internalconfig "github.com/grafana/gcx/internal/config"
	dsquery "github.com/grafana/gcx/internal/datasources/query"
	"github.com/grafana/gcx/internal/format"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/query/influxdb"
	"github.com/grafana/grafana-app-sdk/logging"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type measurementsOpts struct {
	IO         cmdio.Options
	Datasource string
	Bucket     string
}

func (opts *measurementsOpts) setup(flags *pflag.FlagSet) {
	opts.IO.RegisterCustomCodec("table", &measurementsTableCodec{})
	opts.IO.DefaultFormat("table")
	opts.IO.BindFlags(flags)

	flags.StringVarP(&opts.Datasource, "datasource", "d", "", "Datasource UID (required unless datasources.influxdb is configured)")
	flags.StringVar(&opts.Bucket, "bucket", "", "Bucket name for Flux mode (defaults to datasource defaultBucket)")
}

func (opts *measurementsOpts) Validate() error {
	return opts.IO.Validate()
}

// MeasurementsCmd returns the `measurements` subcommand.
func MeasurementsCmd(loader *providers.ConfigLoader) *cobra.Command {
	opts := &measurementsOpts{}

	cmd := &cobra.Command{
		Use:   "measurements",
		Short: "List measurements",
		Long:  "List measurement names from an InfluxDB datasource.",
		Example: `
  # List all measurements (use datasource UID, not name)
  gcx datasources influxdb measurements -d UID

  # List measurements with Flux mode (requires --bucket)
  gcx datasources influxdb measurements -d UID --bucket my-bucket

  # Output as JSON
  gcx datasources influxdb measurements -d UID -o json`,
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

			datasourceUID, err := dsquery.ResolveAndSaveDatasource(ctx, loader, opts.Datasource, cfgCtx, cfg, "influxdb")
			if err != nil {
				return err
			}

			influxCfg, err := dsquery.GetInfluxDBConfig(ctx, cfg, datasourceUID)
			if err != nil {
				return fmt.Errorf("failed to detect influxdb mode: %w", err)
			}

			bucket := opts.Bucket
			if bucket == "" {
				bucket = influxCfg.DefaultBucket
			}

			client, err := influxdb.NewClient(cfg)
			if err != nil {
				return fmt.Errorf("failed to create client: %w", err)
			}

			resp, err := client.Measurements(ctx, datasourceUID, influxdb.Mode(influxCfg.Mode), bucket)
			if err != nil {
				return fmt.Errorf("failed to get measurements: %w", err)
			}

			return opts.IO.Encode(cmd.OutOrStdout(), resp)
		},
	}

	cmd.Annotations = map[string]string{
		agent.AnnotationTokenCost: "small",
		agent.AnnotationLLMHint:   "gcx datasources influxdb measurements -d UID -o json",
	}

	opts.setup(cmd.Flags())

	return cmd
}

type measurementsTableCodec struct{}

func (c *measurementsTableCodec) Format() format.Format {
	return "table"
}

func (c *measurementsTableCodec) Encode(w io.Writer, data any) error {
	resp, ok := data.(*influxdb.MeasurementsResponse)
	if !ok {
		return errors.New("invalid data type for measurements table codec")
	}

	return influxdb.FormatMeasurementsTable(w, resp)
}

func (c *measurementsTableCodec) Decode(io.Reader, any) error {
	return errors.New("measurements table codec does not support decoding")
}
