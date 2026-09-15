package services

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"github.com/grafana/gcx/cmd/gcx/fail"
	"github.com/grafana/gcx/internal/agent"
	dsquery "github.com/grafana/gcx/internal/datasources/query"
	"github.com/grafana/gcx/internal/format"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/providers/appo11y/activation"
	"github.com/grafana/gcx/internal/query/prometheus"
	"github.com/grafana/gcx/internal/style"
	"github.com/prometheus/common/model"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"golang.org/x/sync/errgroup"
)

// fleetOperationsDefaultLimit mirrors operationsDefaultLimit's screenful
// sizing for the per-service view, scaled up slightly since a fleet-wide
// ranking is the more useful default depth.
const fleetOperationsDefaultLimit = 20

// fleetOperationsMaxLimit caps `operations list --limit`. Unlike every
// other `list` command in this repo, 0 does NOT mean unlimited here: the
// unrestricted fleet shape is #services x #operations, a materially
// different risk profile than any single-service listing, so both ends
// of the range are guarded explicitly in Validate.
const fleetOperationsMaxLimit = 500

// OperationsCommands returns the `operations` command group, rooted under
// `gcx appo11y`, alongside `services`.
func OperationsCommands(loader *providers.ConfigLoader) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "operations",
		Short: "Rank and inspect operations (span names) across App Observability services",
	}
	cmd.AddCommand(newFleetOperationsListCommand(loader))
	cmd.AddCommand(newOperationGetCommand(loader))
	return cmd
}

type fleetOperationsListOpts struct {
	IO          cmdio.Options
	Datasource  string
	Since       string
	Kind        string
	MetricsMode string
	Limit       int
	Filters     []string
	GroupBy     []string
	Namespace   string
	Env         string
	KG          kgFlags
}

func (o *fleetOperationsListOpts) setup(flags *pflag.FlagSet) {
	o.IO.RegisterCustomCodec("table", &fleetOperationsTableCodec{})
	o.IO.RegisterCustomCodec("wide", &fleetOperationsTableCodec{Wide: true})
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)

	flags.StringVarP(&o.Datasource, "datasource", "d", "", "Prometheus datasource UID (defaults to datasources.prometheus in config or auto-discovery)")
	flags.StringVar(&o.Since, "since", defaultRedWindow, "Rate/quantile window applied to span metrics (e.g. 1m, 5m, 1h, 1d) — PromQL duration syntax")
	flags.StringVar(&o.Kind, "kind", "inbound", "Span kinds to include. One of: inbound (server+consumer), server, consumer, all, or a comma-separated list of SPAN_KIND_* literals")
	flags.StringVar(&o.MetricsMode, "metrics-mode", metricsModeAuto, "Span-metrics family. One of: auto (probes the stack), v3 (traces_span_metrics_*), tempo (traces_spanmetrics_*), or otel (bare calls_total + duration_seconds_bucket)")
	flags.IntVar(&o.Limit, "limit", fleetOperationsDefaultLimit, fmt.Sprintf("Rank the top N operations fleet-wide by busy-seconds-per-second (must be 1-%d; unlike other list commands, 0 is rejected — the unbounded fleet shape is #services x #operations)", fleetOperationsMaxLimit))
	flags.StringArrayVar(&o.Filters, "filter", nil, "Scope the ranking to series matching a label matcher, e.g. --filter k8s_cluster_name=prod-us (repeatable); the label must exist on the span metrics")
	flags.StringSliceVar(&o.GroupBy, "group-by", nil, "Break each operation out per distinct value of a label, e.g. --group-by k8s_cluster_name (comma-separated or repeatable); the label must exist on the span metrics")
	flags.StringVarP(&o.Namespace, "namespace", "n", "", "Restrict to services in a single namespace (post-query convenience filter, applied after ranking)")
	flags.StringVar(&o.Env, "env", "", "Restrict to a single deployment_environment (post-query convenience filter, applied after ranking; only useful if the span metrics carry the label)")
	o.KG.register(flags)
}

func (o *fleetOperationsListOpts) Validate(cmd *cobra.Command) error {
	if err := o.IO.Validate(); err != nil {
		return err
	}
	if strings.TrimSpace(o.Since) == "" {
		return fail.NewCommandUsageError(cmd, "--since must not be empty", nil)
	}
	if _, err := model.ParseDuration(o.Since); err != nil {
		return fail.NewCommandUsageError(cmd, fmt.Sprintf("--since %q is not a valid PromQL duration", o.Since), err)
	}
	if _, err := resolveSpanKinds(o.Kind); err != nil {
		return fail.NewCommandUsageError(cmd, "", err)
	}
	if _, _, err := resolveMetricsMode(o.MetricsMode); err != nil {
		return fail.NewCommandUsageError(cmd, "", err)
	}
	if o.Limit < 1 {
		return fail.NewCommandUsageError(cmd, fmt.Sprintf("--limit must be at least 1 (got %d); this command has no unlimited mode because the unbounded fleet shape is #services x #operations", o.Limit), nil)
	}
	if o.Limit > fleetOperationsMaxLimit {
		return fail.NewCommandUsageError(cmd, fmt.Sprintf("--limit must be at most %d (got %d)", fleetOperationsMaxLimit, o.Limit), nil)
	}
	if _, err := o.KG.resolve(); err != nil {
		return fail.NewCommandUsageError(cmd, "", err)
	}
	return nil
}

func newFleetOperationsListCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &fleetOperationsListOpts{}
	cmd := &cobra.Command{
		Use:   "list",
		Short: "Rank operations (span names) fleet-wide by time share, across every App Observability service.",
		Long: `Rank the top operations across every service by busy-seconds-per-second
(time share), the fleet-wide counterpart to "gcx appo11y services
list-operations" (which is scoped to one service).

Each row is qualified by the service (job) it belongs to. Time-share is
normalized against the WHOLE FLEET's busy-time, not per-service — so a
--limit N view never claims the top N operations are 100% of anything;
it reports what share of the fleet's total wall-clock time they actually
consume.

The source span-metrics series (Tempo's traces_spanmetrics_*, the v3
traces_span_metrics_*, or bare OTel calls_total) is auto-detected by
default. Use --metrics-mode to pin it.`,
		Example: `
  # Top 20 operations fleet-wide in the default 5m window
  gcx appo11y operations list

  # Top 50, last hour, JSON for scripting
  gcx appo11y operations list --since 1h --limit 50 -o json

  # Restrict to one namespace after ranking
  gcx appo11y operations list --namespace payments

  # Break each operation out per cluster to spot per-cluster hotspots
  gcx appo11y operations list --group-by k8s_cluster_name`,
		Args: cobra.NoArgs,
		RunE: runFleetOperationsList(loader, opts),
		Annotations: map[string]string{
			agent.AnnotationTokenCost: "small",
			agent.AnnotationLLMHint:   `Fleet-wide operations ranking across every App Observability service: one row per (service, span_name), sorted by busy-seconds-per-second (time-share) desc. Unlike 'gcx appo11y services list-operations <service>' (per-service, this command's positional-argument counterpart), this command takes no positional argument and ranks across the whole stack. Time-share is normalized against the fleet's total busy-time (fleet_busy_seconds_per_second in the response), so it reports true fleet share, not a per-row 100%. --limit caps the ranking depth (1-500; 0 is rejected, unlike other list commands, because the unbounded shape is #services x #operations). Use --namespace/--env to narrow the ranked result post-query. Use --group-by <label> to break each ranked operation out per distinct value of that label. Pairs with 'gcx appo11y operations get <operation> --service <svc>' to drill into one ranked row. Examples: gcx appo11y operations list -o json; gcx appo11y operations list --limit 50 --since 1h -o json; gcx appo11y operations list --namespace payments -o json`,
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

func runFleetOperationsList(loader *providers.ConfigLoader, opts *fleetOperationsListOpts) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, _ []string) error {
		if err := opts.Validate(cmd); err != nil {
			return err
		}
		kinds, err := resolveSpanKinds(opts.Kind)
		if err != nil {
			return fail.NewCommandUsageError(cmd, "", err)
		}
		mode, auto, err := resolveMetricsMode(opts.MetricsMode)
		if err != nil {
			return fail.NewCommandUsageError(cmd, "", err)
		}
		matchers, err := parseFilters(opts.Filters)
		if err != nil {
			return fail.NewCommandUsageError(cmd, "", err)
		}
		groupBy, err := parseGroupBy(opts.GroupBy)
		if err != nil {
			return fail.NewCommandUsageError(cmd, "", err)
		}

		ctx := cmd.Context()

		cfgCtx, cfg, err := dsquery.LoadContextAndConfig(ctx, loader)
		if err != nil {
			return err
		}
		if err := activation.Gate(ctx, cfg); err != nil {
			return err
		}
		cat, err := opts.KG.catalog(cfg)
		if err != nil {
			return err
		}

		datasourceUID, err := dsquery.ResolveAndSaveDatasource(ctx, loader, opts.Datasource, cfgCtx, cfg, "prometheus")
		if err != nil {
			return err
		}

		client, err := prometheus.NewClient(cfg)
		if err != nil {
			return fmt.Errorf("failed to create prometheus client: %w", err)
		}

		if auto {
			mode, err = detectFleetMetricsMode(ctx, client, datasourceUID, opts.Since, matchers)
			if err != nil {
				return fmt.Errorf("metrics-mode auto-detect failed: %w", err)
			}
		}

		response, err := fetchFleetOperations(ctx, client, datasourceUID, opts.Since, kinds, mode, matchers, groupBy, opts.Limit, opts.Env)
		if err != nil {
			return err
		}

		response.Items = filterFleetByNamespaceAndEnv(response.Items, opts.Namespace, opts.Env)
		if cat != nil {
			// One bulk index() call — not a per-row lookup() — the same
			// pattern services list uses: --limit permits up to 500 rows,
			// and lookup() costs two serial HTTP round trips each with no
			// caching, which would turn this into a multi-minute command.
			idx := warnKGIndex(cmd.ErrOrStderr(), cat.index(ctx))
			for i := range response.Items {
				response.Items[i].KG = idx[response.Items[i].Service]
			}
		}

		notFound := len(response.Items) == 0
		if notFound {
			cmdio.EmitHint(cmd.ErrOrStderr(),
				"no operations found in the requested window",
				"gcx appo11y services list")
		}
		if err := opts.IO.Encode(cmd.OutOrStdout(), response); err != nil {
			return err
		}
		if notFound {
			return notFoundEmitted(cmd.ErrOrStderr(), "no operations have telemetry in the requested window")
		}
		return nil
	}
}

// detectFleetMetricsMode probes each metrics family fleet-wide (no `job`
// filter) and returns the first with data, biased toward modern names —
// the fleet-wide counterpart to detectMetricsMode in get.go, which scopes
// its probe to one service.
func detectFleetMetricsMode(ctx context.Context, client *prometheus.Client, datasourceUID, window string, matchers []Matcher) (MetricsMode, error) {
	preference := metricsModePreference()
	found := make([]bool, len(preference))

	eg, egCtx := errgroup.WithContext(ctx)
	for i, m := range preference {
		names, ok := metricNamesByMode(m)
		if !ok {
			return "", fmt.Errorf("unknown metrics mode %q", m)
		}
		eg.Go(func() error {
			expr, err := buildFleetModeProbeQuery(names.calls, window, matchers)
			if err != nil {
				return fmt.Errorf("failed to build %s probe query: %w", m, err)
			}
			resp, err := client.Query(egCtx, datasourceUID, prometheus.QueryRequest{Query: expr})
			if err != nil {
				return fmt.Errorf("%s probe query failed: %w", m, err)
			}
			if v, ok := instantScalar(resp); ok && v > 0 {
				found[i] = true
			}
			return nil
		})
	}
	if err := eg.Wait(); err != nil {
		return "", err
	}
	for i, m := range preference {
		if found[i] {
			return m, nil
		}
	}
	return MetricsModeV3, nil
}

// fetchFleetOperations runs the fleet-wide total-time, rate, error-rate,
// avg-latency, and p50/p95/p99 quantile queries in parallel (each already
// restricted server-side to the top-`limit` operations — see
// restrictToFleetTopK) and folds them into a FleetOperationsResponse.
// envGroupLabels are the two possible OTel semconv spellings environmentValue
// resolves from — see its doc comment. Neither is part of any fleet
// aggregation's "by" clause unless env filtering needs it (see queryGroupBy
// in fetchFleetOperations): widening the grouping unconditionally would
// split every row that happens to carry an environment label, even when the
// caller never asked to filter or group by one.
var envGroupLabels = []string{"deployment_environment", "deployment_environment_name"} //nolint:gochecknoglobals // constant-like lookup list; never mutated.

// fetchFleetOperations runs the fleet-wide rate/error/latency queries in
// parallel and folds them into one FleetOperationsResponse. When env is set,
// the aggregations are internally grouped by envGroupLabels in addition to
// groupBy so filterFleetByNamespaceAndEnv's post-filter has an environment
// value to read from each row — env never appears in the returned
// response's GroupBy, which always reflects the caller's own --group-by.
func fetchFleetOperations(ctx context.Context, client *prometheus.Client, datasourceUID, window string, kinds []string, mode MetricsMode, matchers []Matcher, groupBy []string, limit int, env string) (*FleetOperationsResponse, error) {
	names, ok := metricNamesByMode(mode)
	if !ok {
		return nil, fmt.Errorf("unknown metrics mode %q", mode)
	}

	queryGroupBy := groupBy
	if env != "" {
		queryGroupBy = append(append([]string{}, groupBy...), envGroupLabels...)
	}

	var totalResp, rateResp, errorResp, avgResp, p50Resp, p95Resp, p99Resp *prometheus.QueryResponse

	eg, egCtx := errgroup.WithContext(ctx)
	eg.Go(func() error {
		expr, err := buildFleetTotalTimeQuery(names, window, kinds, matchers)
		if err != nil {
			return fmt.Errorf("failed to build fleet total-time query: %w", err)
		}
		resp, err := client.Query(egCtx, datasourceUID, prometheus.QueryRequest{Query: expr})
		if err != nil {
			return fmt.Errorf("fleet total-time query failed: %w", err)
		}
		totalResp = resp
		return nil
	})
	eg.Go(func() error {
		expr, err := buildFleetRateQuery(names, window, kinds, matchers, queryGroupBy, limit)
		if err != nil {
			return fmt.Errorf("failed to build fleet rate query: %w", err)
		}
		resp, err := client.Query(egCtx, datasourceUID, prometheus.QueryRequest{Query: expr})
		if err != nil {
			return fmt.Errorf("fleet rate query failed: %w", err)
		}
		rateResp = resp
		return nil
	})
	eg.Go(func() error {
		expr, err := buildFleetErrorRateQuery(names, window, kinds, matchers, queryGroupBy, limit)
		if err != nil {
			return fmt.Errorf("failed to build fleet error-rate query: %w", err)
		}
		resp, err := client.Query(egCtx, datasourceUID, prometheus.QueryRequest{Query: expr})
		if err != nil {
			return fmt.Errorf("fleet error-rate query failed: %w", err)
		}
		errorResp = resp
		return nil
	})
	eg.Go(func() error {
		expr, err := buildFleetAvgLatencyQuery(names, window, kinds, matchers, queryGroupBy, limit)
		if err != nil {
			return fmt.Errorf("failed to build fleet avg-latency query: %w", err)
		}
		resp, err := client.Query(egCtx, datasourceUID, prometheus.QueryRequest{Query: expr})
		if err != nil {
			return fmt.Errorf("fleet avg-latency query failed: %w", err)
		}
		avgResp = resp
		return nil
	})
	for phi, sink := range map[float64]**prometheus.QueryResponse{
		0.50: &p50Resp,
		0.95: &p95Resp,
		0.99: &p99Resp,
	} {
		eg.Go(func() error {
			expr, err := buildFleetLatencyQuantileQuery(names, window, kinds, phi, matchers, queryGroupBy, limit)
			if err != nil {
				return fmt.Errorf("failed to build fleet p%.0f latency query: %w", phi*100, err)
			}
			resp, err := client.Query(egCtx, datasourceUID, prometheus.QueryRequest{Query: expr})
			if err != nil {
				return fmt.Errorf("fleet p%.0f latency query failed: %w", phi*100, err)
			}
			*sink = resp
			return nil
		})
	}
	if err := eg.Wait(); err != nil {
		return nil, err
	}

	fleetTotal, _ := instantScalar(totalResp)
	groupLabels := append([]string{"job"}, queryGroupBy...)
	items := mergeFleetOperations(
		extractOperations(rateResp, groupLabels),
		extractOperations(errorResp, groupLabels),
		extractOperations(avgResp, groupLabels),
		extractOperations(p50Resp, groupLabels),
		extractOperations(p95Resp, groupLabels),
		extractOperations(p99Resp, groupLabels),
		fleetTotal,
		queryGroupBy,
	)

	return &FleetOperationsResponse{
		Window:                    window,
		MetricsMode:               mode,
		SpanKinds:                 spanKindRegex(kinds),
		GroupBy:                   groupBy,
		FleetBusySecondsPerSecond: fleetTotal,
		Items:                     items,
	}, nil
}

// filterFleetByNamespaceAndEnv narrows fleet rows to a single namespace
// and/or deployment environment, post-query — a convenience filter over
// the already-ranked top-N result, not a PromQL matcher (namespace isn't
// even a span-metric label; it's parsed from job). Mirrors filterByEnv in
// commands.go.
func filterFleetByNamespaceAndEnv(items []FleetOperation, namespace, env string) []FleetOperation {
	if namespace == "" && env == "" {
		return items
	}
	out := make([]FleetOperation, 0, len(items))
	for _, it := range items {
		if namespace != "" && it.Namespace != namespace {
			continue
		}
		if env != "" && environmentValue(it.Labels) != env {
			continue
		}
		out = append(out, it)
	}
	return out
}

func formatBusySeconds(v float64, has bool) string {
	if !has {
		return "-"
	}
	return fmt.Sprintf("%.3f s/s", v)
}

// fleetOperationsTableCodec renders a FleetOperationsResponse. Default
// columns are SERVICE/OPERATION/RATE/ERROR%/P95/TIME%; wide adds
// NAMESPACE/ERRORS/P50/P99/BUSY_S/S.
type fleetOperationsTableCodec struct {
	Wide bool
}

func (c *fleetOperationsTableCodec) Format() format.Format {
	if c.Wide {
		return "wide"
	}
	return "table"
}

func (c *fleetOperationsTableCodec) Decode(io.Reader, any) error {
	return errors.New("appo11y operations list table codec does not support decoding")
}

func (c *fleetOperationsTableCodec) Encode(w io.Writer, v any) error {
	resp, ok := v.(*FleetOperationsResponse)
	if !ok {
		return fmt.Errorf("invalid data type for appo11y operations list table codec: %T", v)
	}
	if len(resp.Items) == 0 {
		_, err := fmt.Fprintln(w, "No operations found. Verify services in the stack are emitting span metrics in the requested window.")
		return err
	}

	headers := []string{"SERVICE"}
	if c.Wide {
		headers = append(headers, "NAMESPACE")
	}
	headers = append(headers, upperHeaders(resp.GroupBy)...)
	if c.Wide {
		headers = append(headers, "OPERATION", "RATE", "ERRORS", "ERROR %", "P50", "P95", "P99", "TIME %", "BUSY S/S")
	} else {
		headers = append(headers, "OPERATION", "RATE", "ERROR %", "P95", "TIME %")
	}

	t := style.NewTable(headers...)
	for i := range resp.Items {
		op := &resp.Items[i]
		row := []string{op.Service}
		if c.Wide {
			row = append(row, orDash(op.Namespace))
		}
		for _, l := range resp.GroupBy {
			row = append(row, orDash(op.Labels[l]))
		}
		hasTimeShare := op.HasAvgLatency && op.HasTraffic
		if c.Wide {
			row = append(row,
				op.Name,
				formatRateWithUnit(op.RatePerSecond, op.HasTraffic),
				formatRateWithUnit(op.ErrorRatePerSec, op.HasErrors),
				formatPercentMaybe(op.ErrorPercent, op.HasTraffic),
				formatDuration(op.P50Seconds, op.HasLatencyP50),
				formatDuration(op.P95Seconds, op.HasLatencyP95),
				formatDuration(op.P99Seconds, op.HasLatencyP99),
				formatPercentMaybe(op.TimeSharePercent, hasTimeShare),
				formatBusySeconds(op.AvgSeconds*op.RatePerSecond, hasTimeShare),
			)
		} else {
			row = append(row,
				op.Name,
				formatRateWithUnit(op.RatePerSecond, op.HasTraffic),
				formatPercentMaybe(op.ErrorPercent, op.HasTraffic),
				formatDuration(op.P95Seconds, op.HasLatencyP95),
				formatPercentMaybe(op.TimeSharePercent, hasTimeShare),
			)
		}
		t.Row(row...)
	}
	return t.Render(w)
}

// OperationDetail is the response shape for `operations get`.
type OperationDetail struct {
	Service     Service     `json:"service" yaml:"service"`
	Window      string      `json:"window" yaml:"window"`
	MetricsMode MetricsMode `json:"metrics_mode" yaml:"metrics_mode"`
	SpanKinds   string      `json:"span_kinds" yaml:"span_kinds"`
	Operation   Operation   `json:"operation" yaml:"operation"`
}

type operationDetailOpts struct {
	IO          cmdio.Options
	Datasource  string
	Service     string
	Namespace   string
	Since       string
	Kind        string
	MetricsMode string
	Filters     []string
	KG          kgFlags
}

func (o *operationDetailOpts) setup(flags *pflag.FlagSet) {
	o.IO.RegisterCustomCodec("table", &operationDetailCodec{})
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)

	flags.StringVarP(&o.Datasource, "datasource", "d", "", "Prometheus datasource UID (defaults to datasources.prometheus in config or auto-discovery)")
	flags.StringVar(&o.Service, "service", "", "Service the operation belongs to: bare name or the canonical \"<namespace>/<name>\" form (required)")
	flags.StringVarP(&o.Namespace, "namespace", "n", "", "Service namespace (only needed when --service is a bare name and multiple namespaces are in play)")
	flags.StringVar(&o.Since, "since", defaultRedWindow, "Rate/quantile window applied to span metrics (e.g. 1m, 5m, 1h, 1d) — PromQL duration syntax")
	flags.StringVar(&o.Kind, "kind", "inbound", "Span kinds to include. One of: inbound (server+consumer), server, consumer, all, or a comma-separated list of SPAN_KIND_* literals")
	flags.StringVar(&o.MetricsMode, "metrics-mode", metricsModeAuto, "Span-metrics family. One of: auto (probes the stack), v3 (traces_span_metrics_*), tempo (traces_spanmetrics_*), or otel (bare calls_total + duration_seconds_bucket)")
	flags.StringArrayVar(&o.Filters, "filter", nil, "Scope the lookup to series matching a label matcher, e.g. --filter k8s_cluster_name=prod-us (repeatable); the label must exist on the span metrics")
	o.KG.register(flags)
}

func (o *operationDetailOpts) Validate(cmd *cobra.Command) error {
	if err := o.IO.Validate(); err != nil {
		return err
	}
	if strings.TrimSpace(o.Service) == "" {
		return fail.NewCommandUsageError(cmd, "--service is required", nil)
	}
	if strings.TrimSpace(o.Since) == "" {
		return fail.NewCommandUsageError(cmd, "--since must not be empty", nil)
	}
	if _, err := model.ParseDuration(o.Since); err != nil {
		return fail.NewCommandUsageError(cmd, fmt.Sprintf("--since %q is not a valid PromQL duration", o.Since), err)
	}
	if _, err := resolveSpanKinds(o.Kind); err != nil {
		return fail.NewCommandUsageError(cmd, "", err)
	}
	if _, _, err := resolveMetricsMode(o.MetricsMode); err != nil {
		return fail.NewCommandUsageError(cmd, "", err)
	}
	if _, err := o.KG.resolve(); err != nil {
		return fail.NewCommandUsageError(cmd, "", err)
	}
	return nil
}

func newOperationGetCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &operationDetailOpts{}
	cmd := &cobra.Command{
		Use:   "get <operation> --service <service> [--namespace ns]",
		Short: "Inspect a single operation (span name) within one service: RED snapshot + its share of the service's time.",
		Long: `Show the rate/errors/duration snapshot for one operation (span_name)
inside one service.

The operation name is the positional argument; the parent service is
always given via --service (bare name or "<namespace>/<name>"), never as
part of the positional — span names routinely contain "/" (e.g.
"GET /api/v1/carts"), which would make a slash-composite positional
ambiguous.

TimeSharePercent here means this operation's share of ITS SERVICE's
wall-clock time — the same number "gcx appo11y services list-operations"
reports for the same operation — not the fleet's (see
"gcx appo11y operations list" for the fleet-wide ranking).`,
		Example: `
  # One operation within the "checkoutservice" service
  gcx appo11y operations get "GET /cart" --service checkoutservice

  # Explicit namespace, last hour, JSON for scripting
  gcx appo11y operations get "GET /cart" --service payments/checkoutservice --since 1h -o json`,
		Args: cobra.ExactArgs(1),
		RunE: runOperationGet(loader, opts),
		Annotations: map[string]string{
			agent.AnnotationTokenCost: "small",
			agent.AnnotationLLMHint:   `Single-operation RED snapshot within one service: rate (req/s), error rate, error percent, avg latency, p50/p95/p99, and time-share % (this operation's share of ITS SERVICE's wall-clock time, matching 'gcx appo11y services list-operations'). --service is required (bare name or <namespace>/<name>); the operation name is the positional argument, never combined with the service into one slash-composite value (span names contain "/"). Distinct from 'gcx appo11y operations list' (fleet-wide ranking, no --service). Examples: gcx appo11y operations get "<span_name>" --service <svc> -o json; gcx appo11y operations get "<span_name>" --service <ns>/<svc> --since 1h -o json`,
		},
	}
	opts.setup(cmd.Flags())
	if err := cmd.MarkFlagRequired("service"); err != nil {
		panic(fmt.Sprintf("operations get: mark --service required: %v", err))
	}
	return cmd
}

// appendSpanNameMatcher narrows a matcher list to one span_name — how
// `operations get` reuses the existing single-service query builders
// (buildOperationsRateQuery et al.) instead of adding new PromQL: the
// operation identity becomes just another --filter-style matcher.
func appendSpanNameMatcher(matchers []Matcher, operation string) []Matcher {
	return append(matchers, Matcher{Label: "span_name", Op: "=", Value: operation})
}

func runOperationGet(loader *providers.ConfigLoader, opts *operationDetailOpts) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		if err := opts.Validate(cmd); err != nil {
			return err
		}
		operation := args[0]
		namespace, name, err := parseServiceArg(opts.Service, opts.Namespace)
		if err != nil {
			return fail.NewCommandUsageError(cmd, "", err)
		}
		kinds, err := resolveSpanKinds(opts.Kind)
		if err != nil {
			return fail.NewCommandUsageError(cmd, "", err)
		}
		mode, auto, err := resolveMetricsMode(opts.MetricsMode)
		if err != nil {
			return fail.NewCommandUsageError(cmd, "", err)
		}
		filterMatchers, err := parseFilters(opts.Filters)
		if err != nil {
			return fail.NewCommandUsageError(cmd, "", err)
		}

		ctx := cmd.Context()

		cfgCtx, cfg, err := dsquery.LoadContextAndConfig(ctx, loader)
		if err != nil {
			return err
		}
		if err := activation.Gate(ctx, cfg); err != nil {
			return err
		}
		cat, err := opts.KG.catalog(cfg)
		if err != nil {
			return err
		}

		datasourceUID, err := dsquery.ResolveAndSaveDatasource(ctx, loader, opts.Datasource, cfgCtx, cfg, "prometheus")
		if err != nil {
			return err
		}

		client, err := prometheus.NewClient(cfg)
		if err != nil {
			return fmt.Errorf("failed to create prometheus client: %w", err)
		}

		if namespace == "" {
			resolved, err := resolveNamespaceForBareName(ctx, client, datasourceUID, name, filterMatchers)
			if err != nil {
				return err
			}
			namespace = resolved
		}

		if auto {
			mode, err = detectMetricsMode(ctx, client, datasourceUID, namespace, name, filterMatchers)
			if err != nil {
				return fmt.Errorf("metrics-mode auto-detect failed: %w", err)
			}
		}

		detail, err := fetchOperationDetail(ctx, client, datasourceUID, namespace, name, operation, opts.Since, kinds, mode, filterMatchers)
		if err != nil {
			return err
		}
		if cat != nil {
			detail.Service.KG = cat.lookup(ctx, name)
		}

		notFound := !detail.Operation.HasTraffic
		if notFound {
			emitNoDataHint(cmd.ErrOrStderr(), namespace, name)
		}
		if err := opts.IO.Encode(cmd.OutOrStdout(), detail); err != nil {
			return err
		}
		if notFound {
			return notFoundEmitted(cmd.ErrOrStderr(),
				fmt.Sprintf("operation %q on service %q has no telemetry in the requested window", operation, jobLabel(namespace, name)))
		}
		return nil
	}
}

// fetchOperationDetail runs the metadata lookup, the operation-scoped RED
// queries (existing single-service builders, narrowed via
// appendSpanNameMatcher), and the parent service's total busy-time query
// in parallel, then folds them into an OperationDetail. The service total
// is queried unscoped by span_name so TimeSharePercent means "this
// operation's share of its service's wall-clock time" — narrowing the
// denominator to the one operation too would trivially compute 100%.
func fetchOperationDetail(ctx context.Context, client *prometheus.Client, datasourceUID, namespace, name, operation, window string, kinds []string, mode MetricsMode, filterMatchers []Matcher) (*OperationDetail, error) {
	names, ok := metricNamesByMode(mode)
	if !ok {
		return nil, fmt.Errorf("unknown metrics mode %q", mode)
	}
	opMatchers := appendSpanNameMatcher(filterMatchers, operation)

	metrics := targetInfoMetrics()
	metadataResponses := make([]*prometheus.QueryResponse, len(metrics))
	var rateResp, errorResp, avgResp, p50Resp, p95Resp, p99Resp, serviceTotalResp *prometheus.QueryResponse

	eg, egCtx := errgroup.WithContext(ctx)
	for i, metric := range metrics {
		eg.Go(func() error {
			expr, err := buildServiceMetadataQuery(metric, namespace, name, filterMatchers)
			if err != nil {
				return fmt.Errorf("failed to build %s metadata query: %w", metric, err)
			}
			resp, err := client.Query(egCtx, datasourceUID, prometheus.QueryRequest{Query: expr})
			if err != nil {
				return fmt.Errorf("%s metadata query failed: %w", metric, err)
			}
			metadataResponses[i] = resp
			return nil
		})
	}
	eg.Go(func() error {
		expr, err := buildOperationsRateQuery(names, namespace, name, window, kinds, opMatchers, nil)
		if err != nil {
			return fmt.Errorf("failed to build rate query: %w", err)
		}
		resp, err := client.Query(egCtx, datasourceUID, prometheus.QueryRequest{Query: expr})
		if err != nil {
			return fmt.Errorf("rate query failed: %w", err)
		}
		rateResp = resp
		return nil
	})
	eg.Go(func() error {
		expr, err := buildOperationsErrorRateQuery(names, namespace, name, window, kinds, opMatchers, nil)
		if err != nil {
			return fmt.Errorf("failed to build error-rate query: %w", err)
		}
		resp, err := client.Query(egCtx, datasourceUID, prometheus.QueryRequest{Query: expr})
		if err != nil {
			return fmt.Errorf("error-rate query failed: %w", err)
		}
		errorResp = resp
		return nil
	})
	eg.Go(func() error {
		expr, err := buildOperationsAvgLatencyQuery(names, namespace, name, window, kinds, opMatchers, nil)
		if err != nil {
			return fmt.Errorf("failed to build avg-latency query: %w", err)
		}
		resp, err := client.Query(egCtx, datasourceUID, prometheus.QueryRequest{Query: expr})
		if err != nil {
			return fmt.Errorf("avg-latency query failed: %w", err)
		}
		avgResp = resp
		return nil
	})
	for phi, sink := range map[float64]**prometheus.QueryResponse{
		0.50: &p50Resp,
		0.95: &p95Resp,
		0.99: &p99Resp,
	} {
		eg.Go(func() error {
			expr, err := buildOperationsLatencyQuantileQuery(names, namespace, name, window, kinds, phi, opMatchers, nil)
			if err != nil {
				return fmt.Errorf("failed to build p%.0f latency query: %w", phi*100, err)
			}
			resp, err := client.Query(egCtx, datasourceUID, prometheus.QueryRequest{Query: expr})
			if err != nil {
				return fmt.Errorf("p%.0f latency query failed: %w", phi*100, err)
			}
			*sink = resp
			return nil
		})
	}
	eg.Go(func() error {
		expr, err := buildServiceTotalBusyQuery(names, namespace, name, window, kinds, filterMatchers)
		if err != nil {
			return fmt.Errorf("failed to build service total-time query: %w", err)
		}
		resp, err := client.Query(egCtx, datasourceUID, prometheus.QueryRequest{Query: expr})
		if err != nil {
			return fmt.Errorf("service total-time query failed: %w", err)
		}
		serviceTotalResp = resp
		return nil
	})
	if err := eg.Wait(); err != nil {
		return nil, err
	}

	metadata, err := parseServicesResponses(metadataResponses)
	if err != nil {
		return nil, fmt.Errorf("failed to parse metadata response: %w", err)
	}
	svc := selectMetadataService(metadata, namespace, name)

	rate, hasRate := instantScalar(rateResp)
	errRate, hasErr := instantScalar(errorResp)
	avg, hasAvg := instantScalar(avgResp)
	p50, hasP50 := instantScalar(p50Resp)
	p95, hasP95 := instantScalar(p95Resp)
	p99, hasP99 := instantScalar(p99Resp)
	serviceTotal, _ := instantScalar(serviceTotalResp)

	hasTraffic := hasRate && rate > 0
	var timeShare float64
	if hasAvg && hasTraffic && serviceTotal > 0 {
		timeShare = (avg * rate / serviceTotal) * 100
	}

	return &OperationDetail{
		Service:     svc,
		Window:      window,
		MetricsMode: mode,
		SpanKinds:   spanKindRegex(kinds),
		Operation: Operation{
			Name:             operation,
			RatePerSecond:    rate,
			ErrorRatePerSec:  errRate,
			ErrorPercent:     computeErrorPercent(errRate, rate),
			AvgSeconds:       avg,
			P50Seconds:       p50,
			P95Seconds:       p95,
			P99Seconds:       p99,
			TimeSharePercent: timeShare,
			HasTraffic:       hasTraffic,
			HasErrors:        hasErr || hasTraffic,
			HasAvgLatency:    hasAvg,
			HasLatencyP50:    hasP50,
			HasLatencyP95:    hasP95,
			HasLatencyP99:    hasP99,
		},
	}, nil
}

// operationDetailCodec renders an OperationDetail as a kubectl-describe-style
// key:value block, mirroring serviceDetailCodec's non-grouped path.
type operationDetailCodec struct{}

func (c *operationDetailCodec) Format() format.Format { return "table" }

func (c *operationDetailCodec) Decode(io.Reader, any) error {
	return errors.New("appo11y operations get table codec does not support decoding")
}

func (c *operationDetailCodec) Encode(w io.Writer, v any) error {
	detail, ok := v.(*OperationDetail)
	if !ok {
		return fmt.Errorf("invalid data type for appo11y operations get table codec: %T", v)
	}
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	writeRow := func(label, value string) {
		fmt.Fprintf(tw, "%s:\t%s\n", label, value)
	}

	writeRow("Service", jobLabel(detail.Service.Namespace, detail.Service.Name))
	writeRow("Operation", detail.Operation.Name)
	writeRow("Window", detail.Window)
	writeRow("Metrics mode", string(detail.MetricsMode))
	writeRow("Span kinds", detail.SpanKinds)

	op := &detail.Operation
	writeRow("Rate", formatRateWithUnit(op.RatePerSecond, op.HasTraffic))
	writeRow("Errors", formatErrors(op.ErrorRatePerSec, op.ErrorPercent, op.HasErrors, op.HasTraffic))
	writeRow("Time share (of service)", formatPercentMaybe(op.TimeSharePercent, op.HasAvgLatency && op.HasTraffic))
	fmt.Fprintln(tw, "Latency:")
	fmt.Fprintf(tw, "  avg:\t%s\n", formatDuration(op.AvgSeconds, op.HasAvgLatency))
	fmt.Fprintf(tw, "  p50:\t%s\n", formatDuration(op.P50Seconds, op.HasLatencyP50))
	fmt.Fprintf(tw, "  p95:\t%s\n", formatDuration(op.P95Seconds, op.HasLatencyP95))
	fmt.Fprintf(tw, "  p99:\t%s\n", formatDuration(op.P99Seconds, op.HasLatencyP99))

	return tw.Flush()
}
