package services

import "sort"

// FleetOperation is one row of `gcx appo11y operations list` — an
// Operation qualified by which service (job) it belongs to.
type FleetOperation struct {
	Operation

	Service   string `json:"service" yaml:"service"`
	Namespace string `json:"namespace,omitempty" yaml:"namespace,omitempty"`
	// KG mirrors Service.KG (see kgcatalog.go): set only under --kg auto,
	// when the Knowledge Graph is active and knows this row's service.
	KG *KGRef `json:"kg,omitempty" yaml:"kg,omitempty"`
}

// FleetOperationsResponse is the response shape for `operations list`.
type FleetOperationsResponse struct {
	Window                    string           `json:"window" yaml:"window"`
	MetricsMode               MetricsMode      `json:"metrics_mode" yaml:"metrics_mode"`
	SpanKinds                 string           `json:"span_kinds" yaml:"span_kinds"`
	GroupBy                   []string         `json:"group_by,omitempty" yaml:"group_by,omitempty"`
	FleetBusySecondsPerSecond float64          `json:"fleet_busy_seconds_per_second" yaml:"fleet_busy_seconds_per_second"`
	Items                     []FleetOperation `json:"items" yaml:"items"`
}

// mergeFleetOperations builds FleetOperation rows from the per-quantity
// maps (each keyed by (job, span_name[, groupBy...]) — see
// extractOperations's groupLabels argument, which for the fleet case
// includes "job"), normalizing TimeSharePercent against fleetTotal (the
// whole fleet's busy-seconds/sec, from buildFleetTotalTimeQuery) rather
// than a per-row/per-group total — this is the one place fleet merge
// logic must diverge from mergeOperations, whose per-group normalization
// would collapse each service to ~100% of itself once "job" is part of
// the group key.
func mergeFleetOperations(rates, errors, avgs, p50s, p95s, p99s map[opAggKey]groupBucket, fleetTotal float64, groupBy []string) []FleetOperation {
	keys := make(map[opAggKey]struct{})
	for _, m := range []map[opAggKey]groupBucket{rates, errors, avgs, p50s, p95s, p99s} {
		for k := range m {
			keys[k] = struct{}{}
		}
	}

	out := make([]FleetOperation, 0, len(keys))
	for k := range keys {
		rate, hasRate := rates[k]
		errRate, hasErr := errors[k]
		avg, hasAvg := avgs[k]
		p50, hasP50 := p50s[k]
		p95, hasP95 := p95s[k]
		p99, hasP99 := p99s[k]

		hasTraffic := hasRate && rate.value > 0
		// Once we know there's traffic, missing error series ⇒ 0 errors,
		// not "unknown" — same logic as REDStats.HasErrors / mergeOperations.
		hasErrors := hasErr || hasTraffic

		labels := opLabelsFor(k, rates, errors, avgs, p50s, p95s, p99s)
		namespace, service := parseJob(labels["job"])

		var timeShare float64
		if hasAvg && hasTraffic && fleetTotal > 0 {
			timeShare = (avg.value * rate.value / fleetTotal) * 100
		}

		out = append(out, FleetOperation{
			Service:   service,
			Namespace: namespace,
			Operation: Operation{
				Name:             k.name,
				Labels:           labels,
				RatePerSecond:    rate.value,
				ErrorRatePerSec:  errRate.value,
				ErrorPercent:     computeErrorPercent(errRate.value, rate.value),
				AvgSeconds:       avg.value,
				P50Seconds:       p50.value,
				P95Seconds:       p95.value,
				P99Seconds:       p99.value,
				TimeSharePercent: timeShare,
				HasTraffic:       hasTraffic,
				HasErrors:        hasErrors,
				HasAvgLatency:    hasAvg,
				HasLatencyP50:    hasP50,
				HasLatencyP95:    hasP95,
				HasLatencyP99:    hasP99,
			},
		})
	}

	sort.Slice(out, func(i, j int) bool {
		bi, bj := out[i].AvgSeconds*out[i].RatePerSecond, out[j].AvgSeconds*out[j].RatePerSecond
		if bi != bj {
			return bi > bj
		}
		if out[i].Service != out[j].Service {
			return out[i].Service < out[j].Service
		}
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		// Tiebreak for --group-by: same operation in the same service can
		// appear once per distinct group-label combination.
		return groupLabelString(out[i].Labels, groupBy) < groupLabelString(out[j].Labels, groupBy)
	})
	return out
}
