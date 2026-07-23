package services

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"text/tabwriter"

	"github.com/grafana/gcx/cmd/gcx/fail"
	"github.com/grafana/gcx/internal/agent"
	internalconfig "github.com/grafana/gcx/internal/config"
	dsquery "github.com/grafana/gcx/internal/datasources/query"
	"github.com/grafana/gcx/internal/format"
	"github.com/grafana/gcx/internal/gcxerrors"
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

// defaultRedWindow is the rate/quantile window when --window isn't set.
// 5m is a useful starting point: short enough to reflect current behaviour,
// long enough that low-traffic services still produce histogram samples.
const defaultRedWindow = "5m"

type getOpts struct {
	IO          cmdio.Options
	Datasource  string
	Since       string
	Namespace   string
	Kind        string
	MetricsMode string
	Filters     []string
	GroupBy     []string
}

func (o *getOpts) setup(flags *pflag.FlagSet) {
	o.IO.RegisterCustomCodec("table", &serviceDetailCodec{})
	o.IO.RegisterCustomCodec("wide", &serviceDetailCodec{Wide: true})
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)

	flags.StringVarP(&o.Datasource, "datasource", "d", "", "Prometheus datasource UID (defaults to datasources.prometheus in config or auto-discovery)")
	flags.StringVarP(&o.Namespace, "namespace", "n", "", "Service namespace (only needed when the argument is the bare service name and multiple namespaces are in play)")
	flags.StringVar(&o.Since, "since", defaultRedWindow, "Rate/quantile window applied to span metrics (e.g. 1m, 5m, 1h, 1d) — PromQL duration syntax")
	flags.StringVar(&o.Kind, "kind", "inbound", "Span kinds to include. One of: inbound (server+consumer), server, consumer, all, or a comma-separated list of SPAN_KIND_* literals")
	flags.StringVar(&o.MetricsMode, "metrics-mode", metricsModeAuto, "Span-metrics family. One of: auto (probes the stack), v3 (traces_span_metrics_*), tempo (traces_spanmetrics_*), or otel (bare calls_total + duration_seconds_bucket)")
	flags.StringArrayVar(&o.Filters, "filter", nil, "Scope the RED snapshot to series matching a label matcher, e.g. --filter k8s_cluster_name=prod-us (repeatable). Use to break a multi-cluster/multi-region service down one cluster at a time; the label must exist on the span metrics")
	flags.StringSliceVar(&o.GroupBy, "group-by", nil, "Pivot the RED snapshot into one row per distinct value of a label, e.g. --group-by k8s_cluster_name (comma-separated or repeatable). Surfaces outliers across clusters/regions without naming each one; the label must exist on the span metrics")
}

func (o *getOpts) Validate(cmd *cobra.Command) error {
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
	return nil
}

// resolveSpanKinds maps the `--kind` flag onto the literal SPAN_KIND_* values
// that Tempo's spanmetrics emits. Aliases are case-insensitive; explicit
// SPAN_KIND_* values are accepted verbatim (and case-corrected) so users can
// pass a custom mix without learning a new vocabulary.
func resolveSpanKinds(raw string) ([]string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return defaultInboundSpanKinds(), nil
	}
	switch strings.ToLower(raw) {
	case "inbound":
		return defaultInboundSpanKinds(), nil
	case "server":
		return []string{spanKindServer}, nil
	case "consumer":
		return []string{spanKindConsumer}, nil
	case "all":
		return []string{"SPAN_KIND_SERVER", "SPAN_KIND_CONSUMER", "SPAN_KIND_CLIENT", "SPAN_KIND_PRODUCER", "SPAN_KIND_INTERNAL"}, nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(strings.ToUpper(p))
		if p == "" {
			continue
		}
		if !strings.HasPrefix(p, "SPAN_KIND_") {
			return nil, fmt.Errorf("--kind %q is not a recognized alias (inbound, server, consumer, all) or a SPAN_KIND_* literal", raw)
		}
		out = append(out, p)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("--kind %q resolved to an empty set", raw)
	}
	return out, nil
}

// parseServiceArg splits a positional argument into (namespace, name).
// Accepts "name" or "namespace/name"; an explicit --namespace flag wins
// when the argument has no slash. If both are given and they disagree,
// returns an error so the caller can't silently target a wrong service.
func parseServiceArg(arg, flagNamespace string) (string, string, error) {
	arg = strings.TrimSpace(arg)
	if arg == "" {
		return "", "", errors.New("service name is required")
	}
	ns, name := parseJob(arg)
	if ns == "" {
		ns = flagNamespace
	} else if flagNamespace != "" && flagNamespace != ns {
		return "", "", fmt.Errorf("namespace %q in argument conflicts with --namespace %q", ns, flagNamespace)
	}
	if name == "" {
		return "", "", fmt.Errorf("service name is empty (got %q)", arg)
	}
	return ns, name, nil
}

func newGetCommand() *cobra.Command {
	opts := &getOpts{}
	cmd := &cobra.Command{
		Use:   "get <service> [--namespace ns]",
		Short: "Inspect a single Application Observability service: metadata + RED snapshot.",
		Long: `Show metadata and a rate/errors/duration snapshot for one service.

The argument is either the bare service name (matching the OTel service.name
resource attribute) or the canonical "<namespace>/<name>" form. When a bare
name is given, the namespace is resolved automatically from target_info;
ambiguity (the same name in multiple namespaces) errors out so the snapshot
can't accidentally target the wrong service. Pass --namespace or use the
"<namespace>/<name>" form to disambiguate.

Metadata comes from the same target_info/traces_target_info union used by
"gcx appo11y services list". RED numbers are computed from span metrics
over --since (default 5m), restricted to inbound spans (SERVER + CONSUMER).

The span-metric family is selected by --metrics-mode:
  auto   probe each family for this service and pick the one with data
         (prefers v3 > tempo > otel when a stack double-emits)
  v3     traces_span_metrics_calls_total / _duration_seconds_bucket
         (OTel Collector >= 0.109, Grafana Alloy >= 1.5.0)
  tempo  traces_spanmetrics_calls_total  / _latency_bucket
         (Tempo metrics-generator — Grafana Cloud default — and Beyla)
  otel   calls_total / duration_seconds_bucket
         (OTel Collector 0.94–0.108, Alloy 1.0–1.4.3, Grafana Agent >= 0.40)
The resolved mode is reported in the snapshot output so you can confirm
which family produced the numbers.`,
		Example: `
  # Bare name — namespace is resolved from target_info
  gcx appo11y services get checkoutservice

  # Same service, explicit namespace (skips the lookup)
  gcx appo11y services get payments/checkoutservice --since 1h

  # JSON for scripting
  gcx appo11y services get checkoutservice -o json

  # Restrict to server-side traffic only
  gcx appo11y services get checkoutservice --kind server

  # Stack double-emits both families; force the Tempo metrics-generator numbers
  gcx appo11y services get checkoutservice --metrics-mode tempo

  # Break a multi-cluster service down to a single cluster
  gcx appo11y services get faro-collector --filter k8s_cluster_name=prod-us-central-0

  # Compare a service's RED across all clusters to spot the outlier
  gcx appo11y services get faro-collector --group-by k8s_cluster_name`,
		Args: cobra.ExactArgs(1),
		RunE: runGet(opts),
		Annotations: map[string]string{
			agent.AnnotationTokenCost: "small",
			agent.AnnotationLLMHint:   `Per-service RED snapshot from Tempo/OTel span metrics: rate (req/s), error rate, error percent, p50/p95/p99 latency (seconds) over --since (default 5m), scoped to inbound spans (SERVER+CONSUMER). --metrics-mode picks the family: auto (default, probes the stack), v3 (traces_span_metrics_*, OTel Collector >= 0.109 / Alloy >= 1.5), tempo (traces_spanmetrics_*, Tempo metrics-generator — Grafana Cloud default — and Beyla), otel (bare calls_total/duration_seconds_bucket, older Collector/Alloy/Agent). Pairs with 'gcx appo11y services list' to drill into a single row. Use --filter <label><op><value> (repeatable) to scope the snapshot to a subset of series — most usefully a cluster/region label (e.g. --filter k8s_cluster_name=prod-us) to break a multi-cluster service down one cluster at a time. Use --group-by <label> to instead pivot the snapshot into one RED row per distinct value of that label (comma-separated for multiple) — the outlier-finding view: 'get <name> --group-by k8s_cluster_name -o json' returns items[] each with the group's labels and RED, so you can spot which cluster/region is slow or erroring without naming them. Examples: gcx appo11y services get <name> -o json; gcx appo11y services get <ns>/<name> --since 1h -o json; gcx appo11y services get <name> --metrics-mode tempo -o json; gcx appo11y services get <name> --filter k8s_cluster_name=<cluster> -o json; gcx appo11y services get <name> --group-by k8s_cluster_name -o json`,
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

func runGet(opts *getOpts) func(*cobra.Command, []string) error {
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

		// Bare-name resolution: if the user passed just `<name>` (no
		// namespace), look the service up in target_info to find the
		// matching namespace. Without this step, a query for a namespaced
		// service via its bare name silently returns no data because the
		// `job` label is `<ns>/<name>`, not `<name>`.
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

		if len(groupBy) > 0 {
			grouped, err := fetchGroupedServiceDetail(ctx, client, datasourceUID, namespace, name, opts.Since, kinds, mode, matchers, groupBy)
			if err != nil {
				return err
			}
			notFound := !anyGroupHasTraffic(grouped.Items)
			if notFound {
				emitNoDataHint(cmd.ErrOrStderr(), namespace, name)
			}
			if err := opts.IO.Encode(cmd.OutOrStdout(), grouped); err != nil {
				return err
			}
			if notFound {
				return notFoundEmitted(cmd.ErrOrStderr(),
					fmt.Sprintf("service %q has no telemetry in the requested window (for group-by %s)", jobLabel(namespace, name), strings.Join(groupBy, ", ")))
			}
			return nil
		}

		detail, err := fetchServiceDetail(ctx, client, datasourceUID, namespace, name, opts.Since, kinds, mode, matchers)
		if err != nil {
			return err
		}
		notFound := !detail.Service.Instrumented && !detail.RED.HasTraffic
		if notFound {
			emitNoDataHint(cmd.ErrOrStderr(), namespace, name)
		}
		if err := opts.IO.Encode(cmd.OutOrStdout(), detail); err != nil {
			return err
		}
		if notFound {
			// Align with other commands' "entity not-found" semantics:
			// emit the snapshot (useful structure for agents) but signal
			// failure via exit code so callers can branch on $?.
			return notFoundEmitted(cmd.ErrOrStderr(),
				fmt.Sprintf("service %q has no telemetry in the requested window", jobLabel(namespace, name)))
		}
		return nil
	}
}

// resolveNamespaceForBareName queries the target_info union for any series
// whose `job` is the bare name or some `<ns>/<name>`, then returns the
// single matching namespace (or "" for a truly un-namespaced service).
//
// Returns:
//   - "" with no error when nothing matched (caller proceeds with the bare
//     name; the resulting snapshot will show no data and the standard hint
//     will fire).
//   - <ns> with no error when exactly one namespace matched (or only the
//     bare-name shape did).
//   - an error listing the candidates when multiple namespaces have a
//     service with the requested name — ambiguity is something the user
//     must resolve explicitly with `<ns>/<name>` or `--namespace`.
func resolveNamespaceForBareName(ctx context.Context, client *prometheus.Client, datasourceUID, name string, matchers []Matcher) (string, error) {
	metrics := targetInfoMetrics()
	responses := make([]*prometheus.QueryResponse, len(metrics))

	eg, egCtx := errgroup.WithContext(ctx)
	for i, metric := range metrics {
		eg.Go(func() error {
			expr, err := buildBareNameLookupQuery(metric, name, matchers)
			if err != nil {
				return fmt.Errorf("failed to build %s lookup query: %w", metric, err)
			}
			resp, err := client.Query(egCtx, datasourceUID, prometheus.QueryRequest{Query: expr})
			if err != nil {
				return fmt.Errorf("%s lookup query failed: %w", metric, err)
			}
			responses[i] = resp
			return nil
		})
	}
	if err := eg.Wait(); err != nil {
		return "", err
	}

	namespaces := namespacesForName(extractJobsFromResponses(responses), name)
	switch len(namespaces) {
	case 0:
		return "", nil
	case 1:
		return namespaces[0], nil
	}
	return "", fmt.Errorf("service %q exists in %d namespaces (%s); disambiguate with <namespace>/%s or --namespace",
		name, len(namespaces), summarizeNamespaces(namespaces, 5), name)
}

// summarizeNamespaces formats a namespace list for the ambiguity error.
// At most `max` entries are listed verbatim — with 30+ candidates (services
// deployed per-region) the full list would push the rest of the error off
// screen and bury the hint. Empty namespaces render as `(none)` so users
// can tell the bare-job shape apart from the namespaced ones.
func summarizeNamespaces(namespaces []string, limit int) string {
	render := func(ns string) string {
		if ns == "" {
			return "(none)"
		}
		return ns
	}
	if len(namespaces) <= limit {
		out := make([]string, len(namespaces))
		for i, ns := range namespaces {
			out[i] = render(ns)
		}
		return strings.Join(out, ", ")
	}
	head := make([]string, limit)
	for i := range limit {
		head[i] = render(namespaces[i])
	}
	return fmt.Sprintf("%s, ... and %d more", strings.Join(head, ", "), len(namespaces)-limit)
}

// detectMetricsMode probes each metrics family in parallel for the given
// service and returns the first one with data, biased toward modern names
// (see metricsModePreference). When no family has data — uninstrumented
// service, stale telemetry, wrong stack — it falls back to v3 so the RED
// snapshot still renders (just with "no traffic") instead of erroring.
func detectMetricsMode(ctx context.Context, client *prometheus.Client, datasourceUID, namespace, name string, matchers []Matcher) (MetricsMode, error) {
	preference := metricsModePreference()
	found := make([]bool, len(preference))
	job := jobLabel(namespace, name)

	eg, egCtx := errgroup.WithContext(ctx)
	for i, m := range preference {
		names, ok := metricNamesByMode(m)
		if !ok {
			return "", fmt.Errorf("unknown metrics mode %q", m)
		}
		eg.Go(func() error {
			expr, err := buildModeProbeQuery(names.calls, job, matchers)
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

// fetchServiceDetail runs the metadata + RED queries in parallel and folds
// the responses into one ServiceDetail. Latency and error queries return
// zero with HasX=false when there's no series in the window; the table
// codec renders those as `-`.
func fetchServiceDetail(ctx context.Context, client *prometheus.Client, datasourceUID, namespace, name, window string, kinds []string, mode MetricsMode, matchers []Matcher) (*ServiceDetail, error) {
	names, ok := metricNamesByMode(mode)
	if !ok {
		return nil, fmt.Errorf("unknown metrics mode %q", mode)
	}

	metrics := targetInfoMetrics()
	metadataResponses := make([]*prometheus.QueryResponse, len(metrics))
	var rateResp, errorResp, p50Resp, p95Resp, p99Resp *prometheus.QueryResponse

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
		expr, err := buildRateQuery(names, namespace, name, window, kinds, matchers, nil)
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
		expr, err := buildErrorRateQuery(names, namespace, name, window, kinds, matchers, nil)
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
	for phi, sink := range map[float64]**prometheus.QueryResponse{
		0.50: &p50Resp,
		0.95: &p95Resp,
		0.99: &p99Resp,
	} {
		eg.Go(func() error {
			expr, err := buildLatencyQuantileQuery(names, namespace, name, window, kinds, phi, matchers, nil)
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
	rate, hasRate := instantScalar(rateResp)
	errRate, hasErr := instantScalar(errorResp)
	p50, hasP50 := instantScalar(p50Resp)
	p95, hasP95 := instantScalar(p95Resp)
	p99, hasP99 := instantScalar(p99Resp)

	return &ServiceDetail{
		Service: svc,
		RED: REDStats{
			Window:          window,
			MetricsMode:     mode,
			SpanKinds:       spanKindRegex(kinds),
			RatePerSecond:   rate,
			ErrorRatePerSec: errRate,
			ErrorPercent:    computeErrorPercent(errRate, rate),
			P50Seconds:      p50,
			P95Seconds:      p95,
			P99Seconds:      p99,
			HasTraffic:      hasRate && rate > 0,
			// When there's traffic we know the error rate (it's 0 if no
			// STATUS_CODE_ERROR series came back). Without this, a healthy
			// service prints "-" for errors which reads as "no data" rather
			// than the truthful "zero errors observed".
			HasErrors:     hasErr || (hasRate && rate > 0),
			HasLatencyP50: hasP50,
			HasLatencyP95: hasP95,
			HasLatencyP99: hasP99,
		},
	}, nil
}

// fetchGroupedServiceDetail runs the metadata lookup plus the grouped
// rate/error/latency queries in parallel and folds them into one
// GroupedServiceDetail — a RED row per distinct --group-by combination.
// Metadata stays service-level (language/labels identify the service, not
// the group); only the RED numbers are pivoted per group.
func fetchGroupedServiceDetail(ctx context.Context, client *prometheus.Client, datasourceUID, namespace, name, window string, kinds []string, mode MetricsMode, matchers []Matcher, groupBy []string) (*GroupedServiceDetail, error) {
	names, ok := metricNamesByMode(mode)
	if !ok {
		return nil, fmt.Errorf("unknown metrics mode %q", mode)
	}

	metrics := targetInfoMetrics()
	metadataResponses := make([]*prometheus.QueryResponse, len(metrics))
	var rateResp, errorResp, p50Resp, p95Resp, p99Resp *prometheus.QueryResponse

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
		expr, err := buildRateQuery(names, namespace, name, window, kinds, matchers, groupBy)
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
		expr, err := buildErrorRateQuery(names, namespace, name, window, kinds, matchers, groupBy)
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
	for phi, sink := range map[float64]**prometheus.QueryResponse{
		0.50: &p50Resp,
		0.95: &p95Resp,
		0.99: &p99Resp,
	} {
		eg.Go(func() error {
			expr, err := buildLatencyQuantileQuery(names, namespace, name, window, kinds, phi, matchers, groupBy)
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

	items := mergeGroupedRED(
		window, mode, kinds, groupBy,
		extractGrouped(rateResp, groupBy),
		extractGrouped(errorResp, groupBy),
		extractGrouped(p50Resp, groupBy),
		extractGrouped(p95Resp, groupBy),
		extractGrouped(p99Resp, groupBy),
	)

	return &GroupedServiceDetail{
		Service: svc,
		GroupBy: groupBy,
		Window:  window,
		Items:   items,
	}, nil
}

// anyGroupHasTraffic reports whether at least one grouped RED row saw
// traffic in the window — used to decide the not-found exit code.
func anyGroupHasTraffic(items []GroupedRED) bool {
	for _, it := range items {
		if it.RED.HasTraffic {
			return true
		}
	}
	return false
}

// selectMetadataService picks the best metadata match from a list returned
// by the filtered target_info query. The filter is already narrow (job
// matches exactly), so 0 or 1 results are expected; on >1 we prefer an exact
// (namespace, name) match, otherwise return the first row. When the query
// returned nothing we synthesize a placeholder so the caller can still
// render the requested identity (marked uninstrumented).
func selectMetadataService(metadata []Service, namespace, name string) Service {
	for _, s := range metadata {
		if s.Namespace == namespace && s.Name == name {
			return s
		}
	}
	if len(metadata) > 0 {
		return metadata[0]
	}
	return Service{Name: name, Namespace: namespace, Instrumented: false}
}

// notFoundEmitted converts a no-data outcome into the agent-contract error
// shape AFTER the full result document was successfully written to stdout:
// a typed stderr diagnostic (JSONL in agent mode, "warn: ..." on a TTY) plus
// an EmittedError so the process still exits non-zero without reportError
// appending a second stdout document. Previously these paths returned a bare
// error, which corrupted agent-mode stdout with a duplicate error JSON after
// the already-encoded result.
func notFoundEmitted(stderr io.Writer, msg string) error {
	cmdio.EmitWarn(stderr, msg)
	return gcxerrors.NewEmittedError(gcxerrors.ExitGeneralError, errors.New(msg))
}

// emitNoDataHint surfaces a stderr line when neither target_info nor span
// metrics returned anything for the requested service. TTY users get a
// runnable suggestion; agent mode gets the structured hint envelope.
func emitNoDataHint(stderr io.Writer, namespace, name string) {
	label := name
	if namespace != "" {
		label = namespace + "/" + name
	}
	cmdio.EmitHint(stderr,
		fmt.Sprintf("no telemetry found for %q in the requested window", label),
		"gcx appo11y services list")
}

// serviceDetailCodec renders a ServiceDetail as a compact two-section
// table: metadata header followed by a RED stats block. Wide adds the
// extended target_info labels surfaced by `services list --output wide`.
type serviceDetailCodec struct {
	Wide bool
}

func (c *serviceDetailCodec) Format() format.Format {
	if c.Wide {
		return "wide"
	}
	return "table"
}

func (c *serviceDetailCodec) Decode(io.Reader, any) error {
	return errors.New("services get table codec does not support decoding")
}

func (c *serviceDetailCodec) Encode(w io.Writer, v any) error {
	if grouped, ok := v.(*GroupedServiceDetail); ok {
		return c.encodeGrouped(w, grouped)
	}
	detail, ok := v.(*ServiceDetail)
	if !ok {
		return fmt.Errorf("invalid data type for services get table codec: %T", v)
	}
	// kubectl-describe-style key:value block, not a bordered table.
	// A single resource doesn't tabulate well: the value column gets
	// stretched to terminal width and the borders dominate ~20 short
	// rows, so we drop the tabular renderer in favour of aligned text
	// with section spacing.
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)

	writeRow := func(label, value string) {
		fmt.Fprintf(tw, "%s:\t%s\n", label, value)
	}

	d := &detail.Service
	writeRow("Name", orDash(d.Name))
	writeRow("Namespace", orDash(d.Namespace))
	writeRow("Language", orDash(d.Language))
	writeRow("Status", instrumentationStatus(d.Instrumented))
	writeRow("Environment", orDash(environmentValue(d.Labels)))

	labels := defaultLabels()
	if c.Wide {
		labels = allTargetInfoLabels()
	}
	wroteLabelHeader := false
	for _, lbl := range labels {
		if lbl == "deployment_environment" || lbl == "deployment_environment_name" {
			continue // already surfaced as `Environment`
		}
		value := d.Labels[lbl]
		if value == "" {
			continue
		}
		if !wroteLabelHeader {
			fmt.Fprintln(tw, "Labels:")
			wroteLabelHeader = true
		}
		fmt.Fprintf(tw, "  %s:\t%s\n", strings.ReplaceAll(lbl, "_", "."), value)
	}

	fmt.Fprintln(tw)

	r := &detail.RED
	writeRow("Window", r.Window)
	writeRow("Metrics mode", string(r.MetricsMode))
	writeRow("Span kinds", r.SpanKinds)
	writeRow("Rate", formatRateWithUnit(r.RatePerSecond, r.HasTraffic))
	writeRow("Errors", formatErrors(r.ErrorRatePerSec, r.ErrorPercent, r.HasErrors, r.HasTraffic))
	fmt.Fprintln(tw, "Latency:")
	fmt.Fprintf(tw, "  p50:\t%s\n", formatDuration(r.P50Seconds, r.HasLatencyP50))
	fmt.Fprintf(tw, "  p95:\t%s\n", formatDuration(r.P95Seconds, r.HasLatencyP95))
	fmt.Fprintf(tw, "  p99:\t%s\n", formatDuration(r.P99Seconds, r.HasLatencyP99))

	return tw.Flush()
}

// encodeGrouped renders a --group-by result: a one-line service header
// followed by a table with a column per group label and the RED columns.
// Rows are already sorted (rate desc) by mergeGroupedRED so the busiest /
// outlier groups sit at the top. Wide adds the absolute error rate and
// p50/p99.
func (c *serviceDetailCodec) encodeGrouped(w io.Writer, g *GroupedServiceDetail) error {
	if len(g.Items) == 0 {
		_, err := fmt.Fprintf(w, "No telemetry found for %q grouped by %s in the requested window.\n",
			jobLabel(g.Service.Namespace, g.Service.Name), strings.Join(g.GroupBy, ", "))
		return err
	}

	groupHeaders := make([]string, len(g.GroupBy))
	for i, l := range g.GroupBy {
		groupHeaders[i] = strings.ToUpper(l)
	}

	var headers []string
	headers = append(headers, groupHeaders...)
	if c.Wide {
		headers = append(headers, "RATE", "ERRORS", "ERROR %", "P50", "P95", "P99")
	} else {
		headers = append(headers, "RATE", "ERROR %", "P95")
	}

	t := style.NewTable(headers...)
	for i := range g.Items {
		item := &g.Items[i]
		row := make([]string, 0, len(headers))
		for _, l := range g.GroupBy {
			row = append(row, orDash(item.Labels[l]))
		}
		r := &item.RED
		if c.Wide {
			row = append(row,
				formatRateWithUnit(r.RatePerSecond, r.HasTraffic),
				formatRateWithUnit(r.ErrorRatePerSec, r.HasErrors),
				formatPercentMaybe(r.ErrorPercent, r.HasTraffic),
				formatDuration(r.P50Seconds, r.HasLatencyP50),
				formatDuration(r.P95Seconds, r.HasLatencyP95),
				formatDuration(r.P99Seconds, r.HasLatencyP99),
			)
		} else {
			row = append(row,
				formatRateWithUnit(r.RatePerSecond, r.HasTraffic),
				formatPercentMaybe(r.ErrorPercent, r.HasTraffic),
				formatDuration(r.P95Seconds, r.HasLatencyP95),
			)
		}
		t.Row(row...)
	}
	return t.Render(w)
}

func formatRateWithUnit(v float64, has bool) string {
	if !has {
		return "-"
	}
	return fmt.Sprintf("%.3f req/s", v)
}

// formatErrors combines the absolute error rate with the percentage so the
// describe view shows both on one line. When there's no traffic, no error
// signal exists either.
func formatErrors(rate, pct float64, hasErrors, hasTraffic bool) string {
	if !hasErrors {
		return "-"
	}
	if !hasTraffic {
		return fmt.Sprintf("%.3f req/s", rate)
	}
	return fmt.Sprintf("%.3f req/s (%.2f%%)", rate, pct)
}

// formatDuration prints a latency value with units that scale to the
// magnitude — sub-millisecond stays in µs, sub-second stays in ms, anything
// larger is shown in seconds. Keeps the table readable across services that
// answer in 200µs and ones that answer in 2s without flipping precision.
func formatDuration(seconds float64, has bool) string {
	if !has {
		return "-"
	}
	switch {
	case seconds < 0.001:
		return fmt.Sprintf("%.0fµs", seconds*1_000_000)
	case seconds < 1:
		return fmt.Sprintf("%.2fms", seconds*1000)
	default:
		return fmt.Sprintf("%.3fs", seconds)
	}
}
