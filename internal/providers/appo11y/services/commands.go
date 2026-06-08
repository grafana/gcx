package services

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strconv"
	"strings"

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

// Commands returns the `services` command group, rooted under `gcx appo11y`.
func Commands() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "services",
		Short: "Inspect Application Observability services discovered from telemetry",
	}
	cmd.AddCommand(newListCommand())
	return cmd
}

const (
	instrAll            = "all"
	instrInstrumented   = "instrumented"
	instrUninstrumented = "uninstrumented"

	// servicesListDefaultLimit caps the row count of the default `services list`
	// view so a stack with thousands of services doesn't dump everything at
	// once. Users opt out with `--limit 0`. Mirrors the IRM list patterns.
	servicesListDefaultLimit = 50
)

type listOpts struct {
	IO              cmdio.Options
	Datasource      string
	ServiceGraph    string
	Filters         []string
	Language        string
	Env             string
	Columns         []string
	Limit           int
	Count           bool
	Instrumentation string
}

func (o *listOpts) setup(flags *pflag.FlagSet) {
	o.IO.RegisterCustomCodec("table", &servicesTableCodec{opts: o})
	o.IO.RegisterCustomCodec("wide", &servicesTableCodec{opts: o, Wide: true})
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)

	flags.StringVarP(&o.Datasource, "datasource", "d", "", "Prometheus datasource UID (defaults to datasources.prometheus in config or auto-discovery)")
	flags.StringVar(&o.ServiceGraph, "service-graph-metric", defaultServiceGraphMetric, "Override the service-graph metric used to find uninstrumented services (advanced)")
	flags.StringVar(&o.Instrumentation, "instrumentation", instrAll, "Which services to list: all, instrumented (target_info only), or uninstrumented (service-graph minus target_info)")
	flags.StringArrayVar(&o.Filters, "filter", nil, "Restrict to services matching a label matcher, e.g. --filter k8s_namespace_name=prod (repeatable; applies to target_info only)")
	flags.StringVar(&o.Language, "language", "", "Restrict to a single telemetry_sdk_language (e.g. go, java, nodejs)")
	flags.StringVar(&o.Env, "env", "", "Restrict to a single deployment_environment (e.g. production)")
	flags.StringSliceVar(&o.Columns, "columns", nil, "Extra target_info labels to surface as table columns (comma-separated)")
	flags.IntVar(&o.Limit, "limit", servicesListDefaultLimit, "Limit the number of services returned (0 = unlimited; applied after sorting)")
	flags.BoolVar(&o.Count, "count", false, "Print a per-language summary instead of the full list")
}

func (o *listOpts) Validate() error {
	if err := o.IO.Validate(); err != nil {
		return err
	}
	if o.Limit < 0 {
		return errors.New("--limit must be zero or positive")
	}
	switch o.Instrumentation {
	case instrAll, instrInstrumented, instrUninstrumented:
	default:
		return fmt.Errorf("--instrumentation must be one of %s, %s, or %s", instrAll, instrInstrumented, instrUninstrumented)
	}
	return nil
}

// reconcileCountLimit resolves the interaction between --count and --limit:
//
//   - if the user explicitly set both, that's a conflict (a per-language
//     summary over a truncated row set would lie about totals);
//   - if --count is on and --limit was left at the default, silently drop
//     the truncation so the summary covers the full inventory.
func (o *listOpts) reconcileCountLimit(flags *pflag.FlagSet) error {
	if !o.Count {
		return nil
	}
	if flags.Changed("limit") && o.Limit > 0 {
		return errors.New("--count cannot be combined with --limit; use --limit 0 to disable the limit when counting")
	}
	o.Limit = 0
	return nil
}

// wantsServiceGraph reports whether the service-graph query should run.
// The target-info queries always run — even in uninstrumented mode the
// instrumented set is needed to diff service-graph results against it.
func (o *listOpts) wantsServiceGraph() bool {
	return o.Instrumentation == instrAll || o.Instrumentation == instrUninstrumented
}

func (o *listOpts) buildFilters() ([]Matcher, error) {
	out := make([]Matcher, 0, len(o.Filters)+1)
	for _, f := range o.Filters {
		parsed, err := parseFilter(f)
		if err != nil {
			return nil, err
		}
		out = append(out, parsed)
	}
	if o.Language != "" {
		out = append(out, Matcher{Label: "telemetry_sdk_language", Op: "=", Value: o.Language})
	}
	// --env is intentionally NOT added as a PromQL matcher: a single matcher
	// can only target one of deployment_environment / deployment_environment_name,
	// but the ENVIRONMENT column reads whichever is populated. Filtering happens
	// post-parse via filterByEnv so the filter and the display agree.
	return out, nil
}

// filterByEnv keeps only services whose resolved environmentValue equals env.
// Returns items unchanged when env is empty.
func filterByEnv(items []Service, env string) []Service {
	if env == "" {
		return items
	}
	out := make([]Service, 0, len(items))
	for _, s := range items {
		if environmentValue(s.Labels) == env {
			out = append(out, s)
		}
	}
	return out
}

func newListCommand() *cobra.Command {
	opts := &listOpts{}
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List Application Observability services discovered from target_info/traces_target_info telemetry.",
		Long: `List the services Grafana Cloud Application Observability has discovered from telemetry.

Discovery uses the same approach as the App Observability plugin: a group-by
query against the OTel target_info metric, projected onto (job, telemetry_sdk_language).
Each result row is one service.

Related: "gcx kg entities --type Service" surfaces services as Knowledge Graph
entities with relationships and insights (requires the Knowledge Graph plugin);
"gcx instrumentation services" lists Kubernetes workloads discovered for setting up
instrumentation.`,
		Example: `
  # List all services in the current stack
  gcx appo11y services list

  # Filter to Go services running in production
  gcx appo11y services list --language go --env production

  # Show the top 20 services with extra target_info labels
  gcx appo11y services list --limit 20 --columns service_version,k8s_pod_name

  # Per-language summary instead of the full list
  gcx appo11y services list --count

  # Pin a datasource and output JSON
  gcx appo11y services list -d grafanacloud-prom -o json`,
		Args: cobra.NoArgs,
		RunE: runList(opts),
		Annotations: map[string]string{
			agent.AnnotationTokenCost: "small",
			agent.AnnotationLLMHint:   `Application Observability service inventory from OTel target_info/traces_target_info: one row per (namespace, service, language) with instrumentation coverage (instrumented vs uninstrumented). Available on any App Observability stack without the Knowledge Graph. Distinct from 'gcx kg entities --type Service' (Knowledge Graph entities with relationships/insights, when the Knowledge Graph plugin is enabled) and 'gcx instrumentation services' (Kubernetes workloads discovered for setting up instrumentation). Examples: gcx appo11y services list -o json; gcx appo11y services list --count -o json; gcx appo11y services list --instrumentation uninstrumented -o json`,
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

func runList(opts *listOpts) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, _ []string) error {
		if err := opts.Validate(); err != nil {
			return err
		}
		if err := opts.reconcileCountLimit(cmd.Flags()); err != nil {
			return err
		}

		ctx := cmd.Context()
		var loader providers.ConfigLoader

		cfg, err := loader.LoadGrafanaConfig(ctx)
		if err != nil {
			return err
		}

		var cfgCtx *internalconfig.Context
		fullCfg, err := loader.LoadFullConfig(ctx)
		if err != nil {
			logging.FromContext(ctx).Warn("could not load config; falling back to auto-discovery", slog.String("error", err.Error()))
		} else {
			cfgCtx = fullCfg.GetCurrentContext()
		}

		datasourceUID, err := dsquery.ResolveAndSaveDatasource(ctx, &loader, opts.Datasource, cfgCtx, cfg, "prometheus")
		if err != nil {
			return err
		}

		filters, err := opts.buildFilters()
		if err != nil {
			return err
		}

		client, err := prometheus.NewClient(cfg)
		if err != nil {
			return fmt.Errorf("failed to create prometheus client: %w", err)
		}

		var graph []Service
		metrics := targetInfoMetrics()
		// `instrumented` is the filtered set used for display rows.
		// `baseline` is the unfiltered set used as the index for the
		// uninstrumented diff so user filters don't bias what's known
		// to target_info. When no filters are set the two are identical
		// and we collapse to a single round-trip per metric.
		instrumentedResponses := make([]*prometheus.QueryResponse, len(metrics))
		needsBaseline := len(filters) > 0 && opts.wantsServiceGraph()
		baselineResponses := make([]*prometheus.QueryResponse, len(metrics))

		eg, egCtx := errgroup.WithContext(ctx)
		for i, metric := range metrics {
			eg.Go(func() error {
				expr, err := buildServicesQuery(metric, filters, opts.Columns)
				if err != nil {
					return fmt.Errorf("failed to build %s query: %w", metric, err)
				}
				resp, err := client.Query(egCtx, datasourceUID, prometheus.QueryRequest{Query: expr})
				if err != nil {
					return fmt.Errorf("%s query failed: %w", metric, err)
				}
				instrumentedResponses[i] = resp
				return nil
			})
			if needsBaseline {
				eg.Go(func() error {
					expr, err := buildServicesQuery(metric, nil, opts.Columns)
					if err != nil {
						return fmt.Errorf("failed to build %s baseline query: %w", metric, err)
					}
					resp, err := client.Query(egCtx, datasourceUID, prometheus.QueryRequest{Query: expr})
					if err != nil {
						return fmt.Errorf("%s baseline query failed: %w", metric, err)
					}
					baselineResponses[i] = resp
					return nil
				})
			}
		}
		if opts.wantsServiceGraph() {
			eg.Go(func() error {
				expr, err := buildServiceGraphQuery(opts.ServiceGraph)
				if err != nil {
					return fmt.Errorf("failed to build service-graph query: %w", err)
				}
				resp, err := client.Query(egCtx, datasourceUID, prometheus.QueryRequest{Query: expr})
				if err != nil {
					return fmt.Errorf("service-graph query failed: %w", err)
				}
				items, err := parseServiceGraphResponse(resp)
				if err != nil {
					return fmt.Errorf("failed to parse service-graph response: %w", err)
				}
				graph = items
				return nil
			})
		}
		if err := eg.Wait(); err != nil {
			return err
		}

		instrumented, err := parseServicesResponses(instrumentedResponses)
		if err != nil {
			return fmt.Errorf("failed to parse services response: %w", err)
		}
		baseline := instrumented
		if needsBaseline {
			baseline, err = parseServicesResponses(baselineResponses)
			if err != nil {
				return fmt.Errorf("failed to parse baseline response: %w", err)
			}
		}

		items := resolveItems(opts.Instrumentation, instrumented, baseline, graph)
		items = filterByEnv(items, opts.Env)

		truncated := false
		if opts.Limit > 0 && len(items) > opts.Limit {
			items = items[:opts.Limit]
			truncated = true
		}

		if opts.Count {
			return opts.IO.Encode(cmd.OutOrStdout(), summarizeByLanguage(items))
		}
		if truncated {
			emitLimitHint(cmd.ErrOrStderr(), opts.Limit)
		}
		return opts.IO.Encode(cmd.OutOrStdout(), &ServicesResponse{Items: items})
	}
}

// resolveItems applies the --instrumentation filter.
//
//   - instrumented: the filtered set used for display rows.
//   - baseline:    the unfiltered set used as the diff index for the
//     uninstrumented bucket. When no user filters are in play the caller
//     passes instrumented == baseline.
//   - graph:        service-graph entries (treated as potentially uninstrumented).
func resolveItems(filter string, instrumented, baseline, graph []Service) []Service {
	switch filter {
	case instrInstrumented:
		return instrumented
	case instrUninstrumented:
		return uninstrumentedFromGraph(baseline, graph)
	default:
		return mergeServiceSets(instrumented, baseline, graph)
	}
}

// servicesTableCodec renders services data as a tabular view. It type-switches
// on the encoded value: `*ServicesResponse` → rows; `*CountSummary` → counts.
// opts is a back-reference so the codec sees flag values that are populated
// after BindFlags but before Encode (e.g. --columns, --count).
type servicesTableCodec struct {
	opts *listOpts
	Wide bool
}

func (c *servicesTableCodec) Format() format.Format {
	if c.Wide {
		return "wide"
	}
	return "table"
}

func (c *servicesTableCodec) Decode(io.Reader, any) error {
	return errors.New("services table codec does not support decoding")
}

// defaultLabels are the resource-attribute labels we pull from target_info
// so the default table view can fill the ENVIRONMENT column via
// environmentValue. Older stacks emit deployment.environment (the plugin's
// default); newer SDKs emit deployment.environment.name. Namespace is parsed
// from the `job` label itself (see parseJob), not from a label.
func defaultLabels() []string {
	return []string{
		"deployment_environment",
		"deployment_environment_name",
	}
}

// wideLabels add infrastructure-flavored context on top of the defaults; only
// shown when --output wide is set. Picked from the plugin's
// RELEVANT_METADATA_PREFIXES set.
func wideLabels() []string {
	return []string{
		"k8s_namespace_name",
		"k8s_cluster_name",
		"cloud_region",
	}
}

// allTargetInfoLabels returns the union projection used by the discovery
// query so we can fill any default/wide column without a follow-up request.
func allTargetInfoLabels() []string {
	return append(defaultLabels(), wideLabels()...)
}

func (c *servicesTableCodec) Encode(w io.Writer, v any) error {
	switch data := v.(type) {
	case *CountSummary:
		return encodeCountTable(w, data)
	case *ServicesResponse:
		return c.encodeServicesTable(w, data)
	default:
		return fmt.Errorf("invalid data type for services table codec: %T", v)
	}
}

func (c *servicesTableCodec) encodeServicesTable(w io.Writer, resp *ServicesResponse) error {
	if len(resp.Items) == 0 {
		_, err := fmt.Fprintln(w, "No services discovered. Verify your stack is receiving OTel target_info telemetry.")
		return err
	}

	extra := c.extraColumns()
	wideCols := []string{}
	if c.Wide {
		wideCols = wideLabels()
	}

	headers := append([]string{"NAME", "NAMESPACE", "ENVIRONMENT", "LANGUAGE", "STATUS"}, upperHeaders(wideCols)...)
	headers = append(headers, upperHeaders(extra)...)

	t := style.NewTable(headers...)
	for _, s := range resp.Items {
		row := []string{
			s.Name,
			orDash(s.Namespace),
			orDash(environmentValue(s.Labels)),
			orDash(s.Language),
			instrumentationStatus(s.Instrumented),
		}
		for _, lbl := range wideCols {
			row = append(row, orDash(s.Labels[lbl]))
		}
		for _, lbl := range extra {
			row = append(row, orDash(s.Labels[lbl]))
		}
		t.Row(row...)
	}
	return t.Render(w)
}

// environmentValue prefers the legacy deployment.environment attribute (the
// long-standing OTel semconv value that App Observability stacks emit by
// default) and falls back to the current-semconv deployment.environment.name
// so stacks on the newer OTel SDK still surface a value. Either may be empty.
func environmentValue(labels map[string]string) string {
	if v := labels["deployment_environment"]; v != "" {
		return v
	}
	return labels["deployment_environment_name"]
}

func instrumentationStatus(instrumented bool) string {
	if instrumented {
		return "instrumented"
	}
	return "uninstrumented"
}

func (c *servicesTableCodec) extraColumns() []string {
	if c.opts == nil {
		return nil
	}
	return c.opts.Columns
}

func encodeCountTable(w io.Writer, s *CountSummary) error {
	if s == nil || s.Total == 0 {
		_, err := fmt.Fprintln(w, "No services discovered. Verify your stack is receiving OTel target_info telemetry.")
		return err
	}
	t := style.NewTable("LANGUAGE", "COUNT")
	for _, row := range s.ByLanguage {
		t.Row(row.Language, strconv.Itoa(row.Count))
	}
	t.Row("TOTAL", strconv.Itoa(s.Total))
	return t.Render(w)
}

func orDash(v string) string {
	if v == "" {
		return "-"
	}
	return v
}

// emitLimitHint surfaces a truncation hint on stderr when the result was
// capped by --limit. Format mirrors the IRM alert-groups list hint: TTY users
// get a runnable command pointing at a doubled limit; agent mode gets the
// structured JSON record.
func emitLimitHint(stderr io.Writer, limit int) {
	cmdio.EmitHint(stderr,
		fmt.Sprintf("showing first %d services", limit),
		fmt.Sprintf("gcx appo11y services list --limit %d", limit*2))
}

func upperHeaders(labels []string) []string {
	out := make([]string, len(labels))
	for i, l := range labels {
		out[i] = strings.ToUpper(strings.ReplaceAll(l, "_", " "))
	}
	return out
}
