package tempo

import (
	"strconv"
	"time"

	dsquery "github.com/grafana/gcx/internal/datasources/query"
	"github.com/grafana/gcx/internal/query/tempo"
)

// TracesDrilldownPluginID is the Grafana app plugin ID for Traces Drilldown.
const TracesDrilldownPluginID = "grafana-exploretraces-app"

// encodeTraceQLFilter renders one var-filters entry, mirroring Traces
// Drilldown's own filter encoding: scope and tag joined by ".", then the
// operator and value, pipe-delimited. Unlike Logs Drilldown, Traces
// Drilldown does not escape delimiter characters in the value.
func encodeTraceQLFilter(f tempo.TraceQLFilter) string {
	return f.Scope + "." + f.Tag + "|" + f.Operator + "|" + f.Value
}

// TracesDrilldownURL builds a Grafana Traces Drilldown deep link for a
// TraceQL query, mirroring the URL shape Traces Drilldown itself builds via
// contextToLink (grafana/traces-drilldown's src/utils/links.ts).
//
// It only supports a single, flat, &&-joined spanset of scope.tag op value
// comparisons — anything else (||, nested spansets, pipeline stages,
// unscoped intrinsics, >=/<=, ...) returns ok=false so the caller can fall
// back to the plain Explore link instead of showing an inaccurate Drilldown
// link.
func TracesDrilldownURL(host, datasourceUID, expr string, start, end time.Time) (string, bool) {
	if host == "" || datasourceUID == "" || expr == "" {
		return "", false
	}

	filters, ok := tempo.ParseFlatSpansetFilters(expr)
	if !ok || len(filters) == 0 {
		return "", false
	}

	if end.IsZero() {
		end = time.Now()
	}
	if start.IsZero() {
		start = end.Add(-1 * time.Minute)
	}

	params := map[string][]string{
		"var-ds":            {datasourceUID},
		"from":              {strconv.FormatInt(start.UnixMilli(), 10)},
		"to":                {strconv.FormatInt(end.UnixMilli(), 10)},
		"var-primarySignal": {"true"},
		"var-metric":        {"rate"},
		"actionView":        {"traceList"},
	}
	for _, f := range filters {
		params["var-filters"] = append(params["var-filters"], encodeTraceQLFilter(f))
	}

	return dsquery.BuildDrilldownURL(host, TracesDrilldownPluginID, "/explore", params), true
}
