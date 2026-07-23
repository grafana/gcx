package agent

import "strings"

// cloudOnlyPaths lists command-tree paths whose subtree only works against
// Grafana Cloud. Anything not matched here is available on self-hosted Grafana
// (OSS and Enterprise) as well as Cloud. Each entry covers the command at that
// path and all of its descendants.
//
// Availability is derived from the command path rather than annotated on every
// leaf because it is a property of whole product groups (plus the Adaptive
// telemetry subtrees), keeping the source of truth in one place. The
// determinations mirror the compatibility matrix in README.md and are backed by
// the official Grafana product docs.
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
	"gcx instrumentation",      // Instrumentation Hub — Grafana Cloud service
	"gcx cloud",                // Grafana Cloud stacks management
	"gcx setup",                // Grafana Cloud product onboarding
	"gcx metrics adaptive",     // Adaptive Metrics — Grafana Cloud
	"gcx metrics billing",      // Grafana Cloud billing/usage metrics (grafanacloud-usage datasource)
	"gcx logs adaptive",        // Adaptive Logs — Grafana Cloud
	"gcx traces adaptive",      // Adaptive Traces — Grafana Cloud
	"gcx profiles adaptive",    // Adaptive Profiles — Grafana Cloud
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
