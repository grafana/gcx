package services

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/grafana/gcx/cmd/gcx/fail"
	"github.com/grafana/gcx/internal/agent"
	dsquery "github.com/grafana/gcx/internal/datasources/query"
	"github.com/grafana/gcx/internal/format"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/query/prometheus"
	"github.com/grafana/gcx/internal/style"
	"github.com/prometheus/common/model"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// labelsDefaultWindow is wider than the RED default (5m): label discovery
// wants to catch every value that appeared recently, not just the current
// instant, so a value that only shows up intermittently still surfaces.
const labelsDefaultWindow = "1h"

// labelsSampleSize caps how many values the default (all-labels) view
// prints per label; the full count is still reported so users know when
// there's more. Drill into one label with --label to see them all.
const labelsSampleSize = 5

type labelsOpts struct {
	IO          cmdio.Options
	Datasource  string
	Since       string
	Namespace   string
	MetricsMode string
	Label       string
	Filters     []string
}

func (o *labelsOpts) setup(flags *pflag.FlagSet) {
	o.IO.RegisterCustomCodec("table", &labelsTableCodec{opts: o})
	o.IO.RegisterCustomCodec("wide", &labelsTableCodec{opts: o, Wide: true})
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)

	flags.StringVarP(&o.Datasource, "datasource", "d", "", "Prometheus datasource UID (defaults to datasources.prometheus in config or auto-discovery)")
	flags.StringVarP(&o.Namespace, "namespace", "n", "", "Service namespace (only needed when the argument is the bare service name and multiple namespaces are in play)")
	flags.StringVar(&o.Since, "since", labelsDefaultWindow, "Lookback window for series discovery (e.g. 15m, 1h, 1d) — PromQL duration syntax")
	flags.StringVar(&o.MetricsMode, "metrics-mode", metricsModeAuto, "Span-metrics family whose labels to inspect. One of: auto (probes the stack), v3, tempo, or otel")
	flags.StringVar(&o.Label, "label", "", "Show the distinct values of a single label instead of the label summary (e.g. --label k8s_cluster_name)")
	flags.StringArrayVar(&o.Filters, "filter", nil, "Restrict discovery to series matching a label matcher, e.g. --filter k8s_cluster_name=prod-us (repeatable)")
}

func (o *labelsOpts) Validate(cmd *cobra.Command) error {
	if err := o.IO.Validate(); err != nil {
		return err
	}
	if strings.TrimSpace(o.Since) == "" {
		return fail.NewCommandUsageError(cmd, "--since must not be empty", nil)
	}
	if _, err := model.ParseDuration(o.Since); err != nil {
		return fail.NewCommandUsageError(cmd, fmt.Sprintf("--since %q is not a valid PromQL duration", o.Since), err)
	}
	if _, _, err := resolveMetricsMode(o.MetricsMode); err != nil {
		return fail.NewCommandUsageError(cmd, "", err)
	}
	return nil
}

func newListLabelsCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &labelsOpts{}
	cmd := &cobra.Command{
		Use:   "list-labels <service> [--namespace ns]",
		Short: "Discover the labels (and values) available to --filter and --group-by for a service.",
		Long: `List the labels present on a service's span-metric series — the exact set
that "gcx appo11y services get/list-operations --filter/--group-by" can
operate on — with each label's distinct-value count.

This answers "what can I break this service down by?" without guessing.
Pass --label <name> to list the distinct values of a single label so you
know what to feed --filter (e.g. --filter k8s_cluster_name=<value>).

Labels come from the span-metric calls series (auto-detected metrics-mode),
which is what the RED commands filter and group on. Note:

  - "services map" groups on the Tempo service-graph metric family, whose
    labels may differ (and often omit cluster labels).
  - "services get <svc> -o json" additionally surfaces the target_info
    resource attributes under .service.labels.`,
		Example: `
  # What can I filter/group checkoutservice by?
  gcx appo11y services list-labels checkoutservice

  # Which clusters does it run in? (values to feed --filter/--group-by)
  gcx appo11y services list-labels checkoutservice --label k8s_cluster_name

  # JSON for scripting
  gcx appo11y services list-labels checkoutservice -o json`,
		Args: cobra.ExactArgs(1),
		RunE: runLabels(loader, opts),
		Annotations: map[string]string{
			agent.AnnotationTokenCost: "small",
			agent.AnnotationLLMHint:   `Discovery helper for 'gcx appo11y services' --filter/--group-by: lists the labels present on a service's span-metric series with each label's distinct-value count (cardinality). Use before --filter/--group-by to learn what dimensions exist. --label <name> lists that label's distinct values (the valid --filter values). Sourced from the span-metric calls series (what get/list-operations filter and group on); map uses the service-graph family whose labels may differ. Examples: gcx appo11y services list-labels <name> -o json; gcx appo11y services list-labels <name> --label k8s_cluster_name -o json`,
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

func runLabels(loader *providers.ConfigLoader, opts *labelsOpts) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		if err := opts.Validate(cmd); err != nil {
			return err
		}
		namespace, name, err := parseServiceArg(args[0], opts.Namespace)
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
		window, err := model.ParseDuration(opts.Since)
		if err != nil {
			return fail.NewCommandUsageError(cmd, fmt.Sprintf("--since %q is not a valid PromQL duration", opts.Since), err)
		}

		ctx := cmd.Context()

		cfgCtx, cfg, err := dsquery.LoadContextAndConfig(ctx, loader)
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

		// Bare-name resolution: same UX as `services get`.
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
		names, ok := metricNamesByMode(mode)
		if !ok {
			return fmt.Errorf("unknown metrics mode %q", mode)
		}

		response, err := fetchServiceLabels(ctx, client, datasourceUID, names.calls, namespace, name, opts.Since, time.Duration(window), matchers)
		if err != nil {
			return err
		}

		// --label: keep only the requested label (full value set).
		if opts.Label != "" {
			response.Items = filterToLabel(response.Items, opts.Label)
		}

		notFound := len(response.Items) == 0
		if notFound {
			emitLabelsNoDataHint(cmd.ErrOrStderr(), namespace, name, opts.Label)
		}
		if err := opts.IO.Encode(cmd.OutOrStdout(), response); err != nil {
			return err
		}
		if notFound {
			if opts.Label != "" {
				return notFoundEmitted(cmd.ErrOrStderr(),
					fmt.Sprintf("label %q not found on %q in the requested window", opts.Label, jobLabel(namespace, name)))
			}
			return notFoundEmitted(cmd.ErrOrStderr(),
				fmt.Sprintf("no labels found for %q in the requested window", jobLabel(namespace, name)))
		}
		return nil
	}
}

// fetchServiceLabels runs a /series query for the service's calls metric
// and folds the result into a ServiceLabelsResponse. sampleN caps the
// per-label value sample in the summary view; the drill-down (--label)
// re-reads the full set from the same response.
func fetchServiceLabels(ctx context.Context, client *prometheus.Client, datasourceUID, metric, namespace, name, windowStr string, window time.Duration, matchers []Matcher) (*ServiceLabelsResponse, error) {
	selector := buildSeriesSelector(metric, jobLabel(namespace, name), matchers)
	end := time.Now()
	start := end.Add(-window)

	resp, err := client.Series(ctx, datasourceUID, []string{selector}, start, end)
	if err != nil {
		return nil, fmt.Errorf("series query failed: %w", err)
	}

	index := collectSeriesLabels(resp.Data)
	return &ServiceLabelsResponse{
		Service: Service{Name: name, Namespace: namespace},
		Metric:  metric,
		Window:  windowStr,
		Items:   summarizeLabels(index, labelsSampleSize),
	}, nil
}

// filterToLabel narrows a summary to the single requested label and drops
// the sample cap so all its values render. Returns an empty slice when the
// label isn't present (caller treats that as not-found).
func filterToLabel(items []LabelSummary, label string) []LabelSummary {
	for _, it := range items {
		if it.Name == label {
			return []LabelSummary{it}
		}
	}
	return nil
}

func emitLabelsNoDataHint(stderr io.Writer, namespace, name, label string) {
	svc := name
	if namespace != "" {
		svc = namespace + "/" + name
	}
	if label != "" {
		cmdio.EmitHint(stderr,
			fmt.Sprintf("label %q not found on %q", label, svc),
			"gcx appo11y services list-labels "+svc)
		return
	}
	cmdio.EmitHint(stderr,
		fmt.Sprintf("no span-metric series found for %q in the window", svc),
		"gcx appo11y services list")
}

// labelsTableCodec renders either the label summary (LABEL / CARDINALITY /
// SAMPLE VALUES) or, when --label is set, a single-column value list.
type labelsTableCodec struct {
	opts *labelsOpts
	Wide bool
}

func (c *labelsTableCodec) Format() format.Format {
	if c.Wide {
		return "wide"
	}
	return "table"
}

func (c *labelsTableCodec) Decode(io.Reader, any) error {
	return errors.New("services list-labels table codec does not support decoding")
}

func (c *labelsTableCodec) Encode(w io.Writer, v any) error {
	resp, ok := v.(*ServiceLabelsResponse)
	if !ok {
		return fmt.Errorf("invalid data type for services list-labels table codec: %T", v)
	}

	// --label drill-down: one column of values for the requested label.
	if c.opts != nil && c.opts.Label != "" {
		if len(resp.Items) == 0 {
			_, err := fmt.Fprintf(w, "Label %q not found on %q in the requested window.\n", c.opts.Label, jobLabel(resp.Service.Namespace, resp.Service.Name))
			return err
		}
		t := style.NewTable(strings.ToUpper(c.opts.Label))
		for _, v := range resp.Items[0].Values {
			t.Row(v)
		}
		return t.Render(w)
	}

	if len(resp.Items) == 0 {
		_, err := fmt.Fprintf(w, "No labels found for %q in the requested window. Verify the service emits span metrics.\n", jobLabel(resp.Service.Namespace, resp.Service.Name))
		return err
	}

	t := style.NewTable("LABEL", "CARDINALITY", "SAMPLE VALUES")
	for _, it := range resp.Items {
		t.Row(it.Name, strconv.Itoa(it.Cardinality), sampleValues(it))
	}
	return t.Render(w)
}

// sampleValues renders the sample column: the carried values joined, with
// a trailing "… (+N more)" when the label has more distinct values than
// the sample holds.
func sampleValues(it LabelSummary) string {
	if len(it.Values) == 0 {
		return "-"
	}
	joined := strings.Join(it.Values, ", ")
	if it.Cardinality > len(it.Values) {
		joined += fmt.Sprintf(", … (+%d more)", it.Cardinality-len(it.Values))
	}
	return joined
}
