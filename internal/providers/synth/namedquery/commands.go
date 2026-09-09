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
	"github.com/grafana/gcx/internal/query/synth"
	"github.com/grafana/gcx/internal/shared"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// Result is what the command reports: the reduced value the app would display,
// alongside the expression the backend ran so the number is explainable.
type Result struct {
	Query    string  `json:"query"`
	Value    float64 `json:"value"`
	HasValue bool    `json:"hasValue"`
	// Reducible is false for a query whose result has no numeric field at all
	// (e.g. a log query), which is a different situation than a metric query
	// that legitimately returned no points -- HasValue covers that case.
	Reducible bool   `json:"reducible"`
	Points    int    `json:"points"`
	Executed  string `json:"executedQuery,omitempty"`
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

  # Reachability over the last day
  gcx synthetic-monitoring query reachability \
    -p job=my-check -p instance=https://example.com -p frequency=60000 --from now-1d`,
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

			value, hasValue := synth.Mean(res)
			out := Result{
				Query:     args[0],
				Value:     value,
				HasValue:  hasValue,
				Reducible: synth.HasNumericField(res),
				Points:    points(res),
				Executed:  res.ExecutedQuery,
			}

			return opts.IO.Encode(cmd.OutOrStdout(), out)
		},
	}

	opts.setup(cmd.Flags())

	return cmd
}

// parseParams turns repeated key=value flags into the parameter map the backend
// expects. Numbers are sent as numbers: the backend unmarshals frequency and
// quantile into numeric fields, and a quoted string fails to decode.
func parseParams(raw []string) (map[string]any, error) {
	params := make(map[string]any, len(raw))

	for _, kv := range raw {
		key, value, ok := strings.Cut(kv, "=")
		if !ok || key == "" {
			return nil, fmt.Errorf("parameter %q must be key=value", kv)
		}

		switch value {
		case "true":
			params[key] = true
		case "false":
			params[key] = false
		default:
			if n, err := strconv.ParseFloat(value, 64); err == nil {
				params[key] = n
				continue
			}
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
	t, err := shared.ParseTime(value, now)
	if err != nil {
		return time.Time{}, fmt.Errorf("expected now, now-<duration>, RFC3339, or a unix timestamp: %w", err)
	}

	return t, nil
}

// points counts the samples behind the reduced value, so an unexpectedly round
// number can be traced to an empty or single-point series.
func points(res *synth.NamedResult) int {
	if res == nil || len(res.Frames) == 0 {
		return 0
	}

	values := res.Frames[0].Data.Values
	if len(values) == 0 {
		return 0
	}

	return len(values[0])
}

// tableCodec renders the single result as a couple of aligned rows.
type tableCodec struct{}

func (c *tableCodec) Format() format.Format { return "table" }

func (c *tableCodec) Encode(w io.Writer, v any) error {
	res, ok := v.(Result)
	if !ok {
		return fmt.Errorf("expected Result, got %T", v)
	}

	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)

	value := "no data"
	switch {
	case res.HasValue:
		value = strconv.FormatFloat(res.Value, 'f', 4, 64)
	case !res.Reducible:
		value = "n/a (not a numeric query; use -o json to see the raw frames)"
	}

	fmt.Fprintf(tw, "QUERY\t%s\n", res.Query)
	fmt.Fprintf(tw, "VALUE\t%s\n", value)
	fmt.Fprintf(tw, "POINTS\t%d\n", res.Points)

	if res.Executed != "" {
		fmt.Fprintf(tw, "EXECUTED\t%s\n", strings.ReplaceAll(res.Executed, "\n", " "))
	}

	return tw.Flush()
}

func (c *tableCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("decoding is not supported")
}
