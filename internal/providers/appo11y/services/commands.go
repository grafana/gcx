package services

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
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
}

func (o *listOpts) setup(flags *pflag.FlagSet) {
	o.IO.RegisterCustomCodec("table", &servicesTableCodec{})
	o.IO.RegisterCustomCodec("wide", &servicesTableCodec{Wide: true})
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)

	flags.StringVarP(&o.Datasource, "datasource", "d", "", "Prometheus datasource UID (defaults to datasources.prometheus in config or auto-discovery)")
	flags.StringVar(&o.Metric, "target-info-metric", defaultTargetInfoMetric, "Override the inventory metric (advanced; mirrors the plugin's metricName:targetInfo variable)")
	flags.StringArrayVar(&o.Filters, "filter", nil, "Restrict to services matching a label matcher, e.g. --filter k8s_namespace_name=prod (repeatable)")
	flags.StringVar(&o.Language, "language", "", "Restrict to a single telemetry_sdk_language (e.g. go, java, nodejs)")
}

func (o *listOpts) Validate() error {
	if err := o.IO.Validate(); err != nil {
		return err
	}
	if strings.TrimSpace(o.Metric) == "" {
		return errors.New("--target-info-metric must not be empty")
	}
	return nil
}

func (o *listOpts) buildFilters() ([]string, error) {
	out := make([]string, 0, len(o.Filters)+1)
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

  # Filter to Go services in a Kubernetes namespace
  gcx appo11y services list --language go --filter k8s_namespace_name=production

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
		expr := buildServicesQuery(opts.Metric, filters)

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

		return opts.IO.Encode(cmd.OutOrStdout(), &ServicesResponse{Items: items})
	}
}

// servicesTableCodec renders a ServicesResponse as a tabular view. Default
// columns: NAME, LANGUAGE. Wide adds the most useful resource-attribute
// labels surfaced in target_info.
type servicesTableCodec struct {
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
	resp, ok := v.(*ServicesResponse)
	if !ok {
		return fmt.Errorf("invalid data type for services table codec: %T", v)
	}

	if len(resp.Items) == 0 {
		_, err := fmt.Fprintln(w, "No services discovered. Verify your stack is receiving OTel target_info telemetry.")
		return err
	}

	if !c.Wide {
		t := style.NewTable("NAME", "LANGUAGE")
		for _, s := range resp.Items {
			t.Row(s.Name, fallback(s.Language, "-"))
		}
		return t.Render(w)
	}

	labels := wideLabels()
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
