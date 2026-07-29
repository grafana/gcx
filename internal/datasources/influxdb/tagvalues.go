package influxdb

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

type tagValuesOpts struct {
	IO          cmdio.Options
	Datasource  string
	Key         string
	Measurement string
}

func (opts *tagValuesOpts) setup(flags *pflag.FlagSet) {
	opts.IO.RegisterCustomCodec("table", &tagValuesTableCodec{})
	opts.IO.DefaultFormat("table")
	opts.IO.BindFlags(flags)

	flags.StringVarP(&opts.Datasource, "datasource", "d", "", "Datasource UID (required unless datasources.influxdb is configured)")
	flags.StringVarP(&opts.Key, "key", "k", "", "Tag key to get values for (required)")
	flags.StringVarP(&opts.Measurement, "measurement", "m", "", "Filter by measurement name")
}

func (opts *tagValuesOpts) Validate() error {
	if opts.Key == "" {
		return errors.New("--key is required")
	}
	return opts.IO.Validate()
}

// TagValuesCmd returns the `list-tag-values` subcommand.
func TagValuesCmd(loader *providers.ConfigLoader) *cobra.Command {
	opts := &tagValuesOpts{}

	cmd := &cobra.Command{
		Use:   "list-tag-values",
		Short: "List tag values",
		Long:  "List tag values for a given key from an InfluxDB datasource. Only supported in InfluxQL mode.",
		Example: `
  # List values for a tag key (use datasource UID, not name)
  gcx datasources influxdb list-tag-values -d UID --key host

  # Filter by measurement
  gcx datasources influxdb list-tag-values -d UID --key host --measurement cpu

  # Output as JSON
  gcx datasources influxdb list-tag-values -d UID --key host -o json`,
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
				return fmt.Errorf("list-tag-values is only supported in InfluxQL mode (datasource is configured for %s mode)", modeStr)
			}

			client, err := influxdb.NewClient(cfg)
			if err != nil {
				return fmt.Errorf("failed to create client: %w", err)
			}

			resp, err := client.TagValues(ctx, datasourceUID, opts.Key, opts.Measurement)
			if err != nil {
				return fmt.Errorf("failed to get tag values: %w", err)
			}

			return opts.IO.Encode(cmd.OutOrStdout(), resp)
		},
	}

	cmd.Annotations = map[string]string{
		agent.AnnotationTokenCost: "small",
		agent.AnnotationLLMHint:   "gcx datasources influxdb list-tag-values -d UID --key host",
	}

	opts.setup(cmd.Flags())

	return cmd
}

type tagValuesTableCodec struct{}

func (c *tagValuesTableCodec) Format() format.Format {
	return "table"
}

func (c *tagValuesTableCodec) Encode(w io.Writer, data any) error {
	resp, ok := data.(*influxdb.TagValuesResponse)
	if !ok {
		return errors.New("invalid data type for tag values table codec")
	}

	return influxdb.FormatTagValuesTable(w, resp)
}

func (c *tagValuesTableCodec) Decode(io.Reader, any) error {
	return errors.New("tag values table codec does not support decoding")
}
