package pyroscope

import (
	"errors"
	"fmt"
	"io"

	"github.com/grafana/gcx/internal/agent"
	dsquery "github.com/grafana/gcx/internal/datasources/query"
	"github.com/grafana/gcx/internal/format"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/query/pyroscope"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type dataRangeOpts struct {
	IO         cmdio.Options
	Datasource string
}

func (opts *dataRangeOpts) setup(flags *pflag.FlagSet) {
	opts.IO.RegisterCustomCodec("table", &dataRangeTableCodec{})
	opts.IO.DefaultFormat("table")
	opts.IO.BindFlags(flags)

	flags.StringVarP(&opts.Datasource, "datasource", "d", "", "Datasource UID (required unless datasources.pyroscope is configured)")
}

func (opts *dataRangeOpts) Validate() error {
	return opts.IO.Validate()
}

func DataRangeCmd(loader *providers.ConfigLoader) *cobra.Command {
	opts := &dataRangeOpts{}

	cmd := &cobra.Command{
		Use:   "data-range",
		Args:  cobra.NoArgs,
		Short: "Show the range of profiling data the datasource holds",
		Long: "Show the range of profiling data a Pyroscope datasource holds: whether any " +
			"data was ever ingested, and the oldest and newest profile times. The range " +
			"covers everything behind the datasource (the whole tenant), not any " +
			"particular service or label selector.\n\n" +
			"Use this to disambiguate empty query results: if no data was ever ingested, " +
			"fix ingestion before adjusting selectors; otherwise the oldest/newest bounds " +
			"show the currently queryable window.\n\n" +
			"Note that data-ingested is a lifetime flag: it stays true even after all data " +
			"has aged out of the retention period (31 days by default, tenant-configurable), " +
			"in which case the bounds render as '-' (0 in JSON). Older backends may also " +
			"report an unknown oldest bound the same way.\n\n" +
			"If gcx auto-discovers the datasource from your Grafana Cloud stack, " +
			"the discovered datasource UID may be saved to your gcx configuration " +
			"for future commands.",
		Example: `
	# Check whether the datasource holds profiling data, and for what range
	gcx datasources pyroscope data-range -d UID

	# Output as JSON (times are milliseconds since epoch)
	gcx datasources pyroscope data-range -d UID -o json`,
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

			resp, err := client.GetProfileStats(ctx, datasourceUID)
			if err != nil {
				return fmt.Errorf("failed to get data range: %w", err)
			}

			return opts.IO.Encode(cmd.OutOrStdout(), resp)
		},
	}

	cmd.Annotations = map[string]string{
		agent.AnnotationTokenCost: "small",
		agent.AnnotationLLMHint:   "gcx datasources pyroscope data-range -d UID -o json",
	}

	opts.setup(cmd.Flags())
	return cmd
}

type dataRangeTableCodec struct{}

func (c *dataRangeTableCodec) Format() format.Format {
	return "table"
}

func (c *dataRangeTableCodec) Encode(w io.Writer, data any) error {
	resp, ok := data.(*pyroscope.ProfileStatsResponse)
	if !ok {
		return errors.New("invalid data type for data-range table codec")
	}
	return pyroscope.FormatDataRangeTable(w, resp)
}

func (c *dataRangeTableCodec) Decode(io.Reader, any) error {
	return errors.New("data-range table codec does not support decoding")
}
