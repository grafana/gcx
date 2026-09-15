package services

import (
	"errors"
	"fmt"

	"github.com/grafana/promql-builder/go/cog"
	"github.com/grafana/promql-builder/go/promql"
)

// fleetTopKAgg returns topk(limit, sum by (job, span_name) (rate(<latencySum>[window]))) —
// the fleet-wide ranking aggregation shared by buildFleetRankQuery (as its
// own query) and every other fleet builder (as the "and on (job, span_name)"
// restriction that narrows their result set to the same top-N operations).
// Ranking by rate(latencySum) directly is algebraically the same
// busy-seconds-per-second numerator mergeOperations already uses for
// per-service time-share (avg_latency * rate) — one series instead of two.
func fleetTopKAgg(names metricNames, window string, kinds []string, matchers []Matcher, limit int) *promql.AggregationExprBuilder {
	v := spanMetricSelector(names.latencySum, "", kinds, window, matchers)
	busy := promql.Sum(promql.Rate(v)).By([]string{"job", "span_name"})
	return promql.Topk(float64(limit), busy)
}

// restrictToFleetTopK wraps agg (any instant-vector builder carrying job
// and span_name labels) as `agg and on (job, span_name) <fleetTopKAgg>`, so
// its result set matches buildFleetRankQuery's ranking exactly.
func restrictToFleetTopK(agg cog.Builder[promql.Expr], names metricNames, window string, kinds []string, matchers []Matcher, limit int) *promql.BinaryExprBuilder {
	return promql.And(agg, fleetTopKAgg(names, window, kinds, matchers, limit)).On([]string{"job", "span_name"})
}

// buildFleetRankQuery returns topk(limit, sum by (job, span_name) (rate(<latencySum>[window]))).
func buildFleetRankQuery(names metricNames, window string, kinds []string, matchers []Matcher, limit int) (string, error) {
	expr, err := fleetTopKAgg(names, window, kinds, matchers, limit).Build()
	if err != nil {
		return "", err
	}
	return expr.String(), nil
}

// buildFleetTotalTimeQuery returns sum(rate(<latencySum>[window])) with no
// by(...) clause — the fleet-wide denominator for TimeSharePercent, so a
// --limit N view doesn't claim the top N operations are 100% of the fleet.
func buildFleetTotalTimeQuery(names metricNames, window string, kinds []string, matchers []Matcher) (string, error) {
	v := spanMetricSelector(names.latencySum, "", kinds, window, matchers)
	expr, err := promql.Sum(promql.Rate(v)).Build()
	if err != nil {
		return "", err
	}
	return expr.String(), nil
}

// buildFleetRateQuery returns the fleet-wide per-operation request rate,
// restricted to the top-`limit` operations by busy-seconds-per-second.
func buildFleetRateQuery(names metricNames, window string, kinds []string, matchers []Matcher, groupBy []string, limit int) (string, error) {
	v := spanMetricSelector(names.calls, "", kinds, window, matchers)
	agg := promql.Sum(promql.Rate(v)).By(append([]string{"job", "span_name"}, groupBy...))
	expr, err := restrictToFleetTopK(agg, names, window, kinds, matchers, limit).Build()
	if err != nil {
		return "", err
	}
	return expr.String(), nil
}

// buildFleetErrorRateQuery returns the fleet-wide per-operation error rate,
// filtered to status_code=STATUS_CODE_ERROR and restricted to the
// top-`limit` operations.
func buildFleetErrorRateQuery(names metricNames, window string, kinds []string, matchers []Matcher, groupBy []string, limit int) (string, error) {
	v := spanMetricSelector(names.calls, "", kinds, window, matchers).Label("status_code", statusCodeError)
	agg := promql.Sum(promql.Rate(v)).By(append([]string{"job", "span_name"}, groupBy...))
	expr, err := restrictToFleetTopK(agg, names, window, kinds, matchers, limit).Build()
	if err != nil {
		return "", err
	}
	return expr.String(), nil
}

// buildFleetLatencyQuantileQuery returns the fleet-wide per-operation
// latency quantile, restricted to the top-`limit` operations.
func buildFleetLatencyQuantileQuery(names metricNames, window string, kinds []string, phi float64, matchers []Matcher, groupBy []string, limit int) (string, error) {
	if phi < 0 || phi > 1 {
		return "", fmt.Errorf("phi must be in [0,1], got %v", phi)
	}
	v := spanMetricSelector(names.latencyBucket, "", kinds, window, matchers)
	sumByLe := promql.Sum(promql.Rate(v)).By(append([]string{"le", "job", "span_name"}, groupBy...))
	quantile := promql.HistogramQuantile(phi, sumByLe)
	expr, err := restrictToFleetTopK(quantile, names, window, kinds, matchers, limit).Build()
	if err != nil {
		return "", err
	}
	return expr.String(), nil
}

// buildFleetModeProbeQuery returns a cheap fleet-wide (no `job` filter)
// PromQL expression that yields a nonzero scalar when the named calls
// metric had any sample in window — used by detectFleetMetricsMode to
// auto-detect which metrics-mode family the stack emits, the fleet-wide
// counterpart to buildModeProbeQuery in query.go (which requires a job).
// count_over_time(...[window]) rather than an instant count(...): an
// instant probe is only sensitive to Prometheus's ~5m staleness window and
// ignores --since entirely, so a --since 1d run could miss a metric family
// that has a full day of history but paused ingesting in the last 5
// minutes.
func buildFleetModeProbeQuery(metric, window string, matchers []Matcher) (string, error) {
	if metric == "" {
		return "", errors.New("metric is required")
	}
	v := promql.Vector(metric)
	for _, m := range matchers {
		v = m.apply(v)
	}
	expr, err := promql.Sum(promql.CountOverTime(v.Range(window))).Build()
	if err != nil {
		return "", err
	}
	return expr.String(), nil
}

// buildFleetAvgLatencyQuery returns the fleet-wide per-operation average
// latency, restricted to the top-`limit` operations.
func buildFleetAvgLatencyQuery(names metricNames, window string, kinds []string, matchers []Matcher, groupBy []string, limit int) (string, error) {
	sumV := spanMetricSelector(names.latencySum, "", kinds, window, matchers)
	countV := spanMetricSelector(names.latencyCount, "", kinds, window, matchers)
	byOp := append([]string{"job", "span_name"}, groupBy...)
	num := promql.Sum(promql.Rate(sumV)).By(byOp)
	den := promql.Sum(promql.Rate(countV)).By(byOp)
	div := promql.Div(num, den)
	expr, err := restrictToFleetTopK(div, names, window, kinds, matchers, limit).Build()
	if err != nil {
		return "", err
	}
	return expr.String(), nil
}
