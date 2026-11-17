package checks

import (
	"context"
	"errors"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/grafana/gcx/internal/query/dataframe"
	"github.com/grafana/gcx/internal/query/synth"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeQuerier answers named queries from canned frames and records every call,
// so tests can assert both what was asked and how the answers were read.
type fakeQuerier struct {
	mu     sync.Mutex
	calls  []synth.NamedQuery
	uids   []string
	frames func(q synth.NamedQuery) ([]dataframe.Frame, error)
}

func (f *fakeQuerier) Query(_ context.Context, uid string, q synth.NamedQuery, _, _ time.Time) (*synth.NamedResult, error) {
	f.mu.Lock()
	f.calls = append(f.calls, q)
	f.uids = append(f.uids, uid)
	f.mu.Unlock()

	frames, err := f.frames(q)
	if err != nil {
		return nil, err
	}

	return &synth.NamedResult{Frames: frames}, nil
}

func (f *fakeQuerier) callsTo(name string) []synth.NamedQuery {
	f.mu.Lock()
	defer f.mu.Unlock()

	var out []synth.NamedQuery
	for _, c := range f.calls {
		if c.Name == name {
			out = append(out, c)
		}
	}

	return out
}

// seriesFrame is one instant-vector series: a single numeric field carrying the
// (job, instance) labels, which is how the SM backend returns tenant-wide queries.
func seriesFrame(job, instance string, value any) dataframe.Frame {
	return dataframe.Frame{
		Schema: dataframe.Schema{Fields: []dataframe.Field{
			{Name: "Time", Type: "time"},
			{Name: "Value", Type: "number", Labels: map[string]string{"job": job, "instance": instance}},
		}},
		Data: dataframe.Data{Values: [][]any{{float64(1000)}, {value}}},
	}
}

func unlabelledFrame(value any) dataframe.Frame {
	return dataframe.Frame{
		Schema: dataframe.Schema{Fields: []dataframe.Field{
			{Name: "Time", Type: "time"},
			{Name: "Value", Type: "number"},
		}},
		Data: dataframe.Data{Values: [][]any{{float64(1000)}, {value}}},
	}
}

func checkOfType(id int64, job, target, checkType string) Check {
	return Check{
		ID: id, Job: job, Target: target,
		Settings: CheckSettings{checkType: map[string]any{}},
	}
}

func TestFetchStatusMetrics_ReadsEachColumnFromItsNamedQuery(t *testing.T) {
	q := &fakeQuerier{frames: func(q synth.NamedQuery) ([]dataframe.Frame, error) {
		switch q.Name {
		case "checks_reachability":
			return []dataframe.Frame{seriesFrame("web", "https://a", 0.99), seriesFrame("api", "https://b", 0.5)}, nil
		case "checks_probe_count":
			return []dataframe.Frame{seriesFrame("web", "https://a", 3.0), seriesFrame("api", "https://b", 2.0)}, nil
		case "checks_latency":
			return []dataframe.Frame{seriesFrame("web", "https://a", 0.25), seriesFrame("api", "https://b", 0.75)}, nil
		}

		return nil, errors.New("unexpected query " + q.Name)
	}}

	cs := []Check{
		checkOfType(1, "web", "https://a", "http"),
		checkOfType(2, "api", "https://b", "http"),
	}

	got := fetchStatusMetrics(context.Background(), q, "sm-uid", cs, time.Unix(10_000, 0))

	assert.Empty(t, got.failures)
	assert.Equal(t, map[string]float64{"web/https://a": 0.99, "api/https://b": 0.5}, got.success)
	assert.Equal(t, map[string]float64{"web/https://a": 3, "api/https://b": 2}, got.probeCount)
	assert.Equal(t, map[string]float64{"web/https://a": 0.25, "api/https://b": 0.75}, got.latency)

	// Every call goes to the SM datasource; gcx never resolves a Prometheus one.
	for _, uid := range q.uids {
		assert.Equal(t, "sm-uid", uid)
	}

	// Reachability and probe count are tenant-wide: no parameters at all.
	require.Len(t, q.callsTo("checks_reachability"), 1)
	assert.Empty(t, q.callsTo("checks_reachability")[0].Params)
	require.Len(t, q.callsTo("checks_probe_count"), 1)
	assert.Empty(t, q.callsTo("checks_probe_count")[0].Params)
}

func TestFetchStatusMetrics_LatencyIsQueriedOncePerCheckType(t *testing.T) {
	// The backend picks the latency metric from checkType, so the same row can
	// differ between calls. Each check must take its value from the call made
	// with its own type, and nothing from the others.
	byType := map[string][]dataframe.Frame{
		"http": {
			seriesFrame("web", "https://a", 0.1),
			seriesFrame("script", "https://c", 999.0), // wrong branch for a scripted check
		},
		"scripted": {
			seriesFrame("web", "https://a", 888.0), // wrong branch for an http check
			seriesFrame("script", "https://c", 2.0),
		},
	}

	q := &fakeQuerier{frames: func(q synth.NamedQuery) ([]dataframe.Frame, error) {
		if q.Name != "checks_latency" {
			return nil, nil
		}
		ct, _ := q.Params["checkType"].(string)

		return byType[ct], nil
	}}

	cs := []Check{
		checkOfType(1, "web", "https://a", "http"),
		checkOfType(2, "web2", "https://a2", "http"),
		checkOfType(3, "script", "https://c", "scripted"),
	}

	got := fetchStatusMetrics(context.Background(), q, "sm-uid", cs, time.Unix(10_000, 0))

	calls := q.callsTo("checks_latency")
	types := make([]string, 0, len(calls))
	for _, c := range calls {
		ct, ok := c.Params["checkType"].(string)
		require.True(t, ok, "checkType must be sent as a string")
		types = append(types, ct)
	}
	sort.Strings(types)
	assert.Equal(t, []string{"http", "scripted"}, types, "one call per distinct check type, not per check")

	assert.Equal(t, map[string]float64{"web/https://a": 0.1, "script/https://c": 2.0}, got.latency)
}

func TestFetchStatusMetrics_SkipsChecksWithoutAType(t *testing.T) {
	q := &fakeQuerier{frames: func(synth.NamedQuery) ([]dataframe.Frame, error) { return nil, nil }}

	cs := []Check{
		{ID: 1, Job: "web", Target: "https://a"}, // no settings -> no type the backend accepts
		checkOfType(2, "api", "https://b", "http"),
	}

	got := fetchStatusMetrics(context.Background(), q, "sm-uid", cs, time.Unix(10_000, 0))

	assert.Empty(t, got.failures)
	for _, c := range q.callsTo("checks_latency") {
		assert.Equal(t, "http", c.Params["checkType"], "a typeless check must not produce a latency query")
	}
}

func TestFetchStatusMetrics_SkipsUnusableFrames(t *testing.T) {
	q := &fakeQuerier{frames: func(q synth.NamedQuery) ([]dataframe.Frame, error) {
		if q.Name != "checks_reachability" {
			return nil, nil
		}

		return []dataframe.Frame{
			seriesFrame("web", "https://a", 1.0),
			seriesFrame("gap", "https://b", nil), // never scraped: must not become 0
			unlabelledFrame(0.3),                 // no job/instance: cannot be attributed
			seriesFrame("", "https://c", 0.2),    // missing job
			seriesFrame("nohost", "", 0.2),       // missing instance
		}, nil
	}}

	got := fetchStatusMetrics(context.Background(), q, "sm-uid",
		[]Check{checkOfType(1, "web", "https://a", "http")}, time.Unix(10_000, 0))

	assert.Equal(t, map[string]float64{"web/https://a": 1.0}, got.success)
}

func TestFetchStatusMetrics_OneFailingColumnDoesNotDropTheOthers(t *testing.T) {
	q := &fakeQuerier{frames: func(q synth.NamedQuery) ([]dataframe.Frame, error) {
		switch q.Name {
		case "checks_latency":
			return nil, errors.New("unknown query")
		case "checks_reachability":
			return []dataframe.Frame{seriesFrame("web", "https://a", 0.9)}, nil
		}

		return []dataframe.Frame{seriesFrame("web", "https://a", 2.0)}, nil
	}}

	got := fetchStatusMetrics(context.Background(), q, "sm-uid",
		[]Check{checkOfType(1, "web", "https://a", "http")}, time.Unix(10_000, 0))

	// The failure is reported in the result, not swallowed: an old SM app without
	// the entry must be distinguishable from "no data".
	require.Len(t, got.failures, 1)
	assert.Contains(t, got.failures[0].Error(), "checks_latency")
	assert.Contains(t, got.failures[0].Error(), "unknown query")

	assert.Equal(t, map[string]float64{"web/https://a": 0.9}, got.success)
	assert.Equal(t, map[string]float64{"web/https://a": 2.0}, got.probeCount)
	assert.Empty(t, got.latency)
}

func TestFetchStatusMetrics_NoChecksMakesNoCalls(t *testing.T) {
	q := &fakeQuerier{frames: func(synth.NamedQuery) ([]dataframe.Frame, error) { return nil, nil }}

	got := fetchStatusMetrics(context.Background(), q, "sm-uid", nil, time.Unix(10_000, 0))

	assert.Empty(t, got.success)
	assert.Empty(t, got.failures)
	assert.Empty(t, q.calls)
}

func TestFetchCheckSuccess(t *testing.T) {
	q := &fakeQuerier{frames: func(synth.NamedQuery) ([]dataframe.Frame, error) {
		return []dataframe.Frame{seriesFrame("other", "https://o", 0.1), seriesFrame("web", "https://a", 0.98)}, nil
	}}

	v, ok, err := fetchCheckSuccess(context.Background(), q, "sm-uid", "web", "https://a", time.Unix(10_000, 0))
	require.NoError(t, err)
	assert.True(t, ok)
	assert.InDelta(t, 0.98, v, 1e-9)

	_, ok, err = fetchCheckSuccess(context.Background(), q, "sm-uid", "missing", "https://m", time.Unix(10_000, 0))
	require.NoError(t, err)
	assert.False(t, ok, "a check with no row has no data; that is not zero")

	failing := &fakeQuerier{frames: func(synth.NamedQuery) ([]dataframe.Frame, error) { return nil, errors.New("boom") }}
	_, ok, err = fetchCheckSuccess(context.Background(), failing, "sm-uid", "web", "https://a", time.Unix(10_000, 0))
	require.Error(t, err)
	assert.False(t, ok)
}

func TestStatusMetrics_ReachabilityFailed(t *testing.T) {
	cs := []Check{checkOfType(1, "web", "https://a", "http")}
	failOnly := func(name string) *fakeQuerier {
		return &fakeQuerier{frames: func(q synth.NamedQuery) ([]dataframe.Frame, error) {
			if q.Name == name {
				return nil, errors.New("boom")
			}
			return nil, nil
		}}
	}

	reach := fetchStatusMetrics(context.Background(), failOnly("checks_reachability"), "sm-uid", cs, time.Unix(10_000, 0))
	assert.True(t, reach.reachabilityFailed, "losing reachability alone must be flagged: it decides every status")
	assert.Len(t, reach.failures, 1)

	for _, name := range []string{"checks_probe_count", "checks_latency"} {
		m := fetchStatusMetrics(context.Background(), failOnly(name), "sm-uid", cs, time.Unix(10_000, 0))
		assert.False(t, m.reachabilityFailed, "losing %s must not be flagged", name)
		assert.Len(t, m.failures, 1)
	}

	none := fetchStatusMetrics(context.Background(), failOnly("checks_reachability"), "sm-uid", nil, time.Unix(10_000, 0))
	assert.False(t, none.reachabilityFailed, "no checks means nothing was asked, so nothing failed")
}

func TestStatusMetrics_DistinctFailures(t *testing.T) {
	// Latency is one query per check type, so a backend that lacks the query
	// fails once per type with the same message. The user needs it said once.
	q := &fakeQuerier{frames: func(q synth.NamedQuery) ([]dataframe.Frame, error) {
		if q.Name == "checks_latency" {
			return nil, errors.New("unknown query")
		}
		return nil, nil
	}}
	cs := []Check{
		checkOfType(1, "a", "https://a", "http"),
		checkOfType(2, "b", "https://b", "scripted"),
		checkOfType(3, "c", "https://c", "dns"),
	}

	got := fetchStatusMetrics(context.Background(), q, "sm-uid", cs, time.Unix(10_000, 0))

	require.Len(t, got.failures, 3, "every failed query is still recorded")
	distinct := got.distinctFailures()
	require.Len(t, distinct, 1)
	assert.Contains(t, distinct[0].Error(), "checks_latency")
}
