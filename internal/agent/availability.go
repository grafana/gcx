package agent

import "strings"

// cloudOnlyPaths lists command-tree paths whose subtree only works against
// Grafana Cloud. Anything not matched here is available on self-hosted Grafana
// (OSS and Enterprise) as well as Cloud. Each entry covers the command at that
// path and all of its descendants.
//
// Availability for whole product groups (plus the Adaptive telemetry subtrees)
// is derived from the command path rather than annotated on every leaf, keeping
// the source of truth in one place. The determinations mirror the compatibility
// matrix in README.md and are backed by the official Grafana product docs.
//
// A single command whose availability does not map to a product group — e.g. an
// experimental, Cloud-only endpoint shared by more than one mount — may instead
// set AnnotationAvailability directly in its builder, so the marking follows the
// builder to every mount. The tree walk in command_annotations.go only fills in
// the annotation where a builder has not already set one, so the two mechanisms
// do not conflict.
//
//nolint:gochecknoglobals // central availability registry, accessed via IsCloudOnlyPath
var cloudOnlyPaths = []string{
	"gcx slo",                  // Service Level Objectives — Grafana Cloud
	"gcx synthetic-monitoring", // Synthetic Monitoring — requires Grafana Cloud
	"gcx irm",                  // IRM: OnCall + Incident — Grafana Cloud
	"gcx k6",                   // Grafana Cloud k6 — cloud load testing service
	"gcx fleet",                // Fleet Management — Grafana Cloud service
	"gcx assistant",            // Grafana Assistant — Grafana Cloud
	"gcx kg",                   // Knowledge Graph / Asserts — Grafana Cloud
	"gcx frontend",             // Frontend Observability — Grafana Cloud
	"gcx appo11y",              // Application Observability — Grafana Cloud
	"gcx agento11y",            // Agent Observability — Grafana Cloud
	// Instrumentation Hub — Grafana Cloud service. Narrowed to specific
	// subtrees because `gcx instrumentation check`, `explain`, and
	// `list-explanations` run entirely locally (against workstation env
	// vars, package manifests, and bundled otel-checker docs) and work
	// on OSS/Enterprise. `check --fix-plan=assistant` requires Cloud but
	// the base command does not.
	"gcx instrumentation setup",    // onboarding wizard
	"gcx instrumentation status",   // observed cluster/service state
	"gcx instrumentation clusters", // cluster + app management
	"gcx instrumentation services", // K8s workload survey
	"gcx cloud",                    // Grafana Cloud stacks management
	"gcx setup",                    // Grafana Cloud product onboarding
	"gcx metrics adaptive",         // Adaptive Metrics — Grafana Cloud
	"gcx metrics billing",          // Grafana Cloud billing/usage metrics (grafanacloud-usage datasource)
	"gcx logs adaptive",            // Adaptive Logs — Grafana Cloud
	"gcx traces adaptive",          // Adaptive Traces — Grafana Cloud
	"gcx profiles adaptive",        // Adaptive Profiles — Grafana Cloud
}

// IsCloudOnlyPath reports whether the given command path (as returned by
// cobra's Command.CommandPath) is Grafana Cloud-only — either an exact match for
// a registered cloud-only path or a descendant of one.
func IsCloudOnlyPath(path string) bool {
	for _, p := range cloudOnlyPaths {
		if path == p || strings.HasPrefix(path, p+" ") {
			return true
		}
	}
	return false
}

// CloudOnlyPaths returns the registered Grafana Cloud-only command paths. Used
// by consistency tests to detect entries that no longer match a real command.
func CloudOnlyPaths() []string {
	out := make([]string, len(cloudOnlyPaths))
	copy(out, cloudOnlyPaths)
	return out
}
