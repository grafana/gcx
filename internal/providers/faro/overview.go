package faro

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/grafana/gcx/cmd/gcx/fail"
	"github.com/grafana/gcx/internal/agent"
	internalconfig "github.com/grafana/gcx/internal/config"
	dsquery "github.com/grafana/gcx/internal/datasources/query"
	"github.com/grafana/gcx/internal/format"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/query/loki"
	"github.com/grafana/grafana-app-sdk/logging"
	"github.com/prometheus/common/model"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// defaultOverviewWindow is the lookback when --since isn't set. RUM volume is
// lower and burstier than backend spans, so 1h gives p75 vitals enough
// samples to be meaningful where a 5m default would often be empty.
const defaultOverviewWindow = "1h"

type overviewOpts struct {
	IO         cmdio.Options
	Datasource string
	Since      string
}

func (o *overviewOpts) setup(flags *pflag.FlagSet) {
	o.IO.RegisterCustomCodec("table", &overviewCodec{})
	o.IO.RegisterCustomCodec("wide", &overviewCodec{Wide: true})
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)

	flags.StringVarP(&o.Datasource, "datasource", "d", "", "Loki datasource UID (defaults to datasources.loki in config or auto-discovery)")
	flags.StringVar(&o.Since, "since", defaultOverviewWindow, "Lookback window for the KPI snapshot (e.g. 15m, 1h, 1d) — PromQL/LogQL duration syntax")
}

func (o *overviewOpts) Validate(cmd *cobra.Command) error {
	if err := o.IO.Validate(); err != nil {
		return err
	}
	if strings.TrimSpace(o.Since) == "" {
		return fail.NewCommandUsageError(cmd, "--since must not be empty", nil)
	}
	if _, err := model.ParseDuration(o.Since); err != nil {
		return fail.NewCommandUsageError(cmd, fmt.Sprintf("--since %q is not a valid duration", o.Since), err)
	}
	return nil
}

func newOverviewCommand() *cobra.Command {
	opts := &overviewOpts{}
	cmd := &cobra.Command{
		Use:   "overview <app>",
		Short: "Show a Frontend Observability KPI snapshot for one app: page loads, errors, and Core Web Vitals.",
		Long: `Show the headline KPIs for one Frontend Observability (Faro) app.

The argument is the app name or its numeric id; a name is resolved to its id
via the same app list used by "gcx frontend apps list". The snapshot mirrors
the Frontend Observability plugin's app overview page, computed from the Faro
RUM telemetry stored in Loki over --since (default 1h):

  - page loads and exception count (with errors as a percentage of loads)
  - the five Core Web Vitals at p75, each rated good / needs-improvement /
    poor against Google's thresholds (LCP, INP, CLS, FCP, TTFB)
  - the most frequent exceptions

Web Vitals use the same LogQL the plugin runs: a p75 over the Faro measurement
stream. The numbers require RUM data flowing to the stack's Loki; an app with
no telemetry in the window renders an empty snapshot and exits non-zero.`,
		Example: `
  # KPI snapshot for an app by name over the last hour
  gcx frontend apps overview faro-shop-demo

  # By numeric id, last 15 minutes
  gcx frontend apps overview 153 --since 15m

  # JSON for scripting / agents
  gcx frontend apps overview faro-shop-demo -o json

  # Pin the Loki datasource
  gcx frontend apps overview faro-shop-demo -d grafanacloud-logs`,
		Args: cobra.ExactArgs(1),
		RunE: runOverview(opts),
		Annotations: map[string]string{
			agent.AnnotationTokenCost: "small",
			agent.AnnotationLLMHint:   `Per-app Frontend Observability (RUM) KPI snapshot from Faro telemetry in Loki: page loads, exception count + error percent, and the five Core Web Vitals at p75 (LCP, INP, CLS, FCP, TTFB) each rated good/needs-improvement/poor, plus top exceptions, over --since (default 1h). The argument is the Faro app name or numeric id. This is the frontend counterpart to 'gcx appo11y services get' (backend RED snapshot). Requires RUM data in the stack's Loki. Examples: gcx frontend apps overview <app> -o json; gcx frontend apps overview <app> --since 15m -o json`,
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

func runOverview(opts *overviewOpts) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		if err := opts.Validate(cmd); err != nil {
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

		appID, appName, err := resolveOverviewApp(ctx, cfg, args[0])
		if err != nil {
			return err
		}

		datasourceUID, err := dsquery.ResolveAndSaveDatasource(ctx, &loader, opts.Datasource, cfgCtx, cfg, "loki")
		if err != nil {
			return err
		}

		client, err := loki.NewClient(cfg)
		if err != nil {
			return fmt.Errorf("failed to create loki client: %w", err)
		}

		overview, err := fetchOverview(ctx, client, datasourceUID, appID, appName, opts.Since)
		if err != nil {
			return err
		}

		if !overview.HasTraffic() {
			emitNoRUMHint(cmd.ErrOrStderr(), appName)
		}
		if err := opts.IO.Encode(cmd.OutOrStdout(), overview); err != nil {
			return err
		}
		if !overview.HasTraffic() {
			// Emit the (empty) snapshot for structure, but signal "no data"
			// via exit code so callers can branch on $?.
			return fmt.Errorf("app %q has no RUM telemetry in the requested window", appName)
		}
		return nil
	}
}

// resolveOverviewApp turns the positional argument (a name or numeric id) into
// the app's id and name. It lists apps once and matches on id first, then
// name, so "153" and "faro-shop-demo" both resolve.
func resolveOverviewApp(ctx context.Context, cfg internalconfig.NamespacedRESTConfig, arg string) (string, string, error) {
	arg = strings.TrimSpace(arg)
	if arg == "" {
		return "", "", errors.New("app name or id is required")
	}
	client, err := NewClient(cfg)
	if err != nil {
		return "", "", err
	}
	apps, err := client.List(ctx)
	if err != nil {
		return "", "", err
	}
	for _, app := range apps {
		if app.ID == arg || app.Name == arg {
			return app.ID, app.Name, nil
		}
	}
	return "", "", fmt.Errorf("frontend app %q not found by id or name; run 'gcx frontend apps list' to see available apps", arg)
}

// emitNoRUMHint surfaces a stderr line when no page loads were observed.
func emitNoRUMHint(stderr io.Writer, appName string) {
	cmdio.EmitHint(stderr,
		fmt.Sprintf("no RUM telemetry found for %q in the requested window", appName),
		"gcx frontend apps list")
}

// overviewCodec renders an Overview as a kubectl-describe-style block: a
// traffic header, the Core Web Vitals with ratings, and a top-errors section.
// Wide adds the error type column to the top-errors list.
type overviewCodec struct {
	Wide bool
}

func (c *overviewCodec) Format() format.Format {
	if c.Wide {
		return "wide"
	}
	return "table"
}

func (c *overviewCodec) Decode(io.Reader, any) error {
	return errors.New("frontend overview codec does not support decoding")
}

func (c *overviewCodec) Encode(w io.Writer, v any) error {
	o, ok := v.(*Overview)
	if !ok {
		return fmt.Errorf("invalid data type for frontend overview codec: %T", v)
	}

	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	row := func(label, value string) { fmt.Fprintf(tw, "%s:\t%s\n", label, value) }

	row("App", appLabel(o.AppName, o.AppID))
	row("Window", o.Window)
	row("Page loads", formatCount(o.PageLoads, o.HasPageLoads))
	row("Errors", formatErrorCount(o.Errors, o.ErrorPercent, o.HasErrors, o.HasTraffic()))

	fmt.Fprintln(tw, "Core Web Vitals (p75):")
	for _, vital := range o.WebVitals {
		fmt.Fprintf(tw, "  %s:\t%s\n", vital.Name, formatVital(vital))
	}

	if len(o.TopErrors) > 0 {
		fmt.Fprintln(tw, "Top errors:")
		for _, e := range o.TopErrors {
			fmt.Fprintf(tw, "  %s\t%s\n", formatTopError(e, c.Wide), formatCountPlain(e.Count))
		}
	}

	return tw.Flush()
}

func appLabel(name, id string) string {
	switch {
	case name != "" && id != "":
		return fmt.Sprintf("%s (id %s)", name, id)
	case name != "":
		return name
	case id != "":
		return "id " + id
	default:
		return "-"
	}
}

func formatCount(v float64, has bool) string {
	if !has {
		return "-"
	}
	return formatCountPlain(int64(v))
}

func formatCountPlain(n int64) string {
	return strconv.FormatInt(n, 10)
}

// formatErrorCount shows the absolute exception count plus its share of page
// loads. With no traffic there's no meaningful denominator, so the percentage
// is dropped.
func formatErrorCount(errs, pct float64, hasErrors, hasTraffic bool) string {
	if !hasErrors {
		return "-"
	}
	if !hasTraffic {
		return formatCountPlain(int64(errs))
	}
	return fmt.Sprintf("%d (%.2f%% of loads)", int64(errs), pct)
}

// formatVital renders a Core Web Vital with units that scale to its magnitude
// and the rating tag. CLS is a unitless score; the timing vitals stay in ms
// until they cross a second.
func formatVital(v WebVital) string {
	if !v.HasData {
		return "-"
	}
	var value string
	switch v.Unit {
	case "score":
		value = fmt.Sprintf("%.3f", v.P75)
	default: // ms
		if v.P75 >= 1000 {
			value = fmt.Sprintf("%.2fs", v.P75/1000)
		} else {
			value = fmt.Sprintf("%.0fms", v.P75)
		}
	}
	if v.Rating != "" {
		return fmt.Sprintf("%s  %s", value, v.Rating)
	}
	return value
}

func formatTopError(e TopError, wide bool) string {
	msg := e.Message
	if msg == "" {
		msg = "(no message)"
	}
	if wide && e.Type != "" {
		return e.Type + ": " + msg
	}
	return msg
}
