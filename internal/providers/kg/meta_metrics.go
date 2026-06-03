package kg

import (
	"fmt"
	"sort"
	"strings"

	"github.com/spf13/cobra"
)

// ---------------------------------------------------------------------------
// `gcx kg meta metrics`
// ---------------------------------------------------------------------------
//
// Metrics-signal equivalent of the log/trace/profile drilldown configs
// (types.go + commands.go): maps a KG entity onto the Prometheus/Mimir selector
// needed to query its `asserts:*` KPI recording rules.
//
// AUTHORITATIVE SOURCE — this encodes the "Prometheus Query Construction Guide"
// from the asserts-app-plugin:
//   asserts-app-plugin/src/features/Assertions/hooks/useProvideAssistantContext.ts
//
// TWO STRUCTURAL DIFFERENCES from log/trace/profile drilldown configs:
//
//  1. No server endpoint. The Asserts API serves /v2/config/{log,trace,profile}
//     but /v2/config/metric returns HTTP 404. So this is shipped static, not
//     fetched. Consequence: no server-provided dataSourceUid — the asserts
//     metrics live in a specific Mimir the entity does not advertise, so
//     `kg entities kpi` should EMIT selectors and let the caller pass `-d`.
//
//  2. Not match/entity-type keyed. The guide is uniform across entity types and
//     instead branches on METRIC CLASS (request vs resource), each with its own
//     identity-label priority order. So we model classes, not per-type Match
//     blocks — faithfulness to the guide over shape-symmetry with the siblings.
//
// GUIDE STEP 3 selector rules, encoded below:
//   - scope (always): asserts_env, asserts_site, namespace (namespace if present)
//   - request metrics: job (required) + service (if the property exists)
//   - resource metrics: workload (preferred) else job
//   - always wrap in an aggregation (sum/avg); NEVER apply rate() — these are
//     pre-computed gauges.

// MetricClass selects the identity-label construction profile (guide STEP 3).
type MetricClass string

const (
	// MetricClassRequest: rate/latency/error → job (required) + service (optional).
	MetricClassRequest MetricClass = "request"
	// MetricClassResource: cpu/memory → workload (preferred) else job.
	MetricClassResource MetricClass = "resource"
)

// MetricKPI is one entry of the guide's pre-defined `asserts:` catalog (STEP 1).
type MetricKPI struct {
	Intent string `json:"intent"` // user-facing intent, e.g. "p99 latency", "cpu usage"
	Metric string `json:"metric"` // metric name, e.g. "asserts:latency:p99" or "asserts:resource"
	// FixedSelector are mandatory label matchers baked into the metric itself
	// (only asserts:resource needs one: asserts_resource_type).
	FixedSelector map[string]string `json:"fixedSelector,omitempty"`
	Class         MetricClass       `json:"class"`
	Unit          string            `json:"unit"`
	// Agg is the suggested default aggregation. The guide mandates "an
	// appropriate aggregation (sum or avg)"; this is the sensible default per
	// metric, overridable by the caller.
	Agg string `json:"agg"`
}

// assertsKPICatalog is STEP 1 of the guide, verbatim (incl. the per-second /
// pre-computed-gauge semantics — do NOT wrap these in rate()).
//
//nolint:gochecknoglobals // static lookup table for the asserts metric guide
var assertsKPICatalog = []MetricKPI{
	{Intent: "request rate", Metric: "asserts:request:rate5m", Class: MetricClassRequest, Unit: "req/s", Agg: "sum"},
	{Intent: "request error ratio", Metric: "asserts:error:ratio", Class: MetricClassRequest, Unit: "ratio", Agg: "avg"},
	{Intent: "average latency", Metric: "asserts:latency:average", Class: MetricClassRequest, Unit: "s", Agg: "avg"},
	{Intent: "p95 latency", Metric: "asserts:latency:p95", Class: MetricClassRequest, Unit: "s", Agg: "avg"},
	{Intent: "p99 latency", Metric: "asserts:latency:p99", Class: MetricClassRequest, Unit: "s", Agg: "avg"},
	// asserts:resource is normalized utilization (0-1); asserts:resource:usage is
	// the raw amount (cpu in cores, memory in bytes). Same selector, different unit.
	// Only cpu/memory are listed; other resource types exist — see metricNotes.
	{Intent: "cpu utilization", Metric: "asserts:resource", FixedSelector: map[string]string{"asserts_resource_type": "cpu:usage"}, Class: MetricClassResource, Unit: "ratio (1.0=100%)", Agg: "avg"},
	{Intent: "cpu usage (cores)", Metric: "asserts:resource:usage", FixedSelector: map[string]string{"asserts_resource_type": "cpu:usage"}, Class: MetricClassResource, Unit: "cores", Agg: "avg"},
	{Intent: "memory utilization", Metric: "asserts:resource", FixedSelector: map[string]string{"asserts_resource_type": "memory:usage"}, Class: MetricClassResource, Unit: "ratio (1.0=100%)", Agg: "avg"},
	{Intent: "memory usage (bytes)", Metric: "asserts:resource:usage", FixedSelector: map[string]string{"asserts_resource_type": "memory:usage"}, Class: MetricClassResource, Unit: "bytes", Agg: "avg"},
}

// scopeToMetricLabel maps the KG entity scope to Prometheus labels. The scope
// keys (env, site, namespace) match the entity's scope object and the renamed
// schema properties (processGraphSchema renames scope_env→env, etc.). Applied to
// every selector regardless of metric class (guide STEP 3: "ALWAYS use entity
// scope filters"). namespace is included only when the entity has one.
//
//nolint:gochecknoglobals // static lookup table for the asserts metric guide
var scopeToMetricLabel = []struct{ Prop, Label string }{
	{"env", "asserts_env"},
	{"site", "asserts_site"},
	{"namespace", "namespace"},
}

// identityByClass documents the entity-property → label priority order per class
// (guide STEP 3). Ordered: required/preferred first. This is the declarative view
// surfaced by `kg meta metrics`, consumed when building a selector for an entity.
//
// NB on resource metrics: prefer workload. cpu/memory may be cAdvisor-sourced,
// and when they are their job is the scrape job (e.g. kube-system/cadvisor), not
// asserts/<service> — so the service job won't match. workload matches
// regardless of source. Fall back to job only for entities with no workload.
//
//nolint:gochecknoglobals // static lookup table for the asserts metric guide
var identityByClass = map[MetricClass][]string{
	MetricClassRequest:  {"job", "service"},  // job required, service if present
	MetricClassResource: {"workload", "job"}, // workload preferred, job fallback when no workload
}

// AssertsMetricGuide is the static config returned in place of the missing
// /v2/config/metric endpoint, and the payload rendered by `kg meta metrics`.
type AssertsMetricGuide struct {
	KPIs            []MetricKPI              `json:"kpis"`
	ScopeMapping    map[string]string        `json:"scopeMapping"`    // scope_* prop → label
	IdentityByClass map[MetricClass][]string `json:"identityByClass"` // class → ordered entity props
	// Notes carries the non-negotiable construction rules so an LLM/agent reading
	// the JSON gets the same constraints the TS guide gives.
	Notes []string `json:"notes"`
}

// DefaultAssertsMetricGuide returns the static guide served by `kg meta metrics`.
// It is purely local (no /v2/config/metric endpoint exists — see header note 1),
// so it needs no Client/ctx and works offline. If the Asserts API ever grows a
// metric-config endpoint, swap this for a Client.FetchMetricConfigs(ctx) fetch.
func DefaultAssertsMetricGuide() AssertsMetricGuide {
	scopeMap := make(map[string]string, len(scopeToMetricLabel))
	for _, s := range scopeToMetricLabel {
		scopeMap[s.Prop] = s.Label
	}
	return AssertsMetricGuide{
		KPIs:            assertsKPICatalog,
		ScopeMapping:    scopeMap,
		IdentityByClass: identityByClass,
		Notes: []string{
			"asserts:* KPIs are pre-computed (rate5m is already a per-second rate) — never wrap them in rate().",
			"resource metrics (cpu/memory) may be cAdvisor-sourced; when they are, their job is the scrape job (e.g. kube-system/cadvisor), not asserts/<service>. Prefer workload so the selector works regardless of source; fall back to job only when the entity has no workload property.",
			"the listed asserts_resource_type values (cpu/memory) are not exhaustive — others exist (e.g. disk:usage, disk:inode_usage, disk:read_latency, cpu:load, cpu:throttle, fd:usage, heap:usage, kafka_lag). Use the same asserts:resource (ratio) / asserts:resource:usage (absolute) pair with the chosen type; enumerate available types with: group by (asserts_resource_type) (asserts:resource).",
		},
	}
}

// metricDisplay renders a KPI's metric name with any baked-in fixed selector,
// e.g. asserts:resource{asserts_resource_type="cpu:usage"}.
func metricDisplay(kpi MetricKPI) string {
	if len(kpi.FixedSelector) == 0 {
		return kpi.Metric
	}
	keys := make([]string, 0, len(kpi.FixedSelector))
	for k := range kpi.FixedSelector {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s=%q", k, kpi.FixedSelector[k]))
	}
	return kpi.Metric + "{" + strings.Join(parts, ", ") + "}"
}

// formatMetricSection renders the asserts metric guide in the compact,
// LLM-friendly text format used by the other meta sections (see DescribeTextCodec).
func formatMetricSection(g AssertsMetricGuide) string {
	var b strings.Builder
	b.WriteString("Metric KPIs (asserts:* — Knowledge Graph recording rules):")
	for _, kpi := range g.KPIs {
		fmt.Fprintf(&b, "\n  %-20s → %s  [%s, agg: %s]", kpi.Intent, metricDisplay(kpi), kpi.Unit, kpi.Agg)
	}

	b.WriteString("\n  scope filters (always): ")
	scopePairs := make([]string, 0, len(scopeToMetricLabel))
	for _, s := range scopeToMetricLabel {
		scopePairs = append(scopePairs, s.Prop+" → "+s.Label)
	}
	b.WriteString(strings.Join(scopePairs, ", "))

	b.WriteString("\n  identity labels (entity property → label, in priority order):")
	for _, class := range []MetricClass{MetricClassRequest, MetricClassResource} {
		fmt.Fprintf(&b, "\n    %s: %s", class, strings.Join(g.IdentityByClass[class], ", "))
	}

	if len(g.Notes) > 0 {
		b.WriteString("\n  rules:")
		for _, n := range g.Notes {
			b.WriteString("\n    - " + n)
		}
	}
	return b.String()
}

// newDescribeMetricsCmd implements `gcx kg meta metrics`. Unlike its
// log/trace/profile siblings it serves a locally-defined guide (no
// /v2/config/metric endpoint exists — see file header), so it needs no client
// round-trip, but keeps the same opts/codec wiring for output uniformity.
func newDescribeMetricsCmd(_ RESTConfigLoader) *cobra.Command {
	opts := &describeOpts{}
	cmd := &cobra.Command{
		Use:   "metrics",
		Short: "Show the asserts:* KPI metrics (Knowledge Graph recording rules) and the entity-property → Prometheus label mapping for querying them.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}
			guide := DefaultAssertsMetricGuide()
			return opts.IO.Encode(cmd.OutOrStdout(), KGMetadataOutput{Metrics: &guide})
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}
