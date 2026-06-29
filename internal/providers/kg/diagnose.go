package kg

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/grafana/gcx/internal/config"
	dsquery "github.com/grafana/gcx/internal/datasources/query"
	"github.com/grafana/gcx/internal/format"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/query/prometheus"
	"github.com/grafana/gcx/internal/style"
	"github.com/grafana/grafana-app-sdk/logging"
	"github.com/grafana/promql-builder/go/promql"
	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"
)

// ---------------------------------------------------------------------------
// Check result types
// ---------------------------------------------------------------------------

// CheckStatus is the outcome of a single diagnostic check.
type CheckStatus string

const (
	CheckPass CheckStatus = "pass"
	CheckFail CheckStatus = "fail"
	CheckWarn CheckStatus = "warn"
	CheckSkip CheckStatus = "skip"
)

// CheckResult is a single diagnostic check outcome.
type CheckResult struct {
	Name           string      `json:"name"`
	Status         CheckStatus `json:"status"`
	Detail         string      `json:"detail,omitempty"`
	Recommendation string      `json:"recommendation,omitempty"`
}

// DiagnoseSummary is the per-check pass/fail tally for a diagnose run.
// Named so functions that derive verdicts from it (e.g.
// pipelineHealthFromSummary) can take it by type rather than coupling to
// an inline anonymous struct.
type DiagnoseSummary struct {
	Total  int `json:"total"`
	Passed int `json:"passed"`
	Failed int `json:"failed"`
	Warned int `json:"warned"`
}

// DiagnoseResult is the full output of the diagnose command.
type DiagnoseResult struct {
	Env       string `json:"env,omitempty"`
	Namespace string `json:"namespace,omitempty"`
	Site      string `json:"site,omitempty"`

	// Orientation is the top-of-output summary that anchors the user in
	// one of the five Entity Graph scenarios. Computed from the same data
	// the per-check goroutines collect; no extra API calls. Omitted from
	// JSON when nil (e.g. legacy callers that bypass the runner).
	Orientation *Orientation `json:"orientation,omitempty"`

	Checks  []CheckResult   `json:"checks"`
	Summary DiagnoseSummary `json:"summary"`
}

func (r *DiagnoseResult) computeSummary() {
	r.Summary.Total = len(r.Checks)
	for _, c := range r.Checks {
		switch c.Status {
		case CheckPass:
			r.Summary.Passed++
		case CheckFail:
			r.Summary.Failed++
		case CheckWarn:
			r.Summary.Warned++
		}
	}
}

// ---------------------------------------------------------------------------
// Command wiring
// ---------------------------------------------------------------------------

type diagnoseOpts struct {
	IO         cmdio.Options
	Scope      scopeFlags
	Datasource string
}

func newDiagnoseCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &diagnoseOpts{}
	cmd := &cobra.Command{
		Use:   "diagnose",
		Short: "Run diagnostic checks on the Knowledge Graph pipeline.",
		Long: `Run diagnostic checks to verify the Knowledge Graph is healthy.

Checks stack status, sanity results, entity counts, scope values,
telemetry drilldown configuration, and recording rule metrics in
Mimir. Use --env to scope checks to a specific environment.

Metric checks require a Prometheus datasource. The datasource UID is
resolved from --datasource, the datasources.prometheus config key, or
auto-discovery. If unavailable, metric checks are skipped.`,
		Example: `  gcx kg diagnose
  gcx kg diagnose --env production
  gcx kg diagnose --env staging --output json
  gcx kg diagnose --datasource grafanacloud-prom`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}
			ctx := cmd.Context()
			cfg, err := loader.LoadGrafanaConfig(ctx)
			if err != nil {
				return err
			}
			client, err := NewClient(cfg)
			if err != nil {
				return err
			}

			// Best-effort Prometheus client for metric checks.
			promClient, datasourceUID := resolvePromClient(ctx, loader, cfg, opts.Datasource, cmd)

			result := runDiagnose(ctx, client, &opts.Scope, promClient, datasourceUID)
			return opts.IO.Encode(cmd.OutOrStdout(), result)
		},
	}

	// Bind scope flags directly to this command.
	cmd.Flags().StringVar(&opts.Scope.env, "env", "", "Environment scope")
	cmd.Flags().StringVar(&opts.Scope.namespace, "namespace", "", "Namespace scope")
	cmd.Flags().StringVar(&opts.Scope.site, "site", "", "Site scope")
	cmd.Flags().StringVarP(&opts.Datasource, "datasource", "d", "", "Prometheus datasource UID (auto-discovered if omitted)")

	// IO flags (--output).
	opts.IO.RegisterCustomCodec("table", &DiagnoseTableCodec{})
	opts.IO.DefaultFormat("table")
	opts.IO.BindFlags(cmd.Flags())

	// Subcommands.
	cmd.AddCommand(newDiagnoseServiceCommand(loader))
	cmd.AddCommand(newDiagnoseLabelsCommand(loader))

	return cmd
}

func newDiagnoseServiceCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &diagnoseOpts{}
	cmd := &cobra.Command{
		Use:   "service NAME",
		Short: "Diagnose a specific service in the Knowledge Graph.",
		Long: `Deep diagnosis for a specific service: entity lookup, relationship
analysis, per-service recording rule checks, and interpreted diagnosis
with suggested next steps.`,
		Example: `  gcx kg diagnose service api-gateway
  gcx kg diagnose service payment-service --env production
  gcx kg diagnose service checkout --env production --namespace default -o json`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}
			ctx := cmd.Context()
			cfg, err := loader.LoadGrafanaConfig(ctx)
			if err != nil {
				return err
			}
			client, err := NewClient(cfg)
			if err != nil {
				return err
			}

			promClient, datasourceUID := resolvePromClient(ctx, loader, cfg, opts.Datasource, cmd)

			result := runServiceDiagnose(ctx, client, args[0], &opts.Scope, promClient, datasourceUID)
			return opts.IO.Encode(cmd.OutOrStdout(), result)
		},
	}

	cmd.Flags().StringVar(&opts.Scope.env, "env", "", "Environment scope")
	cmd.Flags().StringVar(&opts.Scope.namespace, "namespace", "", "Namespace scope")
	cmd.Flags().StringVar(&opts.Scope.site, "site", "", "Site scope")
	cmd.Flags().StringVarP(&opts.Datasource, "datasource", "d", "", "Prometheus datasource UID (auto-discovered if omitted)")

	opts.IO.RegisterCustomCodec("table", &ServiceDiagnoseTableCodec{})
	opts.IO.DefaultFormat("table")
	opts.IO.BindFlags(cmd.Flags())

	return cmd
}

func newDiagnoseLabelsCommand(loader *providers.ConfigLoader) *cobra.Command {
	var datasource string
	ioOpts := cmdio.Options{}
	cmd := &cobra.Command{
		Use:   "labels",
		Short: "Validate the deployment_environment → asserts_env label pipeline.",
		Long: `Check that deployment_environment values in raw metrics are correctly
mapped to asserts_env in recording rule outputs. Identifies unmapped
environments (services that won't appear in Entity Graph) and orphaned
asserts_env values with no deployment_environment source.`,
		Example: `  gcx kg diagnose labels
  gcx kg diagnose labels --datasource grafanacloud-prom
  gcx kg diagnose labels -o json`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := ioOpts.Validate(); err != nil {
				return err
			}
			ctx := cmd.Context()
			cfg, err := loader.LoadGrafanaConfig(ctx)
			if err != nil {
				return err
			}
			kgClient, err := NewClient(cfg)
			if err != nil {
				return err
			}

			promClient, dsUID := resolvePromClient(ctx, loader, cfg, datasource, cmd)

			result := runLabelsDiagnose(ctx, kgClient, promClient, dsUID)
			return ioOpts.Encode(cmd.OutOrStdout(), result)
		},
	}

	cmd.Flags().StringVarP(&datasource, "datasource", "d", "", "Prometheus datasource UID (auto-discovered if omitted)")
	ioOpts.RegisterCustomCodec("table", &LabelsDiagnoseTableCodec{})
	ioOpts.DefaultFormat("table")
	ioOpts.BindFlags(cmd.Flags())

	return cmd
}

// defaultPromDatasourceUID is the conventional datasource UID for the primary
// Prometheus datasource on Grafana Cloud stacks. The Knowledge Graph always
// uses this datasource, so we fall back to it when auto-discovery fails
// (e.g., multiple Prometheus datasources exist and no stack slug is configured).
const defaultPromDatasourceUID = "grafanacloud-prom"

// resolvePromClient creates a Prometheus query client and resolves the
// datasource UID. Falls back to "grafanacloud-prom" (the default Grafana Cloud
// Prometheus datasource) when auto-discovery fails, since the Knowledge Graph
// always reads from the default Prometheus datasource.
func resolvePromClient(ctx context.Context, loader *providers.ConfigLoader, cfg config.NamespacedRESTConfig, flagValue string, cmd *cobra.Command) (*prometheus.Client, string) {
	var cfgCtx *config.Context
	fullCfg, err := loader.LoadFullConfig(ctx)
	if err != nil {
		logging.FromContext(ctx).Warn("could not load full config for datasource resolution", slog.String("error", err.Error()))
	} else {
		cfgCtx = fullCfg.GetCurrentContext()
	}

	var dsUID string
	resolved, err := dsquery.ResolveDatasource(ctx, flagValue, cfgCtx, cfg, "prometheus")
	if err != nil {
		// The KG always uses the default Prometheus datasource. Silently fall back
		// to the conventional Grafana Cloud UID rather than skipping metric checks.
		dsUID = defaultPromDatasourceUID
	} else {
		dsUID = resolved.UID
	}

	promClient, err := prometheus.NewClient(cfg)
	if err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "  note: skipping metric checks (failed to create prometheus client: %v)\n", err)
		return nil, ""
	}

	return promClient, dsUID
}

// ---------------------------------------------------------------------------
// Check runner
// ---------------------------------------------------------------------------

func runDiagnose(ctx context.Context, client *Client, scope *scopeFlags, promClient *prometheus.Client, datasourceUID string) DiagnoseResult {
	result := DiagnoseResult{
		Env:       scope.env,
		Namespace: scope.namespace,
		Site:      scope.site,
	}

	var (
		mu sync.Mutex
		g  errgroup.Group
	)
	addCheck := func(c CheckResult) {
		mu.Lock()
		result.Checks = append(result.Checks, c)
		mu.Unlock()
	}

	// Raw data shared with the Orientation block. Each contributing
	// goroutine fills its own field; computeOrientation reads the
	// composed struct after g.Wait() returns. Series counts default to
	// zero, which is the safe value for absence in the detectors.
	var orientationInput OrientationInput

	// Check 1: Stack status + sanity checks.
	g.Go(func() error {
		checks, enabled := checkStackStatus(ctx, client)
		mu.Lock()
		result.Checks = append(result.Checks, checks...)
		orientationInput.StackEnabled = enabled
		mu.Unlock()
		return nil
	})

	// Check 2: Entity counts.
	g.Go(func() error {
		c, counts := checkEntityCounts(ctx, client)
		mu.Lock()
		result.Checks = append(result.Checks, c)
		orientationInput.EntityCounts = counts
		mu.Unlock()
		return nil
	})

	// Check 3: Scope values.
	g.Go(func() error {
		c, scopes := checkScopeValues(ctx, client)
		mu.Lock()
		result.Checks = append(result.Checks, c)
		orientationInput.Scopes = scopes
		mu.Unlock()
		return nil
	})

	// Check 4: Telemetry configs.
	g.Go(func() error {
		checks := checkTelemetryConfigs(ctx, client)
		mu.Lock()
		result.Checks = append(result.Checks, checks...)
		mu.Unlock()
		return nil
	})

	// Check 5–9: Metric checks (skip if no Prometheus client).
	if promClient != nil && datasourceUID != "" {
		for _, mc := range metricChecks(scope.env, scope.namespace) {
			g.Go(func() error {
				c, series := checkMetric(ctx, promClient, datasourceUID, mc)
				mu.Lock()
				result.Checks = append(result.Checks, c)
				// Capture series counts the Orientation detectors need.
				// metricChecks emits these in a fixed order; match by name.
				switch mc.Name {
				case "Metric: traces_target_info":
					orientationInput.TracesTargetInfoSeries = series
				case "Metric: asserts:relation:calls":
					orientationInput.AssertsRelationCallsSeries = series
				case "Metric: asserts:mixin_workload_job":
					orientationInput.AssertsMixinWorkloadJobSeries = series
				}
				mu.Unlock()
				return nil
			})
		}
		// Check 10+: Edge source gap detection — find metrics that exist but
		// are missing asserts_env (silently dropped by recording rules).
		for _, esc := range edgeSourceGapChecks() {
			g.Go(func() error {
				c := checkEdgeSourceGap(ctx, promClient, datasourceUID, scope.namespace, esc)
				if c != nil {
					addCheck(*c)
				}
				return nil
			})
		}
		// Check N: Trace context propagation — detect the "entities exist
		// but no edges" failure mode where outgoing OTel SDKs aren't
		// injecting traceparent on cross-service calls.
		g.Go(func() error {
			if c := checkTracePropagation(ctx, promClient, datasourceUID, scope.env); c != nil {
				addCheck(*c)
			}
			return nil
		})
	}

	_ = g.Wait() // errors are captured in CheckResults, not returned

	// Stable output order.
	sort.Slice(result.Checks, func(i, j int) bool {
		return checkOrder(result.Checks[i].Name) < checkOrder(result.Checks[j].Name)
	})

	result.computeSummary()

	// Build the Orientation block from the data collected above.
	orient := computeOrientation(orientationInput, scope)
	orient.PipelineHealth = pipelineHealthFromSummary(result.Summary)
	result.Orientation = &orient

	return result
}

// pipelineHealthFromSummary derives the Orientation health verdict from
// the existing pass/warn/fail counts the per-check goroutines produce.
// Kept here (not in orientation.go) because it consumes the DiagnoseSummary
// type defined alongside DiagnoseResult.
func pipelineHealthFromSummary(s DiagnoseSummary) PipelineHealth {
	switch {
	case s.Failed > 0:
		return PipelineFailed
	case s.Warned > 0:
		return PipelineDegraded
	default:
		return PipelineHealthy
	}
}

// checkOrder returns a sort key for deterministic output ordering.
func checkOrder(name string) int {
	order := map[string]int{
		"Stack status":       1,
		"Telemetry: log":     10,
		"Telemetry: trace":   11,
		"Telemetry: profile": 12,
	}
	if v, ok := order[name]; ok {
		return v
	}
	// Sanity checks sort between stack status and entity counts.
	if strings.HasPrefix(name, "Sanity:") {
		return 2
	}
	if name == "Entity counts" {
		return 5
	}
	if name == "Scope values" {
		return 6
	}
	if strings.HasPrefix(name, "Metric:") {
		return 7
	}
	if strings.HasPrefix(name, "Edge source:") {
		return 8
	}
	if name == "Trace context propagation" {
		return 9
	}
	return 50
}

// ---------------------------------------------------------------------------
// Individual checks
// ---------------------------------------------------------------------------

func checkStackStatus(ctx context.Context, client *Client) ([]CheckResult, bool) {
	status, err := client.GetStatus(ctx)
	if err != nil {
		return []CheckResult{{
			Name:           "Stack status",
			Status:         CheckFail,
			Detail:         fmt.Sprintf("API error: %v", err),
			Recommendation: "Verify the Grafana instance is reachable and the Asserts plugin is installed.",
		}}, false
	}

	var results []CheckResult
	enabled := status.Enabled && status.Status == "complete"

	// Main status check.
	if status.Enabled && status.Status == "complete" {
		results = append(results, CheckResult{
			Name:   "Stack status",
			Status: CheckPass,
			Detail: fmt.Sprintf("status=%s, enabled=%t", status.Status, status.Enabled),
		})
	} else {
		results = append(results, CheckResult{
			Name:           "Stack status",
			Status:         CheckFail,
			Detail:         fmt.Sprintf("status=%s, enabled=%t", status.Status, status.Enabled),
			Recommendation: "The Knowledge Graph is not fully active. Check onboarding status in the Asserts app.",
		})
	}

	// Sanity check results from the status response.
	for _, sc := range status.SanityCheckResults {
		c := CheckResult{
			Name: "Sanity: " + sc.CheckName,
		}
		if sc.DataPresent {
			c.Status = CheckPass
			c.Detail = "data present"
		} else {
			c.Status = CheckFail
			c.Detail = "no data"
			c.Recommendation = "This metric sanity check found no data. Verify telemetry is flowing to Mimir."
		}
		// Surface step-level blockers/warnings.
		for _, step := range sc.StepResults {
			if len(step.Blockers) > 0 {
				c.Status = CheckFail
				c.Detail += fmt.Sprintf("; blocker in %q: %s", step.Name, strings.Join(step.Blockers, ", "))
				if step.Troubleshoot != "" {
					c.Recommendation = step.Troubleshoot
				}
			}
			if len(step.Warnings) > 0 {
				if c.Status == CheckPass {
					c.Status = CheckWarn
				}
				c.Detail += fmt.Sprintf("; warning in %q: %s", step.Name, strings.Join(step.Warnings, ", "))
			}
		}
		results = append(results, c)
	}

	return results, enabled
}

// checkEntityCounts returns the user-facing check result plus the raw
// per-type count map so the Orientation block can compute entity totals
// without a second API call. counts is nil on API error.
func checkEntityCounts(ctx context.Context, client *Client) (CheckResult, map[string]int64) {
	now := time.Now()
	counts, err := client.CountEntityTypes(ctx, now.Add(-1*time.Hour).UnixMilli(), now.UnixMilli(), nil)
	if err != nil {
		return CheckResult{
			Name:           "Entity counts",
			Status:         CheckFail,
			Detail:         fmt.Sprintf("API error: %v", err),
			Recommendation: "Failed to retrieve entity counts. Check connectivity to the Asserts API.",
		}, nil
	}

	if len(counts) == 0 {
		return CheckResult{
			Name:           "Entity counts",
			Status:         CheckFail,
			Detail:         "no entity types found",
			Recommendation: "No entities discovered. Verify that traces_target_info or asserts:mixin_workload_job metrics are being produced.",
		}, counts
	}

	var total int64
	var parts []string
	// Sort by type name for stable output.
	types := make([]string, 0, len(counts))
	for t := range counts {
		types = append(types, t)
	}
	sort.Strings(types)
	for _, t := range types {
		cnt := counts[t]
		total += cnt
		parts = append(parts, fmt.Sprintf("%s: %d", t, cnt))
	}

	if total == 0 {
		return CheckResult{
			Name:           "Entity counts",
			Status:         CheckFail,
			Detail:         "all entity type counts are 0",
			Recommendation: "Entity types exist but have no instances. Check that recording rules are producing data and the graph ingestion pipeline is running.",
		}, counts
	}

	return CheckResult{
		Name:   "Entity counts",
		Status: CheckPass,
		Detail: fmt.Sprintf("%d total (%s)", total, strings.Join(parts, ", ")),
	}, counts
}

// checkScopeValues returns the user-facing check result plus the raw
// scopes map so the Orientation block can detect known/unknown scope
// values without a second API call. scopes is nil on API error.
func checkScopeValues(ctx context.Context, client *Client) (CheckResult, map[string][]string) {
	scopes, err := client.ListEntityScopes(ctx, 0, 0)
	if err != nil {
		return CheckResult{
			Name:           "Scope values",
			Status:         CheckFail,
			Detail:         fmt.Sprintf("API error: %v", err),
			Recommendation: "Failed to retrieve scope values. Check connectivity to the Asserts API.",
		}, nil
	}

	if len(scopes) == 0 {
		return CheckResult{
			Name:           "Scope values",
			Status:         CheckWarn,
			Detail:         "no scope dimensions returned",
			Recommendation: "No env/site/namespace values found. Entities may exist without scope labels.",
		}, scopes
	}

	var parts []string
	for _, dim := range []string{"env", "site", "namespace"} {
		if vals, ok := scopes[dim]; ok && len(vals) > 0 {
			parts = append(parts, fmt.Sprintf("%s: [%s]", dim, strings.Join(vals, ", ")))
		}
	}

	if len(parts) == 0 {
		return CheckResult{
			Name:           "Scope values",
			Status:         CheckWarn,
			Detail:         "scope dimensions present but env/site/namespace are empty",
			Recommendation: "The asserts_env label may not be set. Verify that deployment_environment is configured in your OTel SDK and that Mimir relabeling rules map it to asserts_env.",
		}, scopes
	}

	return CheckResult{
		Name:   "Scope values",
		Status: CheckPass,
		Detail: strings.Join(parts, "; "),
	}, scopes
}

func checkTelemetryConfigs(ctx context.Context, client *Client) []CheckResult {
	var (
		results []CheckResult
		mu      sync.Mutex
		g       errgroup.Group
	)

	g.Go(func() error {
		resp, err := client.FetchLogConfigs(ctx)
		c := CheckResult{Name: "Telemetry: log"}
		switch {
		case err != nil:
			c.Status = CheckWarn
			c.Detail = fmt.Sprintf("API error: %v", err)
			c.Recommendation = "Could not fetch log drilldown configs. Log drilldown from entities may not work."
		case len(resp.LogDrilldownConfigs) == 0:
			c.Status = CheckWarn
			c.Detail = "no log drilldown configs"
			c.Recommendation = "No log configs found. Configure a Loki datasource mapping in the Asserts app to enable log drilldown."
		default:
			c.Status = CheckPass
			names := make([]string, 0, len(resp.LogDrilldownConfigs))
			for _, cfg := range resp.LogDrilldownConfigs {
				names = append(names, cfg.Name)
			}
			c.Detail = fmt.Sprintf("%d config(s): %s", len(resp.LogDrilldownConfigs), strings.Join(names, ", "))
		}
		mu.Lock()
		results = append(results, c)
		mu.Unlock()
		return nil
	})

	g.Go(func() error {
		resp, err := client.FetchTraceConfigs(ctx)
		c := CheckResult{Name: "Telemetry: trace"}
		switch {
		case err != nil:
			c.Status = CheckWarn
			c.Detail = fmt.Sprintf("API error: %v", err)
			c.Recommendation = "Could not fetch trace drilldown configs. Trace drilldown from entities may not work."
		case len(resp.TraceDrilldownConfigs) == 0:
			c.Status = CheckWarn
			c.Detail = "no trace drilldown configs"
			c.Recommendation = "No trace configs found. Configure a Tempo datasource mapping in the Asserts app to enable trace drilldown."
		default:
			c.Status = CheckPass
			names := make([]string, 0, len(resp.TraceDrilldownConfigs))
			for _, cfg := range resp.TraceDrilldownConfigs {
				names = append(names, cfg.Name)
			}
			c.Detail = fmt.Sprintf("%d config(s): %s", len(resp.TraceDrilldownConfigs), strings.Join(names, ", "))
		}
		mu.Lock()
		results = append(results, c)
		mu.Unlock()
		return nil
	})

	g.Go(func() error {
		resp, err := client.FetchProfileConfigs(ctx)
		c := CheckResult{Name: "Telemetry: profile"}
		switch {
		case err != nil:
			c.Status = CheckWarn
			c.Detail = fmt.Sprintf("API error: %v", err)
			c.Recommendation = "Could not fetch profile drilldown configs."
		case len(resp.ProfileDrilldownConfigs) == 0:
			c.Status = CheckWarn
			c.Detail = "no profile drilldown configs"
			c.Recommendation = "No profile configs found. This is optional — configure Pyroscope if continuous profiling is available."
		default:
			c.Status = CheckPass
			names := make([]string, 0, len(resp.ProfileDrilldownConfigs))
			for _, cfg := range resp.ProfileDrilldownConfigs {
				names = append(names, cfg.Name)
			}
			c.Detail = fmt.Sprintf("%d config(s): %s", len(resp.ProfileDrilldownConfigs), strings.Join(names, ", "))
		}
		mu.Lock()
		results = append(results, c)
		mu.Unlock()
		return nil
	})

	_ = g.Wait()
	return results
}

// ---------------------------------------------------------------------------
// Metric checks (Phase 2)
// ---------------------------------------------------------------------------

// metricCheckDef defines a single metric presence check.
type metricCheckDef struct {
	Name           string // display name, e.g. "Metric: asserts:relation:calls"
	Query          string // PromQL count() query (scoped: env + namespace, etc.)
	UnscopedQuery  string // optional fallback PromQL count() without env/namespace filters.
	Recommendation string // shown on failure (used when the metric is genuinely absent)
}

// metricChecks returns the metric check definitions, optionally scoped by env and namespace.
func metricChecks(env, namespace string) []metricCheckDef {
	// Build label filters for recording rule metrics (use asserts_env).
	var rrParts []string
	if env != "" {
		rrParts = append(rrParts, fmt.Sprintf(`asserts_env="%s"`, env))
	}
	if namespace != "" {
		rrParts = append(rrParts, fmt.Sprintf(`namespace="%s"`, namespace))
	}
	rrSelector := ""
	if len(rrParts) > 0 {
		rrSelector = "{" + strings.Join(rrParts, ", ") + "}"
	}

	// Build label filters for raw Tempo metrics. Each metric uses different label names:
	// - traces_target_info: deployment_environment (no reliable namespace label)
	// - traces_service_graph_request_total: client_deployment_environment, client_service_namespace
	buildRawSelector := func(envLabel, nsLabel string) string {
		var parts []string
		if env != "" {
			parts = append(parts, fmt.Sprintf(`%s="%s"`, envLabel, env))
		}
		if nsLabel != "" && namespace != "" {
			parts = append(parts, fmt.Sprintf(`%s="%s"`, nsLabel, namespace))
		}
		if len(parts) == 0 {
			return ""
		}
		return "{" + strings.Join(parts, ", ") + "}"
	}

	// Per-check selectors so that `withFallback` can gate the unscoped
	// re-probe on whether *this specific check* actually applies a filter.
	// Without this, e.g. `traces_target_info` (which only ever uses an `env`
	// label) would attach a no-op fallback whenever `--namespace` is set but
	// `--env` is empty: the scoped and unscoped queries would be identical
	// and the reclassification logic in checkMetric would be a no-op
	// round-trip. (See PR #746 review feedback.)
	rawTargetInfoSel := buildRawSelector("deployment_environment", "")
	rawServiceGraphSel := buildRawSelector("client_deployment_environment", "client_service_namespace")

	// withFallback attaches an unscoped fallback only if the scoped query
	// for this check actually carries a label filter. Otherwise the unscoped
	// fallback equals the scoped query and reclassification would never fire.
	withFallback := func(d metricCheckDef, metric, scopedSelector string) metricCheckDef {
		if scopedSelector != "" {
			d.UnscopedQuery = promql.Count(promql.Vector(metric)).String()
		}
		return d
	}

	return []metricCheckDef{
		withFallback(metricCheckDef{
			Name:           "Metric: traces_target_info",
			Query:          fmt.Sprintf("count(traces_target_info%s)", rawTargetInfoSel),
			Recommendation: "Tempo server-side metrics generation may not be enabled, or no traced services are sending telemetry to this stack.",
		}, "traces_target_info", rawTargetInfoSel),
		withFallback(metricCheckDef{
			Name:           "Metric: traces_service_graph_request_total",
			Query:          fmt.Sprintf("count(traces_service_graph_request_total%s)", rawServiceGraphSel),
			Recommendation: "Tempo service graph metrics are not being generated. Enable server-side metrics generation in Tempo, or verify that traced services make inter-service HTTP/gRPC calls.",
		}, "traces_service_graph_request_total", rawServiceGraphSel),
		withFallback(metricCheckDef{
			Name:           "Metric: asserts:mixin_workload_job",
			Query:          fmt.Sprintf("count(asserts:mixin_workload_job%s)", rrSelector),
			Recommendation: "The entity discovery recording rule is not producing data. This metric is central to how services appear in Entity Graph. Verify that asserts_env is set (check deployment_environment in your OTel config) and that 3po recording rules are installed.",
		}, "asserts:mixin_workload_job", rrSelector),
		withFallback(metricCheckDef{
			Name:           "Metric: asserts:relation:calls",
			Query:          fmt.Sprintf("count(asserts:relation:calls%s)", rrSelector),
			Recommendation: "No CALLS edge metrics found. This means Entity Graph will show services with no connections. Check that traces_service_graph_request_total exists and that the asserts_env relabeling pipeline is working.",
		}, "asserts:relation:calls", rrSelector),
		withFallback(metricCheckDef{
			Name:           "Metric: asserts:request:rate5m",
			Query:          fmt.Sprintf("count(asserts:request:rate5m%s)", rrSelector),
			Recommendation: "Request rate KPI recording rule is not producing data. Service KPIs (request rate, error ratio, latency) may not display correctly.",
		}, "asserts:request:rate5m", rrSelector),
	}
}

// ---------------------------------------------------------------------------
// Edge source gap detection
// ---------------------------------------------------------------------------

// edgeSourceGapDef defines a metric to check for the asserts_env gap pattern:
// metric exists but has no asserts_env label, so recording rules silently drop it.
type edgeSourceGapDef struct {
	Name       string // display name
	Metric     string // metric name to check
	SourceType string // human description of the source
}

// edgeSourceGapChecks returns the edge source gap check definitions.
// Namespace scoping happens in checkEdgeSourceGap where the PromQL is built.
func edgeSourceGapChecks() []edgeSourceGapDef {
	return []edgeSourceGapDef{
		{Name: "istio_requests_total", Metric: "istio_requests_total", SourceType: "Istio service mesh"},
		{Name: "http_server_requests_seconds_count", Metric: "http_server_requests_seconds_count", SourceType: "Spring Boot Actuator"},
		{Name: "nginx_ingress_controller_requests", Metric: "nginx_ingress_controller_requests", SourceType: "nginx ingress"},
		{Name: "kafka_server_brokertopicmetrics_messagesin_total", Metric: "kafka_server_brokertopicmetrics_messagesin_total", SourceType: "Kafka (JMX)"},
		{Name: "redis_commands_total", Metric: "redis_commands_total", SourceType: "Redis exporter"},
		{Name: "pg_stat_activity_count", Metric: "pg_stat_activity_count", SourceType: "PostgreSQL exporter"},
		{Name: "mysql_global_status_queries", Metric: "mysql_global_status_queries", SourceType: "MySQL exporter"},
		{Name: "rabbitmq_queue_messages", Metric: "rabbitmq_queue_messages", SourceType: "RabbitMQ"},
		{Name: "elasticsearch_indices_docs", Metric: "elasticsearch_indices_docs", SourceType: "Elasticsearch exporter"},
	}
}

// checkEdgeSourceGap checks if a metric exists but is missing asserts_env.
// Returns nil if the metric doesn't exist at all (nothing to report).
// Returns a CheckResult only when data exists but asserts_env is missing.
// When deployment_environment IS present but asserts_env isn't, the
// recommendation points to the Asserts onboarding UI rather than suggesting
// custom relabeling rules.
func checkEdgeSourceGap(ctx context.Context, client *prometheus.Client, datasourceUID, namespace string, def edgeSourceGapDef) *CheckResult {
	// Build an optional namespace selector applied to every probe query.
	nsSelector := ""
	if namespace != "" {
		nsSelector = fmt.Sprintf(`namespace="%s"`, namespace)
	}
	// joinSelectors builds {sel1,sel2,...}, dropping empty parts.
	joinSelectors := func(parts ...string) string {
		nonEmpty := parts[:0]
		for _, p := range parts {
			if p != "" {
				nonEmpty = append(nonEmpty, p)
			}
		}
		if len(nonEmpty) == 0 {
			return ""
		}
		return "{" + strings.Join(nonEmpty, ",") + "}"
	}

	// First: does the metric exist at all?
	existsResp, err := client.Query(ctx, datasourceUID, prometheus.QueryRequest{
		Query: fmt.Sprintf("count(%s%s)", def.Metric, joinSelectors(nsSelector)),
	})
	if err != nil || len(existsResp.Data.Result) == 0 {
		return nil // metric doesn't exist — nothing to check
	}

	// Second: does it have asserts_env?
	withEnvResp, err := client.Query(ctx, datasourceUID, prometheus.QueryRequest{
		Query: fmt.Sprintf(`count(%s%s)`, def.Metric, joinSelectors(`asserts_env!=""`, nsSelector)),
	})
	if err != nil {
		return nil
	}

	existsCount := extractInstantValue(existsResp.Data.Result[0])

	if len(withEnvResp.Data.Result) > 0 {
		// Has asserts_env — no gap
		return nil
	}

	// Gap found: metric exists but has no asserts_env.
	// Check if deployment_environment is present — this changes the recommendation.
	hasDepEnv := false
	depEnvResp, err := client.Query(ctx, datasourceUID, prometheus.QueryRequest{
		Query: fmt.Sprintf(`count(%s%s)`, def.Metric, joinSelectors(`deployment_environment!=""`, nsSelector)),
	})
	if err == nil && len(depEnvResp.Data.Result) > 0 {
		hasDepEnv = true
	}

	var recommendation string
	if hasDepEnv {
		recommendation = fmt.Sprintf(
			"%s metrics have deployment_environment but not asserts_env. "+
				"The Mimir relabeling rules are not mapping deployment_environment to asserts_env for this metric. "+
				"Go to the Asserts app → Configuration → Connect Environment → Prometheus and set the environment "+
				"label to deployment_environment. This tells the relabeling pipeline to derive asserts_env from "+
				"deployment_environment on all incoming metrics.",
			def.SourceType)
	} else {
		recommendation = fmt.Sprintf(
			"%s metrics are present in Mimir but missing both asserts_env and deployment_environment. "+
				"Ensure your scrape pipeline adds deployment_environment to these metrics (e.g. via "+
				"prometheus.relabel in Alloy), then verify that the Asserts app → Configuration → Connect "+
				"Environment → Prometheus is set to deployment_environment.",
			def.SourceType)
	}

	detail := fmt.Sprintf("%s has %s series but none with asserts_env", def.Name, existsCount)
	if hasDepEnv {
		detail += " (deployment_environment IS present — onboarding config may need updating)"
	} else {
		detail += " — recording rules will ignore this data"
	}

	return &CheckResult{
		Name:           "Edge source: " + def.SourceType,
		Status:         CheckWarn,
		Detail:         detail,
		Recommendation: recommendation,
	}
}

// checkMetric runs a single PromQL instant query and returns a check result
// based on whether the query returns any series.
//
// When the scoped query returns zero results and an UnscopedQuery is
// available, checkMetric re-probes the metric without the env/namespace
// filter. If the unscoped probe finds data, the result is reclassified
// from FAIL ("no data") to WARN ("present but doesn't match the scope")
// with a label-pipeline-investigation hint. This avoids the common
// false-negative pattern where a scoped query masks data that exists
// under a different env label value (e.g. the raw metric carries
// deployment_environment with one value while asserts_env is derived
// from a different source label downstream).
//
// The second return value is the parsed series count (0 on error or
// no-data). Callers that don't need it can discard it; the Orientation
// block uses it as one of its detector inputs.
func checkMetric(ctx context.Context, client *prometheus.Client, datasourceUID string, def metricCheckDef) (CheckResult, int64) {
	resp, err := client.Query(ctx, datasourceUID, prometheus.QueryRequest{
		Query: def.Query,
	})
	if err != nil {
		return CheckResult{
			Name:           def.Name,
			Status:         CheckWarn,
			Detail:         fmt.Sprintf("query error: %v", err),
			Recommendation: "Could not execute PromQL query. Check Prometheus datasource connectivity.",
		}, 0
	}

	if len(resp.Data.Result) == 0 {
		// Re-probe without the env/namespace filter. If the unscoped
		// probe also finds nothing, the metric is genuinely absent.
		// If it finds data, the scoped filter is masking real data —
		// almost always a label-mapping issue, not a missing-data issue.
		if def.UnscopedQuery != "" {
			unscoped, uerr := client.Query(ctx, datasourceUID, prometheus.QueryRequest{
				Query: def.UnscopedQuery,
			})
			if uerr == nil && len(unscoped.Data.Result) > 0 {
				count := extractInstantValue(unscoped.Data.Result[0])
				return CheckResult{
					Name:   def.Name,
					Status: CheckWarn,
					Detail: fmt.Sprintf(
						"metric exists (%s series unscoped) but no series match the requested env/namespace scope",
						count),
					Recommendation: "The metric is flowing but doesn't carry the env/namespace label value you scoped to. " +
						"This usually means the Asserts label-mapping pipeline derives asserts_env from a different source " +
						"label (e.g. cluster or namespace) than the raw metric's deployment_environment. " +
						"Run: gcx metrics labels --label deployment_environment ; gcx metrics labels --label asserts_env " +
						"and cross-reference to identify which value the data actually carries.",
				}, 0
			}
		}
		return CheckResult{
			Name:           def.Name,
			Status:         CheckFail,
			Detail:         "no data",
			Recommendation: def.Recommendation,
		}, 0
	}

	// Extract the count value from the instant query result.
	count := extractInstantValue(resp.Data.Result[0])
	series, _ := strconv.ParseInt(count, 10, 64)
	return CheckResult{
		Name:   def.Name,
		Status: CheckPass,
		Detail: count + " series",
	}, series
}

// extractInstantValue pulls the scalar value from an instant query sample.
func extractInstantValue(s prometheus.Sample) string {
	if len(s.Value) >= 2 {
		if v, ok := s.Value[1].(string); ok {
			// Try to format as integer if it's a whole number.
			if f, err := strconv.ParseFloat(v, 64); err == nil && f == float64(int64(f)) {
				return strconv.FormatInt(int64(f), 10)
			}
			return v
		}
	}
	return "?"
}

// ---------------------------------------------------------------------------
// Trace context propagation detection
// ---------------------------------------------------------------------------

// checkTracePropagation detects the "entities exist but no edges" failure
// mode (canonical Entity Graph problem #3). The telemetry signature is:
//
//   - traces_target_info{deployment_environment=ENV} > 0   (services exist)
//   - traces_service_graph_request_total{server_deployment_environment=ENV,
//     client!="user"} == 0                                  (no real edges)
//   - traces_service_graph_request_total{server_deployment_environment=ENV,
//     client="user"} > 0                                    (only phantom edges)
//
// Tempo's metrics generator synthesizes client="user" edges for SERVER spans
// that arrive with no incoming traceparent header — the unambiguous fingerprint
// of broken outgoing trace context propagation on the upstream service.
//
// Returns nil when env is unset (the check is scoped per-env), when target
// info is empty (telemetry isn't flowing yet — let earlier checks flag that),
// or when real edges exist (no problem to report).
func checkTracePropagation(ctx context.Context, client *prometheus.Client, datasourceUID, env string) *CheckResult {
	if env == "" {
		return nil
	}

	// Gate 1: services must be emitting telemetry for this env.
	targetResp, err := client.Query(ctx, datasourceUID, prometheus.QueryRequest{
		Query: fmt.Sprintf(`count(traces_target_info{deployment_environment="%s"})`, env),
	})
	if err != nil || len(targetResp.Data.Result) == 0 {
		// No telemetry flowing — earlier checks (or the absence of any check
		// passing) will surface this. We don't claim "propagation is broken"
		// when there's no telemetry to propagate.
		return nil
	}

	// Gate 2: are there any real (non-phantom) service-to-service edges?
	realEdgesResp, err := client.Query(ctx, datasourceUID, prometheus.QueryRequest{
		Query: fmt.Sprintf(
			`count(traces_service_graph_request_total{server_deployment_environment="%s",client!="user"})`,
			env),
	})
	if err != nil {
		return nil
	}
	if len(realEdgesResp.Data.Result) > 0 {
		// Real edges exist — propagation is fine for this env.
		return nil
	}

	// Gate 3: are there phantom edges? Their presence confirms data is flowing
	// to the metrics generator but lacks incoming context.
	phantomResp, err := client.Query(ctx, datasourceUID, prometheus.QueryRequest{
		Query: fmt.Sprintf(
			`count(traces_service_graph_request_total{server_deployment_environment="%s",client="user"})`,
			env),
	})
	if err != nil || len(phantomResp.Data.Result) == 0 {
		// No phantom edges either. This is a different failure mode (e.g. no
		// SERVER spans at all yet). Don't report a propagation issue here.
		return nil
	}

	return &CheckResult{
		Name:   "Trace context propagation",
		Status: CheckFail,
		Detail: fmt.Sprintf(
			"services emit telemetry for env %q but only phantom client=\"user\" edges exist — no real inter-service edges",
			env),
		Recommendation: "Outgoing HTTP calls aren't carrying traceparent headers, so the metrics generator can't link the spans into edges. " +
			"Check the OTel SDK config on your services. Common causes: " +
			"(1) HTTP-client auto-instrumentation is disabled or not installed — " +
			"Python: `OTEL_PYTHON_DISABLED_INSTRUMENTATIONS` is a common culprit, or the relevant `opentelemetry-instrumentation-*` package " +
			"(urllib, urllib3, requests) isn't installed; " +
			"Node.js: the `@opentelemetry/instrumentation-*` registration is missing; " +
			"Go: outgoing calls must be wired through `otelhttp.Transport` (auto-instrumentation does not apply). " +
			"(2) The application bypasses the instrumented HTTP client (raw socket calls, a third-party SDK using its own transport). " +
			"(3) No `TracerProvider` was registered, so no context is tracked at all — verify with `gcx traces query` that received spans have a parent. " +
			"Once outgoing propagation is fixed, expect `traces_service_graph_request_total{client!=\"user\"}` to populate within 1–2 minutes " +
			"and `asserts:relation:calls` to follow ~5–10 minutes after that.",
	}
}

// ---------------------------------------------------------------------------
// Text codec for human-readable output
// ---------------------------------------------------------------------------

// DiagnoseTableCodec renders DiagnoseResult as a human-readable table.
type DiagnoseTableCodec struct{}

func (c *DiagnoseTableCodec) Format() format.Format { return "table" }

func (c *DiagnoseTableCodec) Encode(w io.Writer, v any) error {
	result, ok := v.(DiagnoseResult)
	if !ok {
		return errors.New("invalid data type for table codec: expected DiagnoseResult")
	}

	// Orientation block goes above the per-check table.
	if result.Orientation != nil {
		renderOrientation(w, *result.Orientation, result.Env, result.Namespace, result.Site)
	}

	t := style.NewTable("CHECK", "STATUS", "DETAIL")
	for _, check := range result.Checks {
		t.Row(check.Name, strings.ToUpper(string(check.Status)), check.Detail)
	}
	if err := t.Render(w); err != nil {
		return err
	}

	// Print recommendations for failed/warned checks below the table.
	var recs []string
	for _, check := range result.Checks {
		if check.Recommendation != "" && check.Status != CheckPass {
			recs = append(recs, fmt.Sprintf("  %s: %s", check.Name, check.Recommendation))
		}
	}
	if len(recs) > 0 {
		fmt.Fprintln(w)
		fmt.Fprintln(w, "Recommendations:")
		for _, r := range recs {
			fmt.Fprintln(w, r)
		}
	}

	fmt.Fprintln(w)
	fmt.Fprintf(w, "%d/%d checks passed", result.Summary.Passed, result.Summary.Total)
	if result.Summary.Failed > 0 {
		fmt.Fprintf(w, ", %d failed", result.Summary.Failed)
	}
	if result.Summary.Warned > 0 {
		fmt.Fprintf(w, ", %d warning(s)", result.Summary.Warned)
	}
	fmt.Fprintln(w)

	return nil
}

func (c *DiagnoseTableCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("table format does not support decoding")
}

// renderOrientation writes the Orientation block above the per-check table.
// Layout, in order:
//   - One-line pipeline health verdict and scope hint.
//   - Entity overview: total entities (excluding meta types) and the
//     traced-services-over-total ratio.
//   - Scope overview: env list (with "none" called out when present) and
//     namespace count.
//   - Matched scenarios — one block per scenario, naming it verbatim,
//     with reasoning and concrete next commands.
//   - Starting points (only when no scope flag was set): three ways to
//     scope further, with runtime-substituted context.
//   - When scenario 4 is not detected at stack level, a "see also" hint
//     points the user at the per-env comparison workflow.
//
// All numeric values come from the live OrientationInput. No authored
// thresholds.
func renderOrientation(w io.Writer, o Orientation, env, namespace, site string) {
	// Header: one-line stack health verdict and scope. Build the scope hint
	// from whichever dimensions are actually set (not just env) so multi-
	// dimension filters (--namespace foo without --env) render correctly.
	scopeHint := "(unscoped: --env / --namespace / --site not set)"
	if o.Scope.FilterSet {
		var parts []string
		if env != "" {
			parts = append(parts, "env="+env)
		}
		if namespace != "" {
			parts = append(parts, "namespace="+namespace)
		}
		if site != "" {
			parts = append(parts, "site="+site)
		}
		scopeHint = "(" + strings.Join(parts, ", ") + ")"
	}
	fmt.Fprintf(w, "Pipeline health: %s  %s\n", o.PipelineHealth, scopeHint)

	// Entity overview. Entity counts (Total, TotalServiceCount) come from
	// the stack-wide CountEntityTypes call; TracedServiceCount comes from a
	// scope-filtered metric check. When a scope filter is set we label the
	// stack-wide numbers as such and put the scoped trace count on its own
	// clause so the two scopes can't be misread as a single ratio.
	if o.Scope.FilterSet {
		fmt.Fprintf(w, "Entity overview (stack-wide): %d entities", o.EntityOverview.Total)
		if o.EntityOverview.TotalServiceCount > 0 {
			fmt.Fprintf(w, " — %d Service entities", o.EntityOverview.TotalServiceCount)
		}
		fmt.Fprintf(w, ". Tracing in scope: %d service(s) emit traces.\n", o.EntityOverview.TracedServiceCount)
	} else {
		fmt.Fprintf(w, "Entity overview: %d entities", o.EntityOverview.Total)
		if o.EntityOverview.TotalServiceCount > 0 {
			fmt.Fprintf(w, " — %d Service entities, of which %d emit traces",
				o.EntityOverview.TotalServiceCount,
				o.EntityOverview.TracedServiceCount)
		}
		fmt.Fprintln(w, ".")
	}

	// Scope overview. (The NoneBucketPresent callout that previously rendered
	// here was removed alongside the inert NoneBucketHasEntities producer;
	// see ScopeSummary doc.)
	if len(o.Scope.EnvsKnown) > 0 {
		fmt.Fprintf(w, "Scope: %d env(s) known", len(o.Scope.EnvsKnown))
		if n := len(o.Scope.NamespacesKnown); n > 0 {
			fmt.Fprintf(w, "; %d namespace(s) known", n)
		}
		fmt.Fprintln(w, ".")
	}

	// Matched scenarios (already ordered high → medium by computeOrientation).
	if len(o.MatchedScenarios) > 0 {
		fmt.Fprintln(w)
		for _, s := range o.MatchedScenarios {
			fmt.Fprintf(w, "→ Scenario matched: %q (%s confidence)\n", s.Label, s.Confidence)
			if s.Reasoning != "" {
				fmt.Fprintf(w, "  %s\n", s.Reasoning)
			}
			if len(s.NextCommands) > 0 {
				fmt.Fprintln(w, "  Next:")
				for _, cmd := range s.NextCommands {
					fmt.Fprintf(w, "    %s\n", cmd)
				}
			}
		}
	} else {
		// No scenario matched. Tailor the "no match" line to pipeline health
		// — don't say "looks healthy" when checks below the orientation are
		// failing.
		fmt.Fprintln(w)
		switch o.PipelineHealth {
		case PipelineHealthy:
			fmt.Fprintln(w, "→ Stack looks healthy. For per-service investigation:")
			fmt.Fprintln(w, "    gcx kg diagnose service NAME --env ENV")
		case PipelineDegraded:
			fmt.Fprintln(w, "→ No top-level scenario matched, but some checks reported warnings.")
			fmt.Fprintln(w, "  See the table below for details, or run:")
			fmt.Fprintln(w, "    gcx kg diagnose service NAME --env ENV")
		case PipelineFailed:
			fmt.Fprintln(w, "→ No top-level scenario matched, but some checks failed.")
			fmt.Fprintln(w, "  See the table and recommendations below for details.")
		}
	}

	// Starting points (only shown when no scope flag is set).
	if len(o.StartingPoints) > 0 {
		fmt.Fprintln(w)
		fmt.Fprintln(w, "Ways to scope this further:")
		for _, p := range o.StartingPoints {
			if p.Context != "" {
				fmt.Fprintf(w, "  • %s (%s):\n      %s\n", p.Label, p.Context, p.Command)
			} else {
				fmt.Fprintf(w, "  • %s:\n      %s\n", p.Label, p.Command)
			}
		}
	}

	// Scenario 4 "see also" hint (deferred detector).
	fmt.Fprintln(w)
	fmt.Fprintln(w, "See also: to detect disconnected clusters across environments,")
	fmt.Fprintln(w, "re-run scoped to each --env separately and compare.")
	fmt.Fprintln(w)
}
