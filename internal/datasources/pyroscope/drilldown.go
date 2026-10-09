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
// traceIDs, profileIDs, and stacktraceSelector (from --trace-id/--profile-id/
// --stacktrace-selector) have no Drilldown URL equivalent — none of those
// params exist anywhere in the app's extension points — so any one present
// forces a fallback to the plain Explore link instead of showing an
// inaccurate Drilldown link. spanIDs (from --span-id) does have one and is
// threaded through to var-spanSelector.
func ProfilesDrilldownURL(host, datasourceUID, selector, profileType string, spanIDs, traceIDs, profileIDs, stacktraceSelector []string, start, end time.Time) (string, bool) {
	if host == "" || datasourceUID == "" || selector == "" || profileType == "" ||
		len(traceIDs) > 0 || len(profileIDs) > 0 || len(stacktraceSelector) > 0 {
		return "", false
	}

	matchers, ok := pyroquery.ParseLabelSelector(selector)
	if !ok {
		return "", false
	}

	var serviceName string
	var extra []pyroquery.LabelMatcher
	for _, m := range matchers {
		if m.Key == "service_name" && m.Operator == "=" && serviceName == "" {
			serviceName = m.Value
			continue
		}
		extra = append(extra, m)
	}

	if serviceName == "" && len(extra) > 0 {
		// var-filters is only representable in the "labels"/"flame-graph"
		// exploration views, which require a service_name to scope into.
		// Without one, these matchers have no representation in the "all"
		// view either — fall back to Explore instead of silently dropping
		// them from the link.
		return "", false
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
	params["explorationType"] = []string{explorationType}

	if len(spanIDs) > 0 {
		params["var-spanSelector"] = []string{strings.Join(spanIDs, ",")}
	}

	if explorationType == "labels" && len(extra) > 0 {
		// One var-filters parameter per filter, as the Scenes decoder expects.
		for _, m := range extra {
			encoded, ok := encodeProfilesFilter(m)
			if !ok {
				return "", false
			}
			params["var-filters"] = append(params["var-filters"], encoded)
		}
	}

	return dsquery.BuildDrilldownURL(host, ProfilesDrilldownPluginID, "/explore", params), true
}
