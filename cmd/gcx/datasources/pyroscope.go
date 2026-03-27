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
	"github.com/grafana/gcx/internal/query/pyroscope"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

func pyroscopeCmd(configOpts *cmdconfig.Options) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "pyroscope",
		Short: "Pyroscope datasource operations",
		Long:  "Operations specific to Pyroscope datasources such as profile-types and labels.",
	}

	cmd.AddCommand(profileTypesCmd(configOpts))
	cmd.AddCommand(pyroscopeLabelsCmd(configOpts))
	cmd.AddCommand(query.PyroscopeCmd(configOpts))

	return cmd
}

type profileTypesOpts struct {
	IO         cmdio.Options
	Datasource string
}

func (opts *profileTypesOpts) setup(flags *pflag.FlagSet) {
	opts.IO.RegisterCustomCodec("table", &profileTypesTableCodec{})
	opts.IO.DefaultFormat("table")
	opts.IO.BindFlags(flags)

	flags.StringVarP(&opts.Datasource, "datasource", "d", "", "Datasource UID (required unless default-pyroscope-datasource is configured)")
}

func (opts *profileTypesOpts) Validate() error {
	return opts.IO.Validate()
}

func profileTypesCmd(configOpts *cmdconfig.Options) *cobra.Command {
	opts := &profileTypesOpts{}

	cmd := &cobra.Command{
		Use:   "profile-types",
		Short: "List available profile types",
		Long:  "List available profile types from a Pyroscope datasource.",
		Example: `
	# List profile types (use datasource UID, not name)
	gcx datasources pyroscope profile-types -d <datasource-uid>

	# Output as JSON
	gcx datasources pyroscope profile-types -d <datasource-uid> -o json`,
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
				datasourceUID = internalconfig.DefaultDatasourceUID(*fullCfg.GetCurrentContext(), "pyroscope")
			}
			if datasourceUID == "" {
				return errors.New("datasource UID is required: use -d flag or set default-pyroscope-datasource in config")
			}

			client, err := pyroscope.NewClient(cfg)
			if err != nil {
				return fmt.Errorf("failed to create client: %w", err)
			}

			resp, err := client.ProfileTypes(ctx, datasourceUID, pyroscope.ProfileTypesRequest{})
			if err != nil {
				return fmt.Errorf("failed to get profile types: %w", err)
			}

			codec, err := opts.IO.Codec()
			if err != nil {
				return err
			}

			if opts.IO.OutputFormat == "table" {
				return pyroscope.FormatProfileTypesTable(cmd.OutOrStdout(), resp)
			}
			return codec.Encode(cmd.OutOrStdout(), resp)
		},
	}

	opts.setup(cmd.Flags())
	return cmd
}

type profileTypesTableCodec struct{}

func (c *profileTypesTableCodec) Format() format.Format {
	return "table"
}

func (c *profileTypesTableCodec) Encode(w io.Writer, data any) error {
	resp, ok := data.(*pyroscope.ProfileTypesResponse)
	if !ok {
		return errors.New("invalid data type for profile types table codec")
	}
	return pyroscope.FormatProfileTypesTable(w, resp)
}

func (c *profileTypesTableCodec) Decode(io.Reader, any) error {
	return errors.New("profile types table codec does not support decoding")
}

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

func pyroscopeLabelsCmd(configOpts *cmdconfig.Options) *cobra.Command {
	opts := &pyroscopeLabelsOpts{}

	cmd := &cobra.Command{
		Use:   "labels",
		Short: "List labels or label values",
		Long:  "List all labels or get values for a specific label from a Pyroscope datasource.",
		Example: `
	# List all labels (use datasource UID, not name)
	gcx datasources pyroscope labels -d <datasource-uid>

	# Get values for a specific label
	gcx datasources pyroscope labels -d <datasource-uid> --label service_name

	# Output as JSON
	gcx datasources pyroscope labels -d <datasource-uid> -o json`,
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
				datasourceUID = internalconfig.DefaultDatasourceUID(*fullCfg.GetCurrentContext(), "pyroscope")
			}
			if datasourceUID == "" {
				return errors.New("datasource UID is required: use -d flag or set default-pyroscope-datasource in config")
			}

			client, err := pyroscope.NewClient(cfg)
			if err != nil {
				return fmt.Errorf("failed to create client: %w", err)
			}

			codec, err := opts.IO.Codec()
			if err != nil {
				return err
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
				return codec.Encode(cmd.OutOrStdout(), resp)
			}

			resp, err := client.LabelNames(ctx, datasourceUID, pyroscope.LabelNamesRequest{})
			if err != nil {
				return fmt.Errorf("failed to get labels: %w", err)
			}

			if opts.IO.OutputFormat == "table" {
				return pyroscope.FormatLabelsTable(cmd.OutOrStdout(), resp.Names)
			}
			return codec.Encode(cmd.OutOrStdout(), resp)
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
