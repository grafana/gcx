package services

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"

	"github.com/grafana/gcx/internal/agent"
	internalconfig "github.com/grafana/gcx/internal/config"
	dsquery "github.com/grafana/gcx/internal/datasources/query"
	"github.com/grafana/gcx/internal/format"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/query/prometheus"
	"github.com/grafana/gcx/internal/style"
	"github.com/grafana/grafana-app-sdk/logging"
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
	Window      string
	Namespace   string
	Kind        string
	MetricsMode string
}

func (o *getOpts) setup(flags *pflag.FlagSet) {
	o.IO.RegisterCustomCodec("table", &serviceDetailCodec{})
	o.IO.RegisterCustomCodec("wide", &serviceDetailCodec{Wide: true})
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)

	flags.StringVarP(&o.Datasource, "datasource", "d", "", "Prometheus datasource UID (defaults to datasources.prometheus in config or auto-discovery)")
	flags.StringVarP(&o.Namespace, "namespace", "n", "", "Service namespace (only needed when the argument is the bare service name and multiple namespaces are in play)")
	flags.StringVar(&o.Window, "window", defaultRedWindow, "Rate/quantile window (e.g. 1m, 5m, 1h)")
	flags.StringVar(&o.Kind, "kind", "inbound", "Span kinds to include: inbound (server+consumer), server, consumer, all, or a comma list of SPAN_KIND_* literals")
	flags.StringVar(&o.MetricsMode, "metrics-mode", metricsModeAuto, "Span-metrics family: auto (default, probes the stack), v3 (traces_span_metrics_*), tempo (traces_spanmetrics_*), or otel (bare calls_total + duration_seconds_bucket)")
}

func (o *getOpts) Validate() error {
	if err := o.IO.Validate(); err != nil {
		return err
	}
	if strings.TrimSpace(o.Window) == "" {
		return errors.New("--window must not be empty")
	}
	if _, err := time.ParseDuration(o.Window); err != nil {
		return fmt.Errorf("--window %q is not a valid duration: %w", o.Window, err)
	}
	if _, err := resolveSpanKinds(o.Kind); err != nil {
		return err
	}
	if _, _, err := resolveMetricsMode(o.MetricsMode); err != nil {
		return err
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
over --window (default 5m), restricted to inbound spans (SERVER + CONSUMER).

The span-metric family is selected by --metrics-mode:
  auto   probe each family for this service and pick the one with data
         (default — prefers v3 > tempo > otel when a stack double-emits)
  v3     traces_span_metrics_calls_total / _duration_seconds_bucket
         (modern Tempo / OTel Collector ≥ 1.0.9)
  tempo  traces_spanmetrics_calls_total  / _latency_bucket
         (legacy Tempo metrics-generator, Beyla)
  otel   calls_total / duration_seconds_bucket
         (bare OTel Collector spanmetrics connector)
The resolved mode is reported in the snapshot output so you can confirm
which family produced the numbers.`,
		Example: `
  # Bare name — namespace is resolved from target_info
  gcx appo11y services get checkoutservice

  # Same service, explicit namespace (skips the lookup)
  gcx appo11y services get payments/checkoutservice --window 1h

  # JSON for scripting
  gcx appo11y services get checkoutservice -o json

  # Restrict to server-side traffic only
  gcx appo11y services get checkoutservice --kind server

  # Stack still on the legacy Tempo metrics-generator
  gcx appo11y services get checkoutservice --metrics-mode tempo`,
		Args: cobra.ExactArgs(1),
		RunE: runGet(opts),
		Annotations: map[string]string{
			agent.AnnotationTokenCost: "small",
			agent.AnnotationLLMHint:   `Per-service RED snapshot from Tempo/OTel span metrics: rate (req/s), error rate, error percent, p50/p95/p99 latency (seconds) over --window (default 5m), scoped to inbound spans (SERVER+CONSUMER). --metrics-mode picks the family: v3 (default, traces_span_metrics_*), tempo (legacy traces_spanmetrics_*), otel (bare calls_total/duration_seconds_bucket). Pairs with 'gcx appo11y services list' to drill into a single row. Examples: gcx appo11y services get <name> -o json; gcx appo11y services get <ns>/<name> --window 1h -o json; gcx appo11y services get <name> --metrics-mode tempo -o json`,
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

func runGet(opts *getOpts) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		if err := opts.Validate(); err != nil {
			return err
		}
		namespace, name, err := parseServiceArg(args[0], opts.Namespace)
		if err != nil {
			return err
		}
		kinds, err := resolveSpanKinds(opts.Kind)
		if err != nil {
			return err
		}
		mode, auto, err := resolveMetricsMode(opts.MetricsMode)
		if err != nil {
			return err
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
			resolved, err := resolveNamespaceForBareName(ctx, client, datasourceUID, name)
			if err != nil {
				return err
			}
			namespace = resolved
		}

		if auto {
			mode, err = detectMetricsMode(ctx, client, datasourceUID, namespace, name)
			if err != nil {
				return fmt.Errorf("metrics-mode auto-detect failed: %w", err)
			}
		}

		detail, err := fetchServiceDetail(ctx, client, datasourceUID, namespace, name, opts.Window, kinds, mode)
		if err != nil {
			return err
		}
		if !detail.Service.Instrumented && !detail.RED.HasTraffic {
			emitNoDataHint(cmd.ErrOrStderr(), namespace, name)
		}
		return opts.IO.Encode(cmd.OutOrStdout(), detail)
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
func resolveNamespaceForBareName(ctx context.Context, client *prometheus.Client, datasourceUID, name string) (string, error) {
	metrics := targetInfoMetrics()
	responses := make([]*prometheus.QueryResponse, len(metrics))

	eg, egCtx := errgroup.WithContext(ctx)
	for i, metric := range metrics {
		eg.Go(func() error {
			expr, err := buildBareNameLookupQuery(metric, name)
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
	return "", fmt.Errorf("service %q exists in multiple namespaces (%s); disambiguate with <namespace>/%s or --namespace", name, strings.Join(namespaces, ", "), name)
}

// detectMetricsMode probes each metrics family in parallel for the given
// service and returns the first one with data, biased toward modern names
// (see metricsModePreference). When no family has data — uninstrumented
// service, stale telemetry, wrong stack — it falls back to v3 so the RED
// snapshot still renders (just with "no traffic") instead of erroring.
func detectMetricsMode(ctx context.Context, client *prometheus.Client, datasourceUID, namespace, name string) (MetricsMode, error) {
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
			expr, err := buildModeProbeQuery(names.calls, job)
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
func fetchServiceDetail(ctx context.Context, client *prometheus.Client, datasourceUID, namespace, name, window string, kinds []string, mode MetricsMode) (*ServiceDetail, error) {
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
			expr, err := buildServiceMetadataQuery(metric, namespace, name)
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
		expr, err := buildRateQuery(names, namespace, name, window, kinds)
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
		expr, err := buildErrorRateQuery(names, namespace, name, window, kinds)
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
			expr, err := buildLatencyQuantileQuery(names, namespace, name, window, kinds, phi)
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
	detail, ok := v.(*ServiceDetail)
	if !ok {
		return fmt.Errorf("invalid data type for services get table codec: %T", v)
	}
	if err := c.encodeMetadata(w, detail); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w); err != nil {
		return err
	}
	return c.encodeRED(w, &detail.RED)
}

func (c *serviceDetailCodec) encodeMetadata(w io.Writer, d *ServiceDetail) error {
	labels := defaultLabels()
	if c.Wide {
		labels = allTargetInfoLabels()
	}
	headers := []string{"FIELD", "VALUE"}
	t := style.NewTable(headers...)
	t.Row("name", orDash(d.Service.Name))
	t.Row("namespace", orDash(d.Service.Namespace))
	t.Row("language", orDash(d.Service.Language))
	t.Row("status", instrumentationStatus(d.Service.Instrumented))
	t.Row("environment", orDash(environmentValue(d.Service.Labels)))
	for _, lbl := range labels {
		if lbl == "deployment_environment" || lbl == "deployment_environment_name" {
			continue // already surfaced as `environment`
		}
		t.Row(strings.ReplaceAll(lbl, "_", "."), orDash(d.Service.Labels[lbl]))
	}
	return t.Render(w)
}

func (c *serviceDetailCodec) encodeRED(w io.Writer, r *REDStats) error {
	t := style.NewTable("METRIC", "VALUE")
	t.Row("window", r.Window)
	t.Row("metrics mode", string(r.MetricsMode))
	t.Row("span kinds", r.SpanKinds)
	t.Row("rate (req/s)", formatRate(r.RatePerSecond, r.HasTraffic))
	t.Row("errors (req/s)", formatRate(r.ErrorRatePerSec, r.HasErrors))
	t.Row("error %", formatPercent(r.ErrorPercent, r.HasTraffic))
	t.Row("p50 latency", formatDuration(r.P50Seconds, r.HasLatencyP50))
	t.Row("p95 latency", formatDuration(r.P95Seconds, r.HasLatencyP95))
	t.Row("p99 latency", formatDuration(r.P99Seconds, r.HasLatencyP99))
	return t.Render(w)
}

func formatRate(v float64, has bool) string {
	if !has {
		return "-"
	}
	return fmt.Sprintf("%.3f", v)
}

func formatPercent(v float64, has bool) string {
	if !has {
		return "-"
	}
	return fmt.Sprintf("%.2f%%", v)
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
