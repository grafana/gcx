package loki

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
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

// validatePatternsSelector rejects input Loki's patterns endpoint cannot accept:
// it parses only a bare stream selector, so anything that is not "{...}" fails.
func validatePatternsSelector(expr string) error {
	trimmed := strings.TrimSpace(expr)
	if trimmed == "" {
		return errors.New("a stream selector is required, e.g. '{job=\"varlogs\"}'")
	}
	if !strings.HasPrefix(trimmed, "{") || !strings.HasSuffix(trimmed, "}") {
		return fmt.Errorf("the expression must be a bare stream selector such as '{job=\"varlogs\"}'; pipeline stages, line filters and metric expressions are rejected by Loki (got %q)", expr)
	}
	return nil
}

// validatePatternsStep accepts what Loki does: a positive duration or a
// positive number of seconds. An empty step is valid (Loki picks one).
func validatePatternsStep(step string) error {
	if step == "" {
		return nil
	}
	if d, err := time.ParseDuration(step); err == nil && d > 0 {
		return nil
	}
	if f, err := strconv.ParseFloat(step, 64); err == nil && f > 0 && !math.IsInf(f, 0) {
		return nil
	}
	return fmt.Errorf("invalid --step %q: must be a positive duration (e.g. 30s) or a positive number of seconds (e.g. 1.5)", step)
}

// QueryPatternsCmd returns the `query-patterns` subcommand for a Loki datasource parent.
func QueryPatternsCmd(loader *providers.ConfigLoader) *cobra.Command {
	shared := &dsquery.SharedOpts{}
	var datasource string

	cmd := &cobra.Command{
		Use:   "query-patterns [EXPR]",
		Short: "Detect recurring log patterns",
		Long: `Detect recurring log line patterns for a LogQL stream selector.

EXPR must be a bare stream selector (e.g., '{job="varlogs"}'). Loki's patterns
endpoint rejects pipeline stages, line filters, and metric expressions.
Datasource is resolved from -d flag or datasources.loki in your context.

Requires a Loki version that supports the patterns API (3.x) with the pattern
ingester and querier enabled. Otherwise the endpoint may be unavailable and the
command fails with an error rather than returning empty data.

The result is the patterns and sample counts the backend retained for the
range, not a complete inventory: Loki prunes low-volume patterns and caps the
number returned, and retention depends on the deployment, so a long --since
does not guarantee complete coverage of that window.

Default time range is the last hour when no time flags are given. --step is
optional: a positive duration (e.g., 30s) or a positive number of seconds
(e.g., 1.5); when omitted, Loki chooses the bucket size.`,
		Example: `
  # Detect patterns using configured default datasource
  gcx datasources loki query-patterns '{job="varlogs"}'

  # Detect patterns over a specific window
  gcx datasources loki query-patterns -d UID '{job="varlogs"}' --since 6h

  # Output as JSON
  gcx datasources loki query-patterns -d UID '{job="varlogs"}' -o json`,
		Args: cobra.RangeArgs(0, 1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := shared.Validate(); err != nil {
				return err
			}

			expr, err := shared.ResolveExpr(args, 0)
			if err != nil {
				return err
			}

			// Everything below up to the config load is statically decidable, so
			// reject bad input before any config or datasource I/O.
			if err := validatePatternsSelector(expr); err != nil {
				return err
			}
			if err := validatePatternsStep(shared.Step); err != nil {
				return err
			}

			now := time.Now()
			// ParseTimeRange, not ParseTimes: Loki's patterns endpoint accepts
			// step as a duration OR a bare float number of seconds, which
			// ParseTimes' duration-only parsing would reject.
			start, end, err := shared.ParseTimeRange(now)
			if err != nil {
				return err
			}
			if start.IsZero() && end.IsZero() {
				end = now
				start = now.Add(-defaultPatternsWindow)
			}
			if !start.Before(end) {
				return fmt.Errorf("invalid time range: start (%s) must be before end (%s)", start.Format(time.RFC3339), end.Format(time.RFC3339))
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

			client, err := loki.NewClient(cfg)
			if err != nil {
				return fmt.Errorf("failed to create client: %w", err)
			}

			resp, err := client.Patterns(ctx, datasourceUID, expr, start, end, shared.Step)
			if err != nil {
				return fmt.Errorf("failed to get patterns: %w", err)
			}

			return shared.IO.Encode(cmd.OutOrStdout(), resp)
		},
	}

	cmd.Annotations = map[string]string{
		agent.AnnotationTokenCost: "medium",
		agent.AnnotationLLMHint:   `gcx datasources loki query-patterns -d UID '{job="varlogs"}' --since 1h -o json`,
	}

	shared.Setup(cmd.Flags(), false)
	cmd.Flags().StringVarP(&datasource, "datasource", "d", "", "Datasource UID (required unless datasources.loki is configured)")

	return cmd
}
