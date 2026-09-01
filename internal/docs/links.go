// Package docs is the single source of truth for canonical Grafana
// documentation URLs surfaced to users and agents.
//
// Every URL points at the Markdown rendering of a Grafana docs page (the
// ".md" suffix that grafana.com/docs serves) so that agents fetching a link
// from --help output or an error message receive clean Markdown rather than
// HTML. Reference these constants instead of hardcoding URLs in help text,
// llm_hint annotations, or DetailedError.DocsLink, so a single edit updates
// every surface and the link-validity test guards the whole set.
//
// All URLs are verified to resolve and intentionally kept canonical and
// stack-agnostic (no stack slugs, regions, or org IDs).
package docs

import "strings"

// HumanURL converts a registry link to the HTML rendering of the same page,
// for messages shown only to humans (e.g. output gated on agent mode being
// off), where the .md rendering is the wrong surface.
func HumanURL(link string) string {
	return strings.TrimSuffix(link, ".md") + "/"
}

const (
	// ServiceAccounts documents creating a Grafana service-account token,
	// the value passed to `gcx login --token`.
	ServiceAccounts = "https://grafana.com/docs/grafana/latest/administration/service-accounts.md"

	// AccessPolicies documents creating a Grafana Cloud access-policy token,
	// the value passed to `gcx login --cloud-token`. It is also the canonical
	// reference for the "invalid scope" / "permission denied" cloud errors.
	AccessPolicies = "https://grafana.com/docs/grafana-cloud/security-and-account-management/authentication-and-permissions/access-policies/create-access-policies.md"

	// GrafanaInstallation documents Grafana setup, referenced when a stack is
	// older than the minimum supported version.
	GrafanaInstallation = "https://grafana.com/docs/grafana/latest/setup-grafana/installation.md"

	// PromQL documents the Prometheus query editor / PromQL.
	PromQL = "https://grafana.com/docs/grafana/latest/datasources/prometheus/query-editor.md"

	// LogQL documents querying Loki (LogQL).
	LogQL = "https://grafana.com/docs/loki/latest/query.md"

	// TraceQL documents querying Tempo (TraceQL).
	TraceQL = "https://grafana.com/docs/tempo/latest/traceql.md"

	// PyroscopeQueries documents viewing and analyzing Pyroscope profile data.
	PyroscopeQueries = "https://grafana.com/docs/pyroscope/latest/view-and-analyze-profile-data.md"

	// DashboardJSONModel documents the dashboard JSON model, referenced for
	// resource manifest authoring (push/pull/validate).
	DashboardJSONModel = "https://grafana.com/docs/grafana/latest/dashboards/build-dashboards/view-dashboard-json-model.md"

	// SyntheticMonitoring documents Synthetic Monitoring.
	SyntheticMonitoring = "https://grafana.com/docs/grafana-cloud/testing/synthetic-monitoring.md"

	// FleetManagement documents Fleet Management.
	FleetManagement = "https://grafana.com/docs/grafana-cloud/send-data/fleet-management.md"

	// KubernetesMonitoring documents Kubernetes Monitoring, referenced by the
	// instrumentation setup flow.
	KubernetesMonitoring = "https://grafana.com/docs/grafana-cloud/monitor-infrastructure/kubernetes-monitoring.md"

	// AdaptiveMetrics documents Adaptive Metrics cost control.
	AdaptiveMetrics = "https://grafana.com/docs/grafana-cloud/observe-and-act/adaptive-telemetry/adaptive-metrics.md"

	// AdaptiveLogs documents Adaptive Logs cost control.
	AdaptiveLogs = "https://grafana.com/docs/grafana-cloud/observe-and-act/adaptive-telemetry/adaptive-logs.md"

	// AdaptiveTraces documents reducing traces costs (Adaptive Traces).
	AdaptiveTraces = "https://grafana.com/docs/grafana-cloud/observe-and-act/adaptive-telemetry/adaptive-traces.md"

	// AssistantPricing documents Grafana Assistant token-based pricing,
	// which explicitly counts usage made through the gcx CLI.
	AssistantPricing = "https://grafana.com/docs/grafana-cloud/machine-learning/assistant/pricing.md"

	// SyntheticMonitoringInvoice documents how Synthetic Monitoring test
	// executions and their resulting metrics/logs are billed.
	SyntheticMonitoringInvoice = "https://grafana.com/docs/grafana-cloud/cost-management-and-billing/manage-invoices/understand-your-invoice/synthetic-monitoring-invoice.md"

	// PerformanceTestingInvoice documents how Grafana Cloud k6 test runs are
	// billed in Virtual User Hours (VUh).
	PerformanceTestingInvoice = "https://grafana.com/docs/grafana-cloud/cost-management-and-billing/manage-invoices/understand-your-invoice/performance-testing-invoice.md"

	// IRMInvoice documents how Grafana IRM is billed per monthly active user.
	IRMInvoice = "https://grafana.com/docs/grafana-cloud/cost-management-and-billing/manage-invoices/understand-your-invoice/irm-invoice.md"

	// Keychain documents credential storage and keychain error procedures for gcx.
	Keychain = "https://grafana.com/docs/grafana/latest/as-code/observability-as-code/grafana-cli/gcx/keychain.md"

	// ConfigMigration documents migrating a legacy (unversioned) gcx config
	// to the version 1 stacks/cloud/contexts format, including the manual
	// field mapping. Referenced by the loader's automatic migration on both
	// success and failure.
	ConfigMigration = "https://grafana.com/docs/grafana/latest/as-code/observability-as-code/grafana-cli/gcx/migrate-configuration.md"

	// AnonymousUsageStats documents gcx's anonymous usage statistics: what is
	// collected and how to opt out. Referenced by the first-run telemetry
	// notice.
	AnonymousUsageStats = "https://grafana.com/docs/grafana/latest/as-code/observability-as-code/grafana-cli/gcx/anonymous-usage-statistics.md"

	// CloudAPI documents the Grafana Cloud API (GCOM), the canonical reference
	// for stack create/update parameters — including the slug rules (stack
	// slugs become <slug>.grafana.net subdomains and accept lowercase
	// characters only).
	CloudAPI = "https://grafana.com/docs/grafana-cloud/developer-resources/api-reference/cloud-api.md"
)

// NamedLink pairs a registry constant's name with its documentation URL.
// The name is the stable identifier surfaced to agents (e.g. via
// `gcx docs list-links`) so they can reference a canonical doc without guessing.
type NamedLink struct {
	Name string
	URL  string
}

// AllNamed returns every documentation link in the registry paired with its
// name, in a stable order. This is the single source of truth for the link
// set; All derives from it. Every constant defined above must appear here so
// that a canonical URL surfaced to an agent (via help text, an llm_hint
// annotation, or a DetailedError.DocsLink) is always discoverable through
// `gcx docs list-links`.
func AllNamed() []NamedLink {
	return []NamedLink{
		{Name: "ServiceAccounts", URL: ServiceAccounts},
		{Name: "AccessPolicies", URL: AccessPolicies},
		{Name: "GrafanaInstallation", URL: GrafanaInstallation},
		{Name: "PromQL", URL: PromQL},
		{Name: "LogQL", URL: LogQL},
		{Name: "TraceQL", URL: TraceQL},
		{Name: "PyroscopeQueries", URL: PyroscopeQueries},
		{Name: "DashboardJSONModel", URL: DashboardJSONModel},
		{Name: "SyntheticMonitoring", URL: SyntheticMonitoring},
		{Name: "FleetManagement", URL: FleetManagement},
		{Name: "KubernetesMonitoring", URL: KubernetesMonitoring},
		{Name: "AdaptiveMetrics", URL: AdaptiveMetrics},
		{Name: "AdaptiveLogs", URL: AdaptiveLogs},
		{Name: "AdaptiveTraces", URL: AdaptiveTraces},
		{Name: "AssistantPricing", URL: AssistantPricing},
		{Name: "SyntheticMonitoringInvoice", URL: SyntheticMonitoringInvoice},
		{Name: "PerformanceTestingInvoice", URL: PerformanceTestingInvoice},
		{Name: "IRMInvoice", URL: IRMInvoice},
		{Name: "Keychain", URL: Keychain},
		{Name: "ConfigMigration", URL: ConfigMigration},
		{Name: "AnonymousUsageStats", URL: AnonymousUsageStats},
		{Name: "CloudAPI", URL: CloudAPI},
	}
}

// All returns every documentation URL in the registry, derived from AllNamed
// so the two never drift. Used by the link-validity test to assert the entire
// set is well-formed Markdown.
func All() []string {
	named := AllNamed()
	urls := make([]string, len(named))
	for i, l := range named {
		urls[i] = l.URL
	}
	return urls
}
