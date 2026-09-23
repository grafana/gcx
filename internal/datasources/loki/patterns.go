package loki

import (
	"fmt"
	"time"

	"github.com/grafana/gcx/internal/agent"
	dsquery "github.com/grafana/gcx/internal/datasources/query"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/query/loki"
	"github.com/spf13/cobra"
)

// defaultPatternsWindow is the lookback applied when no time flags are given
// at all, since Loki's patterns endpoint requires a real start/end (unlike
// query's implicit instant-query default) — mirrors
// tempo/metrics.go's defaultTraceMetricsWindow.
const defaultPatternsWindow = time.Hour

// PatternsCmd returns the `patterns` subcommand for a Loki datasource parent.
func PatternsCmd(loader *providers.ConfigLoader) *cobra.Command {
	shared := &dsquery.SharedOpts{}
	var datasource string

	cmd := &cobra.Command{
		Use:   "patterns [EXPR]",
		Short: "Detect recurring log patterns",
		Long: `Detect recurring log line patterns for a LogQL stream selector.

EXPR is the LogQL stream selector (e.g., '{job="varlogs"}'); Loki extracts the
stream selector from a full LogQL expression server-side, so a query/metrics
expression works here too.
Datasource is resolved from -d flag or datasources.loki in your context.
Requires the Loki server to have pattern_ingester enabled — returns no
patterns (not an error) otherwise.
Default time range is the last hour when no time flags are given.`,
		Example: `
  # Detect patterns using configured default datasource
  gcx datasources loki patterns '{job="varlogs"}'

  # Detect patterns over a specific window
  gcx datasources loki patterns -d UID '{job="varlogs"}' --since 6h

  # Output as JSON
  gcx datasources loki patterns -d UID '{job="varlogs"}' -o json`,
		Args: cobra.RangeArgs(0, 1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := shared.Validate(); err != nil {
				return err
			}

			expr, err := shared.ResolveExpr(args, 0)
			if err != nil {
				return err
			}

			ctx := cmd.Context()

			cfgCtx, cfg, err := dsquery.LoadContextAndConfig(ctx, loader)
			if err != nil {
				return err
			}

			datasourceUID, err := dsquery.ResolveAndSaveDatasource(ctx, loader, datasource, cfgCtx, cfg, "loki")
			if err != nil {
				return err
			}

			now := time.Now()
			// Loki's patterns endpoint accepts step as a duration string or
			// float seconds, same shape as --step, so the raw flag value is
			// passed straight through rather than the time.Duration
			// ParseTimes would otherwise resolve it to.
			start, end, _, err := shared.ParseTimes(now)
			if err != nil {
				return err
			}
			if start.IsZero() && end.IsZero() {
				end = now
				start = now.Add(-defaultPatternsWindow)
			}

			client, err := loki.NewClient(cfg)
			if err != nil {
				return fmt.Errorf("failed to create client: %w", err)
			}

			resp, err := client.Patterns(ctx, datasourceUID, expr, start, end, shared.Step)
			if err != nil {
				return fmt.Errorf("failed to get patterns: %w", err)
			}

			if shared.IO.OutputFormat == "table" {
				return loki.FormatPatternsTable(cmd.OutOrStdout(), resp)
			}

			return shared.IO.Encode(cmd.OutOrStdout(), resp)
		},
	}

	cmd.Annotations = map[string]string{
		agent.AnnotationTokenCost: "medium",
		agent.AnnotationLLMHint:   `gcx datasources loki patterns -d UID '{job="varlogs"}' -o json`,
	}

	shared.Setup(cmd.Flags(), false)
	cmd.Flags().StringVarP(&datasource, "datasource", "d", "", "Datasource UID (required unless datasources.loki is configured)")

	return cmd
}
