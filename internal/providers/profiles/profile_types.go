package profiles

import (
	"errors"
	"fmt"
	"io"

	internalconfig "github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/format"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/query/pyroscope"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

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

func profileTypesCmd(loader *providers.ConfigLoader) *cobra.Command {
	opts := &profileTypesOpts{}

	cmd := &cobra.Command{
		Use:   "profile-types",
		Short: "List available profile types",
		Long:  "List available profile types from a Pyroscope datasource.",
		Example: `
	# List profile types (use datasource UID, not name)
	gcx profiles profile-types -d <datasource-uid>

	# Output as JSON
	gcx profiles profile-types -d <datasource-uid> -o json`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}

			ctx := cmd.Context()

			cfg, err := loader.LoadGrafanaConfig(ctx)
			if err != nil {
				return err
			}

			datasourceUID := opts.Datasource
			if datasourceUID == "" {
				fullCfg, err := loader.LoadFullConfig(ctx)
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
