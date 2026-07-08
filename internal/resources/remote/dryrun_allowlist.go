package remote

import (
	"fmt"

	"k8s.io/apimachinery/pkg/runtime/schema"
)

// staticServerDryRunAllowlist is the built-in set of GroupResources known to honor
// server-side dryRun regardless of storage mode. folders and playlists have no legacy
// storage leg at all (served purely by the generic registry), and dashboards default to
// unified storage (Mode 5) via Grafana's MigratedUnifiedResources enforcement. Everything
// else is denied by default, notably all alerting resources, which are legacy-only and
// whose bridges ignore dryRun, so a dry-run push silently applies the mutation.
//
// The criterion is "honors dryRun regardless of storage mode", not "the legacy bridge
// honors dryRun": safety is decided by the mode a request routes to, not by the bridge.
// See docs/plans/2026-07-07-dryrun-client-side-mitigation.md and grafana-enterprise#12569.
//
//nolint:gochecknoglobals // constant lookup table.
var staticServerDryRunAllowlist = map[schema.GroupResource]struct{}{
	{Group: "dashboard.grafana.app", Resource: "dashboards"}: {},
	{Group: "folder.grafana.app", Resource: "folders"}:       {},
	{Group: "playlist.grafana.app", Resource: "playlists"}:   {},
}

// dryRunAllowlist decides whether a GroupResource honors server-side dryRun. Membership
// means "known or user-asserted to honor dryRun"; the default is false (fail safe), so an
// unknown resource is treated as not honoring dryRun and routed to the best-effort path.
type dryRunAllowlist struct {
	// extra holds user-asserted GroupResources (from --assume-server-dry-run and the
	// resources.assume-server-dry-run config), augmenting the static seed. It never
	// removes static entries.
	extra map[schema.GroupResource]struct{}
}

// newDryRunAllowlist builds an allowlist from user-asserted GroupResource strings, each of
// the form "<resource>.<group>", e.g. "alertrules.rules.alerting.grafana.app". Malformed
// values are skipped and returned separately rather than failing: a typo'd assertion simply
// does not take effect, so the resource falls back to the fail-safe best-effort path instead
// of a bad value blocking every operation.
func newDryRunAllowlist(assumed []string) (dryRunAllowlist, []string) {
	extra := make(map[schema.GroupResource]struct{}, len(assumed))
	var invalid []string
	for _, s := range assumed {
		gr, err := parseGroupResource(s)
		if err != nil {
			invalid = append(invalid, s)
			continue
		}
		extra[gr] = struct{}{}
	}
	return dryRunAllowlist{extra: extra}, invalid
}

// classify reports (honored, static): whether gr honors server-side dryRun, and (when it
// does) whether that comes from the built-in static seed (true) or from a user assertion
// (false). A false static tells callers the safety assertion came from the user, not gcx.
func (a dryRunAllowlist) classify(gr schema.GroupResource) (bool, bool) {
	if _, ok := staticServerDryRunAllowlist[gr]; ok {
		return true, true
	}
	_, ok := a.extra[gr]
	return ok, false
}

// parseGroupResource parses a "<resource>.<group>" string into a schema.GroupResource. The
// group must be present so a typo (bare resource name) degrades to best-effort rather than
// silently matching.
func parseGroupResource(s string) (schema.GroupResource, error) {
	gr := schema.ParseGroupResource(s)
	if gr.Resource == "" || gr.Group == "" {
		return schema.GroupResource{}, fmt.Errorf(
			"invalid group resource %q: expected <resource>.<group>, e.g. alertrules.rules.alerting.grafana.app", s)
	}
	return gr, nil
}
