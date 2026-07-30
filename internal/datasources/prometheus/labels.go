package prometheus

import (
	"errors"
	"fmt"
	"strings"

	"github.com/grafana/gcx/internal/agent"
	dsquery "github.com/grafana/gcx/internal/datasources/query"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/query/prometheus"
	"github.com/prometheus/common/model"
	promlabels "github.com/prometheus/prometheus/model/labels"
	promparser "github.com/prometheus/prometheus/promql/parser"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type labelsOpts struct {
	IO         cmdio.Options
	Datasource string
	Label      string
	Metric     string
	Match      []string
}

func (opts *labelsOpts) setup(flags *pflag.FlagSet) {
	opts.IO.RegisterCustomCodec("table", &prometheus.SingleColumnTableCodec{
		Header: "LABEL",
		Rows: func(data any) ([]string, bool) {
			resp, ok := data.(*prometheus.LabelsResponse)
			if !ok {
				return nil, false
			}
			return resp.Data, true
		},
	})
	opts.IO.DefaultFormat("table")
	opts.IO.BindFlags(flags)

	flags.StringVarP(&opts.Datasource, "datasource", "d", "", "Datasource UID (required unless datasources.prometheus is configured)")
	flags.StringVarP(&opts.Label, "label", "l", "", "Get values for this label (omit to list all labels)")
	flags.StringVar(&opts.Metric, "metric", "", "Only results from series of this metric (narrows every --match selector)")
	flags.StringArrayVar(&opts.Match, "match", nil, "Series selector(s) to scope results; repeatable (repeated selectors combine as a union, per the Prometheus match[] API)")
}

// selectors returns the match[] selectors to send. When --metric is set it is
// folded into every --match selector as a __name__ matcher, so --metric always
// narrows. Repeated --match selectors remain a union: the Prometheus API
// returns results from series matching any match[] parameter.
func (opts *labelsOpts) selectors() ([]string, error) {
	nameMatcher := promlabels.MustNewMatcher(promlabels.MatchEqual, model.MetricNameLabel, opts.Metric)

	if len(opts.Match) == 0 {
		if opts.Metric == "" {
			return nil, nil
		}
		return []string{"{" + nameMatcher.String() + "}"}, nil
	}

	parser := promparser.NewParser(promparser.Options{})

	if opts.Metric == "" {
		// Validate client-side so a selector typo fails here with a clear
		// error instead of an opaque proxied 400; valid selectors are sent
		// exactly as written.
		for _, sel := range opts.Match {
			if _, err := parser.ParseMetricSelector(sel); err != nil {
				return nil, fmt.Errorf("invalid --match selector %q: %w", sel, err)
			}
		}
		return opts.Match, nil
	}

	folded := make([]string, 0, len(opts.Match))
	for _, sel := range opts.Match {
		matchers, err := parser.ParseMetricSelector(sel)
		if err != nil {
			return nil, fmt.Errorf("invalid --match selector %q: %w", sel, err)
		}

		// A selector may already constrain __name__ (a bare metric name or an
		// explicit matcher). Consistent constraints (same metric, regex
		// superset) fold fine; a constraint the metric cannot satisfy would
		// silently match nothing, so reject it instead.
		redundant := false
		for _, m := range matchers {
			if m.Name != model.MetricNameLabel {
				continue
			}
			if !m.Matches(opts.Metric) {
				return nil, fmt.Errorf("--metric %q contradicts the __name__ matcher in --match selector %q: the intersection matches nothing", opts.Metric, sel)
			}
			if m.Type == promlabels.MatchEqual {
				redundant = true
			}
		}

		// Brace-joining is safe despite Pattern 14's no-string-formatting rule:
		// every part is a canonical Matcher.String() rendering, and
		// promql-builder cannot express a matcher-only selector.
		parts := make([]string, 0, len(matchers)+1)
		for _, m := range matchers {
			parts = append(parts, m.String())
		}
		if !redundant {
			parts = append(parts, nameMatcher.String())
		}

		folded = append(folded, "{"+strings.Join(parts, ",")+"}")
	}
	return folded, nil
}

func (opts *labelsOpts) Validate() error {
	return opts.IO.Validate()
}

func LabelsCmd(loader *providers.ConfigLoader) *cobra.Command {
	return LabelsCmdWithDefault(loader, "")
}

// LabelsCmdWithDefault returns the labels command with a fallback datasource
// UID used when --datasource is not provided. Pass "" for no default.
func LabelsCmdWithDefault(loader *providers.ConfigLoader, defaultDS string) *cobra.Command {
	opts := &labelsOpts{}

	cmd := &cobra.Command{
		Use:   "labels",
		Short: "List labels or label values",
		Args:  cobra.NoArgs,
		Long:  "List all labels or get values for a specific label from a Prometheus datasource.",
		Example: `
	# List all labels (use datasource UID, not name)
	gcx datasources prometheus labels -d UID

	# Get values for a specific label
	gcx datasources prometheus labels -d UID --label job

	# List labels present on a metric
	gcx datasources prometheus labels -d UID --metric http_requests_total

	# Get values a label takes on a metric
	gcx datasources prometheus labels -d UID --metric http_requests_total --label job

	# Scope with an arbitrary series selector
	gcx datasources prometheus labels -d UID --match '{job="api"}'

	# Output as JSON
	gcx datasources prometheus labels -d UID -o json`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}

			// An explicitly empty --metric (typically an unset shell variable,
			// as in --metric "$METRIC") would silently drop scoping; error
			// instead of returning unscoped results.
			if cmd.Flags().Changed("metric") && opts.Metric == "" {
				return errors.New(`invalid --metric: value is empty (unset shell variable?)`)
			}

			selectors, err := opts.selectors()
			if err != nil {
				return err
			}

			ctx := cmd.Context()

			cfgCtx, cfg, err := dsquery.LoadContextAndConfig(ctx, loader)
			if err != nil {
				return err
			}

			effectiveDS := opts.Datasource
			if effectiveDS == "" {
				effectiveDS = defaultDS
			}

			datasourceUID, err := dsquery.ResolveAndSaveDatasource(ctx, loader, effectiveDS, cfgCtx, cfg, "prometheus")
			if err != nil {
				return err
			}

			client, err := prometheus.NewClient(cfg)
			if err != nil {
				return fmt.Errorf("failed to create client: %w", err)
			}

			if opts.Label != "" {
				resp, err := client.LabelValues(ctx, datasourceUID, opts.Label, selectors)
				if err != nil {
					return fmt.Errorf("failed to get label values: %w", err)
				}

				return opts.IO.Encode(cmd.OutOrStdout(), resp)
			}

			resp, err := client.Labels(ctx, datasourceUID, selectors)
			if err != nil {
				return fmt.Errorf("failed to get labels: %w", err)
			}

			return opts.IO.Encode(cmd.OutOrStdout(), resp)
		},
	}

	cmd.Annotations = map[string]string{
		agent.AnnotationTokenCost: "small",
		agent.AnnotationLLMHint:   "gcx datasources prometheus labels -d UID -o json",
	}

	opts.setup(cmd.Flags())

	return cmd
}
