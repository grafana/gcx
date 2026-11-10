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
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/query/loki"
	promparser "github.com/prometheus/prometheus/promql/parser"
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
	// Loki's matcher syntax matches PromQL's; this rejects unquoted values, bad regexes and multiple selectors.
	if _, err := promparser.NewParser(promparser.Options{}).ParseMetricSelector(trimmed); err != nil {
		return fmt.Errorf("invalid stream selector %q: %w", expr, err)
	}
	return nil
}

// validatePatternsStep accepts what Loki does: a positive duration or a
// positive number of seconds. An empty step is valid (Loki picks one).
func validatePatternsStep(step string) error {
	if step == "" {
		return nil
	}
	if d, err := dsquery.ParseDuration(step); err == nil && d > 0 {
		return nil
	}
	if f, err := strconv.ParseFloat(step, 64); err == nil && f > 0 && !math.IsInf(f, 0) {
		return nil
	}
	return fmt.Errorf("invalid --step %q: must be a positive duration (e.g. 30s) or a positive number of seconds (e.g. 1.5)", step)
}

// normalizePatternsLevels spells --level values the way Loki does (see
// loki.CanonicalLevel) and de-duplicates them. Values Loki does not emit itself
// are accepted: Loki passes through any detected_level a client supplies, so a
// typo cannot be told from a real custom level here. Empty values are rejected so
// a stray comma cannot silently match nothing.
func normalizePatternsLevels(levels []string) ([]string, error) {
	var out []string
	seen := map[string]bool{}
	for _, l := range levels {
		l = loki.CanonicalLevel(l)
		if l == "" {
			return nil, errors.New("invalid --level: values must not be empty (e.g. --level error,warn)")
		}
		if !seen[l] {
			seen[l] = true
			out = append(out, l)
		}
	}
	return out, nil
}

// QueryPatternsCmd returns the `query-patterns` subcommand for a Loki datasource parent.
func QueryPatternsCmd(loader *providers.ConfigLoader) *cobra.Command {
	shared := &dsquery.SharedOpts{}
	var datasource string
	var levels []string

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

Patterns are sorted by total sample count descending, then by pattern text.
JSON and YAML add totalCount, the sum of each pattern's sample counts.
Table and wide output show this total in SAMPLES.
The total describes the returned samples, not all logs in the window.

--level keeps only patterns whose level matches (case-insensitive, repeatable
or comma-separated, e.g. --level error,warn). It filters the returned patterns
client-side using the level Loki reports for each pattern, so patterns for which
Loki reports no level are dropped when --level is set. Loki's own levels are
trace, debug, info, warn, error, critical, fatal and unknown (warning is read as
warn). Other values are accepted, since Loki passes through any detected_level a
client sets, so a misspelled level matches nothing: when the filter empties the
result, the message lists the levels Loki did return. Wide output adds a LEVEL
column.

Default time range is the last hour when no time flags are given. --step is
optional: a positive duration (e.g., 30s, 1d) or a positive number of seconds
(e.g., 1.5); when omitted, Loki chooses the bucket size.`,
		Example: `
  # Detect patterns using configured default datasource
  gcx datasources loki query-patterns '{job="varlogs"}'

  # Detect patterns over a specific window
  gcx datasources loki query-patterns -d UID '{job="varlogs"}' --since 3h

  # Only error and warning patterns
  gcx datasources loki query-patterns -d UID '{job="varlogs"}' --level error,warn

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
			wantLevels, err := normalizePatternsLevels(levels)
			if err != nil {
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

			resp.FilterByLevel(wantLevels)
			// The table codecs say this themselves; structured output would
			// otherwise be an empty list indistinguishable from "no patterns".
			if len(resp.Data) == 0 && shared.IO.OutputFormat != "table" && shared.IO.OutputFormat != "wide" {
				if msg := resp.Filter.EmptyMessage(); msg != "" {
					cmdio.Warning(cmd.ErrOrStderr(), "%s", msg)
				}
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
	cmd.Flags().StringSliceVar(&levels, "level", nil, "Only show patterns with these levels, e.g. error,warn (case-insensitive; repeatable or comma-separated)")

	return cmd
}
