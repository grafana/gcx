package tempo

import (
	"strconv"
	"strings"
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
// link.
func TracesDrilldownURL(host, datasourceUID, expr string, start, end time.Time) (string, bool) {
	filters, ok := drilldownFilters(host, datasourceUID, expr)
	if !ok {
		return "", false
	}
	return tracesDrilldownURL(host, datasourceUID, filters, start, end, "rate", "traceList")
}

// TracesExceptionsURL builds a Traces Drilldown link that opens the Exceptions
// tab (var-metric=errors, actionView=exceptions). An empty expr opens the tab
// with no var-filters, which is every service. A non-empty expr must be the
// same flat spanset TracesDrilldownURL accepts; the tab adds status = error
// itself, so that comparison does not belong in var-filters.
func TracesExceptionsURL(host, datasourceUID, expr string, start, end time.Time) (string, bool) {
	if host == "" || datasourceUID == "" {
		return "", false
	}
	var filters []tempo.TraceQLFilter
	if strings.TrimSpace(expr) != "" {
		parsed, ok := drilldownFilters(host, datasourceUID, expr)
		if !ok {
			return "", false
		}
		filters = parsed
	}
	return tracesDrilldownURL(host, datasourceUID, filters, start, end, "errors", "exceptions")
}

// ExceptionRowFilters are the Exceptions tab filters for one row: the caller's
// flat comparisons, plus that row's service and exception type. ok is false
// when a non-empty caller filter is not a flat spanset, so the row gets no
// link rather than a wider one. An empty filter is every service. "Unknown"
// is the placeholder for a missing type.
func ExceptionRowFilters(filter, service, typ string) ([]tempo.TraceQLFilter, bool) {
	var filters []tempo.TraceQLFilter
	if strings.TrimSpace(filter) != "" {
		parsed, ok := tempo.ParseFlatSpansetFilters(filter)
		if !ok {
			return nil, false
		}
		filters = append(filters, parsed...)
	}
	if service != "" && !hasExactFilter(filters, "resource", "service.name", service) {
		filters = append(filters, tempo.TraceQLFilter{Scope: "resource", Tag: "service.name", Operator: "=", Value: service})
	}
	if typ != "" && typ != "Unknown" && !hasExactFilter(filters, "event", "exception.type", typ) {
		filters = append(filters, tempo.TraceQLFilter{Scope: "event", Tag: "exception.type", Operator: "=", Value: typ})
	}
	return filters, true
}

func hasExactFilter(filters []tempo.TraceQLFilter, scope, tag, value string) bool {
	for _, f := range filters {
		if f.Scope == scope && f.Tag == tag && f.Operator == "=" && f.Value == value {
			return true
		}
	}
	return false
}

func drilldownFilters(host, datasourceUID, expr string) ([]tempo.TraceQLFilter, bool) {
	if host == "" || datasourceUID == "" || expr == "" {
		return nil, false
	}
	filters, ok := tempo.ParseFlatSpansetFilters(expr)
	if !ok || len(filters) == 0 {
		return nil, false
	}
	return filters, true
}

func tracesDrilldownURL(host, datasourceUID string, filters []tempo.TraceQLFilter, start, end time.Time, metric, actionView string) (string, bool) {
	if host == "" || datasourceUID == "" {
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
