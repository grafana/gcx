package prometheus

import (
	"strconv"
	"strings"
	"time"

	dsquery "github.com/grafana/gcx/internal/datasources/query"
	promquery "github.com/grafana/gcx/internal/query/prometheus"
)

// MetricsDrilldownPluginID is the Grafana app plugin ID for Metrics
// Drilldown.
const MetricsDrilldownPluginID = "grafana-metricsdrilldown-app"

// escapeURLPipe mirrors Metrics Drilldown's own escapeUrlPipeDelimiters: it
// replaces the pipe character used as the var-filters field separator so a
// label value containing one doesn't corrupt the encoding. Unlike Logs
// Drilldown, Metrics Drilldown doesn't escape commas here.
func escapeURLPipe(value string) string {
	return strings.ReplaceAll(value, "|", "__gfp__")
}

// encodeMetricsFilter renders one var-filters entry, mirroring Metrics
// Drilldown's own filterToUrlParameter encoding.
func encodeMetricsFilter(f promquery.PromQLFilter) string {
	return f.Label + "|" + f.Operator + "|" + escapeURLPipe(f.Value)
}

// MetricsDrilldownURL builds a Grafana Metrics Drilldown deep link for a
// PromQL query, mirroring the URL shape Metrics Drilldown itself builds via
// buildDrilldownUrl (grafana/metrics-drilldown's src/extensions/links.ts).
//
// It only supports a safe subset of PromQL (see
// internal/query/prometheus.ParseSimpleVectorQuery) — anything involving more
// than one distinct metric reference or unrecognized structure returns
// ok=false so the caller can fall back to the plain Explore link instead of
// showing a potentially misleading Drilldown link.
func MetricsDrilldownURL(host, datasourceUID, expr string, start, end time.Time) (string, bool) {
	if host == "" || datasourceUID == "" || expr == "" {
		return "", false
	}

	metric, filters, ok := promquery.ParseSimpleVectorQuery(expr)
	if !ok || metric == "" {
		return "", false
	}

	if end.IsZero() {
		end = time.Now()
	}
	if start.IsZero() {
		start = end.Add(-1 * time.Minute)
	}

	params := map[string][]string{
		"metric": {metric},
		"var-ds": {datasourceUID},
		"from":   {strconv.FormatInt(start.UnixMilli(), 10)},
		"to":     {strconv.FormatInt(end.UnixMilli(), 10)},
	}
	for _, f := range filters {
		params["var-filters"] = append(params["var-filters"], encodeMetricsFilter(f))
	}

	return dsquery.BuildDrilldownURL(host, MetricsDrilldownPluginID, "/drilldown", params), true
}
