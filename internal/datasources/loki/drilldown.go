package loki

import (
	"strconv"
	"strings"
	"time"

	dsquery "github.com/grafana/gcx/internal/datasources/query"
	"github.com/grafana/gcx/internal/query/loki"
)

// LogsDrilldownURL builds a Grafana Logs Drilldown deep link for a LogQL
// query, mirroring the URL shape Logs Drilldown itself builds via
// contextToLink (grafana/logs-drilldown's src/services/extensions/links.ts).
//
// It only supports a bare stream selector with optional simple line filters
// — anything else (parser stages, label_format, aggregation functions, ...)
// returns ok=false so the caller can fall back to the plain Explore link
// instead of showing an inaccurate Drilldown link.
//
// start/end should be the actual resolved query time range; when both are
// zero (no time flags given), a short 1-minute-lookback default is used,
// matching ShortExploreRange's convention for the sibling Explore link.
func LogsDrilldownURL(host, datasourceUID, expr string, start, end time.Time) (string, bool) {
	if strings.TrimSpace(host) == "" || datasourceUID == "" || strings.TrimSpace(expr) == "" {
		return "", false
	}

	matchers, lineFilters, ok := loki.ParseStreamSelector(expr)
	if !ok {
		return "", false
	}

	primaryIdx := -1
	for i, m := range matchers {
		if m.Inclusive() {
			primaryIdx = i
			break
		}
	}
	if primaryIdx < 0 {
		return "", false
	}
	primary := matchers[primaryIdx]

	pathLabelName := primary.Key
	if pathLabelName == "service_name" {
		pathLabelName = "service"
	}
	// Logs Drilldown's own contextToLink uses only the first |-separated
	// alternative of an inclusive regex matcher (e.g. {app=~"foo|bar"}) for
	// the path segment; the full value is unaffected and still goes into
	// var-filters below via primary.Value.
	pathValue := strings.SplitN(primary.Value, "|", 2)[0]
	path := "/explore/" + pathLabelName + "/" + escapePrimaryLabel(pathValue) + "/logs"

	if end.IsZero() {
		end = time.Now()
	}
	if start.IsZero() {
		start = end.Add(-1 * time.Minute)
	}

	params := map[string][]string{
		"var-ds": {datasourceUID},
		"from":   {strconv.FormatInt(start.UnixMilli(), 10)},
		"to":     {strconv.FormatInt(end.UnixMilli(), 10)},
	}
	for _, m := range matchers {
		params["var-filters"] = append(params["var-filters"], encodeLabelFilter(m.Key, m.Operator, m.Value))
	}
	for i, lf := range lineFilters {
		key, value := lineFilterKeyAndValue(i, lf.Operator, lf.Value)
		params["var-lineFilters"] = append(params["var-lineFilters"], encodeLineFilter(key, lf.Operator, value))
	}

	return dsquery.BuildDrilldownURL(host, logsDrilldownPluginID, path, params), true
}
