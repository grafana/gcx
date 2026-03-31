package checks

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/datasources"
	"github.com/grafana/gcx/internal/format"
	"github.com/grafana/gcx/internal/graph"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/providers/synth/probes"
	"github.com/grafana/gcx/internal/providers/synth/smcfg"
	"github.com/grafana/gcx/internal/query/prometheus"
	"github.com/grafana/grafana-app-sdk/logging"
	"github.com/grafana/promql-builder/go/promql"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"golang.org/x/sync/errgroup"
)

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

// CheckStatusResult holds merged check + metric data for a single check.
type CheckStatusResult struct {
	ID          int64    `json:"id"`
	Job         string   `json:"job"`
	Target      string   `json:"target"`
	Type        string   `json:"type"`
	Success     *float64 `json:"success,omitempty"`
	ProbesUp    int      `json:"probesUp"`
	ProbesTotal int      `json:"probesTotal"`
	ProbeNames  []string `json:"probeNames,omitempty"`
	Status      string   `json:"status"`
}

// CheckTimelinePayload is passed to timeline codecs for encoding.
type CheckTimelinePayload struct {
	Check  Check
	Series []TimelineSeries
	Start  time.Time
	End    time.Time
}

// TimelineSeries holds time series data for a single probe.
type TimelineSeries struct {
	Probe  string
	Points []TimelinePoint
}

// TimelinePoint represents a single data point in the timeline.
type TimelinePoint struct {
	Time  time.Time
	Value float64
}

// ---------------------------------------------------------------------------
// status command
// ---------------------------------------------------------------------------

type statusOpts struct {
	IO            cmdio.Options
	DatasourceUID string
}

func (o *statusOpts) setup(flags *pflag.FlagSet) {
	o.IO.RegisterCustomCodec("table", &StatusTableCodec{})
	o.IO.RegisterCustomCodec("wide", &StatusTableCodec{Wide: true})
	o.IO.RegisterCustomCodec("graph", &StatusGraphCodec{})
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)

	flags.StringVar(&o.DatasourceUID, "datasource-uid", "", "UID of the Prometheus datasource to query")
}

func newStatusCommand(loader smcfg.StatusLoader) *cobra.Command {
	opts := &statusOpts{}
	cmd := &cobra.Command{
		Use:   "status [ID]",
		Short: "Show pass/fail status of Synthetic Monitoring checks.",
		Long: `Show pass/fail status by combining the SM API with Prometheus probe_success metrics.

Displays current success rate, number of probes reporting, and health status
for each check. Requires a Prometheus datasource containing SM metrics.`,
		Example: `  # Show status of all checks.
  gcx synth checks status

  # Show status of a specific check by ID.
  gcx synth checks status 42

  # Specify the Prometheus datasource to query.
  gcx synth checks status --datasource-uid my-prometheus

  # Output as JSON for scripting.
  gcx synth checks status -o json`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}

			ctx := cmd.Context()

			// Load SM config — needed by all parallel branches below.
			baseURL, token, _, err := loader.LoadSMConfig(ctx)
			if err != nil {
				return err
			}

			smClient := NewClient(baseURL, token)

			// Parse optional check ID arg before launching goroutines.
			var filterID int64
			if len(args) == 1 {
				filterID, err = strconv.ParseInt(args[0], 10, 64)
				if err != nil {
					return fmt.Errorf("invalid check ID %q: must be a number", args[0])
				}
			}

			// Fan-out: fetch checks, probes, datasource UID, and REST config in parallel.
			var (
				checks       []Check
				probeNameMap = map[int64]string{}
				dsUID        string
				restCfg      config.NamespacedRESTConfig
			)

			initG, initCtx := errgroup.WithContext(ctx)

			initG.Go(func() error {
				if filterID != 0 {
					c, err := smClient.Get(initCtx, filterID)
					if err != nil {
						return err
					}
					checks = []Check{*c}
				} else {
					var listErr error
					checks, listErr = smClient.List(initCtx)
					return listErr
				}
				return nil
			})

			initG.Go(func() error {
				probeList, err := probes.NewClient(baseURL, token).List(initCtx)
				if err == nil {
					probeNameMap = buildProbeNameMap(probeList)
				}
				return nil // best-effort
			})

			initG.Go(func() error {
				var err error
				dsUID, err = resolveDataSourceUID(initCtx, opts.DatasourceUID, loader)
				return err
			})

			initG.Go(func() error {
				var err error
				restCfg, err = loader.LoadGrafanaConfig(initCtx)
				return err
			})

			if err := initG.Wait(); err != nil {
				return err
			}

			if len(checks) == 0 {
				cmdio.Info(cmd.OutOrStdout(), "No checks found.")
				return nil
			}

			promClient, err := prometheus.NewClient(restCfg)
			if err != nil {
				return err
			}

			// Two aggregate queries — one HTTP call each, covering all checks at once.
			successQ, err := BuildAllSuccessRateQuery()
			if err != nil {
				return err
			}
			probeCountQ, err := BuildAllProbeCountQuery()
			if err != nil {
				return err
			}

			var (
				successMap    map[string]float64
				probeCountMap map[string]float64
			)

			promG, promCtx := errgroup.WithContext(ctx)
			promG.Go(func() error {
				successMap = queryInstantByJobInstance(promCtx, promClient, dsUID, successQ)
				return nil
			})
			promG.Go(func() error {
				probeCountMap = queryInstantByJobInstance(promCtx, promClient, dsUID, probeCountQ)
				return nil
			})
			if err := promG.Wait(); err != nil {
				return err
			}

			results := BuildCheckStatusResults(checks, successMap, probeCountMap, probeNameMap)

			codec, err := opts.IO.Codec()
			if err != nil {
				return err
			}

			return codec.Encode(cmd.OutOrStdout(), results)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// ---------------------------------------------------------------------------
// timeline command
// ---------------------------------------------------------------------------

type timelineOpts struct {
	IO            cmdio.Options
	DatasourceUID string
	From          string
	To            string
	Window        string
}

func (o *timelineOpts) setup(flags *pflag.FlagSet) {
	o.IO.RegisterCustomCodec("table", &TimelineTableCodec{})
	o.IO.RegisterCustomCodec("graph", &TimelineGraphCodec{})
	o.IO.DefaultFormat("graph")
	o.IO.BindFlags(flags)

	flags.StringVar(&o.DatasourceUID, "datasource-uid", "", "UID of the Prometheus datasource to query")
	flags.StringVar(&o.From, "from", "", "Start of the time range (e.g. now-6h, now-24h, RFC3339, Unix timestamp)")
	flags.StringVar(&o.To, "to", "", "End of the time range (e.g. now, RFC3339, Unix timestamp)")
	flags.StringVar(&o.Window, "window", "6h", "Time window to display (e.g. 1h, 6h, 24h, 7d)")
}

func newTimelineCommand(loader smcfg.StatusLoader) *cobra.Command {
	opts := &timelineOpts{}
	cmd := &cobra.Command{
		Use:   "timeline ID",
		Short: "Render probe_success over time as a terminal line chart.",
		Long: `Render probe_success values over time as a line chart by executing a range
query against the Prometheus datasource.

Each probe reporting for the check is rendered as a separate series.
Requires a Prometheus datasource containing SM metrics.`,
		Example: `  # Render timeline for a check over the past 6 hours (default).
  gcx synth checks timeline 42

  # Custom time window.
  gcx synth checks timeline 42 --window 24h

  # Explicit time range.
  gcx synth checks timeline 42 --from now-24h --to now

  # Output timeline data as a table.
  gcx synth checks timeline 42 -o table

  # Specify the Prometheus datasource.
  gcx synth checks timeline 42 --datasource-uid my-prometheus`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}

			// Validate flag combinations.
			windowSet := cmd.Flags().Changed("window")
			fromToSet := cmd.Flags().Changed("from") || cmd.Flags().Changed("to")
			if windowSet && fromToSet {
				return errors.New("--window and --from/--to are mutually exclusive")
			}

			ctx := cmd.Context()

			// Parse check ID.
			id, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil {
				return fmt.Errorf("invalid check ID %q: must be a number", args[0])
			}

			// Load SM config and get the check.
			baseURL, token, _, err := loader.LoadSMConfig(ctx)
			if err != nil {
				return err
			}

			client := NewClient(baseURL, token)

			c, err := client.Get(ctx, id)
			if err != nil {
				return err
			}

			// Compute time range from --from/--to or --window.
			now := time.Now()
			var start, end time.Time

			start, end, err = parseCheckTimeRange(fromToSet, opts.From, opts.To, opts.Window, now)
			if err != nil {
				return err
			}
			step := autoStep(start, end)

			// Resolve datasource UID.
			dsUID, err := resolveDataSourceUID(ctx, opts.DatasourceUID, loader)
			if err != nil {
				return err
			}

			// Load REST config and create Prometheus client.
			restCfg, err := loader.LoadGrafanaConfig(ctx)
			if err != nil {
				return err
			}

			promClient, err := prometheus.NewClient(restCfg)
			if err != nil {
				return err
			}

			// Range query for probe_success.
			q, err := BuildTimelineQuery(c.Job, c.Target)
			if err != nil {
				return fmt.Errorf("building timeline query: %w", err)
			}

			resp, err := promClient.Query(ctx, dsUID, prometheus.QueryRequest{
				Query: q,
				Start: start,
				End:   end,
				Step:  step,
			})
			if err != nil {
				return fmt.Errorf("querying timeline metrics: %w", err)
			}

			// Build series from response: one series per probe label value.
			series := buildTimelineSeries(resp)

			if len(series) == 0 {
				cmdio.Info(cmd.OutOrStdout(), "No time-series data available for check %d.", id)
				return nil
			}

			codec, err := opts.IO.Codec()
			if err != nil {
				return err
			}

			return codec.Encode(cmd.OutOrStdout(), CheckTimelinePayload{
				Check:  *c,
				Series: series,
				Start:  start,
				End:    end,
			})
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// ---------------------------------------------------------------------------
// PromQL query builders
// ---------------------------------------------------------------------------

// BuildSuccessRateQuery builds a PromQL query for the average probe_success
// rate over 5 minutes, grouped by job and instance.
func BuildSuccessRateQuery(job, instance string) (string, error) {
	expr, err := promql.Avg(
		promql.AvgOverTime(
			promql.Vector("probe_success").
				Label("job", job).
				Label("instance", instance).
				Range("5m"),
		),
	).By([]string{"job", "instance"}).Build()
	if err != nil {
		return "", err
	}
	return expr.String(), nil
}

// BuildProbeCountQuery builds a PromQL query that counts the number of probes
// currently reporting for a check.
func BuildProbeCountQuery(job, instance string) (string, error) {
	expr, err := promql.Count(
		promql.Vector("probe_success").
			Label("job", job).
			Label("instance", instance),
	).By([]string{"job", "instance"}).Build()
	if err != nil {
		return "", err
	}
	return expr.String(), nil
}

// BuildAllSuccessRateQuery builds a PromQL query for the success rate of all checks.
// The result is keyed by (job, instance) labels and covers all checks in one HTTP call.
func BuildAllSuccessRateQuery() (string, error) {
	expr, err := promql.Avg(
		promql.AvgOverTime(
			promql.Vector("probe_success").Range("5m"),
		),
	).By([]string{"job", "instance"}).Build()
	if err != nil {
		return "", err
	}
	return expr.String(), nil
}

// BuildAllProbeCountQuery builds a PromQL query counting probes per check across all checks.
func BuildAllProbeCountQuery() (string, error) {
	expr, err := promql.Count(
		promql.Vector("probe_success"),
	).By([]string{"job", "instance"}).Build()
	if err != nil {
		return "", err
	}
	return expr.String(), nil
}

// BuildTimelineQuery builds a PromQL query for raw probe_success values.
func BuildTimelineQuery(job, instance string) (string, error) {
	expr, err := promql.Vector("probe_success").
		Label("job", job).
		Label("instance", instance).
		Build()
	if err != nil {
		return "", err
	}
	return expr.String(), nil
}

// ---------------------------------------------------------------------------
// Metric fetching helpers
// ---------------------------------------------------------------------------

// queryInstantByJobInstance executes a multi-series instant query and returns a map
// keyed by "job/instance" containing the scalar value for each series.
func queryInstantByJobInstance(ctx context.Context, client *prometheus.Client, dsUID, query string) map[string]float64 {
	resp, err := client.Query(ctx, dsUID, prometheus.QueryRequest{Query: query})
	if err != nil || resp.Status != "success" {
		return nil
	}
	result := make(map[string]float64, len(resp.Data.Result))
	for _, sample := range resp.Data.Result {
		job := sample.Metric["job"]
		instance := sample.Metric["instance"]
		if job == "" || instance == "" {
			continue
		}
		if val := parseSampleValue(sample); val != nil {
			result[job+"/"+instance] = *val
		}
	}
	return result
}

// parseSampleValue extracts the float64 value from an instant query sample.
func parseSampleValue(sample prometheus.Sample) *float64 {
	if len(sample.Value) < 2 {
		return nil
	}

	var val float64
	switch v := sample.Value[1].(type) {
	case string:
		f, err := strconv.ParseFloat(v, 64)
		if err != nil {
			return nil
		}
		val = f
	case float64:
		val = v
	default:
		return nil
	}

	if math.IsNaN(val) || math.IsInf(val, 0) {
		return nil
	}

	return &val
}

// buildTimelineSeries converts a Prometheus query response into timeline series,
// one per distinct "probe" label value.
func buildTimelineSeries(resp *prometheus.QueryResponse) []TimelineSeries {
	if resp == nil || resp.Status != "success" {
		return nil
	}

	var result []TimelineSeries
	for _, sample := range resp.Data.Result {
		probeName := sample.Metric["probe"]
		if probeName == "" {
			probeName = "unknown"
		}

		var points []TimelinePoint
		for _, vals := range sample.Values {
			if len(vals) < 2 {
				continue
			}

			ts, ok := vals[0].(float64)
			if !ok {
				continue
			}

			val, err := parseMatrixValue(vals[1])
			if err != nil {
				continue
			}

			points = append(points, TimelinePoint{
				Time:  time.Unix(int64(ts), 0),
				Value: val,
			})
		}

		if len(points) > 0 {
			result = append(result, TimelineSeries{
				Probe:  probeName,
				Points: points,
			})
		}
	}

	return result
}

// parseMatrixValue extracts a float64 value from an any (string or float64).
func parseMatrixValue(raw any) (float64, error) {
	var val float64

	switch v := raw.(type) {
	case string:
		f, err := strconv.ParseFloat(v, 64)
		if err != nil {
			return 0, err
		}
		val = f
	case float64:
		val = v
	default:
		return 0, fmt.Errorf("unexpected value type %T", raw)
	}

	if math.IsNaN(val) || math.IsInf(val, 0) {
		return 0, errors.New("value is NaN or Inf")
	}

	return val, nil
}

// ---------------------------------------------------------------------------
// Result building
// ---------------------------------------------------------------------------

// BuildCheckStatusResults merges check definitions with metric data.
// probeNames maps probe ID to display name (e.g. "Oregon" or "Paris (offline)").
// Pass nil or an empty map if probe names are unavailable.
func BuildCheckStatusResults(checks []Check, successMap, probeCountMap map[string]float64, probeNames map[int64]string) []CheckStatusResult {
	results := make([]CheckStatusResult, 0, len(checks))

	for _, c := range checks {
		key := c.Job + "/" + c.Target

		r := CheckStatusResult{
			ID:          c.ID,
			Job:         c.Job,
			Target:      c.Target,
			Type:        c.Settings.CheckType(),
			ProbesTotal: len(c.Probes),
		}

		if val, ok := successMap[key]; ok {
			r.Success = &val
		}

		if cnt, ok := probeCountMap[key]; ok {
			r.ProbesUp = int(cnt)
		}

		for _, pid := range c.Probes {
			if name, ok := probeNames[pid]; ok {
				r.ProbeNames = append(r.ProbeNames, name)
			}
		}

		r.Status = computeCheckStatus(r.Success)
		results = append(results, r)
	}

	return results
}

// buildProbeNameMap builds a probe ID → display name map.
// Offline probes get a "(offline)" suffix.
func buildProbeNameMap(ps []probes.Probe) map[int64]string {
	m := make(map[int64]string, len(ps))
	for _, p := range ps {
		name := p.Name
		if !p.Online {
			name += " (offline)"
		}
		m[p.ID] = name
	}
	return m
}

// computeCheckStatus determines the display status for a check.
func computeCheckStatus(success *float64) string {
	if success == nil {
		return "NODATA"
	}
	if *success >= 0.5 {
		return "OK"
	}
	return "FAILING"
}

// ---------------------------------------------------------------------------
// Datasource resolution
// ---------------------------------------------------------------------------

// resolveDataSourceUID resolves the Prometheus datasource UID from:
// 1. Explicit flag value (highest priority).
// 2. Shared config resolver: datasources.prometheus → default-prometheus-datasource.
// 3. SM provider cache: providers.synth.sm-metrics-datasource-uid.
// 4. Auto-discover via SM plugin settings — result saved to SM cache for next run.
func resolveDataSourceUID(ctx context.Context, flagUID string, loader smcfg.StatusLoader) (string, error) {
	if flagUID != "" {
		return flagUID, nil
	}

	cfg, err := loader.LoadConfig(ctx)
	if err != nil {
		return "", fmt.Errorf(
			"loading config: %w; use --datasource-uid flag or set default-prometheus-datasource in config", err)
	}

	curCtx := cfg.GetCurrentContext()
	if curCtx == nil {
		return "", errors.New(
			"datasource UID is required: use --datasource-uid flag or set default-prometheus-datasource in config")
	}

	// Tier 2: shared config resolver — covers datasources.prometheus (new section)
	// then default-prometheus-datasource (legacy key) in priority order.
	if uid := config.DefaultDatasourceUID(*curCtx, "prometheus"); uid != "" {
		return uid, nil
	}

	// Tier 3: SM provider cache.
	if prov := curCtx.Providers["synth"]; prov != nil {
		if uid := prov["sm-metrics-datasource-uid"]; uid != "" {
			return uid, nil
		}
	}

	// Tier 4: auto-discover via SM plugin settings, then cache for next run.
	restCfg, err := loader.LoadGrafanaConfig(ctx)
	if err != nil {
		return "", fmt.Errorf(
			"loading REST config: %w; use --datasource-uid flag or set default-prometheus-datasource in config", err)
	}
	uid, err := discoverPrometheusDatasource(ctx, curCtx, restCfg)
	if err != nil {
		return "", err
	}

	// Best-effort save — don't fail the command if writing config fails.
	if saveErr := loader.SaveMetricsDatasourceUID(ctx, uid); saveErr != nil {
		logging.FromContext(ctx).Warn("could not save discovered datasource UID to config", slog.String("error", saveErr.Error()))
	}

	return uid, nil
}

// discoverPrometheusDatasource queries the Grafana SM plugin settings to find the
// Prometheus datasource configured for Synthetic Monitoring metrics.
func discoverPrometheusDatasource(ctx context.Context, curCtx *config.Context, restCfg config.NamespacedRESTConfig) (string, error) {
	// Query SM plugin settings for the metrics datasource name.
	dsName, err := smMetricsDatasourceName(ctx, curCtx)
	if err != nil {
		return "", fmt.Errorf(
			"could not auto-discover SM metrics datasource: %w; use --datasource-uid or set default-prometheus-datasource in config",
			err)
	}

	// Resolve name → UID via the REST-config transport (OAuth proxy-aware).
	dsClient, err := datasources.NewClient(restCfg)
	if err != nil {
		return "", errors.New(
			"datasource UID is required: use --datasource-uid flag or set default-prometheus-datasource in config")
	}
	ds, err := dsClient.GetByName(ctx, dsName)
	if err != nil {
		return "", fmt.Errorf(
			"SM metrics datasource %q not found in Grafana: %w; use --datasource-uid or set default-prometheus-datasource in config",
			dsName, err)
	}

	return ds.UID, nil
}

// smMetricsDatasourceName queries the grafana-synthetic-monitoring-app plugin settings
// and returns the configured metrics datasource name (jsonData.metrics.grafanaName).
func smMetricsDatasourceName(ctx context.Context, grafanaCtx *config.Context) (string, error) {
	if grafanaCtx.Grafana == nil {
		return "", errors.New("grafana not configured in context")
	}

	url := strings.TrimRight(grafanaCtx.Grafana.Server, "/") +
		"/api/plugins/grafana-synthetic-monitoring-app/settings"

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}

	if grafanaCtx.Grafana.APIToken != "" {
		req.Header.Set("Authorization", "Bearer "+grafanaCtx.Grafana.APIToken)
	} else if grafanaCtx.Grafana.User != "" {
		req.SetBasicAuth(grafanaCtx.Grafana.User, grafanaCtx.Grafana.Password)
	}

	resp, err := providers.ExternalHTTPClient().Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("plugin settings returned HTTP %d", resp.StatusCode)
	}

	var body struct {
		JSONData struct {
			Metrics struct {
				GrafanaName string `json:"grafanaName"`
			} `json:"metrics"`
		} `json:"jsonData"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return "", err
	}

	if body.JSONData.Metrics.GrafanaName == "" {
		return "", errors.New("metrics datasource not configured in SM plugin settings")
	}

	return body.JSONData.Metrics.GrafanaName, nil
}

// ---------------------------------------------------------------------------
// Window parsing
// ---------------------------------------------------------------------------

// parseCheckTimeRange resolves the start/end time range from either
// --from/--to flags or --window shorthand.
func parseCheckTimeRange(fromToSet bool, from, to, window string, now time.Time) (time.Time, time.Time, error) {
	if fromToSet {
		start, err := ParseCheckTimelineTime(from, now)
		if err != nil {
			return time.Time{}, time.Time{}, fmt.Errorf("invalid --from: %w", err)
		}
		end, err := ParseCheckTimelineTime(to, now)
		if err != nil {
			return time.Time{}, time.Time{}, fmt.Errorf("invalid --to: %w", err)
		}
		if !start.Before(end) {
			return time.Time{}, time.Time{}, errors.New("--from must be before --to")
		}
		return start, end, nil
	}
	w, err := ParseWindow(window)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("invalid --window: %w", err)
	}
	return now.Add(-w), now, nil
}

// ParseCheckTimelineTime parses a time string for the check timeline command.
// Supports "now", "now-Xd", "now-Xh", RFC3339, and Unix timestamps.
func ParseCheckTimelineTime(s string, now time.Time) (time.Time, error) {
	if s == "" {
		return now, nil
	}

	s = strings.TrimSpace(s)

	// Relative time: now, now-1h, now-7d, etc.
	if strings.HasPrefix(s, "now") {
		if s == "now" {
			return now, nil
		}
		// Parse as a window-style offset: "now-6h" → now.Add(-6h).
		rest := s[3:]
		if len(rest) == 0 {
			return now, nil
		}
		sign := 1
		switch rest[0] {
		case '-':
			sign = -1
			rest = rest[1:]
		case '+':
			rest = rest[1:]
		default:
			return time.Time{}, fmt.Errorf("invalid relative time format: %q", s)
		}
		d, err := ParseWindow(rest)
		if err != nil {
			return time.Time{}, fmt.Errorf("invalid relative time format: %q", s)
		}
		return now.Add(d * time.Duration(sign)), nil
	}

	// RFC3339
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, nil
	}

	// Unix timestamp (integer or float)
	if ts, err := strconv.ParseFloat(s, 64); err == nil {
		sec := int64(ts)
		nsec := int64((ts - float64(sec)) * 1e9)
		return time.Unix(sec, nsec), nil
	}

	return time.Time{}, fmt.Errorf("unable to parse time: %q", s)
}

// ParseWindow parses a duration string like "6h", "24h", "7d", "30m".
func ParseWindow(s string) (time.Duration, error) {
	if s == "" {
		return 0, errors.New("window must not be empty")
	}

	// Try standard Go duration first (handles "6h", "30m", "1h30m", etc.).
	if d, err := time.ParseDuration(s); err == nil {
		return d, nil
	}

	// Handle "d" suffix for days.
	if strings.HasSuffix(s, "d") {
		numStr := s[:len(s)-1]
		n, err := strconv.Atoi(numStr)
		if err != nil {
			return 0, fmt.Errorf("invalid window %q: %w", s, err)
		}
		return time.Duration(n) * 24 * time.Hour, nil
	}

	return 0, fmt.Errorf("invalid window %q: expected format like 1h, 6h, 24h, 7d", s)
}

// autoStep calculates a reasonable query step for the given time range,
// targeting ~200 data points. The minimum step is 1 minute.
func autoStep(start, end time.Time) time.Duration {
	const targetPoints = 200
	const minStep = time.Minute

	d := end.Sub(start)
	step := max(d/targetPoints, minStep)

	return step.Truncate(time.Minute)
}

// ---------------------------------------------------------------------------
// Status table codec
// ---------------------------------------------------------------------------

type StatusTableCodec struct {
	Wide bool
}

func (c *StatusTableCodec) Format() format.Format {
	if c.Wide {
		return "wide"
	}
	return "table"
}

func (c *StatusTableCodec) Encode(w io.Writer, v any) error {
	results, ok := v.([]CheckStatusResult)
	if !ok {
		return errors.New("invalid data type for status table codec: expected []CheckStatusResult")
	}

	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)

	if c.Wide {
		fmt.Fprintln(tw, "ID\tJOB\tTARGET\tTYPE\tSUCCESS\tPROBES_UP\tPROBES_TOTAL\tPROBES\tSTATUS")
	} else {
		fmt.Fprintln(tw, "ID\tJOB\tTARGET\tSUCCESS\tSTATUS")
	}

	for _, r := range results {
		successStr := "--"
		if r.Success != nil {
			successStr = fmt.Sprintf("%.2f%%", *r.Success*100)
		}

		if c.Wide {
			probesStr := strings.Join(r.ProbeNames, ", ")
			fmt.Fprintf(tw, "%d\t%s\t%s\t%s\t%s\t%d\t%d\t%s\t%s\n",
				r.ID, r.Job, r.Target, r.Type, successStr, r.ProbesUp, r.ProbesTotal, probesStr, r.Status)
		} else {
			fmt.Fprintf(tw, "%d\t%s\t%s\t%s\t%s\n",
				r.ID, r.Job, r.Target, successStr, r.Status)
		}
	}

	return tw.Flush()
}

func (c *StatusTableCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("status table codec does not support decoding")
}

// ---------------------------------------------------------------------------
// Timeline graph codec
// ---------------------------------------------------------------------------

type TimelineGraphCodec struct{}

func (c *TimelineGraphCodec) Format() format.Format { return "graph" }

func (c *TimelineGraphCodec) Encode(w io.Writer, v any) error {
	payload, ok := v.(CheckTimelinePayload)
	if !ok {
		return fmt.Errorf("TimelineGraphCodec: expected CheckTimelinePayload, got %T", v)
	}

	if len(payload.Series) == 0 {
		fmt.Fprintln(w, "No time-series data available.")
		return nil
	}

	chartData := &graph.ChartData{
		Title:  fmt.Sprintf("probe_success — %s (%s)", payload.Check.Job, payload.Check.Target),
		Series: make([]graph.Series, 0, len(payload.Series)),
	}

	for _, ts := range payload.Series {
		points := make([]graph.Point, len(ts.Points))
		for i, pt := range ts.Points {
			points[i] = graph.Point{
				Time:  pt.Time,
				Value: pt.Value,
			}
		}
		chartData.Series = append(chartData.Series, graph.Series{
			Name:   ts.Probe,
			Points: points,
		})
	}

	opts := graph.DefaultChartOptions()
	return graph.RenderLineChart(w, chartData, opts)
}

func (c *TimelineGraphCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("TimelineGraphCodec: decode not supported")
}

// ---------------------------------------------------------------------------
// Timeline table codec
// ---------------------------------------------------------------------------

type TimelineTableCodec struct{}

func (c *TimelineTableCodec) Format() format.Format { return "table" }

func (c *TimelineTableCodec) Encode(w io.Writer, v any) error {
	payload, ok := v.(CheckTimelinePayload)
	if !ok {
		return fmt.Errorf("TimelineTableCodec: expected CheckTimelinePayload, got %T", v)
	}

	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "PROBE\tTIMESTAMP\tSUCCESS")

	for _, ts := range payload.Series {
		for _, pt := range ts.Points {
			fmt.Fprintf(tw, "%s\t%s\t%.4f\n",
				ts.Probe,
				pt.Time.Format(time.RFC3339),
				pt.Value,
			)
		}
	}

	return tw.Flush()
}

func (c *TimelineTableCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("TimelineTableCodec: decode not supported")
}

// ---------------------------------------------------------------------------
// Status graph codec
// ---------------------------------------------------------------------------

type StatusGraphCodec struct{}

func (c *StatusGraphCodec) Format() format.Format { return "graph" }

func (c *StatusGraphCodec) Encode(w io.Writer, v any) error {
	results, ok := v.([]CheckStatusResult)
	if !ok {
		return fmt.Errorf("StatusGraphCodec: expected []CheckStatusResult, got %T", v)
	}

	if len(results) == 0 {
		fmt.Fprintln(w, "No checks found.")
		return nil
	}

	items := make([]graph.PercentageBarItem, 0, len(results))
	for _, r := range results {
		if r.Success == nil {
			continue
		}
		label := r.Job
		if label == "" {
			label = fmt.Sprintf("check-%d", r.ID)
		}
		items = append(items, graph.PercentageBarItem{
			Name:  label,
			Value: *r.Success * 100,
		})
	}

	if len(items) == 0 {
		fmt.Fprintln(w, "No metric data available for graph rendering.")
		return nil
	}

	opts := graph.DefaultChartOptions()
	return graph.RenderPercentageBars(w, "Synthetic Monitoring Check Success Rate", items, opts)
}

func (c *StatusGraphCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("StatusGraphCodec: decode not supported")
}
