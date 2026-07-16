package pyroscope

import (
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/grafana/gcx/internal/agent"
	dsquery "github.com/grafana/gcx/internal/datasources/query"
	"github.com/grafana/gcx/internal/format"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/query/pyroscope"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type profileTypesOpts struct {
	dsquery.TimeRangeOpts

	IO         cmdio.Options
	Datasource string
}

func (opts *profileTypesOpts) setup(flags *pflag.FlagSet) {
	opts.IO.RegisterCustomCodec("table", &profileTypesTableCodec{})
	opts.IO.DefaultFormat("table")
	opts.IO.BindFlags(flags)

	flags.StringVarP(&opts.Datasource, "datasource", "d", "", "Datasource UID (required unless default-pyroscope-datasource is configured)")
	opts.SetupTimeFlags(flags)
}

func (opts *profileTypesOpts) Validate() error {
	if err := opts.IO.Validate(); err != nil {
		return err
	}
	return opts.ValidateTimeRange()
}

func ListProfileTypesCmd(loader *providers.ConfigLoader) *cobra.Command {
	opts := &profileTypesOpts{}

	cmd := &cobra.Command{
		Use:        "list-profile-types",
		SuggestFor: []string{"profile-types"},
		Short:      "List available profile types",
		Long: "List available profile types from a Pyroscope datasource.\n\n" +
			"If gcx auto-discovers the datasource from your Grafana Cloud stack, " +
			"the discovered datasource UID may be saved to your gcx configuration " +
			"for future commands.",
		Example: `
	# List profile types (use datasource UID, not name)
	gcx datasources pyroscope list-profile-types -d UID

	# Search a wider window than the default last hour
	gcx datasources pyroscope list-profile-types -d UID --since 24h

	# Output as JSON
	gcx datasources pyroscope list-profile-types -d UID -o json`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}

			ctx := cmd.Context()

			cfgCtx, cfg, err := dsquery.LoadContextAndConfig(ctx, loader)
			if err != nil {
				return err
			}

			datasourceUID, err := dsquery.ResolveAndSaveDatasource(ctx, loader, opts.Datasource, cfgCtx, cfg, "pyroscope")
			if err != nil {
				return err
			}

			client, err := pyroscope.NewClient(cfg)
			if err != nil {
				return fmt.Errorf("failed to create client: %w", err)
			}

			start, end, err := opts.ParseTimeRange(time.Now())
			if err != nil {
				return err
			}

			resp, err := client.ProfileTypes(ctx, datasourceUID, pyroscope.ProfileTypesRequest{
				Start: start,
				End:   end,
			})
			if err != nil {
				return fmt.Errorf("failed to get profile types: %w", err)
			}

			if len(resp.ProfileTypes) == 0 {
				emitEmptyWindowHint(cmd.ErrOrStderr(), "profile types", start, end, opts.IsRange())
			}
			if opts.IO.OutputFormat == "table" {
				return pyroscope.FormatProfileTypesTable(cmd.OutOrStdout(), resp)
			}
			return opts.IO.Encode(cmd.OutOrStdout(), resp)
		},
	}

	cmd.Annotations = map[string]string{
		agent.AnnotationTokenCost: "small",
		agent.AnnotationLLMHint:   "gcx datasources pyroscope list-profile-types -d UID -o json",
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
