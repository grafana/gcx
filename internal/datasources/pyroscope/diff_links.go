package pyroscope

import (
	"strconv"
	"time"

	dsquery "github.com/grafana/gcx/internal/datasources/query"
	pyroquery "github.com/grafana/gcx/internal/query/pyroscope"
)

// ProfilesDiffDrilldownURL follows Profiles Drilldown v3.1.0's Scenes URL
// contract (SceneComparePanel, SceneTimeRangeWithAnnotations, and
// useBuildPyroscopeQuery). Both sides share serviceName/profileMetricId;
// each has its own filters and selected diff range. from/to and comparisonFrom/
// comparisonTo are the surrounding time-series ranges, not the diff selections.
// Unlike standard Scenes time URL keys, comparisonFrom/comparisonTo are passed
// straight into SceneTimeRange as strings: epoch-millisecond strings produce
// Invalid date. They must be RFC3339; the diff selections accept epoch millis.
func ProfilesDiffDrilldownURL(host, uid string, req pyroquery.DiffRequest) (string, bool) {
	link := buildProfilesDrilldownURL(host, 0, profilesDiffDrilldownParams(uid, req))
	return link, link != ""
}

func profilesDiffDrilldownParams(uid string, req pyroquery.DiffRequest) map[string][]string {
	beforeService, beforeFilters, ok := profilesServiceFilters(req.Before.Selector)
	if !ok || beforeService == "" {
		return nil
	}
	afterService, afterFilters, ok := profilesServiceFilters(req.After.Selector)
	if !ok || beforeService != afterService || uid == "" || req.ProfileType == "" {
		return nil
	}
	beforeFrom := strconv.FormatInt(req.Before.From.UnixMilli(), 10)
	beforeTo := strconv.FormatInt(req.Before.To.UnixMilli(), 10)
	afterFrom := strconv.FormatInt(req.After.From.UnixMilli(), 10)
	afterTo := strconv.FormatInt(req.After.To.UnixMilli(), 10)
	params := map[string][]string{
		"explorationType":     {"diff-flame-graph"},
		"var-dataSource":      {uid},
		"var-serviceName":     {beforeService},
		"var-profileMetricId": {req.ProfileType},
		"from":                {beforeFrom},
		"to":                  {beforeTo},
		"comparisonFrom":      {req.After.From.UTC().Format(time.RFC3339)},
		"comparisonTo":        {req.After.To.UTC().Format(time.RFC3339)},
		"diffFrom":            {beforeFrom},
		"diffTo":              {beforeTo},
		"diffFrom-2":          {afterFrom},
		"diffTo-2":            {afterTo},
		"maxNodes":            {strconv.FormatInt(req.MaxNodes, 10)},
	}
	for key, filters := range map[string][]pyroquery.LabelMatcher{
		"var-filtersBaseline": beforeFilters, "var-filtersComparison": afterFilters,
	} {
		for _, filter := range filters {
			encoded, ok := encodeProfilesFilter(filter)
			if !ok {
				return nil
			}
			params[key] = append(params[key], encoded)
		}
	}
	return params
}

func profileDiffLinks(host, uid, dsType string, orgID int64, req pyroquery.DiffRequest) map[string]string {
	links := map[string]string{}
	panes := map[string]any{}
	// Explore displays panes in JSON property order. The shared builder sorts
	// map keys, so use ordered IDs rather than "after" sorting before "before".
	for _, item := range []struct {
		name, paneID string
		selection    pyroquery.ProfileSelection
	}{
		{"before", "A", req.Before},
		{"after", "B", req.After},
	} {
		name, side := item.name, item.selection
		query := dsquery.ExploreQuery{DatasourceUID: uid, DatasourceType: dsType, Expr: side.Selector, OrgID: orgID}
		from, to := strconv.FormatInt(side.From.UnixMilli(), 10), strconv.FormatInt(side.To.UnixMilli(), 10)
		pane := dsquery.SinglePane(uid, []any{profileExplorePayload(query, req.ProfileType, nil, nil, nil, req.MaxNodes)}, from, to, nil)
		panes[item.paneID] = pane[dsquery.DefaultExplorePaneID]
		params := profilesDrilldownParams(uid, side.Selector, req.ProfileType, nil, nil, nil, nil, side.From, side.To)
		if len(params["var-serviceName"]) > 0 {
			// A profile link must land on the flamegraph, not the labels catalog.
			params["explorationType"] = []string{"flame-graph"}
			params["maxNodes"] = []string{strconv.FormatInt(req.MaxNodes, 10)}
			links[name] = buildProfilesDrilldownURL(host, orgID, params)
		}
		if links[name] == "" {
			links[name] = dsquery.BuildExploreURL(host, orgID, pane, nil)
		}
	}
	links["explore"] = dsquery.BuildExploreURL(host, orgID, panes, nil)
	if comparison := buildProfilesDrilldownURL(host, orgID, profilesDiffDrilldownParams(uid, req)); comparison != "" {
		links["comparison"] = comparison
	}
	return links
}
