package datasources

import (
	"errors"
	"fmt"
	"time"

	cmdconfig "github.com/grafana/gcx/cmd/gcx/config"
	dsquery "github.com/grafana/gcx/internal/datasources/query"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// genericQueryOpts holds the flags and routing policy for the auto-detecting
// query command. The per-kind behaviour lives in query_routes.go.
type genericQueryOpts struct {
	config cmdconfig.Options
	shared dsquery.SharedOpts

	profileType string
	maxNodes    int64
	limit       int

	routes queryRoutes
}

func (o *genericQueryOpts) setup(flags *pflag.FlagSet) {
	o.config.BindFlags(flags)
	o.shared.Setup(flags, true)
	flags.StringVar(&o.profileType, "profile-type", "", "Profile type ID for pyroscope queries (e.g., 'process_cpu:cpu:nanoseconds:cpu:nanoseconds')")
	flags.Int64Var(&o.maxNodes, "max-nodes", 1024, "Maximum nodes in flame graph (pyroscope only)")
	flags.IntVar(&o.limit, "limit", dsquery.DefaultLokiLimit, "Maximum number of log lines to return for loki queries (0 means no limit)")
}

// Validate runs the checks that need no I/O. args carries the optional
// positional expression, which cannot be combined with --expr.
func (o *genericQueryOpts) Validate(args []string) error {
	if err := o.shared.Validate(); err != nil {
		return err
	}

	// Reject "both positional and --expr" before any HTTP call.
	if len(args) > 1 && o.shared.Expr != "" {
		return errors.New("provide the expression as a positional argument or via --expr, not both")
	}

	return nil
}

// run resolves the datasource kind and hands the request to its route. The
// order of the steps below is load-bearing and pinned by tests; see
// query_characterization_test.go.
func (o *genericQueryOpts) run(cmd *cobra.Command, args []string) error {
	if err := o.Validate(args); err != nil {
		return err
	}

	ctx := cmd.Context()
	datasourceUID := args[0]

	cfg, err := o.config.LoadGrafanaConfig(ctx)
	if err != nil {
		return err
	}

	rawType, err := dsquery.GetDatasourceType(ctx, cfg, datasourceUID)
	if err != nil {
		return err
	}
	dsType := dsquery.NormalizeKind(rawType)

	// Redirects run before the expression is resolved, so an argument-less call
	// gets the typed-command redirect instead of "expression is required".
	if redirect, ok := o.routes.redirects[dsType]; ok {
		return errors.New(redirect)
	}

	expr, err := o.shared.ResolveExpr(args, 1)
	if err != nil {
		return err
	}

	start, end, step, err := o.shared.ParseTimes(time.Now())
	if err != nil {
		return err
	}

	dispatch, ok := o.routes.dispatch[dsType]
	if !ok {
		return fmt.Errorf("datasource type %q is not supported (supported: prometheus, loki, pyroscope, influxdb, clickhouse)", dsType)
	}

	resp, err := dispatch(ctx, genericQueryRequest{
		cfg:         cfg,
		uid:         datasourceUID,
		expr:        expr,
		start:       start,
		end:         end,
		step:        step,
		profileType: o.profileType,
		maxNodes:    o.maxNodes,
		limit:       o.limit,
		warn:        cmd.ErrOrStderr(),
	})
	if err != nil {
		return err
	}

	return o.shared.IO.Encode(cmd.OutOrStdout(), resp)
}

// QueryCmd returns the auto-detecting query command for the datasources group.
func QueryCmd() *cobra.Command {
	opts := &genericQueryOpts{routes: newQueryRoutes()}

	cmd := &cobra.Command{
		Use:   "query DATASOURCE_UID [EXPR]",
		Short: "Execute a query against any datasource (auto-detects type)",
		Long: `Execute a query against any datasource, automatically detecting the datasource type.

DATASOURCE_UID is always required (no default resolution for generic).
EXPR is the query expression appropriate for the datasource type.

The datasource type is detected via the Grafana API and the appropriate query
client is used automatically. This is the escape hatch for datasource types
that do not have a dedicated subcommand.`,
		Example: `
  # Auto-detect and query any supported datasource
  gcx datasources query ds-001 'up{job="grafana"}' --from now-1h --to now

  # Loki via auto-detect (with limit)
  gcx datasources query loki-001 '{job="varlogs"}' --from now-1h --to now --limit 200

  # Pyroscope via auto-detect
  gcx datasources query pyro-001 '{service_name="frontend"}' \
    --profile-type process_cpu:cpu:nanoseconds:cpu:nanoseconds --from now-1h --to now`,
		Args: cobra.RangeArgs(1, 2),
		RunE: opts.run,
	}

	opts.setup(cmd.Flags())

	return cmd
}
