package remote

import (
	"fmt"

	"k8s.io/apimachinery/pkg/runtime/schema"
)

// staticServerDryRunAllowlist lists the resources known to honor server-side dryRun in any
// storage mode: folders and playlists have no legacy storage path, and dashboards default
// to unified storage. Everything else is treated as unsafe, notably alerting, whose legacy
// storage ignores dryRun and applies the change for real. Only add a resource here if it is
// guaranteed to honor dryRun. See grafana-enterprise#12569.
//
//nolint:gochecknoglobals // constant lookup table.
var staticServerDryRunAllowlist = map[schema.GroupResource]struct{}{
	{Group: "dashboard.grafana.app", Resource: "dashboards"}: {},
	{Group: "folder.grafana.app", Resource: "folders"}:       {},
	{Group: "playlist.grafana.app", Resource: "playlists"}:   {},
}

// dryRunAllowlist decides whether a GroupResource honors server-side dryRun. Unknown
// resources default to false (fail safe) and take the best-effort path.
type dryRunAllowlist struct {
	// extra holds resources the user asserted honor dryRun (via --assume-server-dry-run or
	// config), on top of the static list.
	extra map[schema.GroupResource]struct{}
}

// newDryRunAllowlist builds an allowlist from user-asserted "<resource>.<group>" strings.
// Malformed values are returned in invalid instead of failing, so a typo just does not take
// effect rather than blocking every operation.
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

// classify returns (honored, static): whether gr honors dryRun, and whether that is built-in
// (true) or user-asserted (false).
func (a dryRunAllowlist) classify(gr schema.GroupResource) (bool, bool) {
	if _, ok := staticServerDryRunAllowlist[gr]; ok {
		return true, true
	}
	_, ok := a.extra[gr]
	return ok, false
}

// parseGroupResource parses "<resource>.<group>" into a GroupResource. The group must be
// present, so a bare resource name is rejected rather than matched by accident.
func parseGroupResource(s string) (schema.GroupResource, error) {
	gr := schema.ParseGroupResource(s)
	if gr.Resource == "" || gr.Group == "" {
		return schema.GroupResource{}, fmt.Errorf(
			"invalid group resource %q: expected <resource>.<group>, e.g. alertrules.rules.alerting.grafana.app", s)
	}
	return gr, nil
}
