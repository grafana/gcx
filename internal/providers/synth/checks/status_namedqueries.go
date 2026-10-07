package checks

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"time"

	"github.com/grafana/gcx/internal/query/synth"
	"golang.org/x/sync/errgroup"
)

// Named queries `checks status` reads. The SM backend owns the expressions, so
// gcx and the app present the same values; gcx only supplies the name and, for
// latency, the check type.
const (
	queryReachability = "checks_reachability"
	queryProbeCount   = "checks_probe_count"
	queryLatency      = "checks_latency"
)

// statusAnchorRange is the range sent with each query. These are instant queries
// that carry their own look-back windows (the backend registry marks them
// instant, and a live stack returns one point per series), so the range only
// anchors the evaluation time at "now". If a query were a range query,
// synth.Mean would average this whole range.
const statusAnchorRange = 5 * time.Minute

// statusConcurrency bounds the parallel queries, matching the repo's default for
// batch I/O. One reachability, one probe count and at most one latency query per
// check type, so it is rarely reached.
const statusConcurrency = 10

// namedQuerier runs an SM named query. *synth.BackendDatasourceClient satisfies it.
type namedQuerier interface {
	Query(ctx context.Context, datasourceUID string, q synth.NamedQuery, from, to time.Time) (*synth.NamedResult, error)
}

// statusColumn is a column of `checks status` that comes from a named query.
type statusColumn int

const (
	columnSuccess statusColumn = iota
	columnProbeCount
	columnLatency
)

// statusMetrics holds the per-check values behind `checks status`, each keyed by
// checkKey. A check missing from a map has no data for that column.
type statusMetrics struct {
	// success is reachability: the ratio the app shows on its check list card. It
	// feeds CheckStatusResult.Success, whose name predates that definition.
	success map[string]float64
	// probeCount is the number of probes currently reporting.
	probeCount map[string]float64
	// latency is the average latency in seconds.
	latency map[string]float64

	// failures lists the queries that could not be answered, each already
	// prefixed with the query name. Their columns are empty, which is not the same
	// as the backend having no data for a check.
	failures []error
	// reachabilityFailed is set when the reachability query could not be answered.
	// Reachability is the only input to OK/FAILING, so without it every check would
	// read NODATA, which says "no data" when the truth is "could not be asked".
	reachabilityFailed bool
}

// distinctFailures returns failures with repeats removed, in first-seen order.
// Latency is queried once per check type, so a backend that lacks the query fails
// once per type with the same message.
func (m statusMetrics) distinctFailures() []error {
	seen := make(map[string]bool, len(m.failures))
	var out []error
	for _, err := range m.failures {
		if seen[err.Error()] {
			continue
		}
		seen[err.Error()] = true
		out = append(out, err)
	}

	return out
}

func (m statusMetrics) column(c statusColumn) map[string]float64 {
	switch c {
	case columnSuccess:
		return m.success
	case columnProbeCount:
		return m.probeCount
	default:
		return m.latency
	}
}

// checkKey identifies a check in the maps returned by the SM queries.
func checkKey(job, target string) string {
	return job + "/" + target
}

// statusJob is one named query and the column its answer fills.
type statusJob struct {
	query  synth.NamedQuery
	column statusColumn
	// forChecks is set for a query that is only meaningful for some checks. The
	// backend returns a row for every check in the tenant, so a type-specific
	// answer is read only for the checks of that type.
	forChecks []Check
	// typed distinguishes "only these checks" (even if the list were empty) from
	// "every row".
	typed bool
}

type statusJobResult struct {
	vals map[string]float64
	err  error
}

// fetchStatusMetrics reads reachability, probe count and latency for checks from
// the SM backend datasource. Reachability and probe count are one tenant-wide
// query each; latency is one query per distinct check type, because the backend
// picks the latency metric from the type.
func fetchStatusMetrics(ctx context.Context, querier namedQuerier, datasourceUID string, checks []Check, now time.Time) statusMetrics {
	metrics := statusMetrics{
		success:    map[string]float64{},
		probeCount: map[string]float64{},
		latency:    map[string]float64{},
	}
	if len(checks) == 0 {
		return metrics
	}

	jobs := []statusJob{
		{query: synth.NamedQuery{Name: queryReachability}, column: columnSuccess},
		{query: synth.NamedQuery{Name: queryProbeCount}, column: columnProbeCount},
	}

	byType := checksByType(checks)
	for _, t := range slices.Sorted(maps.Keys(byType)) {
		jobs = append(jobs, statusJob{
			query:     synth.NamedQuery{Name: queryLatency, Params: map[string]any{"checkType": t}},
			column:    columnLatency,
			forChecks: byType[t],
			typed:     true,
		})
	}
	from := now.Add(-statusAnchorRange)
	results := make([]statusJobResult, len(jobs))

	var g errgroup.Group
	g.SetLimit(statusConcurrency)
	for i, job := range jobs {
		g.Go(func() error {
			vals, err := queryByJobInstance(ctx, querier, datasourceUID, job.query, from, now)
			results[i] = statusJobResult{vals: vals, err: err}
			// A failed query is that column's result, not a reason to cancel the
			// others, so the group itself never fails.
			return nil
		})
	}
	_ = g.Wait()

	for i, job := range jobs {
		res := results[i]
		if res.err != nil {
			metrics.failures = append(metrics.failures, res.err)
			if job.column == columnSuccess {
				metrics.reachabilityFailed = true
			}
			continue
		}

		into := metrics.column(job.column)
		if !job.typed {
			maps.Copy(into, res.vals)
			continue
		}
		for _, c := range job.forChecks {
			key := checkKey(c.Job, c.Target)
			if v, ok := res.vals[key]; ok {
				into[key] = v
			}
		}
	}

	return metrics
}

// fetchCheckSuccess returns the reachability of the check job/target, and whether
// the backend has any data for it. It reads the same tenant-wide query as
// `checks status`, so `checks get --show-status` agrees with it. The registry has
// no per-check reachability entry, so this reads every check's row to use one:
// the cost grows with the size of the tenant.
func fetchCheckSuccess(ctx context.Context, querier namedQuerier, datasourceUID, job, target string, now time.Time) (float64, bool, error) {
	vals, err := queryByJobInstance(ctx, querier, datasourceUID, synth.NamedQuery{Name: queryReachability}, now.Add(-statusAnchorRange), now)
	if err != nil {
		return 0, false, err
	}

	v, ok := vals[checkKey(job, target)]

	return v, ok, nil
}

// queryByJobInstance runs a tenant-wide named query and reduces each returned
// frame (one per check) to a single value keyed by checkKey. The map is never nil
// when err is nil.
func queryByJobInstance(ctx context.Context, querier namedQuerier, datasourceUID string, query synth.NamedQuery, from, to time.Time) (map[string]float64, error) {
	res, err := querier.Query(ctx, datasourceUID, query, from, to)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", query.Name, err)
	}

	out := make(map[string]float64, len(res.Frames))
	for _, frame := range res.Frames {
		labels := synth.Labels(frame)
		job, instance := labels["job"], labels["instance"]
		if job == "" || instance == "" {
			continue
		}

		if v, ok := synth.Mean(frame); ok {
			out[checkKey(job, instance)] = v
		}
	}

	return out, nil
}

// checksByType groups checks by check type. A check without settings has no type
// the backend would accept, so it is left out rather than failing the whole
// latency query for its type.
func checksByType(checks []Check) map[string][]Check {
	out := map[string][]Check{}
	for _, c := range checks {
		t := c.Settings.CheckType()
		if t == "unknown" {
			continue
		}
		out[t] = append(out[t], c)
	}

	return out
}
