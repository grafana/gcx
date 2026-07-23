package services

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"

	"github.com/grafana/gcx/cmd/gcx/fail"
	"github.com/grafana/gcx/internal/agent"
	internalconfig "github.com/grafana/gcx/internal/config"
	dsquery "github.com/grafana/gcx/internal/datasources/query"
	"github.com/grafana/gcx/internal/format"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/query/prometheus"
	"github.com/grafana/gcx/internal/style"
	"github.com/grafana/grafana-app-sdk/logging"
	"github.com/prometheus/common/model"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"golang.org/x/sync/errgroup"
)

// operationsDefaultLimit caps the row count to ~one screenful by default.
// Services rarely have fewer than 15 useful operations and rarely need
// more than that at a glance. Users opt out with `--limit 0`.
const operationsDefaultLimit = 15

type operationsOpts struct {
	IO          cmdio.Options
	Datasource  string
	Since       string
	Namespace   string
	Kind        string
	MetricsMode string
	Limit       int
	Filters     []string
	GroupBy     []string
}

func (o *operationsOpts) setup(flags *pflag.FlagSet) {
	o.IO.RegisterCustomCodec("table", &operationsTableCodec{})
	o.IO.RegisterCustomCodec("wide", &operationsTableCodec{Wide: true})
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)

	flags.StringVarP(&o.Datasource, "datasource", "d", "", "Prometheus datasource UID (defaults to datasources.prometheus in config or auto-discovery)")
	flags.StringVarP(&o.Namespace, "namespace", "n", "", "Service namespace (only needed when the argument is the bare service name and multiple namespaces are in play)")
	flags.StringVar(&o.Since, "since", defaultRedWindow, "Rate/quantile window applied to span metrics (e.g. 1m, 5m, 1h, 1d) — PromQL duration syntax")
	flags.StringVar(&o.Kind, "kind", "inbound", "Span kinds to include. One of: inbound (server+consumer), server, consumer, all, or a comma-separated list of SPAN_KIND_* literals")
	flags.StringVar(&o.MetricsMode, "metrics-mode", metricsModeAuto, "Span-metrics family. One of: auto (probes the stack), v3 (traces_span_metrics_*), tempo (traces_spanmetrics_*), or otel (bare calls_total + duration_seconds_bucket)")
	flags.IntVar(&o.Limit, "limit", operationsDefaultLimit, "Limit the number of operations returned (0 = unlimited; applied after sorting by time-share desc)")
	flags.StringArrayVar(&o.Filters, "filter", nil, "Scope the operations breakdown to series matching a label matcher, e.g. --filter k8s_cluster_name=prod-us (repeatable). Use to break a multi-cluster/multi-region service down one cluster at a time; the label must exist on the span metrics")
	flags.StringSliceVar(&o.GroupBy, "group-by", nil, "Break each operation out per distinct value of a label, e.g. --group-by k8s_cluster_name (comma-separated or repeatable). Time-share is normalized within each group so per-cluster hotspots are comparable; the label must exist on the span metrics")
}

func (o *operationsOpts) Validate(cmd *cobra.Command) error {
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
	if o.Limit < 0 {
		return fail.NewCommandUsageError(cmd, "--limit must be zero or positive", nil)
	}
	return nil
}

func newListOperationsCommand() *cobra.Command {
	opts := &operationsOpts{}
	cmd := &cobra.Command{
		Use:   "list-operations <service> [--namespace ns]",
		Short: "List a service's operations with per-operation RED (span_name × rate/errors/latency).",
		Long: `List the per-operation RED breakdown for one service.

The argument is either the bare service name or the canonical
"<namespace>/<name>" form; bare names are auto-resolved against
target_info the same way as "gcx appo11y services get".

Rows are sorted by time-share desc — the share of the service's total
wall-clock time each operation consumes, computed as
(avg_latency × rate) / sum(all). This surfaces operations that dominate
latency regardless of whether they're high-rate-fast or low-rate-slow.

The source span-metrics series (Tempo's traces_spanmetrics_*, the v3
traces_span_metrics_*, or bare OTel calls_total) is auto-detected by
default. Use --metrics-mode to pin it; see "gcx appo11y services get
--help" for the full reference.`,
		Example: `
  # Top operations for the "checkoutservice" service in the default 5m window
  gcx appo11y services list-operations checkoutservice

  # Wider view with p50/p99 and absolute error rate
  gcx appo11y services list-operations checkoutservice -o wide

  # Last hour, unlimited rows, JSON for scripting
  gcx appo11y services list-operations payments/checkoutservice --since 1h --limit 0 -o json

  # Break a multi-cluster service down to a single cluster
  gcx appo11y services list-operations faro-collector --filter k8s_cluster_name=prod-us-central-0

  # Break each operation out per cluster to spot per-cluster hotspots
  gcx appo11y services list-operations faro-collector --group-by k8s_cluster_name`,
		Args: cobra.ExactArgs(1),
		RunE: runOperations(opts),
		Annotations: map[string]string{
			agent.AnnotationTokenCost: "small",
			agent.AnnotationLLMHint:   `Per-operation RED breakdown for one App Observability service: one row per span_name with rate (req/s), error rate, error percent, avg latency, p50/p95/p99, and time-share % (rate * avg_latency normalized across the service). Sorted by time-share desc to surface latency hotspots. Pairs with 'gcx appo11y services get' (which is the headline summary) — use 'list-operations' once a service is identified as hot to find which endpoints carry the load. Use --filter <label><op><value> (repeatable) to scope the breakdown to a subset of series — most usefully a cluster/region label (e.g. --filter k8s_cluster_name=prod-us) to break a multi-cluster service down one cluster at a time. Use --group-by <label> to instead break every operation out per distinct value of that label (time-share is normalized within each group) — surfaces per-cluster/per-region hotspots. Examples: gcx appo11y services list-operations <name> -o json; gcx appo11y services list-operations <ns>/<name> --since 1h --limit 0 -o json; gcx appo11y services list-operations <name> -o wide; gcx appo11y services list-operations <name> --filter k8s_cluster_name=<cluster> -o json; gcx appo11y services list-operations <name> --group-by k8s_cluster_name -o json`,
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

func runOperations(opts *operationsOpts) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		if err := opts.Validate(cmd); err != nil {
			return err
		}
		namespace, name, err := parseServiceArg(args[0], opts.Namespace)
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
		matchers, err := parseFilters(opts.Filters)
		if err != nil {
			return fail.NewCommandUsageError(cmd, "", err)
		}
		groupBy, err := parseGroupBy(opts.GroupBy)
		if err != nil {
			return fail.NewCommandUsageError(cmd, "", err)
		}

		ctx := cmd.Context()
		var loader providers.ConfigLoader

		cfg, err := loader.LoadGrafanaConfig(ctx)
		if err != nil {
			return err
		}

		var cfgCtx *internalconfig.Context
		if fullCfg, err := loader.LoadFullConfig(ctx); err != nil {
			logging.FromContext(ctx).Warn("could not load config; falling back to auto-discovery", slog.String("error", err.Error()))
		} else {
			cfgCtx = fullCfg.GetCurrentContext()
		}

		datasourceUID, err := dsquery.ResolveAndSaveDatasource(ctx, &loader, opts.Datasource, cfgCtx, cfg, "prometheus")
		if err != nil {
			return err
		}

		client, err := prometheus.NewClient(cfg)
		if err != nil {
			return fmt.Errorf("failed to create prometheus client: %w", err)
		}

		// Bare-name resolution: same UX as `services get`. Without this
		// step, an operations query against a namespaced service via its
		// bare name silently returns no rows because the `job` label is
		// `<ns>/<name>`, not `<name>`.
		if namespace == "" {
			resolved, err := resolveNamespaceForBareName(ctx, client, datasourceUID, name, matchers)
			if err != nil {
				return err
			}
			namespace = resolved
		}

		if auto {
			mode, err = detectMetricsMode(ctx, client, datasourceUID, namespace, name, matchers)
			if err != nil {
				return fmt.Errorf("metrics-mode auto-detect failed: %w", err)
			}
		}

		response, err := fetchOperations(ctx, client, datasourceUID, namespace, name, opts.Since, kinds, mode, matchers, groupBy)
		if err != nil {
			return err
		}

		notFound := !response.Service.Instrumented && len(response.Items) == 0
		if notFound {
			emitNoDataHint(cmd.ErrOrStderr(), namespace, name)
		}

		truncated := false
		if opts.Limit > 0 && len(response.Items) > opts.Limit {
			response.Items = response.Items[:opts.Limit]
			truncated = true
		}
		if truncated {
			emitOperationsLimitHint(cmd.ErrOrStderr(), opts.Limit)
		}

		if err := opts.IO.Encode(cmd.OutOrStdout(), response); err != nil {
			return err
		}
		if notFound {
			return notFoundEmitted(cmd.ErrOrStderr(),
				fmt.Sprintf("service %q has no telemetry in the requested window", jobLabel(namespace, name)))
		}
		return nil
	}
}

// fetchOperations runs the five per-operation queries plus the metadata
// lookup in parallel and folds the responses into an OperationsResponse.
// Metadata uses the same target_info union as `services get` so the
// language/labels/env fields are consistent between commands.
func fetchOperations(ctx context.Context, client *prometheus.Client, datasourceUID, namespace, name, window string, kinds []string, mode MetricsMode, matchers []Matcher, groupBy []string) (*OperationsResponse, error) {
	names, ok := metricNamesByMode(mode)
	if !ok {
		return nil, fmt.Errorf("unknown metrics mode %q", mode)
	}

	metrics := targetInfoMetrics()
	metadataResponses := make([]*prometheus.QueryResponse, len(metrics))
	var rateResp, errorResp, avgResp, p50Resp, p95Resp, p99Resp *prometheus.QueryResponse

	eg, egCtx := errgroup.WithContext(ctx)
	for i, metric := range metrics {
		eg.Go(func() error {
			expr, err := buildServiceMetadataQuery(metric, namespace, name, matchers)
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
		expr, err := buildOperationsRateQuery(names, namespace, name, window, kinds, matchers, groupBy)
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
		expr, err := buildOperationsErrorRateQuery(names, namespace, name, window, kinds, matchers, groupBy)
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
		expr, err := buildOperationsAvgLatencyQuery(names, namespace, name, window, kinds, matchers, groupBy)
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
			expr, err := buildOperationsLatencyQuantileQuery(names, namespace, name, window, kinds, phi, matchers, groupBy)
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
	if err := eg.Wait(); err != nil {
		return nil, err
	}

	metadata, err := parseServicesResponses(metadataResponses)
	if err != nil {
		return nil, fmt.Errorf("failed to parse metadata response: %w", err)
	}
	svc := selectMetadataService(metadata, namespace, name)

	items := mergeOperations(
		extractOperations(rateResp, groupBy),
		extractOperations(errorResp, groupBy),
		extractOperations(avgResp, groupBy),
		extractOperations(p50Resp, groupBy),
		extractOperations(p95Resp, groupBy),
		extractOperations(p99Resp, groupBy),
		groupBy,
	)

	return &OperationsResponse{
		Service:     svc,
		Window:      window,
		MetricsMode: mode,
		SpanKinds:   spanKindRegex(kinds),
		GroupBy:     groupBy,
		Items:       items,
	}, nil
}

// emitOperationsLimitHint mirrors the truncation hint pattern from
// `services list`: a runnable command pointing at a doubled limit
// (TTY) or the structured envelope (agent mode).
func emitOperationsLimitHint(stderr io.Writer, limit int) {
	cmdio.EmitHint(stderr,
		fmt.Sprintf("showing top %d operations by time share", limit),
		fmt.Sprintf("gcx appo11y services list-operations <service> --limit %d", limit*2))
}

type operationsTableCodec struct {
	Wide bool
}

func (c *operationsTableCodec) Format() format.Format {
	if c.Wide {
		return "wide"
	}
	return "table"
}

func (c *operationsTableCodec) Decode(io.Reader, any) error {
	return errors.New("services operations table codec does not support decoding")
}

func (c *operationsTableCodec) Encode(w io.Writer, v any) error {
	resp, ok := v.(*OperationsResponse)
	if !ok {
		return fmt.Errorf("invalid data type for services operations table codec: %T", v)
	}
	if len(resp.Items) == 0 {
		_, err := fmt.Fprintln(w, "No operations found. Verify the service is emitting span metrics in the requested window.")
		return err
	}

	// When grouping, a column per group label is prepended so each row
	// reads "<group> <operation> ...". mergeOperations already clusters
	// rows by group then time-share.
	groupHeaders := upperHeaders(resp.GroupBy)

	var headers []string
	headers = append(headers, groupHeaders...)
	if c.Wide {
		headers = append(headers, "OPERATION", "RATE", "ERRORS", "ERROR %", "P50", "P95", "P99", "TIME %")
	} else {
		headers = append(headers, "OPERATION", "RATE", "ERROR %", "P95", "TIME %")
	}
	t := style.NewTable(headers...)
	for i := range resp.Items {
		op := &resp.Items[i]
		row := make([]string, 0, len(headers))
		for _, l := range resp.GroupBy {
			row = append(row, orDash(op.Labels[l]))
		}
		if c.Wide {
			row = append(row,
				op.Name,
				formatRateWithUnit(op.RatePerSecond, op.HasTraffic),
				formatRateWithUnit(op.ErrorRatePerSec, op.HasErrors),
				formatPercentMaybe(op.ErrorPercent, op.HasTraffic),
				formatDuration(op.P50Seconds, op.HasLatencyP50),
				formatDuration(op.P95Seconds, op.HasLatencyP95),
				formatDuration(op.P99Seconds, op.HasLatencyP99),
				formatPercentMaybe(op.TimeSharePercent, op.HasAvgLatency && op.HasTraffic),
			)
		} else {
			row = append(row,
				op.Name,
				formatRateWithUnit(op.RatePerSecond, op.HasTraffic),
				formatPercentMaybe(op.ErrorPercent, op.HasTraffic),
				formatDuration(op.P95Seconds, op.HasLatencyP95),
				formatPercentMaybe(op.TimeSharePercent, op.HasAvgLatency && op.HasTraffic),
			)
		}
		t.Row(row...)
	}
	return t.Render(w)
}

// formatPercentMaybe is the operations-table variant of formatPercent
// that returns "-" when the underlying metric has no signal, instead
// of "0.00%" — keeps the table honest about what's measured.
func formatPercentMaybe(v float64, has bool) string {
	if !has {
		return "-"
	}
	return fmt.Sprintf("%.2f%%", v)
}
