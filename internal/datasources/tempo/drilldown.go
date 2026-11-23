package tempo

import (
	"strconv"
	"time"

	dsquery "github.com/grafana/gcx/internal/datasources/query"
	"github.com/grafana/gcx/internal/query/tempo"
)

// TracesDrilldownPluginID is the Grafana app plugin ID for Traces Drilldown.
const TracesDrilldownPluginID = "grafana-exploretraces-app"

// encodeTraceQLFilter renders one var-filters entry: scope and tag joined by
// ".", then the operator and value, pipe-delimited. ok is false when a field
// can't round-trip through the Scenes filter decoder.
func encodeTraceQLFilter(f tempo.TraceQLFilter) (string, bool) {
	key, ok := dsquery.EscapeScenesFilterField(f.Scope + "." + f.Tag)
	if !ok {
		return "", false
	}
	value, ok := dsquery.EscapeScenesFilterField(f.Value)
	if !ok {
		return "", false
	}
	return key + "|" + f.Operator + "|" + value, true
}

// TracesDrilldownURL builds a Grafana Traces Drilldown deep link for a
// TraceQL query, mirroring the URL shape Traces Drilldown itself builds via
// contextToLink (grafana/traces-drilldown's src/utils/links.ts).
//
// It only supports a single, flat, &&-joined spanset of scope.tag op value
// comparisons — anything else (||, nested spansets, pipeline stages,
// unscoped intrinsics, >=/<=, ...) returns ok=false so the caller can fall
// back to the plain Explore link instead of showing an inaccurate Drilldown
// link. The link opens the rate metric on the trace list. TracesExceptionsURL
// is the same link opened on the Exceptions tab.
func TracesDrilldownURL(host, datasourceUID, expr string, start, end time.Time) (string, bool) {
	return tracesDrilldownURL(host, datasourceUID, expr, start, end, "rate", "traceList")
}

// TracesExceptionsURL builds a Traces Drilldown link that opens the Exceptions
// tab (var-metric=errors, actionView=exceptions). It accepts the same flat
// filters as TracesDrilldownURL.
func TracesExceptionsURL(host, datasourceUID, expr string, start, end time.Time) (string, bool) {
	return tracesDrilldownURL(host, datasourceUID, expr, start, end, "errors", "exceptions")
}

func tracesDrilldownURL(host, datasourceUID, expr string, start, end time.Time, metric, actionView string) (string, bool) {
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
		"var-metric":        {metric},
		"actionView":        {actionView},
	}
	for _, f := range filters {
		encoded, ok := encodeTraceQLFilter(f)
		if !ok {
			return "", false
		}
		params["var-filters"] = append(params["var-filters"], encoded)
	}

	return dsquery.BuildDrilldownURL(host, TracesDrilldownPluginID, "/explore", params), true
}
