package tempo

import (
	"fmt"

	"github.com/grafana/gcx/internal/agent"
	dsquery "github.com/grafana/gcx/internal/datasources/query"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/query/tempo"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type diffOpts struct {
	IO         cmdio.Options
	Datasource string
}

func (opts *diffOpts) setup(flags *pflag.FlagSet) {
	// The trace-diff patch is an opaque map; table/wide have no meaningful
	// projection and would fail at encode time, so only JSON/YAML are offered.
	dsquery.RegisterStructuredCodecs(&opts.IO)
	opts.IO.BindFlags(flags)

	flags.StringVarP(&opts.Datasource, "datasource", "d", "", "Datasource UID (required unless datasources.tempo is configured)")
}

func (opts *diffOpts) Validate() error {
	return opts.IO.Validate()
}

// DiffCmd returns the `diff` subcommand for comparing two traces.
func DiffCmd(loader *providers.ConfigLoader) *cobra.Command {
	opts := &diffOpts{}

	cmd := &cobra.Command{
		Use:   "diff TRACE_A TRACE_B",
		Short: "[experimental] Compare two traces (baseline vs comparison)",
		Long: `[experimental] Compare two traces using the Tempo trace-diff API.

This is an experimental, Grafana Cloud-only endpoint: it may be unavailable on
self-hosted or OSS Tempo, and its request/response shape may change.

TRACE_A is the baseline trace and TRACE_B is the comparison trace. Deltas use
B - A semantics: negative means B is faster (improvement), positive means B is
slower (regression).

Datasource is resolved from the -d flag or datasources.tempo in your context.`,
		Example: `
  # Compare two traces (B - A semantics)
  gcx traces diff abc123 def456

  # With an explicit datasource UID, JSON output
  gcx traces diff -d UID abc123 def456 -o json`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}

			ctx := cmd.Context()

			cfgCtx, cfg, err := dsquery.LoadContextAndConfig(ctx, loader)
			if err != nil {
				return err
			}

			datasourceUID, err := dsquery.ResolveAndSaveDatasource(ctx, loader, opts.Datasource, cfgCtx, cfg, "tempo")
			if err != nil {
				return err
			}

			client, err := tempo.NewClient(cfg)
			if err != nil {
				return fmt.Errorf("failed to create client: %w", err)
			}

			resp, err := client.Diff(ctx, datasourceUID, tempo.DiffRequest{
				BaseTraceID:    args[0],
				CompareTraceID: args[1],
			})
			if err != nil {
				return fmt.Errorf("trace diff failed: %w", err)
			}

			return opts.IO.Encode(cmd.OutOrStdout(), resp)
		},
	}

	cmd.Annotations = map[string]string{
		agent.AnnotationTokenCost:    "medium",
		agent.AnnotationLLMHint:      "gcx traces diff -d UID <trace-a> <trace-b> -o json",
		agent.AnnotationAvailability: agent.AvailabilityCloudOnly,
		agent.AnnotationStability:    agent.StabilityExperimental,
	}

	opts.setup(cmd.Flags())

	return cmd
}
