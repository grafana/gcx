package influxdb //nolint:dupl

import (
	"errors"
	"fmt"
	"io"

	"github.com/grafana/gcx/internal/agent"
	dsquery "github.com/grafana/gcx/internal/datasources/query"
	"github.com/grafana/gcx/internal/format"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/query/influxdb"
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

			cfgCtx, cfg, err := dsquery.LoadContextAndConfig(ctx, loader)
			if err != nil {
				return err
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
				return fmt.Errorf("field-keys is only supported in InfluxQL mode (datasource is configured for %s mode)\n\nTo get fields from a Flux datasource, query directly:\n\n  from(bucket: \"<bucket>\") |> range(start: -5m) |> filter(fn: (r) => r._measurement == \"<measurement>\") |> keep(columns: [\"_field\"]) |> distinct(column: \"_field\")", modeStr)
			}

			client, err := influxdb.NewClient(cfg)
			if err != nil {
				return fmt.Errorf("failed to create client: %w", err)
			}

			resp, err := client.FieldKeys(ctx, datasourceUID, opts.Measurement)
			if err != nil {
				return fmt.Errorf("failed to get field keys: %w", err)
			}

			return opts.IO.Encode(cmd.OutOrStdout(), resp)
		},
	}

	cmd.Annotations = map[string]string{
		agent.AnnotationTokenCost: "small",
		agent.AnnotationLLMHint:   "gcx datasources influxdb field-keys -d UID",
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
