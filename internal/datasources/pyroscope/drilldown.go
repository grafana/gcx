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

// encodeProfilesFilter renders one entry of a comma-joined var-filters
// value, mirroring Profiles Drilldown's own extractAdditionalLabels/
// parseRawFilters encoding: key, operator, and value pipe-delimited, with no
// escaping of embedded delimiter characters (unlike Logs Drilldown).
func encodeProfilesFilter(m pyroquery.LabelMatcher) string {
	return m.Key + "|" + m.Operator + "|" + m.Value
}

// ProfilesDrilldownURL builds a Grafana Profiles Drilldown deep link for a
// Pyroscope label selector and profile type, mirroring the URL shape
// Profiles Drilldown itself builds via buildURL
// (grafana/profiles-drilldown's src/links.ts).
//
// hasUnsupportedDrillDown must be true whenever --trace-id or --profile-id
// was used — neither has any Drilldown URL equivalent (confirmed, not found
// anywhere in the app's extension points) — which forces a fallback to the
// plain Explore link instead of showing an inaccurate Drilldown link.
func ProfilesDrilldownURL(host, datasourceUID, selector, profileType string, spanIDs []string, hasUnsupportedDrillDown bool, start, end time.Time) (string, bool) {
	if host == "" || datasourceUID == "" || selector == "" || profileType == "" || hasUnsupportedDrillDown {
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
		filters := make([]string, len(extra))
		for i, m := range extra {
			filters[i] = encodeProfilesFilter(m)
		}
		params["var-filters"] = []string{strings.Join(filters, ",")}
	}

	return dsquery.BuildDrilldownURL(host, ProfilesDrilldownPluginID, "/explore", params), true
}
