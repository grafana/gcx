package pyroscope

import (
	"strconv"
	"strings"
	"time"

	dsquery "github.com/grafana/gcx/internal/datasources/query"
	pyroquery "github.com/grafana/gcx/internal/query/pyroscope"
)

// ProfilesDrilldownPluginID is the Grafana app plugin ID for Profiles
// Drilldown.
const ProfilesDrilldownPluginID = "grafana-pyroscope-app"

// encodeProfilesFilter renders one var-filters entry. ok is false when a field
// can't round-trip through the Scenes filter decoder.
func encodeProfilesFilter(m pyroquery.LabelMatcher) (string, bool) {
	key, ok := dsquery.EscapeScenesFilterField(m.Key)
	if !ok {
		return "", false
	}
	value, ok := dsquery.EscapeScenesFilterField(m.Value)
	if !ok {
		return "", false
	}
	return key + "|" + m.Operator + "|" + value, true
}

// ProfilesDrilldownURL builds a Grafana Profiles Drilldown deep link for a
// Pyroscope label selector and profile type, mirroring the URL shape
// Profiles Drilldown itself builds via buildURL
// (grafana/profiles-drilldown's src/links.ts).
//
// Trace IDs and stacktrace selectors have no Drilldown equivalent. Version
// 3.1.0 supports one profile ID via var-profileIdSelector in flame-graph view;
// multiple IDs still fall back to Explore rather than silently losing scope.
func ProfilesDrilldownURL(host, datasourceUID, selector, profileType string, spanIDs, traceIDs, profileIDs, stacktraceSelector []string, start, end time.Time) (string, bool) {
	params := profilesDrilldownParams(datasourceUID, selector, profileType, spanIDs, traceIDs, profileIDs, stacktraceSelector, start, end)
	link := buildProfilesDrilldownURL(host, 0, params)
	return link, link != ""
}

func profilesDrilldownParams(datasourceUID, selector, profileType string, spanIDs, traceIDs, profileIDs, stacktraceSelector []string, start, end time.Time) map[string][]string {
	if datasourceUID == "" || selector == "" || profileType == "" || len(traceIDs) > 0 || len(profileIDs) > 1 || len(stacktraceSelector) > 0 {
		return nil
	}

	serviceName, extra, ok := profilesServiceFilters(selector)
	if !ok || (len(profileIDs) > 0 && serviceName == "") {
		return nil
	}

	if serviceName == "" && len(extra) > 0 {
		// var-filters is only representable in the "labels"/"flame-graph"
		// exploration views, which require a service_name to scope into.
		// Without one, these matchers have no representation in the "all"
		// view either — fall back to Explore instead of silently dropping
		// them from the link.
		return nil
	}

	if end.IsZero() {
		end = time.Now()
	}
	if start.IsZero() {
		start = end.Add(-1 * time.Minute)
	}

	explorationType := "all"
	params := map[string][]string{
		"var-dataSource":      {datasourceUID},
		"var-profileMetricId": {profileType},
		"from":                {strconv.FormatInt(start.UnixMilli(), 10)},
		"to":                  {strconv.FormatInt(end.UnixMilli(), 10)},
	}
	if serviceName != "" {
		explorationType = "labels"
		params["var-serviceName"] = []string{serviceName}
	}
	if len(profileIDs) == 1 {
		explorationType = "flame-graph"
		params["var-profileIdSelector"] = profileIDs
	}
	params["explorationType"] = []string{explorationType}

	if len(spanIDs) > 0 {
		params["var-spanSelector"] = []string{strings.Join(spanIDs, ",")}
	}

	if (explorationType == "labels" || explorationType == "flame-graph") && len(extra) > 0 {
		// One var-filters parameter per filter, as the Scenes decoder expects.
		for _, m := range extra {
			encoded, ok := encodeProfilesFilter(m)
			if !ok {
				return nil
			}
			params["var-filters"] = append(params["var-filters"], encoded)
		}
	}

	return params
}

// Render only after the caller has selected the view, node budget, and org.
func buildProfilesDrilldownURL(host string, orgID int64, params map[string][]string) string {
	if params == nil {
		return ""
	}
	if orgID > 0 {
		params["orgId"] = []string{strconv.FormatInt(orgID, 10)}
	}
	return dsquery.BuildDrilldownURL(host, ProfilesDrilldownPluginID, "/explore", params)
}

// profilesServiceFilters separates the app's shared service variable from the
// per-view matchers. The parser is intentionally narrower than Pyroscope: a
// nonrepresentable selector must use Explore, not drop its unsupported parts.
func profilesServiceFilters(selector string) (string, []pyroquery.LabelMatcher, bool) {
	matchers, ok := pyroquery.ParseLabelSelector(selector)
	if !ok {
		return "", nil, false
	}
	var service string
	extra := []pyroquery.LabelMatcher{}
	for _, m := range matchers {
		if m.Key == "service_name" && m.Operator == "=" && service == "" {
			// The app interpolates serviceName inside a quoted selector without
			// escaping it. Only values that survive that interpolation are safe;
			// URL encoding cannot fix quotes, backslashes, or control characters.
			if m.Value == "" || strconv.Quote(m.Value) != `"`+m.Value+`"` {
				return "", nil, false
			}
			service = m.Value
			continue
		}
		extra = append(extra, m)
	}
	return service, extra, true
}
