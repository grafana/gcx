// Package namedquery exposes the Synthetic Monitoring backend's named queries.
//
// The command is deliberately generic -- any query name, arbitrary key=value
// parameters -- because the registry it talks to is still growing. Once the set
// of queries the CLI actually needs settles, some of these may also gain typed
// flags on the existing read commands (`checks status`), but this generic form
// stays useful for exploring or exercising queries the typed commands don't
// cover yet.
package namedquery

import (
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/grafana/gcx/internal/format"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers/synth/smcfg"
	"github.com/grafana/gcx/internal/query/dataframe"
	"github.com/grafana/gcx/internal/query/synth"
	"github.com/grafana/gcx/internal/shared"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// Result is what the command reports: one Series per frame the backend
// returned, alongside the expression it ran so the numbers are explainable.
//
// A query is always treated as N series, never specially collapsed to one --
// checks_uptime (unlabeled, one frame) is just the N=1 case of
// probe_execution_rate (labeled by probe, one frame per probe).
type Result struct {
	Query    string   `json:"query"`
	Series   []Series `json:"series"`
	Executed string   `json:"executedQuery,omitempty"`
}

// Series is the reduced value for one frame the backend returned.
type Series struct {
	// Labels is the Prometheus-selector-style rendering of the frame's series
	// labels ("{}" for an unlabeled frame such as checks_uptime).
	Labels   string  `json:"labels"`
	Value    float64 `json:"value"`
	HasValue bool    `json:"hasValue"`
	// Reducible is false for a frame with no numeric field at all (e.g. a log
	// query), which is a different situation than a metric frame that
	// legitimately returned no points -- HasValue covers that case.
	Reducible bool `json:"reducible"`
	Points    int  `json:"points"`
}

type queryOpts struct {
	IO     cmdio.Options
	Params []string
	From   string
	To     string
}

func (o *queryOpts) setup(flags *pflag.FlagSet) {
	o.IO.RegisterCustomCodec("table", &tableCodec{})
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)

	flags.StringArrayVarP(&o.Params, "param", "p", nil,
		"Query parameter as key=value (repeatable), e.g. -p job=my-check")
	flags.StringVar(&o.From, "from", "now-3h", "Start of the time range")
	flags.StringVar(&o.To, "to", "now", "End of the time range")
}

// Commands returns the named-query command.
func Commands(loader smcfg.Loader) *cobra.Command {
	opts := &queryOpts{}

	cmd := &cobra.Command{
		Use:   "query NAME",
		Short: "Run a Synthetic Monitoring query by name.",
		Long: `Run a query the Synthetic Monitoring backend knows by name.

The backend owns the expression and picks the datasource that holds the data, so
no PromQL or LogQL is sent or required. Parameters are passed with -p and are
validated by the backend, which reports the expression it ran.`,
		Example: `
  # Uptime for a check, as the app computes it
  gcx synthetic-monitoring query checks_uptime \
    -p job=my-check -p instance=https://example.com -p frequency=60000

  # Execution rate per probe over the last day
  gcx synthetic-monitoring query probe_execution_rate \
    -p job=my-check -p instance=https://example.com --from now-1d`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}

			params, err := parseParams(opts.Params)
			if err != nil {
				return err
			}

			from, to, err := parseRange(opts.From, opts.To, time.Now())
			if err != nil {
				return err
			}

			ctx := cmd.Context()

			restCfg, datasourceUID, _, err := loader.LoadSMProxyConfig(ctx)
			if err != nil {
				return err
			}
			if datasourceUID == "" {
				return errors.New("no synthetic monitoring datasource found in this context; " +
					"named queries are served by the datasource, so one must be installed")
			}

			client, err := synth.NewBackendDatasourceClient(restCfg)
			if err != nil {
				return err
			}

			res, err := client.Query(ctx, datasourceUID, synth.NamedQuery{
				Name:   args[0],
				Params: params,
			}, from, to)
			if err != nil {
				return err
			}

			// Mean(frame) assumes the mean over the frame's time samples is the
			// correct reduction. That holds for the two queries registered today:
			// - checks_uptime is a single, unlabeled range series that the app reduces
			// -  probe_execution_rate is an instant query so Mean is a no-op on its single sample per probe.
			//
			// It would NOT hold for a query that is both grouped by a label and
			// a range query (multiple time samples per label). No such query is
			// registered yet, but several exist client-side in
			// synthetic-monitoring-app and are charted raw, never reduced to a
			// mean -- porting any of these as-is would need a different
			// reduction here, not Mean:
			//   src/queries/sumDurationByProbe.ts
			//   src/queries/browserDataReceived.ts
			//   src/queries/browserDataSent.ts
			//   src/queries/scriptedDataReceived.ts
			//   src/queries/scriptedDataSent.ts
			//   src/queries/scriptedHTTPRequestsErrorRate.ts
			//   src/queries/avgQuantileWebVital.ts
			series := make([]Series, 0, len(res.Frames))
			for _, frame := range res.Frames {
				value, hasValue := synth.Mean(frame)
				series = append(series, Series{
					Labels:    dataframe.FormatLabels(synth.Labels(frame)),
					Value:     value,
					HasValue:  hasValue,
					Reducible: synth.HasNumericField(frame),
					Points:    points(frame),
				})
			}

			out := Result{
				Query:    args[0],
				Series:   series,
				Executed: res.ExecutedQuery,
			}

			return opts.IO.Encode(cmd.OutOrStdout(), out)
		},
	}

	opts.setup(cmd.Flags())

	return cmd
}

// parseParams turns repeated key=value flags into the parameter map the backend
// expects.
func parseParams(raw []string) (map[string]any, error) {
	// numericParams are the only keys sent as JSON numbers. The backend
	// unmarshals frequency into an int and quantile into a float, so a quoted
	// string fails to decode for them -- but most params (job, instance,
	// probe, ...) are strings that can look numeric (e.g. a numeric job ID)
	// and must not be silently coerced.
	numericParams := map[string]bool{
		"frequency": true,
		"quantile":  true,
	}

	params := make(map[string]any, len(raw))

	for _, kv := range raw {
		key, value, ok := strings.Cut(kv, "=")
		if !ok || key == "" {
			return nil, fmt.Errorf("parameter %q must be key=value", kv)
		}

		switch {
		case value == "true":
			params[key] = true
		case value == "false":
			params[key] = false
		case numericParams[key]:
			n, err := strconv.ParseFloat(value, 64)
			if err != nil {
				return nil, fmt.Errorf("parameter %q must be a number: %w", kv, err)
			}
			params[key] = n
		default:
			params[key] = value
		}
	}

	return params, nil
}

// parseRange resolves the relative or absolute bounds of the query window.
func parseRange(from, to string, now time.Time) (time.Time, time.Time, error) {
	start, err := parseTime(from, now)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("--from: %w", err)
	}

	end, err := parseTime(to, now)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("--to: %w", err)
	}

	if !end.After(start) {
		return time.Time{}, time.Time{}, fmt.Errorf("--to (%s) must be after --from (%s)", to, from)
	}

	return start, end, nil
}

// parseTime accepts "now", "now-<duration>" (s/m/h/d/w/M/y, matching
// Prometheus and Explore), RFC3339, or a unix timestamp.
func parseTime(value string, now time.Time) (time.Time, error) {
	if value == "" {
		return time.Time{}, errors.New("must not be empty")
	}

	t, err := shared.ParseTime(value, now)
	if err != nil {
		return time.Time{}, fmt.Errorf("expected now, now-<duration>, RFC3339, or a unix timestamp: %w", err)
	}

	return t, nil
}

// points counts the samples behind frame's reduced value, so an unexpectedly
// round number can be traced to an empty or single-point series. It reports 0
// for a frame with no numeric field, since there is no reduced value for it
// to count samples behind.
func points(frame dataframe.Frame) int {
	if !synth.HasNumericField(frame) {
		return 0
	}

	values := frame.Data.Values
	if len(values) == 0 {
		return 0
	}

	return len(values[0])
}

// tableCodec renders the result as aligned rows. A result whose only series
// is unlabeled (e.g. checks_uptime) prints the same QUERY/VALUE/POINTS rows
// as before labels existed; a result with labeled series (e.g. one series per
// probe) adds a LABELS column and prints one row per series.
type tableCodec struct{}

func (c *tableCodec) Format() format.Format { return "table" }

func (c *tableCodec) Encode(w io.Writer, v any) error {
	res, ok := v.(Result)
	if !ok {
		return fmt.Errorf("expected Result, got %T", v)
	}

	if len(res.Series) == 1 && res.Series[0].Labels == "{}" {
		return encodeSingleSeries(w, res)
	}

	return encodeMultiSeries(w, res)
}

func encodeSingleSeries(w io.Writer, res Result) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)

	fmt.Fprintf(tw, "QUERY\t%s\n", res.Query)
	fmt.Fprintf(tw, "VALUE\t%s\n", formatValue(res.Series[0]))
	fmt.Fprintf(tw, "POINTS\t%d\n", res.Series[0].Points)

	if res.Executed != "" {
		fmt.Fprintf(tw, "EXECUTED\t%s\n", strings.ReplaceAll(res.Executed, "\n", " "))
	}

	return tw.Flush()
}

func encodeMultiSeries(w io.Writer, res Result) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)

	fmt.Fprintf(tw, "QUERY\t%s\n", res.Query)
	fmt.Fprintln(tw, "LABELS\tVALUE\tPOINTS")
	for _, s := range res.Series {
		fmt.Fprintf(tw, "%s\t%s\t%d\n", s.Labels, formatValue(s), s.Points)
	}

	if res.Executed != "" {
		fmt.Fprintf(tw, "EXECUTED\t%s\n", strings.ReplaceAll(res.Executed, "\n", " "))
	}

	return tw.Flush()
}

func formatValue(s Series) string {
	switch {
	case s.HasValue:
		return strconv.FormatFloat(s.Value, 'f', 4, 64)
	case !s.Reducible:
		return "n/a (not a numeric query)"
	default:
		return "no data"
	}
}

func (c *tableCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("decoding is not supported")
}
