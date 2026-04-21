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

type fieldKeysOpts struct {
	IO          cmdio.Options
	Datasource  string
	Measurement string
}

func (opts *fieldKeysOpts) setup(flags *pflag.FlagSet) {
	opts.IO.RegisterCustomCodec("table", &fieldKeysTableCodec{})
	opts.IO.DefaultFormat("table")
	opts.IO.BindFlags(flags)

	flags.StringVarP(&opts.Datasource, "datasource", "d", "", "Datasource UID (required unless datasources.influxdb is configured)")
	flags.StringVarP(&opts.Measurement, "measurement", "m", "", "Filter by measurement name")
}

func (opts *fieldKeysOpts) Validate() error {
	return opts.IO.Validate()
}

// FieldKeysCmd returns the `field-keys` subcommand.
func FieldKeysCmd(loader *providers.ConfigLoader) *cobra.Command {
	opts := &fieldKeysOpts{}

	cmd := &cobra.Command{
		Use:   "field-keys",
		Short: "List field keys",
		Long:  "List field keys from an InfluxDB datasource. Only supported in InfluxQL mode.",
		Example: `
  # List all field keys (use datasource UID, not name)
  gcx datasources influxdb field-keys -d UID

  # Filter by measurement
  gcx datasources influxdb field-keys -d UID --measurement cpu

  # Output as JSON
  gcx datasources influxdb field-keys -d UID -o json`,
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

			modeStr, err := dsquery.GetInfluxDBMode(ctx, cfg, datasourceUID)
			if err != nil {
				return fmt.Errorf("failed to detect influxdb mode: %w", err)
			}

			if modeStr != "InfluxQL" {
				return fmt.Errorf("field-keys is only supported in InfluxQL mode (datasource is configured for %s mode)", modeStr)
			}

			client, err := influxdb.NewClient(cfg)
			if err != nil {
				return fmt.Errorf("failed to create client: %w", err)
			}

			resp, err := client.FieldKeys(ctx, datasourceUID, opts.Measurement)
			if err != nil {
				return fmt.Errorf("failed to get field keys: %w", err)
			}

			if opts.IO.OutputFormat == "table" {
				return influxdb.FormatFieldKeysTable(cmd.OutOrStdout(), resp)
			}

			return opts.IO.Encode(cmd.OutOrStdout(), resp)
		},
	}

	cmd.Annotations = map[string]string{
		agent.AnnotationTokenCost: "small",
		agent.AnnotationLLMHint:   "gcx datasources influxdb field-keys -d UID -o json",
	}

	opts.setup(cmd.Flags())

	return cmd
}

type fieldKeysTableCodec struct{}

func (c *fieldKeysTableCodec) Format() format.Format {
	return "table"
}

func (c *fieldKeysTableCodec) Encode(w io.Writer, data any) error {
	resp, ok := data.(*influxdb.FieldKeysResponse)
	if !ok {
		return errors.New("invalid data type for field keys table codec")
	}

	return influxdb.FormatFieldKeysTable(w, resp)
}

func (c *fieldKeysTableCodec) Decode(io.Reader, any) error {
	return errors.New("field keys table codec does not support decoding")
}
