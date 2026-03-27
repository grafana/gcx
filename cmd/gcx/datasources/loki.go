package datasources

import (
	"errors"
	"fmt"
	"io"

	cmdconfig "github.com/grafana/gcx/cmd/gcx/config"
	"github.com/grafana/gcx/cmd/gcx/datasources/query"
	internalconfig "github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/format"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/query/loki"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

func lokiCmd(configOpts *cmdconfig.Options) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "loki",
		Short: "Loki datasource operations",
		Long:  "Operations specific to Loki datasources such as labels and series.",
	}

	cmd.AddCommand(lokiLabelsCmd(configOpts))
	cmd.AddCommand(seriesCmd(configOpts))
	cmd.AddCommand(query.LokiCmd(configOpts))

	return cmd
}

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

//nolint:dupl
func lokiLabelsCmd(configOpts *cmdconfig.Options) *cobra.Command {
	opts := &lokiLabelsOpts{}

	cmd := &cobra.Command{
		Use:   "labels",
		Short: "List labels or label values",
		Long:  "List all labels or get values for a specific label from a Loki datasource.",
		Example: `
	# List all labels (use datasource UID, not name)
	gcx loki labels -d <datasource-uid>

	# Get values for a specific label
	gcx loki labels -d <datasource-uid> --label job

	# Output as JSON
	gcx loki labels -d <datasource-uid> -o json`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}

			ctx := cmd.Context()

			cfg, err := configOpts.LoadGrafanaConfig(ctx)
			if err != nil {
				return err
			}

			datasourceUID := opts.Datasource
			if datasourceUID == "" {
				fullCfg, err := configOpts.LoadConfig(ctx)
				if err != nil {
					return err
				}
				datasourceUID = internalconfig.DefaultDatasourceUID(*fullCfg.GetCurrentContext(), "loki")
			}
			if datasourceUID == "" {
				return errors.New("datasource UID is required: use -d flag or set default-loki-datasource in config")
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

type seriesOpts struct {
	IO         cmdio.Options
	Datasource string
	Matchers   []string
}

func (opts *seriesOpts) setup(flags *pflag.FlagSet) {
	opts.IO.RegisterCustomCodec("table", &seriesTableCodec{})
	opts.IO.DefaultFormat("table")
	opts.IO.BindFlags(flags)

	flags.StringVarP(&opts.Datasource, "datasource", "d", "", "Datasource UID (required unless default-loki-datasource is configured)")
	flags.StringArrayVarP(&opts.Matchers, "match", "M", nil, "LogQL stream selector (required, e.g., '{job=\"varlogs\"}')")
}

func (opts *seriesOpts) Validate() error {
	if err := opts.IO.Validate(); err != nil {
		return err
	}
	if len(opts.Matchers) == 0 {
		return errors.New("at least one --match selector is required")
	}
	return nil
}

//nolint:dupl
func seriesCmd(configOpts *cmdconfig.Options) *cobra.Command {
	opts := &seriesOpts{}

	cmd := &cobra.Command{
		Use:   "series",
		Short: "List log streams",
		Long:  "List log streams (series) from a Loki datasource using LogQL stream selectors. At least one --match selector is required.",
		Example: `
	# List series matching a selector (use datasource UID, not name)
	gcx loki series -d <datasource-uid> --match '{job="varlogs"}'

	# Match with regex and multiple labels
	gcx loki series -d <datasource-uid> --match '{container_name=~"prometheus.*", component="server"}'

	# Multiple matchers (OR logic)
	gcx loki series -d <datasource-uid> --match '{job="varlogs"}' --match '{namespace="default"}'

	# Output as JSON
	gcx loki series -d <datasource-uid> --match '{job="varlogs"}' -o json`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}

			ctx := cmd.Context()

			cfg, err := configOpts.LoadGrafanaConfig(ctx)
			if err != nil {
				return err
			}

			datasourceUID := opts.Datasource
			if datasourceUID == "" {
				fullCfg, err := configOpts.LoadConfig(ctx)
				if err != nil {
					return err
				}
				datasourceUID = internalconfig.DefaultDatasourceUID(*fullCfg.GetCurrentContext(), "loki")
			}
			if datasourceUID == "" {
				return errors.New("datasource UID is required: use -d flag or set default-loki-datasource in config")
			}

			client, err := loki.NewClient(cfg)
			if err != nil {
				return fmt.Errorf("failed to create client: %w", err)
			}

			resp, err := client.Series(ctx, datasourceUID, opts.Matchers)
			if err != nil {
				return fmt.Errorf("failed to get series: %w", err)
			}

			if opts.IO.OutputFormat == "table" {
				return loki.FormatSeriesTable(cmd.OutOrStdout(), resp)
			}

			return opts.IO.Encode(cmd.OutOrStdout(), resp)
		},
	}

	opts.setup(cmd.Flags())

	return cmd
}

type seriesTableCodec struct{}

func (c *seriesTableCodec) Format() format.Format {
	return "table"
}

func (c *seriesTableCodec) Encode(w io.Writer, data any) error {
	resp, ok := data.(*loki.SeriesResponse)
	if !ok {
		return errors.New("invalid data type for series table codec")
	}

	return loki.FormatSeriesTable(w, resp)
}

func (c *seriesTableCodec) Decode(io.Reader, any) error {
	return errors.New("series table codec does not support decoding")
}
