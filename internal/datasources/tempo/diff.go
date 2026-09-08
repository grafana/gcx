package tempo

import (
	"fmt"
	"time"

	"github.com/grafana/gcx/internal/agent"
	dsquery "github.com/grafana/gcx/internal/datasources/query"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/query/tempo"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type diffOpts struct {
	dsquery.TimeRangeOpts

	IO         cmdio.Options
	Datasource string
}

func (opts *diffOpts) setup(flags *pflag.FlagSet) {
	// The trace-diff patch is an opaque map; table/wide have no meaningful
	// projection and would fail at encode time, so only JSON/YAML are offered.
	dsquery.RegisterStructuredCodecs(&opts.IO)
	opts.IO.BindFlags(flags)

	flags.StringVarP(&opts.Datasource, "datasource", "d", "", "Datasource UID (required unless datasources.tempo is configured)")
	opts.SetupTimeFlags(flags)
}

func (opts *diffOpts) Validate() error {
	if err := opts.IO.Validate(); err != nil {
		return err
	}
	return opts.ValidateTimeRange()
}

// DiffCmd returns the `diff` subcommand for comparing two traces.
func DiffCmd(loader *providers.ConfigLoader) *cobra.Command {
	opts := &diffOpts{}

	cmd := &cobra.Command{
		Use:   "diff TRACE_A TRACE_B",
		Short: "[experimental] Compare execution of two traces.",
		Long: `This command is experimental. It may be removed, or its subcommands, flags and
responses may change without following the normal semantic versioning conventions.

Compare two known trace IDs using the Grafana Cloud-only trace-diff API;
use 'gcx traces baseline' first only when you need candidate IDs.

TRACE_A is the prospective baseline and TRACE_B is the comparison, with deltas
B - A: positive duration means B is slower and negative means faster, not
automatically a regression or improvement (a request can fail early).

Use candidate bodies and exploratory diffs to assess comparability, rejecting
obvious context mismatches first and interpreting timing at the affected request
boundary; a diff localizes execution changes but does not establish their cause.

Keep the same context/datasource and make --from/--to (or --since) cover both
executions; without time bounds the lookup uses the full lookback, and if the
endpoint is unavailable, use 'gcx traces get --llm' to compare both bodies
manually instead.`,
		Example: `
  # Compare an already-known pair directly; baseline search is not required
  gcx traces diff --context prod -d UID <baseline-id> <comparison-id>

  # Inspect a plausible candidate before using a diff to assess it
  gcx traces get --context prod -d UID <candidate-id> --llm -o agents

  # Assess selected candidates against the seed; repeat only as useful
  gcx traces diff --context prod -d UID <candidate-id> <seed-id>

  # Bound BOTH trace lookups, including an older candidate, and allow spilling
  gcx traces diff --context prod -d UID <candidate-id> <seed-id> \
    --from 2026-01-15T08:00:00Z --to 2026-01-15T10:00:00Z -o agents`,
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

			start, end, err := opts.ParseTimeRange(time.Now())
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
				Start:          start,
				End:            end,
			})
			if err != nil {
				return fmt.Errorf("trace diff failed: %w", err)
			}

			return opts.IO.Encode(cmd.OutOrStdout(), resp)
		},
	}

	cmd.Annotations = map[string]string{
		agent.AnnotationTokenCost:    "medium",
		agent.AnnotationLLMHint:      `gcx datasources tempo diff --context <context> -d UID <candidate-id> <seed-id> --from "$PAIR_FROM" --to "$PAIR_TO" -o agents`,
		agent.AnnotationAvailability: agent.AvailabilityCloudOnly,
		agent.AnnotationStability:    agent.StabilityExperimental,
	}

	opts.setup(cmd.Flags())

	return cmd
}
