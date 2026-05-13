package loki

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"time"

	"github.com/grafana/gcx/internal/agent"
	internalconfig "github.com/grafana/gcx/internal/config"
	dsquery "github.com/grafana/gcx/internal/datasources/query"
	"github.com/grafana/gcx/internal/format"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/query/loki"
	"github.com/grafana/grafana-app-sdk/logging"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type lokiPatternsOpts struct {
	dsquery.TimeRangeOpts

	IO         cmdio.Options
	Datasource string
	Step       string
	Expr       string
}

func (opts *lokiPatternsOpts) setup(flags *pflag.FlagSet) {
	opts.IO.RegisterCustomCodec("table", &lokiPatternsTableCodec{})
	opts.IO.DefaultFormat("table")
	opts.IO.BindFlags(flags)

	flags.StringVarP(&opts.Datasource, "datasource", "d", "", "Datasource UID (required unless datasources.loki is configured)")
	flags.StringVar(&opts.Expr, "expr", "", "LogQL stream selector (alternative to positional argument)")
	opts.SetupTimeFlags(flags)
	flags.StringVar(&opts.Step, "step", "", "Step between pattern samples (e.g., '15s', '1m')")
}

func (opts *lokiPatternsOpts) Validate() error {
	if err := opts.IO.Validate(); err != nil {
		return err
	}
	return opts.ValidateTimeRange()
}

func (opts *lokiPatternsOpts) resolveExpr(args []string) (string, error) {
	haveFlag := opts.Expr != ""
	haveArg := len(args) > 0

	if haveFlag && haveArg {
		return "", errors.New("provide the expression as a positional argument or via --expr, not both")
	}
	if !haveFlag && !haveArg {
		return "", errors.New("expression is required: provide a LogQL stream selector as a positional argument or via --expr")
	}
	if haveFlag {
		return opts.Expr, nil
	}
	return args[0], nil
}

// PatternsCmd returns the `patterns` subcommand for a Loki datasource parent.
func PatternsCmd(loader *providers.ConfigLoader) *cobra.Command {
	opts := &lokiPatternsOpts{}

	cmd := &cobra.Command{
		Use:   "patterns [EXPR]",
		Short: "Detect log patterns from a Loki datasource",
		Long: `Detect log patterns from a Loki datasource.

EXPR is a LogQL stream selector (e.g., {job="varlogs"}).
The result shows detected patterns aggregated across matching streams,
sorted by total occurrence count.

Requires pattern_ingester to be enabled on the Loki instance.
A time range is required; defaults to --since 1h if not specified.`,
		Example: `
  # Detect patterns over the last hour
  gcx datasources loki patterns '{job="varlogs"}'

  # Patterns with explicit time range
  gcx datasources loki patterns '{job="varlogs"}' --from 2024-01-01T00:00:00Z --to 2024-01-01T01:00:00Z

  # Output as JSON (includes per-timestamp samples)
  gcx datasources loki patterns '{job="varlogs"}' --since 1h -o json`,
		Args: cobra.RangeArgs(0, 1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}

			expr, err := opts.resolveExpr(args)
			if err != nil {
				return err
			}

			// Default to --since 1h when no time range is specified.
			if !opts.IsRange() && opts.Since == "" {
				opts.Since = "1h"
				if err := opts.ValidateTimeRange(); err != nil {
					return err
				}
			}

			ctx := cmd.Context()

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

			datasourceUID, err := dsquery.ResolveAndSaveDatasource(ctx, loader, opts.Datasource, cfgCtx, cfg, "loki")
			if err != nil {
				return err
			}

			dsType, err := dsquery.GetDatasourceType(ctx, cfg, datasourceUID)
			if err != nil {
				return err
			}
			if err := dsquery.ValidateDatasourceType(dsType, "loki"); err != nil {
				return err
			}

			now := time.Now()
			start, end, err := opts.ParseTimeRange(now)
			if err != nil {
				return err
			}

			var step time.Duration
			if opts.Step != "" {
				step, err = dsquery.ParseDuration(opts.Step)
				if err != nil {
					return fmt.Errorf("invalid --step duration: %w", err)
				}
			}

			client, err := loki.NewClient(cfg)
			if err != nil {
				return fmt.Errorf("failed to create client: %w", err)
			}

			req := loki.PatternsRequest{
				Query: expr,
				Start: start,
				End:   end,
				Step:  step,
			}

			resp, err := client.Patterns(ctx, datasourceUID, req)
			if err != nil {
				return fmt.Errorf("patterns query failed: %w", err)
			}

			return opts.IO.Encode(cmd.OutOrStdout(), resp)
		},
	}

	cmd.Annotations = map[string]string{
		agent.AnnotationTokenCost: "medium",
		agent.AnnotationLLMHint:   `gcx datasources loki patterns -d UID '{job="grafana"}' --since 1h -o json`,
	}

	opts.setup(cmd.Flags())

	return cmd
}

type lokiPatternsTableCodec struct{}

func (c *lokiPatternsTableCodec) Format() format.Format {
	return "table"
}

func (c *lokiPatternsTableCodec) Encode(w io.Writer, data any) error {
	resp, ok := data.(*loki.PatternsResponse)
	if !ok {
		return errors.New("invalid data type for loki patterns table codec")
	}
	return loki.FormatPatternsTable(w, resp)
}

func (c *lokiPatternsTableCodec) Decode(io.Reader, any) error {
	return errors.New("loki patterns table codec does not support decoding")
}
