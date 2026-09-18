package k6

import (
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	dsquery "github.com/grafana/gcx/internal/datasources/query"
	"github.com/grafana/gcx/internal/format"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/query/prometheus"
	"github.com/grafana/gcx/internal/style"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type testRunMetricsTableCodec struct{}

func (*testRunMetricsTableCodec) Format() format.Format { return "table" }

func (*testRunMetricsTableCodec) Encode(w io.Writer, data any) error {
	resp, ok := data.(*TestRunMetricsResponse)
	if !ok {
		return errors.New("invalid data type for test run metrics table codec")
	}
	if len(resp.Value) == 0 {
		_, err := fmt.Fprintln(w, "No metrics")
		return err
	}
	t := style.NewTable("ID", "NAME", "TYPE", "ORIGIN", "TEST RUN ID")
	for _, metric := range resp.Value {
		t.Row(metric.ID, metric.Name, metric.Type, metric.Origin, strconv.Itoa(metric.TestRunID))
	}
	return t.Render(w)
}

func (*testRunMetricsTableCodec) Decode(io.Reader, any) error {
	return errors.New("test run metrics table codec does not support decoding")
}

type loadTestMetricsTableCodec struct{}

func (*loadTestMetricsTableCodec) Format() format.Format { return "table" }

func (*loadTestMetricsTableCodec) Encode(w io.Writer, data any) error {
	resp, ok := data.(*LoadTestMetricsResponse)
	if !ok {
		return errors.New("invalid data type for load test metrics table codec")
	}
	if len(resp.Value) == 0 {
		_, err := fmt.Fprintln(w, "No metrics")
		return err
	}
	t := style.NewTable("NAME", "TYPE", "LABELS")
	for _, metric := range resp.Value {
		t.Row(metric.Name, metric.Type, strings.Join(metric.Labels, ","))
	}
	return t.Render(w)
}

func (*loadTestMetricsTableCodec) Decode(io.Reader, any) error {
	return errors.New("load test metrics table codec does not support decoding")
}

type seriesTableCodec struct{}

func (*seriesTableCodec) Format() format.Format { return "table" }

func (*seriesTableCodec) Encode(w io.Writer, data any) error {
	resp, ok := data.(*prometheus.SeriesResponse)
	if !ok {
		return errors.New("invalid data type for k6 series table codec")
	}
	return prometheus.FormatSeriesTable(w, resp)
}

func (*seriesTableCodec) Decode(io.Reader, any) error {
	return errors.New("k6 series table codec does not support decoding")
}

type labelsTableCodec struct {
	values *bool
}

func (*labelsTableCodec) Format() format.Format { return "table" }

func (c *labelsTableCodec) Encode(w io.Writer, data any) error {
	resp, ok := data.(*prometheus.LabelsResponse)
	if !ok {
		return errors.New("invalid data type for k6 labels table codec")
	}
	header := "LABEL"
	if c.values != nil && *c.values {
		header = "VALUE"
	}
	codec := &prometheus.SingleColumnTableCodec{
		Header: header,
		Rows: func(value any) ([]string, bool) {
			labels, valid := value.(*prometheus.LabelsResponse)
			if !valid {
				return nil, false
			}
			return labels.Data, true
		},
	}
	return codec.Encode(w, resp)
}

func (*labelsTableCodec) Decode(io.Reader, any) error {
	return errors.New("k6 labels table codec does not support decoding")
}

type listTestRunMetricsOpts struct {
	IO cmdio.Options
}

func (o *listTestRunMetricsOpts) setup(flags *pflag.FlagSet) {
	o.IO.RegisterCustomCodec("table", &testRunMetricsTableCodec{})
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)
}

func (o *listTestRunMetricsOpts) Validate() error { return o.IO.Validate() }

func newRunsListMetricsCommand(loader CloudConfigLoader) *cobra.Command {
	opts := &listTestRunMetricsOpts{}
	cmd := &cobra.Command{
		Use:     "list-metrics <run-id>",
		Aliases: []string{"metrics"},
		Short:   "List metric metadata for a k6 test run.",
		Example: "  gcx k6 runs list-metrics 12345\n" +
			"  gcx k6 runs list-metrics 12345 -o json",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}
			runID, err := parsePositiveID(args[0], "run")
			if err != nil {
				return err
			}
			client, _, err := authenticatedClient(cmd.Context(), loader)
			if err != nil {
				return err
			}
			resp, err := client.ListTestRunMetrics(cmd.Context(), runID)
			if err != nil {
				return err
			}
			return opts.IO.Encode(cmd.OutOrStdout(), resp)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

type listLoadTestMetricsOpts struct {
	IO          cmdio.Options
	RunCount    int
	RunIDs      []int
	RunCountSet bool
}

func (o *listLoadTestMetricsOpts) setup(flags *pflag.FlagSet) {
	o.IO.RegisterCustomCodec("table", &loadTestMetricsTableCodec{})
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)
	flags.IntVar(&o.RunCount, "run-count", 0, "Use the last N test runs (default: the server uses the last 30 runs)")
	flags.IntSliceVar(&o.RunIDs, "run-id", nil, "Use specific test run IDs (repeatable or comma-separated)")
}

func (o *listLoadTestMetricsOpts) Validate() error {
	if err := o.IO.Validate(); err != nil {
		return err
	}
	if o.RunCountSet && o.RunCount <= 0 {
		return fmt.Errorf("invalid --run-count %d: must be greater than 0", o.RunCount)
	}
	return (MetricsRunSelection{RunCount: o.RunCount, RunIDs: o.RunIDs}).validate(false)
}

func newTestsListMetricsCommand(loader CloudConfigLoader) *cobra.Command {
	opts := &listLoadTestMetricsOpts{}
	cmd := &cobra.Command{
		Use:     "list-metrics <load-test-id>",
		Aliases: []string{"metrics"},
		Short:   "List metric metadata across k6 test runs.",
		Long:    "List metric metadata across selected runs of one k6 load test. Selectors are mutually exclusive. If neither selector is set, the server uses the last 30 runs.",
		Example: "  gcx k6 load-tests list-metrics 12345\n" +
			"  gcx k6 load-tests list-metrics 12345 --run-count 5\n" +
			"  gcx k6 load-tests list-metrics 12345 --run-id 1001,1002 -o json",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.RunCountSet = cmd.Flags().Changed("run-count")
			if err := opts.Validate(); err != nil {
				return err
			}
			loadTestID, err := parsePositiveID(args[0], "load test")
			if err != nil {
				return err
			}
			client, _, err := authenticatedClient(cmd.Context(), loader)
			if err != nil {
				return err
			}
			resp, err := client.ListLoadTestMetrics(cmd.Context(), loadTestID, MetricsRunSelection{
				RunCount: opts.RunCount,
				RunIDs:   opts.RunIDs,
			})
			if err != nil {
				return err
			}
			return opts.IO.Encode(cmd.OutOrStdout(), resp)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

type listTestRunSeriesOpts struct {
	IO      cmdio.Options
	Matches []string
}

func (o *listTestRunSeriesOpts) setup(flags *pflag.FlagSet) {
	o.IO.RegisterCustomCodec("table", &seriesTableCodec{})
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)
	flags.StringArrayVar(&o.Matches, "match", nil, "Additional series selectors (repeatable; selectors combine with OR)")
}

func (o *listTestRunSeriesOpts) Validate(selectors []string) error {
	if err := o.IO.Validate(); err != nil {
		return err
	}
	if len(selectors) == 0 {
		return errors.New("at least one series selector is required")
	}
	for _, selector := range selectors {
		selector = strings.TrimSpace(selector)
		if selector == "" {
			return errors.New("series selector must not be empty")
		}
	}
	return nil
}

func newRunsListSeriesCommand(loader CloudConfigLoader) *cobra.Command {
	opts := &listTestRunSeriesOpts{}
	cmd := &cobra.Command{
		Use:     "list-series <run-id> [SELECTOR]",
		Aliases: []string{"series"},
		Short:   "List metric series for a k6 test run.",
		Long:    "List metric series for one k6 test run. A positional selector and repeated --match selectors combine with OR.",
		Example: "  gcx k6 runs list-series 12345 http_reqs\n" +
			"  gcx k6 runs list-series 12345 --match 'http_reqs{scenario=\"api\"}' --match 'http_reqs{scenario=\"browser\"}'",
		Args: cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			selectors := append([]string(nil), opts.Matches...)
			if len(args) == 2 {
				selectors = append(selectors, args[1])
			}
			if err := opts.Validate(selectors); err != nil {
				return err
			}
			runID, err := parsePositiveID(args[0], "run")
			if err != nil {
				return err
			}
			client, _, err := authenticatedClient(cmd.Context(), loader)
			if err != nil {
				return err
			}
			resp, err := client.ListTestRunSeries(cmd.Context(), runID, selectors)
			if err != nil {
				return err
			}
			return opts.IO.Encode(cmd.OutOrStdout(), resp)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

type listTestRunLabelsOpts struct {
	IO       cmdio.Options
	Label    string
	Matches  []string
	LabelSet bool
}

func (o *listTestRunLabelsOpts) setup(flags *pflag.FlagSet) {
	o.IO.RegisterCustomCodec("table", &labelsTableCodec{values: &o.LabelSet})
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)
	flags.StringVar(&o.Label, "label", "", "List values for this label instead of label names")
	flags.StringArrayVar(&o.Matches, "match", nil, "Limit labels to matching series (repeatable; selectors combine with OR)")
}

func (o *listTestRunLabelsOpts) Validate() error {
	if err := o.IO.Validate(); err != nil {
		return err
	}
	if o.LabelSet && strings.TrimSpace(o.Label) == "" {
		return errors.New("--label must not be empty when it is set")
	}
	for _, selector := range o.Matches {
		selector = strings.TrimSpace(selector)
		if selector == "" {
			return errors.New("--match selector must not be empty")
		}
	}
	return nil
}

func newRunsListLabelsCommand(loader CloudConfigLoader) *cobra.Command {
	opts := &listTestRunLabelsOpts{}
	cmd := &cobra.Command{
		Use:     "list-labels <run-id>",
		Aliases: []string{"labels"},
		Short:   "List metric label names or values for a k6 test run.",
		Long:    "List metric label names for one k6 test run. Set --label to list values. Repeated --match selectors combine with OR.",
		Example: "  gcx k6 runs list-labels 12345\n" +
			"  gcx k6 runs list-labels 12345 --label scenario\n" +
			"  gcx k6 runs list-labels 12345 --match '{__name__=\"http_reqs\"}' -o json",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.LabelSet = cmd.Flags().Changed("label")
			if err := opts.Validate(); err != nil {
				return err
			}
			runID, err := parsePositiveID(args[0], "run")
			if err != nil {
				return err
			}
			client, _, err := authenticatedClient(cmd.Context(), loader)
			if err != nil {
				return err
			}
			var resp *prometheus.LabelsResponse
			if opts.LabelSet {
				resp, err = client.ListTestRunLabelValues(cmd.Context(), runID, opts.Label, opts.Matches)
			} else {
				resp, err = client.ListTestRunLabels(cmd.Context(), runID, opts.Matches)
			}
			if err != nil {
				return err
			}
			return opts.IO.Encode(cmd.OutOrStdout(), resp)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

type testRunMetricsQueryOpts struct {
	Shared    dsquery.SharedOpts
	Metric    string
	Aggregate bool
	ExprSet   bool
}

func (o *testRunMetricsQueryOpts) setup(flags *pflag.FlagSet) {
	o.Shared.Setup(flags, false)
	flags.StringVar(&o.Metric, "metric", "", "Metric name with optional label selectors (required)")
	flags.BoolVar(&o.Aggregate, "aggregate", false, "Return one value for the selected run duration")
}

func (o *testRunMetricsQueryOpts) Validate() error {
	if err := o.Shared.Validate(); err != nil {
		return err
	}
	if strings.TrimSpace(o.Metric) == "" {
		return errors.New("--metric is required and must not be empty")
	}
	if o.ExprSet && strings.TrimSpace(o.Shared.Expr) == "" {
		return errors.New("--expr must not be empty when it is set")
	}
	if o.Aggregate && o.Shared.Step != "" {
		return errors.New("--step is not supported with --aggregate")
	}
	return nil
}

func (o *testRunMetricsQueryOpts) resolveExpression(args []string) (string, error) {
	haveArg := len(args) == 2
	if o.ExprSet && haveArg {
		return "", errors.New("provide the expression as a positional argument or via --expr, not both")
	}
	if !o.ExprSet && !haveArg {
		return "", errors.New("expression is required: provide it as a positional argument or via --expr")
	}
	expression := o.Shared.Expr
	if haveArg {
		expression = args[1]
	}
	if strings.TrimSpace(expression) == "" {
		return "", errors.New("metric query expression must not be empty")
	}
	return expression, nil
}

func newRunsMetricsQueryCommand(loader CloudConfigLoader) *cobra.Command {
	opts := &testRunMetricsQueryOpts{}
	cmd := &cobra.Command{
		Use:   "query <run-id> [EXPR]",
		Short: "Query metric values for a k6 test run.",
		Long:  "Query metric values for one k6 test run. The default is a range query over the run duration. Set --aggregate to return one value per series.",
		Example: "  gcx k6 runs query 12345 'histogram_avg' --metric http_req_duration\n" +
			"  gcx k6 runs query 12345 'histogram_quantile(0.95)' --metric 'http_req_duration{scenario=\"api\"}' --aggregate\n" +
			"  gcx k6 runs query 12345 'rate' --metric http_reqs --since 15m --step 30s -o wide",
		Args: cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.ExprSet = cmd.Flags().Changed("expr")
			if err := opts.Validate(); err != nil {
				return err
			}
			expression, err := opts.resolveExpression(args)
			if err != nil {
				return err
			}
			runID, err := parsePositiveID(args[0], "run")
			if err != nil {
				return err
			}
			start, end, step, err := opts.Shared.ParseTimes(time.Now())
			if err != nil {
				return err
			}
			request := MetricsQueryRequest{
				Expression: expression,
				Metric:     opts.Metric,
				Aggregate:  opts.Aggregate,
				Start:      start,
				End:        end,
				Step:       step,
			}
			if err := request.validate(); err != nil {
				return err
			}
			client, _, err := authenticatedClient(cmd.Context(), loader)
			if err != nil {
				return err
			}
			resp, err := client.QueryTestRunMetrics(cmd.Context(), runID, request)
			if err != nil {
				return err
			}
			return opts.Shared.IO.Encode(cmd.OutOrStdout(), resp)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

type loadTestMetricsQueryOpts struct {
	IO          cmdio.Options
	Expr        string
	Metric      string
	RunCount    int
	RunIDs      []int
	RunCountSet bool
	ExprSet     bool
}

func (o *loadTestMetricsQueryOpts) setup(flags *pflag.FlagSet) {
	dsquery.RegisterCodecs(&o.IO, false)
	o.IO.BindFlags(flags)
	flags.StringVar(&o.Expr, "expr", "", "Query expression (alternative to positional argument)")
	flags.StringVar(&o.Metric, "metric", "", "Metric name with optional label selectors (required)")
	flags.IntVar(&o.RunCount, "run-count", 0, "Query the last N test runs")
	flags.IntSliceVar(&o.RunIDs, "run-id", nil, "Query specific test run IDs (repeatable or comma-separated)")
}

func (o *loadTestMetricsQueryOpts) resolveExpression(args []string) (string, error) {
	haveArg := len(args) == 2
	if o.ExprSet && haveArg {
		return "", errors.New("provide the expression as a positional argument or via --expr, not both")
	}
	if !o.ExprSet && !haveArg {
		return "", errors.New("expression is required: provide it as a positional argument or via --expr")
	}
	expression := o.Expr
	if haveArg {
		expression = args[1]
	}
	if strings.TrimSpace(expression) == "" {
		return "", errors.New("metric query expression must not be empty")
	}
	return expression, nil
}

func (o *loadTestMetricsQueryOpts) Validate() error {
	if err := o.IO.Validate(); err != nil {
		return err
	}
	if strings.TrimSpace(o.Metric) == "" {
		return errors.New("--metric is required and must not be empty")
	}
	if o.RunCountSet && o.RunCount <= 0 {
		return fmt.Errorf("invalid --run-count %d: must be greater than 0", o.RunCount)
	}
	return (MetricsRunSelection{RunCount: o.RunCount, RunIDs: o.RunIDs}).validate(true)
}

func newTestsMetricsQueryCommand(loader CloudConfigLoader) *cobra.Command {
	opts := &loadTestMetricsQueryOpts{}
	cmd := &cobra.Command{
		Use:   "query <load-test-id> [EXPR]",
		Short: "Query aggregate metric values across k6 test runs.",
		Long:  "Run one aggregate metric query across selected runs of a k6 load test. Select the last N runs or explicit run IDs.",
		Example: "  gcx k6 load-tests query 12345 'histogram_quantile(0.95)' --metric http_req_duration --run-count 5\n" +
			"  gcx k6 load-tests query 12345 'sum' --metric http_reqs --run-id 1001,1002 -o json",
		Args: cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.RunCountSet = cmd.Flags().Changed("run-count")
			opts.ExprSet = cmd.Flags().Changed("expr")
			if err := opts.Validate(); err != nil {
				return err
			}
			expression, err := opts.resolveExpression(args)
			if err != nil {
				return err
			}
			loadTestID, err := parsePositiveID(args[0], "load test")
			if err != nil {
				return err
			}
			request := LoadTestMetricsQueryRequest{
				Expression: expression,
				Metric:     opts.Metric,
				Selection: MetricsRunSelection{
					RunCount: opts.RunCount,
					RunIDs:   opts.RunIDs,
				},
			}
			client, _, err := authenticatedClient(cmd.Context(), loader)
			if err != nil {
				return err
			}
			resp, err := client.QueryLoadTestMetrics(cmd.Context(), loadTestID, request)
			if err != nil {
				return err
			}
			return opts.IO.Encode(cmd.OutOrStdout(), resp)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}
