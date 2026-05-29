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
)

type listOpts struct {
	IO              cmdio.Options
	Datasource      string
	Metric          string
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
	flags.StringVar(&o.Metric, "target-info-metric", defaultTargetInfoMetric, "Override the inventory metric (advanced; mirrors the plugin's metricName:targetInfo variable)")
	flags.StringVar(&o.ServiceGraph, "service-graph-metric", defaultServiceGraphMetric, "Override the service-graph metric used to find uninstrumented services (advanced)")
	flags.StringVar(&o.Instrumentation, "instrumentation", instrAll, "Which services to list: all, instrumented (target_info only), or uninstrumented (service-graph minus target_info)")
	flags.StringArrayVar(&o.Filters, "filter", nil, "Restrict to services matching a label matcher, e.g. --filter k8s_namespace_name=prod (repeatable; applies to target_info only)")
	flags.StringVar(&o.Language, "language", "", "Restrict to a single telemetry_sdk_language (e.g. go, java, nodejs)")
	flags.StringVar(&o.Env, "env", "", "Restrict to a single deployment_environment (e.g. production)")
	flags.StringSliceVar(&o.Columns, "columns", nil, "Extra target_info labels to surface as table columns (comma-separated)")
	flags.IntVar(&o.Limit, "limit", 0, "Limit the number of services returned (0 = unlimited; applied after sorting)")
	flags.BoolVar(&o.Count, "count", false, "Print a per-language summary instead of the full list")
}

func (o *listOpts) Validate() error {
	if err := o.IO.Validate(); err != nil {
		return err
	}
	if strings.TrimSpace(o.Metric) == "" {
		return errors.New("--target-info-metric must not be empty")
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

// wantsInstrumented controls whether to issue the target_info query.
// Even in --instrumentation uninstrumented mode we need this set so the
// service-graph results can be diffed against it.
func (o *listOpts) wantsInstrumented() bool {
	return true
}

func (o *listOpts) wantsServiceGraph() bool {
	return o.Instrumentation == instrAll || o.Instrumentation == instrUninstrumented
}

func (o *listOpts) buildFilters() ([]Matcher, error) {
	out := make([]Matcher, 0, len(o.Filters)+2)
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
	if o.Env != "" {
		out = append(out, Matcher{Label: "deployment_environment", Op: "=", Value: o.Env})
	}
	return out, nil
}

func newListCommand() *cobra.Command {
	opts := &listOpts{}
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List services discovered from target_info telemetry",
		Long: `List the services Grafana Cloud Application Observability has discovered from telemetry.

Discovery uses the same approach as the App Observability plugin: a group-by
query against the OTel target_info metric, projected onto (job, telemetry_sdk_language).
Each result row is one service.`,
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
			agent.AnnotationLLMHint:   `gcx appo11y services list -o json; gcx appo11y services list --count -o json; gcx appo11y services list --env production --language go -o json`,
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

		var instrumented, graph []Service
		eg, egCtx := errgroup.WithContext(ctx)
		if opts.wantsInstrumented() {
			eg.Go(func() error {
				expr, err := buildServicesQuery(opts.Metric, filters, opts.Columns)
				if err != nil {
					return fmt.Errorf("failed to build services discovery query: %w", err)
				}
				resp, err := client.Query(egCtx, datasourceUID, prometheus.QueryRequest{Query: expr})
				if err != nil {
					return fmt.Errorf("services discovery query failed: %w", err)
				}
				items, err := parseServicesResponse(resp)
				if err != nil {
					return fmt.Errorf("failed to parse services response: %w", err)
				}
				instrumented = items
				return nil
			})
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

		items := resolveItems(opts.Instrumentation, instrumented, graph)

		if opts.Limit > 0 && len(items) > opts.Limit {
			items = items[:opts.Limit]
		}

		if opts.Count {
			return opts.IO.Encode(cmd.OutOrStdout(), summarizeByLanguage(items))
		}
		return opts.IO.Encode(cmd.OutOrStdout(), &ServicesResponse{Items: items})
	}
}

// resolveItems applies the --instrumentation filter to the (instrumented, graph)
// result pair. The merge prefers target_info data, then drops or keeps rows
// based on the filter.
func resolveItems(filter string, instrumented, graph []Service) []Service {
	switch filter {
	case instrInstrumented:
		return instrumented
	case instrUninstrumented:
		idx := instrumentedIndex(instrumented)
		out := make([]Service, 0, len(graph))
		for _, s := range graph {
			if _, has := idx[instrumentedKey{namespace: s.Namespace, name: s.Name}]; has {
				continue
			}
			if _, has := idx[instrumentedKey{name: s.Name}]; has {
				continue
			}
			out = append(out, s)
		}
		return out
	default:
		return mergeServiceSets(instrumented, graph)
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

func upperHeaders(labels []string) []string {
	out := make([]string, len(labels))
	for i, l := range labels {
		out[i] = strings.ToUpper(strings.ReplaceAll(l, "_", " "))
	}
	return out
}
