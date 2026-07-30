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

type statsOpts struct {
	IO         cmdio.Options
	Datasource string
}

func (opts *statsOpts) setup(flags *pflag.FlagSet) {
	opts.IO.RegisterCustomCodec("table", &statsTableCodec{})
	opts.IO.DefaultFormat("table")
	opts.IO.BindFlags(flags)

	flags.StringVarP(&opts.Datasource, "datasource", "d", "", "Datasource UID (required unless datasources.pyroscope is configured)")
}

func (opts *statsOpts) Validate() error {
	return opts.IO.Validate()
}

func StatsCmd(loader *providers.ConfigLoader) *cobra.Command {
	opts := &statsOpts{}

	cmd := &cobra.Command{
		Use:   "stats",
		Short: "Show ingestion stats (data ingested, oldest/newest profile times)",
		Long: "Show ingestion stats for a Pyroscope datasource: whether any " +
			"profiling data was ever ingested, and the oldest and newest profile times.\n\n" +
			"Use this to disambiguate empty query results: if no data was ever ingested, " +
			"fix ingestion before adjusting selectors; otherwise the oldest/newest bounds " +
			"show the actual queryable time window.\n\n" +
			"If gcx auto-discovers the datasource from your Grafana Cloud stack, " +
			"the discovered datasource UID may be saved to your gcx configuration " +
			"for future commands.",
		Example: `
	# Check whether the datasource is receiving profiling data
	gcx datasources pyroscope stats -d UID

	# Output as JSON (times are milliseconds since epoch)
	gcx datasources pyroscope stats -d UID -o json`,
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
				return fmt.Errorf("failed to get profile stats: %w", err)
			}

			return opts.IO.Encode(cmd.OutOrStdout(), resp)
		},
	}

	cmd.Annotations = map[string]string{
		agent.AnnotationTokenCost: "small",
		agent.AnnotationLLMHint:   "gcx datasources pyroscope stats -d UID -o json",
	}

	opts.setup(cmd.Flags())
	return cmd
}

type statsTableCodec struct{}

func (c *statsTableCodec) Format() format.Format {
	return "table"
}

func (c *statsTableCodec) Encode(w io.Writer, data any) error {
	resp, ok := data.(*pyroscope.ProfileStatsResponse)
	if !ok {
		return errors.New("invalid data type for profile stats table codec")
	}
	return pyroscope.FormatProfileStatsTable(w, resp)
}

func (c *statsTableCodec) Decode(io.Reader, any) error {
	return errors.New("profile stats table codec does not support decoding")
}
