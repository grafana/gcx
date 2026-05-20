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

// DiagnoseResult is the full output of the diagnose command.
type DiagnoseResult struct {
	Env     string        `json:"env,omitempty"`
	Checks  []CheckResult `json:"checks"`
	Summary struct {
		Total  int `json:"total"`
		Passed int `json:"passed"`
		Failed int `json:"failed"`
		Warned int `json:"warned"`
	} `json:"summary"`
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
	opts.IO.RegisterCustomCodec("text", &DiagnoseTextCodec{})
	opts.IO.DefaultFormat("text")
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

	opts.IO.RegisterCustomCodec("text", &ServiceDiagnoseTextCodec{})
	opts.IO.DefaultFormat("text")
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
	ioOpts.RegisterCustomCodec("text", &LabelsDiagnoseTextCodec{})
	ioOpts.DefaultFormat("text")
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
	result := DiagnoseResult{Env: scope.env}

	var (
		mu sync.Mutex
		g  errgroup.Group
	)
	addCheck := func(c CheckResult) {
		mu.Lock()
		result.Checks = append(result.Checks, c)
		mu.Unlock()
	}

	// Check 1: Stack status + sanity checks.
	g.Go(func() error {
		checks := checkStackStatus(ctx, client)
		mu.Lock()
		result.Checks = append(result.Checks, checks...)
		mu.Unlock()
		return nil
	})

	// Check 2: Entity counts.
	g.Go(func() error {
		addCheck(checkEntityCounts(ctx, client))
		return nil
	})

	// Check 3: Scope values.
	g.Go(func() error {
		addCheck(checkScopeValues(ctx, client))
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
				addCheck(checkMetric(ctx, promClient, datasourceUID, mc))
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
	}

	_ = g.Wait() // errors are captured in CheckResults, not returned

	// Stable output order.
	sort.Slice(result.Checks, func(i, j int) bool {
		return checkOrder(result.Checks[i].Name) < checkOrder(result.Checks[j].Name)
	})

	result.computeSummary()
	return result
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
	return 50
}

// ---------------------------------------------------------------------------
// Individual checks
// ---------------------------------------------------------------------------

func checkStackStatus(ctx context.Context, client *Client) []CheckResult {
	status, err := client.GetStatus(ctx)
	if err != nil {
		return []CheckResult{{
			Name:           "Stack status",
			Status:         CheckFail,
			Detail:         fmt.Sprintf("API error: %v", err),
			Recommendation: "Verify the Grafana instance is reachable and the Asserts plugin is installed.",
		}}
	}

	var results []CheckResult

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

	return results
}

func checkEntityCounts(ctx context.Context, client *Client) CheckResult {
	now := time.Now()
	counts, err := client.CountEntityTypes(ctx, now.Add(-1*time.Hour).UnixMilli(), now.UnixMilli(), nil)
	if err != nil {
		return CheckResult{
			Name:           "Entity counts",
			Status:         CheckFail,
			Detail:         fmt.Sprintf("API error: %v", err),
			Recommendation: "Failed to retrieve entity counts. Check connectivity to the Asserts API.",
		}
	}

	if len(counts) == 0 {
		return CheckResult{
			Name:           "Entity counts",
			Status:         CheckFail,
			Detail:         "no entity types found",
			Recommendation: "No entities discovered. Verify that traces_target_info or asserts:mixin_workload_job metrics are being produced.",
		}
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
		}
	}

	return CheckResult{
		Name:   "Entity counts",
		Status: CheckPass,
		Detail: fmt.Sprintf("%d total (%s)", total, strings.Join(parts, ", ")),
	}
}

func checkScopeValues(ctx context.Context, client *Client) CheckResult {
	scopes, err := client.ListEntityScopes(ctx)
	if err != nil {
		return CheckResult{
			Name:           "Scope values",
			Status:         CheckFail,
			Detail:         fmt.Sprintf("API error: %v", err),
			Recommendation: "Failed to retrieve scope values. Check connectivity to the Asserts API.",
		}
	}

	if len(scopes) == 0 {
		return CheckResult{
			Name:           "Scope values",
			Status:         CheckWarn,
			Detail:         "no scope dimensions returned",
			Recommendation: "No env/site/namespace values found. Entities may exist without scope labels.",
		}
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
		}
	}

	return CheckResult{
		Name:   "Scope values",
		Status: CheckPass,
		Detail: strings.Join(parts, "; "),
	}
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
	Query          string // PromQL count() query
	Recommendation string // shown on failure
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

	return []metricCheckDef{
		{
			Name:           "Metric: traces_target_info",
			Query:          fmt.Sprintf("count(traces_target_info%s)", buildRawSelector("deployment_environment", "")),
			Recommendation: "Tempo server-side metrics generation may not be enabled, or no traced services are sending telemetry to this stack.",
		},
		{
			Name:           "Metric: traces_service_graph_request_total",
			Query:          fmt.Sprintf("count(traces_service_graph_request_total%s)", buildRawSelector("client_deployment_environment", "client_service_namespace")),
			Recommendation: "Tempo service graph metrics are not being generated. Enable server-side metrics generation in Tempo, or verify that traced services make inter-service HTTP/gRPC calls.",
		},
		{
			Name:           "Metric: asserts:mixin_workload_job",
			Query:          fmt.Sprintf("count(asserts:mixin_workload_job%s)", rrSelector),
			Recommendation: "The entity discovery recording rule is not producing data. This metric is central to how services appear in Entity Graph. Verify that asserts_env is set (check deployment_environment in your OTel config) and that 3po recording rules are installed.",
		},
		{
			Name:           "Metric: asserts:relation:calls",
			Query:          fmt.Sprintf("count(asserts:relation:calls%s)", rrSelector),
			Recommendation: "No CALLS edge metrics found. This means Entity Graph will show services with no connections. Check that traces_service_graph_request_total exists and that the asserts_env relabeling pipeline is working.",
		},
		{
			Name:           "Metric: asserts:request:rate5m",
			Query:          fmt.Sprintf("count(asserts:request:rate5m%s)", rrSelector),
			Recommendation: "Request rate KPI recording rule is not producing data. Service KPIs (request rate, error ratio, latency) may not display correctly.",
		},
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
func checkMetric(ctx context.Context, client *prometheus.Client, datasourceUID string, def metricCheckDef) CheckResult {
	resp, err := client.Query(ctx, datasourceUID, prometheus.QueryRequest{
		Query: def.Query,
	})
	if err != nil {
		return CheckResult{
			Name:           def.Name,
			Status:         CheckWarn,
			Detail:         fmt.Sprintf("query error: %v", err),
			Recommendation: "Could not execute PromQL query. Check Prometheus datasource connectivity.",
		}
	}

	if len(resp.Data.Result) == 0 {
		return CheckResult{
			Name:           def.Name,
			Status:         CheckFail,
			Detail:         "no data",
			Recommendation: def.Recommendation,
		}
	}

	// Extract the count value from the instant query result.
	count := extractInstantValue(resp.Data.Result[0])
	return CheckResult{
		Name:   def.Name,
		Status: CheckPass,
		Detail: count + " series",
	}
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
// Text codec for human-readable output
// ---------------------------------------------------------------------------

// DiagnoseTextCodec renders DiagnoseResult as a human-readable table.
type DiagnoseTextCodec struct{}

func (c *DiagnoseTextCodec) Format() format.Format { return "text" }

func (c *DiagnoseTextCodec) Encode(w io.Writer, v any) error {
	result, ok := v.(DiagnoseResult)
	if !ok {
		return errors.New("invalid data type for text codec: expected DiagnoseResult")
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

func (c *DiagnoseTextCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("text format does not support decoding")
}
