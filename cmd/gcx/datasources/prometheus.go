package datasources

import (
	"errors"
	"fmt"
	"io"
	"text/tabwriter"

	cmdconfig "github.com/grafana/gcx/cmd/gcx/config"
	internalconfig "github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/format"
	cmdio "github.com/grafana/gcx/internal/output"
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

//nolint:dupl
func LabelsCmd(configOpts *cmdconfig.Options) *cobra.Command {
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

			cfg, err := configOpts.LoadGrafanaConfig(ctx)
			if err != nil {
				return err
			}

			// Resolve datasource
			datasourceUID := opts.Datasource
			if datasourceUID == "" {
				fullCfg, err := configOpts.LoadConfig(ctx)
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

type metadataOpts struct {
	IO         cmdio.Options
	Datasource string
	Metric     string
}

func (opts *metadataOpts) setup(flags *pflag.FlagSet) {
	opts.IO.RegisterCustomCodec("table", &metadataTableCodec{})
	opts.IO.DefaultFormat("table")
	opts.IO.BindFlags(flags)

	flags.StringVarP(&opts.Datasource, "datasource", "d", "", "Datasource UID (required unless default-prometheus-datasource is configured)")
	flags.StringVarP(&opts.Metric, "metric", "m", "", "Filter by metric name")
}

func (opts *metadataOpts) Validate() error {
	return opts.IO.Validate()
}

//nolint:dupl
func MetadataCmd(configOpts *cmdconfig.Options) *cobra.Command {
	opts := &metadataOpts{}

	cmd := &cobra.Command{
		Use:   "metadata",
		Short: "Get metric metadata",
		Long:  "Get metadata (type, help text) for metrics from a Prometheus datasource.",
		Example: `
	# Get all metric metadata (use datasource UID, not name)
	gcx datasources prometheus metadata -d <datasource-uid>

	# Get metadata for a specific metric
	gcx datasources prometheus metadata -d <datasource-uid> --metric http_requests_total

	# Output as JSON
	gcx datasources prometheus metadata -d <datasource-uid> -o json`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}

			ctx := cmd.Context()

			cfg, err := configOpts.LoadGrafanaConfig(ctx)
			if err != nil {
				return err
			}

			// Resolve datasource
			datasourceUID := opts.Datasource
			if datasourceUID == "" {
				fullCfg, err := configOpts.LoadConfig(ctx)
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

	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "METRIC\tTYPE\tHELP")

	for metric, entries := range resp.Data {
		for _, entry := range entries {
			fmt.Fprintf(tw, "%s\t%s\t%s\n", metric, entry.Type, entry.Help)
		}
	}

	return tw.Flush()
}

func (c *metadataTableCodec) Decode(io.Reader, any) error {
	return errors.New("metadata table codec does not support decoding")
}

type targetsOpts struct {
	IO         cmdio.Options
	Datasource string
	State      string
}

func (opts *targetsOpts) setup(flags *pflag.FlagSet) {
	opts.IO.RegisterCustomCodec("table", &targetsTableCodec{})
	opts.IO.DefaultFormat("table")
	opts.IO.BindFlags(flags)

	flags.StringVarP(&opts.Datasource, "datasource", "d", "", "Datasource UID (required unless default-prometheus-datasource is configured)")
	flags.StringVar(&opts.State, "state", "", "Filter by target state: active, dropped, any (default: active)")
}

func (opts *targetsOpts) Validate() error {
	if opts.State != "" && opts.State != "active" && opts.State != "dropped" && opts.State != "any" {
		return errors.New("state must be 'active', 'dropped', or 'any'")
	}
	return opts.IO.Validate()
}

//nolint:dupl
func TargetsCmd(configOpts *cmdconfig.Options) *cobra.Command {
	opts := &targetsOpts{}

	cmd := &cobra.Command{
		Use:   "targets",
		Short: "List scrape targets",
		Long:  "List scrape targets from a Prometheus datasource.",
		Example: `
	# List active targets (use datasource UID, not name)
	gcx datasources prometheus targets -d <datasource-uid>

	# List dropped targets
	gcx datasources prometheus targets -d <datasource-uid> --state dropped

	# List all targets
	gcx datasources prometheus targets -d <datasource-uid> --state any

	# Output as JSON
	gcx datasources prometheus targets -d <datasource-uid> -o json`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}

			ctx := cmd.Context()

			cfg, err := configOpts.LoadGrafanaConfig(ctx)
			if err != nil {
				return err
			}

			// Resolve datasource
			datasourceUID := opts.Datasource
			if datasourceUID == "" {
				fullCfg, err := configOpts.LoadConfig(ctx)
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

			resp, err := client.Targets(ctx, datasourceUID, opts.State)
			if err != nil {
				return fmt.Errorf("failed to get targets: %w", err)
			}

			if opts.IO.OutputFormat == "table" {
				return prometheus.FormatTargetsTable(cmd.OutOrStdout(), resp)
			}

			return opts.IO.Encode(cmd.OutOrStdout(), resp)
		},
	}

	opts.setup(cmd.Flags())
	return cmd
}

type targetsTableCodec struct{}

func (c *targetsTableCodec) Format() format.Format {
	return "table"
}

func (c *targetsTableCodec) Encode(w io.Writer, data any) error {
	resp, ok := data.(*prometheus.TargetsResponse)
	if !ok {
		return errors.New("invalid data type for targets table codec")
	}

	return prometheus.FormatTargetsTable(w, resp)
}

func (c *targetsTableCodec) Decode(io.Reader, any) error {
	return errors.New("targets table codec does not support decoding")
}
