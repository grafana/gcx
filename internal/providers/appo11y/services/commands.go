package services

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strconv"
	"strings"

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

type listOpts struct {
	IO         cmdio.Options
	Datasource string
	Metric     string
	Filters    []string
	Language   string
	Env        string
	Columns    []string
	Limit      int
	Count      bool
}

func (o *listOpts) setup(flags *pflag.FlagSet) {
	o.IO.RegisterCustomCodec("table", &servicesTableCodec{opts: o})
	o.IO.RegisterCustomCodec("wide", &servicesTableCodec{opts: o, Wide: true})
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)

	flags.StringVarP(&o.Datasource, "datasource", "d", "", "Prometheus datasource UID (defaults to datasources.prometheus in config or auto-discovery)")
	flags.StringVar(&o.Metric, "target-info-metric", defaultTargetInfoMetric, "Override the inventory metric (advanced; mirrors the plugin's metricName:targetInfo variable)")
	flags.StringArrayVar(&o.Filters, "filter", nil, "Restrict to services matching a label matcher, e.g. --filter k8s_namespace_name=prod (repeatable)")
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
	return nil
}

func (o *listOpts) buildFilters() ([]string, error) {
	out := make([]string, 0, len(o.Filters)+2)
	for _, f := range o.Filters {
		parsed, err := parseFilter(f)
		if err != nil {
			return nil, err
		}
		out = append(out, parsed)
	}
	if o.Language != "" {
		parsed, err := parseFilter("telemetry_sdk_language=" + o.Language)
		if err != nil {
			return nil, err
		}
		out = append(out, parsed)
	}
	if o.Env != "" {
		parsed, err := parseFilter("deployment_environment=" + o.Env)
		if err != nil {
			return nil, err
		}
		out = append(out, parsed)
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
		expr := buildServicesQuery(opts.Metric, filters, opts.Columns)

		client, err := prometheus.NewClient(cfg)
		if err != nil {
			return fmt.Errorf("failed to create prometheus client: %w", err)
		}

		resp, err := client.Query(ctx, datasourceUID, prometheus.QueryRequest{Query: expr})
		if err != nil {
			return fmt.Errorf("services discovery query failed: %w", err)
		}

		items, err := parseServicesResponse(resp)
		if err != nil {
			return fmt.Errorf("failed to parse services response: %w", err)
		}

		if opts.Limit > 0 && len(items) > opts.Limit {
			items = items[:opts.Limit]
		}

		if opts.Count {
			return opts.IO.Encode(cmd.OutOrStdout(), summarizeByLanguage(items))
		}
		return opts.IO.Encode(cmd.OutOrStdout(), &ServicesResponse{Items: items})
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

// wideLabels are the resource-attribute labels we surface in --output wide,
// in display order. Picked from the plugin's RELEVANT_METADATA_PREFIXES set.
func wideLabels() []string {
	return []string{
		"service_namespace",
		"k8s_namespace_name",
		"k8s_cluster_name",
		"cloud_region",
	}
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
	if !c.Wide && len(extra) == 0 {
		t := style.NewTable("NAME", "LANGUAGE")
		for _, s := range resp.Items {
			t.Row(s.Name, fallback(s.Language, "-"))
		}
		return t.Render(w)
	}

	labels := extra
	if c.Wide {
		labels = append(wideLabels(), extra...)
	}
	headers := append([]string{"NAME", "LANGUAGE"}, upperHeaders(labels)...)
	t := style.NewTable(headers...)
	for _, s := range resp.Items {
		row := []string{s.Name, fallback(s.Language, "-")}
		for _, lbl := range labels {
			row = append(row, fallback(s.Labels[lbl], "-"))
		}
		t.Row(row...)
	}
	return t.Render(w)
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

func fallback(v, def string) string {
	if v == "" {
		return def
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
