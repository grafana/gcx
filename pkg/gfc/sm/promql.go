package sm

import "github.com/grafana/promql-builder/go/promql"

// BuildSuccessRateQuery builds a PromQL query for the average probe_success
// rate over 5 minutes for a single check, grouped by job and instance.
func BuildSuccessRateQuery(job, instance string) (string, error) {
	expr, err := promql.Avg(
		promql.AvgOverTime(
			promql.Vector("probe_success").
				Label("job", job).
				Label("instance", instance).
				Range("5m"),
		),
	).By([]string{"job", "instance"}).Build()
	if err != nil {
		return "", err
	}
	return expr.String(), nil
}

// BuildProbeCountQuery builds a PromQL query that counts probes reporting for a check.
func BuildProbeCountQuery(job, instance string) (string, error) {
	expr, err := promql.Count(
		promql.Vector("probe_success").
			Label("job", job).
			Label("instance", instance),
	).By([]string{"job", "instance"}).Build()
	if err != nil {
		return "", err
	}
	return expr.String(), nil
}

// BuildAllSuccessRateQuery builds a PromQL query for the success rate of all checks.
func BuildAllSuccessRateQuery() (string, error) {
	expr, err := promql.Avg(
		promql.AvgOverTime(
			promql.Vector("probe_success").Range("5m"),
		),
	).By([]string{"job", "instance"}).Build()
	if err != nil {
		return "", err
	}
	return expr.String(), nil
}

// BuildAllLatencyQuery builds a PromQL query for the average probe_duration_seconds
// of all checks.
func BuildAllLatencyQuery() (string, error) {
	expr, err := promql.Avg(
		promql.AvgOverTime(
			promql.Vector("probe_duration_seconds").Range("5m"),
		),
	).By([]string{"job", "instance"}).Build()
	if err != nil {
		return "", err
	}
	return expr.String(), nil
}

// BuildLatencyQuery builds a PromQL query for the average probe_duration_seconds
// over 5 minutes for a single check.
func BuildLatencyQuery(job, instance string) (string, error) {
	expr, err := promql.Avg(
		promql.AvgOverTime(
			promql.Vector("probe_duration_seconds").
				Label("job", job).
				Label("instance", instance).
				Range("5m"),
		),
	).By([]string{"job", "instance"}).Build()
	if err != nil {
		return "", err
	}
	return expr.String(), nil
}

// BuildAllProbeCountQuery builds a PromQL query counting probes per check across all checks.
func BuildAllProbeCountQuery() (string, error) {
	expr, err := promql.Count(
		promql.Vector("probe_success"),
	).By([]string{"job", "instance"}).Build()
	if err != nil {
		return "", err
	}
	return expr.String(), nil
}

// BuildTimelineQuery builds a PromQL query for raw probe_success values.
func BuildTimelineQuery(job, instance string) (string, error) {
	expr, err := promql.Vector("probe_success").
		Label("job", job).
		Label("instance", instance).
		Build()
	if err != nil {
		return "", err
	}
	return expr.String(), nil
}
