package traces

import (
	"fmt"
	"log/slog"
	"time"

	internalconfig "github.com/grafana/gcx/internal/config"
	dsquery "github.com/grafana/gcx/internal/datasources/query"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/query/tempo"
	"github.com/grafana/grafana-app-sdk/logging"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type getOpts struct {
	dsquery.TimeRangeOpts

	IO         cmdio.Options
	Datasource string
	LLM        bool
}

func (opts *getOpts) setup(flags *pflag.FlagSet) {
	opts.IO.DefaultFormat("json")
	opts.IO.BindFlags(flags)

	flags.StringVarP(&opts.Datasource, "datasource", "d", "", "Datasource UID (required unless datasources.tempo is configured)")
	flags.BoolVar(&opts.LLM, "llm", false, "Request LLM-friendly trace format")
	opts.SetupTimeFlags(flags)
}

func (opts *getOpts) Validate() error {
	if err := opts.IO.Validate(); err != nil {
		return err
	}
	return opts.ValidateTimeRange()
}

func getCmd(loader *providers.ConfigLoader) *cobra.Command {
	opts := &getOpts{}

	cmd := &cobra.Command{
		Use:   "get TRACE_ID",
		Short: "Retrieve a trace by ID",
		Long: `Retrieve a single trace by its trace ID from a Tempo datasource.

TRACE_ID is the hex-encoded trace identifier to retrieve.
Datasource is resolved from -d flag or datasources.tempo in your context.`,
		Example: `
  # Get a trace using configured default datasource
  gcx traces get abc123def456

  # Get a trace with explicit datasource UID
  gcx traces get -d tempo-001 abc123def456

  # Get LLM-friendly output
  gcx traces get abc123def456 --llm

  # Get a trace within a time range
  gcx traces get abc123def456 --since 1h`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}

			ctx := cmd.Context()

			// Resolve datasource UID from -d flag, config, or Grafana auto-discovery.
			var cfgCtx *internalconfig.Context
			fullCfg, err := loader.LoadFullConfig(ctx)
			if err != nil {
				logging.FromContext(ctx).Warn("could not load config; falling back to auto-discovery", slog.String("error", err.Error()))
			} else {
				cfgCtx = fullCfg.GetCurrentContext()
			}

			cfg, err := loader.LoadGrafanaConfig(ctx)
			if err != nil {
				return err
			}

			datasourceUID, err := dsquery.ResolveAndSaveDatasource(ctx, loader, opts.Datasource, cfgCtx, cfg, "tempo")
			if err != nil {
				return err
			}

			traceID := args[0]

			now := time.Now()
			start, end, err := opts.ParseTimeRange(now)
			if err != nil {
				return err
			}

			client, err := tempo.NewClient(cfg)
			if err != nil {
				return fmt.Errorf("failed to create client: %w", err)
			}

			req := tempo.GetTraceRequest{
				TraceID:   traceID,
				Start:     start,
				End:       end,
				LLMFormat: opts.LLM,
			}

			resp, err := client.GetTrace(ctx, datasourceUID, req)
			if err != nil {
				return fmt.Errorf("get trace failed: %w", err)
			}

			return opts.IO.Encode(cmd.OutOrStdout(), resp)
		},
	}

	opts.setup(cmd.Flags())

	return cmd
}
