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

type tagKeysOpts struct {
	IO          cmdio.Options
	Datasource  string
	Measurement string
}

func (opts *tagKeysOpts) setup(flags *pflag.FlagSet) {
	opts.IO.RegisterCustomCodec("table", &tagKeysTableCodec{})
	opts.IO.DefaultFormat("table")
	opts.IO.BindFlags(flags)

	flags.StringVarP(&opts.Datasource, "datasource", "d", "", "Datasource UID (required unless datasources.influxdb is configured)")
	flags.StringVarP(&opts.Measurement, "measurement", "m", "", "Filter by measurement name")
}

func (opts *tagKeysOpts) Validate() error {
	return opts.IO.Validate()
}

// TagKeysCmd returns the `list-tag-keys` subcommand.
func TagKeysCmd(loader *providers.ConfigLoader) *cobra.Command {
	opts := &tagKeysOpts{}

	cmd := &cobra.Command{
		Use:   "list-tag-keys",
		Short: "List tag keys",
		Long:  "List tag keys from an InfluxDB datasource. Only supported in InfluxQL mode.",
		Example: `
  # List all tag keys (use datasource UID, not name)
  gcx datasources influxdb list-tag-keys -d UID

  # Filter by measurement
  gcx datasources influxdb list-tag-keys -d UID --measurement cpu

  # Output as JSON
  gcx datasources influxdb list-tag-keys -d UID -o json`,
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
				return fmt.Errorf("list-tag-keys is only supported in InfluxQL mode (datasource is configured for %s mode)", modeStr)
			}

			client, err := influxdb.NewClient(cfg)
			if err != nil {
				return fmt.Errorf("failed to create client: %w", err)
			}

			resp, err := client.TagKeys(ctx, datasourceUID, opts.Measurement)
			if err != nil {
				return fmt.Errorf("failed to get tag keys: %w", err)
			}

			return opts.IO.Encode(cmd.OutOrStdout(), resp)
		},
	}

	cmd.Annotations = map[string]string{
		agent.AnnotationTokenCost: "small",
		agent.AnnotationLLMHint:   "gcx datasources influxdb list-tag-keys -d UID",
	}

	opts.setup(cmd.Flags())

	return cmd
}

type tagKeysTableCodec struct{}

func (c *tagKeysTableCodec) Format() format.Format {
	return "table"
}

func (c *tagKeysTableCodec) Encode(w io.Writer, data any) error {
	resp, ok := data.(*influxdb.TagKeysResponse)
	if !ok {
		return errors.New("invalid data type for tag keys table codec")
	}

	return influxdb.FormatTagKeysTable(w, resp)
}

func (c *tagKeysTableCodec) Decode(io.Reader, any) error {
	return errors.New("tag keys table codec does not support decoding")
}
