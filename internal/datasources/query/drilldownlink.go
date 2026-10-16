package query

import (
	"net/url"
	"strings"
)

// BuildDrilldownURL renders a Grafana Drilldown app URL for pluginID (e.g.
// a Logs/Traces/Metrics/Profiles Drilldown app plugin ID). path is the
// app-relative path (e.g. "/explore/service/foo/logs" or "/explore" for the
// root), and params holds the flat, possibly-repeated query parameters
// (var-filters, var-lineFilters, etc.) already in that app's encoded form.
func BuildDrilldownURL(host, pluginID, path string, params map[string][]string) string {
	host = strings.TrimRight(strings.TrimSpace(host), "/")
	if host == "" {
		return ""
	}
	if !strings.HasPrefix(host, "http://") && !strings.HasPrefix(host, "https://") {
		return ""
	}

	u, err := url.Parse(host + "/a/" + pluginID + path)
	if err != nil {
		return ""
	}

	query := u.Query()
	for key, values := range params {
		for _, v := range values {
			query.Add(key, v)
		}
	}
	u.RawQuery = query.Encode()

	return u.String()
}
