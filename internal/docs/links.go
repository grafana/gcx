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
	AdaptiveMetrics = "https://grafana.com/docs/grafana-cloud/cost-management-and-billing/reduce-costs/metrics-costs/control-metrics-usage-via-adaptive-metrics.md"

	// AdaptiveLogs documents Adaptive Logs cost control.
	AdaptiveLogs = "https://grafana.com/docs/grafana-cloud/cost-management-and-billing/reduce-costs/logs-costs/adaptive-logs.md"

	// AdaptiveTraces documents reducing traces costs (Adaptive Traces).
	AdaptiveTraces = "https://grafana.com/docs/grafana-cloud/cost-management-and-billing/reduce-costs/traces-costs.md"

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

	// ConfigMigration documents migrating a legacy (unversioned) gcx config
	// to the version 1 stacks/cloud/contexts format, including the manual
	// field mapping. Referenced by the loader's automatic migration on both
	// success and failure.
	ConfigMigration = "https://grafana.com/docs/grafana/latest/as-code/observability-as-code/grafana-cli/gcx/migrate-configuration.md"
)

// All returns every documentation URL in the registry. Used by the
// link-validity test to assert the entire set is well-formed Markdown.
func All() []string {
	return []string{
		ServiceAccounts,
		AccessPolicies,
		GrafanaInstallation,
		PromQL,
		LogQL,
		TraceQL,
		PyroscopeQueries,
		DashboardJSONModel,
		SyntheticMonitoring,
		FleetManagement,
		KubernetesMonitoring,
		AdaptiveMetrics,
		AdaptiveLogs,
		AdaptiveTraces,
		AssistantPricing,
		SyntheticMonitoringInvoice,
		PerformanceTestingInvoice,
		IRMInvoice,
	}
}
