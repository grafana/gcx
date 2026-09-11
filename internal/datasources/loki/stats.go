package loki

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
	"github.com/grafana/gcx/internal/query/loki"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type statsOpts struct {
	IO         cmdio.Options
	Time       dsquery.TimeRangeOpts
	Expr       dsquery.ExprOpts
	Datasource string
}

func (opts *statsOpts) setup(flags *pflag.FlagSet) {
	opts.IO.RegisterCustomCodec("table", &lokiStatsTableCodec{})
	opts.IO.DefaultFormat("table")
	opts.IO.BindFlags(flags)
	opts.Time.SetupTimeFlags(flags)
	opts.Expr.SetupExprFlag(flags, "LogQL expression (alternative to positional argument)")

	flags.StringVarP(&opts.Datasource, "datasource", "d", "", "Datasource UID (required unless datasources.loki is configured)")
}

func (opts *statsOpts) Validate() error {
	if err := opts.IO.Validate(); err != nil {
		return err
	}
	return opts.Time.ValidateTimeRange()
}

// StatsCmd returns the `stats` subcommand, which queries Loki's index-stats
// endpoint for a label matcher and time range without executing the query —
// useful to estimate the cost of a query before running it with 'query' or
// 'metrics'.
func StatsCmd(loader *providers.ConfigLoader) *cobra.Command {
	opts := &statsOpts{}

	cmd := &cobra.Command{
		Use:   "stats [EXPR]",
		Short: "Show index stats (streams/chunks/bytes/entries) for a LogQL selector without executing it",
		Long: `Query Loki's index-stats endpoint for a label matcher and time range.

Returns stream/chunk/byte/entry counts WITHOUT executing the query — useful to
estimate the cost of a query before running it with 'loki query' or 'loki metrics'.

EXPR is the LogQL expression to evaluate (the same expression accepted by
'query'/'metrics' can be reused as-is) — only its stream selector(s) (the
'{...}' matcher) are sent to the index-stats endpoint, since Loki's index only
tracks streams, not line filters or parsing stages, aggregations, or range
vectors. The estimate reflects all data in the matched streams, not the
narrower set a filter like '|= "error"' would actually return. An expression
combining multiple selectors (e.g. via a binary operator) sums each
selector's stats into a single total.
When no time flags are given, defaults to the last minute (now-1m to now),
matching the instant-query default used by 'query'/'metrics'. That window is
widened by any range-vector duration or offset in EXPR (e.g. '[24h]',
'offset 1h'), since Loki evaluates further back than --from/--to/--since
alone would suggest.`,
		Example: `
  # Estimate bytes scanned by a selector over the last hour
  gcx datasources loki stats -d UID '{job="varlogs"}' --since 1h

  # Output as JSON
  gcx datasources loki stats -d UID '{job="varlogs"}' --since 1h -o json`,
		Args: cobra.RangeArgs(0, 1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}

			expr, err := opts.Expr.ResolveExpr(args, 0)
			if err != nil {
				return err
			}

			selectors := loki.ExtractStreamSelectors(expr)
			if len(selectors) == 0 {
				return fmt.Errorf("no LogQL stream selector (e.g. {app=\"...\"}) found in expression %q", expr)
			}

			ctx := cmd.Context()

			cfgCtx, cfg, err := dsquery.LoadContextAndConfig(ctx, loader)
			if err != nil {
				return err
			}

			datasourceUID, err := dsquery.ResolveAndSaveDatasource(ctx, loader, opts.Datasource, cfgCtx, cfg, "loki")
			if err != nil {
				return err
			}

			now := time.Now()
			start, end, err := opts.Time.ParseTimeRange(now)
			if err != nil {
				return err
			}
			if !opts.Time.IsRange() {
				start, end = now.Add(-time.Minute), now
			}
			// A range-vector duration (e.g. "[24h]") or "offset" modifier in
			// expr makes Loki actually evaluate further back than start/end
			// alone would suggest — widen the window so the estimate doesn't
			// undercount what 'query'/'metrics' would really scan.
			if lookback := loki.MaxLookback(expr); lookback > 0 {
				start = start.Add(-lookback)
			}

			client, err := loki.NewClient(cfg)
			if err != nil {
				return fmt.Errorf("failed to create client: %w", err)
			}

			resp := &loki.IndexStatsResponse{}
			for _, selector := range selectors {
				selectorResp, err := client.IndexStats(ctx, datasourceUID, selector, start, end)
				if err != nil {
					return fmt.Errorf("failed to get index stats for selector %s: %w", selector, err)
				}
				resp.Streams += selectorResp.Streams
				resp.Chunks += selectorResp.Chunks
				resp.Bytes += selectorResp.Bytes
				resp.Entries += selectorResp.Entries
			}

			if opts.IO.OutputFormat == "table" {
				return loki.FormatIndexStatsTable(cmd.OutOrStdout(), resp)
			}

			return opts.IO.Encode(cmd.OutOrStdout(), resp)
		},
	}

	cmd.Annotations = map[string]string{
		agent.AnnotationTokenCost: "small",
		agent.AnnotationLLMHint:   `gcx datasources loki stats -d UID '{job="varlogs"}' --since 1h -o json`,
	}

	opts.setup(cmd.Flags())

	return cmd
}

type lokiStatsTableCodec struct{}

func (c *lokiStatsTableCodec) Format() format.Format {
	return "table"
}

func (c *lokiStatsTableCodec) Encode(w io.Writer, data any) error {
	resp, ok := data.(*loki.IndexStatsResponse)
	if !ok {
		return errors.New("invalid data type for stats table codec")
	}

	return loki.FormatIndexStatsTable(w, resp)
}

func (c *lokiStatsTableCodec) Decode(io.Reader, any) error {
	return errors.New("stats table codec does not support decoding")
}
