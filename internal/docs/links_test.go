package docs_test

import (
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/docs"
)

// TestAllURLsAreMarkdown asserts every registry URL is a well-formed https
// grafana.com/docs link ending in .md. This guards the core invariant of the
// package: agents must always be pointed at the Markdown rendering of a doc.
func TestAllURLsAreMarkdown(t *testing.T) {
	seen := map[string]bool{}
	for _, raw := range docs.All() {
		u, err := url.Parse(raw)
		if err != nil {
			t.Errorf("not a valid URL: %q: %v", raw, err)
			continue
		}
		if u.Scheme != "https" {
			t.Errorf("URL must use https: %q", raw)
		}
		if u.Host != "grafana.com" {
			t.Errorf("URL must be on grafana.com: %q", raw)
		}
		if !strings.HasPrefix(u.Path, "/docs/") {
			t.Errorf("URL must be under /docs/: %q", raw)
		}
		if !strings.HasSuffix(u.Path, ".md") {
			t.Errorf("URL must end in .md so agents fetch Markdown: %q", raw)
		}
		if seen[raw] {
			t.Errorf("duplicate URL in registry: %q", raw)
		}
		seen[raw] = true
	}
}

// registryConstants lists every exported URL constant that must appear in
// AllNamed(). Add a new entry here when introducing a registry constant.
//
//nolint:gochecknoglobals // static test fixture list; never mutated.
var registryConstants = []struct {
	name string
	url  string
}{
	{"ServiceAccounts", docs.ServiceAccounts},
	{"AccessPolicies", docs.AccessPolicies},
	{"RolesAndPermissions", docs.RolesAndPermissions},
	{"GrafanaInstallation", docs.GrafanaInstallation},
	{"PromQL", docs.PromQL},
	{"LogQL", docs.LogQL},
	{"TraceQL", docs.TraceQL},
	{"PyroscopeQueries", docs.PyroscopeQueries},
	{"DashboardJSONModel", docs.DashboardJSONModel},
	{"SyntheticMonitoring", docs.SyntheticMonitoring},
	{"FleetManagement", docs.FleetManagement},
	{"KubernetesMonitoring", docs.KubernetesMonitoring},
	{"AdaptiveMetrics", docs.AdaptiveMetrics},
	{"AdaptiveLogs", docs.AdaptiveLogs},
	{"AdaptiveTraces", docs.AdaptiveTraces},
	{"AssistantPricing", docs.AssistantPricing},
	{"SyntheticMonitoringInvoice", docs.SyntheticMonitoringInvoice},
	{"PerformanceTestingInvoice", docs.PerformanceTestingInvoice},
	{"IRMInvoice", docs.IRMInvoice},
	{"Keychain", docs.Keychain},
	{"ConfigMigration", docs.ConfigMigration},
	{"AnonymousUsageStats", docs.AnonymousUsageStats},
	{"CloudAPI", docs.CloudAPI},
}

// TestAllNamedContainsEveryConstant guards against a constant being defined
// but left out of AllNamed() — the drift that omitted RolesAndPermissions and
// ConfigMigration before this PR.
func TestAllNamedContainsEveryConstant(t *testing.T) {
	byName := map[string]string{}
	for _, l := range docs.AllNamed() {
		byName[l.Name] = l.URL
	}
	for _, want := range registryConstants {
		got, ok := byName[want.name]
		if !ok {
			t.Errorf("docs.AllNamed() is missing %q", want.name)
			continue
		}
		if got != want.url {
			t.Errorf("docs.AllNamed()[%q] = %q, want %q", want.name, got, want.url)
		}
	}
}

// TestAllNamedHasUniqueNames asserts every registry entry has a unique,
// non-empty name.
func TestAllNamedHasUniqueNames(t *testing.T) {
	seenNames := map[string]bool{}
	for i, l := range docs.AllNamed() {
		if l.Name == "" {
			t.Errorf("entry %d has empty name (url %q)", i, l.URL)
		}
		if seenNames[l.Name] {
			t.Errorf("duplicate name in registry: %q", l.Name)
		}
		seenNames[l.Name] = true
	}
}

// TestAllContainsCloudAPI guards against the constant being defined but left
// out of the registry — CloudAPI is surfaced to agents as a DetailedError
// DocsLink, so it must be discoverable via docs.All()/AllNamed().
func TestAllContainsCloudAPI(t *testing.T) {
	if !slices.Contains(docs.All(), docs.CloudAPI) {
		t.Errorf("docs.CloudAPI (%q) is missing from docs.All()", docs.CloudAPI)
	}
}
