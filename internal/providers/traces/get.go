package traces

import (
	"errors"
	"fmt"
	"time"

	internalconfig "github.com/grafana/gcx/internal/config"
	dsquery "github.com/grafana/gcx/internal/datasources/query"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/query/tempo"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type getOpts struct {
	IO         cmdio.Options
	Datasource string
	LLM        bool
	From       string
	To         string
	Since      string
}

func (opts *getOpts) setup(flags *pflag.FlagSet) {
	opts.IO.DefaultFormat("json")
	opts.IO.BindFlags(flags)

	flags.StringVarP(&opts.Datasource, "datasource", "d", "", "Datasource UID (required unless datasources.tempo is configured)")
	flags.BoolVar(&opts.LLM, "llm", false, "Request LLM-friendly trace format")
	flags.StringVar(&opts.From, "from", "", "Start time (RFC3339, Unix timestamp, or relative like 'now-1h')")
	flags.StringVar(&opts.To, "to", "", "End time (RFC3339, Unix timestamp, or relative like 'now')")
	flags.StringVar(&opts.Since, "since", "", "Duration before --to (or now if omitted); mutually exclusive with --from")
}

func (opts *getOpts) Validate() error {
	if err := opts.IO.Validate(); err != nil {
		return err
	}

	if opts.Since == "" {
		return nil
	}

	if opts.From != "" {
		return errors.New("--since is mutually exclusive with --from")
	}

	d, err := dsquery.ParseDuration(opts.Since)
	if err != nil {
		return fmt.Errorf("invalid --since duration: %w", err)
	}
	if d <= 0 {
		return errors.New("--since must be greater than 0")
	}

	now := time.Now()
	end, err := dsquery.ParseTime(opts.To, now)
	if err != nil {
		return fmt.Errorf("invalid --to time: %w", err)
	}
	if end.IsZero() {
		end = now
	}
	opts.From = end.Add(-d).Format(time.RFC3339)
	opts.To = end.Format(time.RFC3339)

	return nil
}

func getCmd(loader *providers.ConfigLoader) *cobra.Command {
	opts := &getOpts{}

	cmd := &cobra.Command{
		Use:   "get [DATASOURCE_UID] TRACE_ID",
		Short: "Retrieve a trace by ID",
		Long: `Retrieve a single trace by its trace ID from a Tempo datasource.

DATASOURCE_UID is optional when datasources.tempo is configured in your context.
TRACE_ID is the hex-encoded trace identifier to retrieve.`,
		Example: `
  # Get a trace using configured default datasource
  gcx traces get abc123def456

  # Get a trace with explicit datasource UID
  gcx traces get tempo-001 abc123def456

  # Get LLM-friendly output
  gcx traces get abc123def456 --llm

  # Get a trace within a time range
  gcx traces get abc123def456 --since 1h`,
		Args: validateGetArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}

			ctx := cmd.Context()

			cfg, err := loader.LoadGrafanaConfig(ctx)
			if err != nil {
				return err
			}

			datasourceUID := opts.Datasource
			var traceID string

			switch len(args) {
			case 1:
				traceID = args[0]
				if datasourceUID == "" {
					fullCfg, err := loader.LoadFullConfig(ctx)
					if err != nil {
						return err
					}
					datasourceUID = internalconfig.DefaultDatasourceUID(*fullCfg.GetCurrentContext(), "tempo")
				}
			case 2:
				datasourceUID = args[0]
				traceID = args[1]
			}

			if datasourceUID == "" {
				return errors.New("datasource UID is required: use -d flag, provide it as the first positional argument, or set datasources.tempo in config")
			}

			now := time.Now()
			start, err := dsquery.ParseTime(opts.From, now)
			if err != nil {
				return fmt.Errorf("invalid --from time: %w", err)
			}
			end, err := dsquery.ParseTime(opts.To, now)
			if err != nil {
				return fmt.Errorf("invalid --to time: %w", err)
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

func validateGetArgs(_ *cobra.Command, args []string) error {
	switch len(args) {
	case 0:
		return errors.New("TRACE_ID is required")
	case 1, 2:
		return nil
	default:
		return errors.New("too many arguments: expected [DATASOURCE_UID] TRACE_ID")
	}
}
