package metrics_test

import (
	"encoding/json"
	"testing"

	cmdio "github.com/grafana/gcx/internal/output"
	metrics "github.com/grafana/gcx/internal/providers/metrics/adaptive"
	"github.com/grafana/gcx/internal/resources/adapter"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type metricsEnvelope struct {
	Items    []metrics.AdaptiveMetric `json:"items"`
	ListMeta *cmdio.ListMeta          `json:"list_meta"`
}

func metricRecommendation() fakeRec {
	return fakeRec{
		Action:             "update",
		Rule:               &ruleB,
		CurrentSeries:      80,
		RecommendedSeries:  20,
		RawSeries:          100,
		CurrentReduction:   20,
		ProjectedChange:    -60,
		StatsUpdatedAt:     "2026-01-02T03:04:05Z",
		UsagesInRules:      1,
		UsagesInQueries:    2,
		UsagesInDashboards: 3,
	}
}

func TestMetricsList(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		forEachTransport(t, func(t *testing.T, _ rulesTransport, _ *fakeRules, run func(...string) cmdResult) {
			t.Helper()
			res := run("metrics", "list", "-o", "json")
			require.NoError(t, res.err)
			assert.JSONEq(t, `{"items":[]}`, res.stdout)
			assert.Contains(t, res.stderr, "0 metric(s)")
		})
	})

	t.Run("catalog and default sort", func(t *testing.T) {
		forEachTransport(t, func(t *testing.T, tr rulesTransport, f *fakeRules, run func(...string) cmdResult) {
			t.Helper()
			f.recommend("default", "a_metric", metricRecommendation()).
				recommend("default", "b_metric", fakeRec{Action: "keep"}).
				recommend("default", "c_metric", fakeRec{Action: "none"})

			res := run("metrics", "list", "-o", "json")
			require.NoError(t, res.err)
			assert.JSONEq(t, `{
				"items": [
					{"metric":"a_metric","recommended_action":"update","raw_series_count":100,"current_series_count_reduction":20,"projected_series_count_change":-60,"stats_updated_at":"2026-01-02T03:04:05Z"},
					{"metric":"b_metric","recommended_action":"keep","raw_series_count":0,"current_series_count_reduction":0,"projected_series_count_change":0},
					{"metric":"c_metric","recommended_action":"none","raw_series_count":0,"current_series_count_reduction":0,"projected_series_count_change":0}
				]
			}`, res.stdout)
			assert.Contains(t, res.stderr, "3 metric(s)")

			reqs := f.applied(tr.via, "GET")
			require.Len(t, reqs, 1)
			assert.Equal(t, "51", reqs[0].Query.Get("page_size"))
			assert.Equal(t, "projected_series_count_change", reqs[0].Query.Get("sort"))
			assert.Equal(t, "asc", reqs[0].Query.Get("sort_direction"))
			assert.Empty(t, reqs[0].Query["action"])
		})
	})

	t.Run("action filter", func(t *testing.T) {
		forEachTransport(t, func(t *testing.T, tr rulesTransport, f *fakeRules, run func(...string) cmdResult) {
			t.Helper()
			f.recommend("default", "a_metric", fakeRec{Action: "add"}).
				recommend("default", "b_metric", fakeRec{Action: "keep"}).
				recommend("default", "c_metric", fakeRec{Action: "remove"})

			res := run("metrics", "list", "--action", "add", "--action", "keep", "-o", "json")
			require.NoError(t, res.err)
			assert.JSONEq(t, `{
				"items": [
					{"metric":"a_metric","recommended_action":"add","raw_series_count":0,"current_series_count_reduction":0,"projected_series_count_change":0},
					{"metric":"b_metric","recommended_action":"keep","raw_series_count":0,"current_series_count_reduction":0,"projected_series_count_change":0}
				]
			}`, res.stdout)

			reqs := f.applied(tr.via, "GET")
			require.Len(t, reqs, 1)
			assert.Equal(t, []string{"add", "keep"}, reqs[0].Query["action"])
		})
	})

	t.Run("sorts", func(t *testing.T) {
		f := newFakeRules(t)
		loader := f.loader(t, fakeConfig{grafana: true})
		tests := []struct {
			sort      string
			direction string
		}{
			{sort: "projected-change", direction: "asc"},
			{sort: "raw-series", direction: "desc"},
			{sort: "current-reduction", direction: "desc"},
			{sort: "metric", direction: "asc"},
		}
		for _, tt := range tests {
			t.Run(tt.sort, func(t *testing.T) {
				res := runAdaptiveCmd(t, loader, false, "metrics", "list", "--sort", tt.sort, "-o", "json")
				require.NoError(t, res.err)
				reqs := f.applied(viaProxy, "GET")
				require.NotEmpty(t, reqs)
				last := reqs[len(reqs)-1]
				assert.Equal(t, tt.direction, last.Query.Get("sort_direction"))
				assert.NotEmpty(t, last.Query.Get("sort"))
			})
		}
	})

	t.Run("table and wide", func(t *testing.T) {
		forEachTransport(t, func(t *testing.T, _ rulesTransport, f *fakeRules, run func(...string) cmdResult) {
			t.Helper()
			f.recommend("default", "a_metric", metricRecommendation())

			table := run("metrics", "list")
			require.NoError(t, table.err)
			assert.Contains(t, table.stdout, "METRIC")
			assert.Contains(t, table.stdout, "ACTION")
			assert.Contains(t, table.stdout, "RAW SERIES")
			assert.Contains(t, table.stdout, "CURRENT REDUCTION")
			assert.Contains(t, table.stdout, "PROJECTED CHANGE")
			assert.NotContains(t, table.stdout, "STATS UPDATED")
			assert.NotContains(t, table.stdout, "URGENT")
			assert.Contains(t, table.stdout, "a_metric")
			assert.Contains(t, table.stdout, "update")

			wide := run("metrics", "list", "-o", "wide")
			require.NoError(t, wide.err)
			assert.Contains(t, wide.stdout, "STATS UPDATED")
			assert.Contains(t, wide.stdout, "2026-01-02T03:04:05Z")
		})
	})

	t.Run("limit truncates and hints", func(t *testing.T) {
		forEachTransport(t, func(t *testing.T, tr rulesTransport, f *fakeRules, run func(...string) cmdResult) {
			t.Helper()
			f.recommend("default", "a_metric", fakeRec{Action: "add"}).
				recommend("default", "b_metric", fakeRec{Action: "add"}).
				recommend("default", "c_metric", fakeRec{Action: "add"})
			f.pageSize = 1

			res := run("metrics", "list", "--limit", "2", "-o", "json")
			require.NoError(t, res.err)
			var got metricsEnvelope
			require.NoError(t, json.Unmarshal([]byte(res.stdout), &got))
			require.Len(t, got.Items, 2)
			assert.Equal(t, "a_metric", got.Items[0].Metric)
			assert.Equal(t, "b_metric", got.Items[1].Metric)
			require.NotNil(t, got.ListMeta)
			assert.True(t, got.ListMeta.Truncated)
			assert.Equal(t, 2, got.ListMeta.Returned)
			assert.Contains(t, res.stderr, "showing first 2")
			assert.Contains(t, res.stderr, "2 metric(s)")
			assert.Len(t, f.applied(tr.via, "GET"), 3)
		})
	})

	t.Run("limit zero drains", func(t *testing.T) {
		forEachTransport(t, func(t *testing.T, tr rulesTransport, f *fakeRules, run func(...string) cmdResult) {
			t.Helper()
			for _, m := range []string{"a_metric", "b_metric", "c_metric", "d_metric", "e_metric"} {
				f.recommend("default", m, fakeRec{Action: "add"})
			}
			f.pageSize = 2

			res := run("metrics", "list", "--limit", "0", "-o", "json")
			require.NoError(t, res.err)
			var got metricsEnvelope
			require.NoError(t, json.Unmarshal([]byte(res.stdout), &got))
			assert.Len(t, got.Items, 5)
			assert.Nil(t, got.ListMeta)
			assert.NotContains(t, res.stderr, "showing first")
			assert.Len(t, f.applied(tr.via, "GET"), 3)
		})
	})

	t.Run("negative limit is rejected before any request", func(t *testing.T) {
		f := newFakeRules(t)
		res := runAdaptiveCmd(t, f.loader(t, fakeConfig{grafana: true}), false, "metrics", "list", "--limit", "-1")
		require.ErrorContains(t, res.err, "invalid --limit -1")
		assert.Empty(t, f.requests("", ""))
	})

	t.Run("invalid sort and action are rejected before any request", func(t *testing.T) {
		f := newFakeRules(t)
		loader := f.loader(t, fakeConfig{grafana: true})
		for _, args := range [][]string{
			{"metrics", "list", "--sort", "nope"},
			{"metrics", "list", "--action", "nope"},
		} {
			res := runAdaptiveCmd(t, loader, false, args...)
			require.ErrorContains(t, res.err, "invalid")
		}
		assert.Empty(t, f.requests("", ""))
	})

	t.Run("segment is escaped into the path", func(t *testing.T) {
		forEachTransport(t, func(t *testing.T, tr rulesTransport, f *fakeRules, run func(...string) cmdResult) {
			t.Helper()
			f.recommend("team a/b", "a_metric", fakeRec{Action: "add"})
			res := run("metrics", "list", "--segment", "team a/b", "-o", "json")
			require.NoError(t, res.err)
			reqs := f.applied(tr.via, "GET")
			require.Len(t, reqs, 1)
			assert.Contains(t, reqs[0].Path, "/segments/team%20a%2Fb/metrics")
		})
	})

	t.Run("unknown segment", func(t *testing.T) {
		forEachTransport(t, func(t *testing.T, _ rulesTransport, _ *fakeRules, run func(...string) cmdResult) {
			t.Helper()
			res := run("metrics", "list", "--segment", "nope")
			require.ErrorContains(t, res.err, "segment not found: nope")
			assert.Empty(t, res.stdout)
		})
	})

	t.Run("server error is surfaced", func(t *testing.T) {
		forEachTransport(t, func(t *testing.T, tr rulesTransport, f *fakeRules, run func(...string) cmdResult) {
			t.Helper()
			f.fail(tr.via, "GET", 500, `{"code":"internal","error":"failed to list metrics","trace_id":"abc123"}`)
			res := run("metrics", "list")
			require.ErrorContains(t, res.err, "failed to list metrics")
			assert.Contains(t, res.err.Error(), "abc123")
		})
	})

	t.Run("repeated page token is rejected", func(t *testing.T) {
		f := newFakeRules(t)
		f.fail(viaDirect, "", 200, `{"metrics":[],"next_page_token":"same"}`)
		res := runAdaptiveCmd(t, f.loader(t, fakeConfig{cloud: true}), false, "metrics", "list", "--limit", "0")
		require.ErrorContains(t, res.err, "repeated a page token")
	})
}

func TestMetricsListFieldSelection(t *testing.T) {
	f := newFakeRules(t)
	f.recommend("default", "a_metric", fakeRec{Action: "keep"}).
		recommend("default", "b_metric", fakeRec{Action: "keep"})
	loader := f.loader(t, fakeConfig{grafana: true})

	complete := runAdaptiveCmd(t, loader, false, "metrics", "list", "-o", "json", "--json", "metric,recommended_action")
	require.NoError(t, complete.err)
	assert.JSONEq(t, `{"items":[
		{"metric":"a_metric","recommended_action":"keep"},
		{"metric":"b_metric","recommended_action":"keep"}
	]}`, complete.stdout)

	f.pageSize = 1
	truncated := runAdaptiveCmd(t, loader, false, "metrics", "list", "--limit", "1", "-o", "json", "--json", "metric,recommended_action")
	require.NoError(t, truncated.err)
	var got struct {
		Items    []map[string]any `json:"items"`
		ListMeta *cmdio.ListMeta  `json:"list_meta"`
	}
	require.NoError(t, json.Unmarshal([]byte(truncated.stdout), &got))
	require.Len(t, got.Items, 1)
	require.NotNil(t, got.ListMeta)
	assert.True(t, got.ListMeta.Truncated)
}

func TestMetricsGet(t *testing.T) {
	t.Run("yaml default", func(t *testing.T) {
		forEachTransport(t, func(t *testing.T, _ rulesTransport, f *fakeRules, run func(...string) cmdResult) {
			t.Helper()
			f.seed("default", ruleA).recommend("default", "a_metric", metricRecommendation())

			res := run("metrics", "get", "a_metric")
			require.NoError(t, res.err)
			assert.Contains(t, res.stdout, "metric: a_metric")
			assert.Contains(t, res.stdout, "recommended_action: update")
			assert.Contains(t, res.stdout, "current_rule:")
			assert.Contains(t, res.stdout, "recommended_rule:")
			assert.Contains(t, res.stdout, "stats:")
			assert.Contains(t, res.stdout, "raw_series_count: 100")
			assert.NotContains(t, res.stdout, "urgent")
			assert.NotContains(t, res.stdout, "dashboard_uids")
		})
	})

	t.Run("json shape", func(t *testing.T) {
		forEachTransport(t, func(t *testing.T, _ rulesTransport, f *fakeRules, run func(...string) cmdResult) {
			t.Helper()
			f.seed("default", ruleA).recommend("default", "a_metric", metricRecommendation())

			res := run("metrics", "get", "a_metric", "-o", "json")
			require.NoError(t, res.err)
			var got metrics.AdaptiveMetricDetail
			require.NoError(t, json.Unmarshal([]byte(res.stdout), &got))
			assert.Equal(t, "a_metric", got.Metric)
			assert.Equal(t, "update", got.RecommendedAction)
			require.NotNil(t, got.CurrentRule)
			assert.Equal(t, []string{"sum", "count"}, got.CurrentRule.Aggregations)
			assert.Equal(t, []string{"pod"}, got.CurrentRule.DropLabels)
			require.NotNil(t, got.RecommendedRule)
			assert.True(t, got.RecommendedRule.Drop)
			require.NotNil(t, got.Stats)
			assert.Equal(t, 100, got.Stats.RawSeriesCount)
			assert.Equal(t, 80, got.Stats.CurrentSeriesCount)
			assert.Equal(t, 20, got.Stats.RecommendedSeriesCount)
			assert.Equal(t, 1, got.Stats.UsagesInRules)
			assert.Equal(t, 2, got.Stats.UsagesInQueries)
			assert.Equal(t, 3, got.Stats.UsagesInDashboards)
			assert.NotContains(t, res.stdout, "urgent")
			assert.NotContains(t, res.stdout, "dashboard_uids")
		})
	})

	t.Run("missing metric is not found", func(t *testing.T) {
		forEachTransport(t, func(t *testing.T, _ rulesTransport, f *fakeRules, run func(...string) cmdResult) {
			t.Helper()
			res := run("metrics", "get", "nope")
			require.ErrorIs(t, res.err, adapter.ErrNotFound)
			assert.Contains(t, res.err.Error(), "metric nope")
		})
	})

	t.Run("invalid metric sends nothing", func(t *testing.T) {
		forEachTransport(t, func(t *testing.T, _ rulesTransport, f *fakeRules, run func(...string) cmdResult) {
			t.Helper()
			res := run("metrics", "get", "..")
			require.ErrorContains(t, res.err, "invalid metric")
			assert.Empty(t, f.requests("", ""))
		})
	})

	t.Run("rule-only metric is not found", func(t *testing.T) {
		forEachTransport(t, func(t *testing.T, _ rulesTransport, f *fakeRules, run func(...string) cmdResult) {
			t.Helper()
			f.seed("default", ruleB)

			res := run("metrics", "get", "b_metric")
			require.ErrorIs(t, res.err, adapter.ErrNotFound)
			assert.Contains(t, res.err.Error(), "metric b_metric")
			assert.Empty(t, res.stdout)
		})
	})

	t.Run("catalog metric without an action is returned", func(t *testing.T) {
		forEachTransport(t, func(t *testing.T, _ rulesTransport, f *fakeRules, run func(...string) cmdResult) {
			t.Helper()
			f.seed("default", ruleB).recommend("default", "b_metric", fakeRec{Action: ""})

			res := run("metrics", "get", "b_metric", "-o", "json")
			require.NoError(t, res.err)
			var got metrics.AdaptiveMetricDetail
			require.NoError(t, json.Unmarshal([]byte(res.stdout), &got))
			assert.Empty(t, got.RecommendedAction)
			require.NotNil(t, got.CurrentRule)
			require.NotNil(t, got.Stats)
			assert.NotContains(t, res.stdout, "recommended_action")
		})
	})

	t.Run("catalog metric without a rule", func(t *testing.T) {
		forEachTransport(t, func(t *testing.T, _ rulesTransport, f *fakeRules, run func(...string) cmdResult) {
			t.Helper()
			f.recommend("default", "none_metric", fakeRec{Action: "none"})

			res := run("metrics", "get", "none_metric", "-o", "json")
			require.NoError(t, res.err)
			var got metrics.AdaptiveMetricDetail
			require.NoError(t, json.Unmarshal([]byte(res.stdout), &got))
			assert.Equal(t, "none", got.RecommendedAction)
			assert.Nil(t, got.CurrentRule)
			assert.Nil(t, got.RecommendedRule)
			require.NotNil(t, got.Stats)
		})
	})

	t.Run("server error is surfaced", func(t *testing.T) {
		forEachTransport(t, func(t *testing.T, tr rulesTransport, f *fakeRules, run func(...string) cmdResult) {
			t.Helper()
			f.fail(tr.via, "GET", 500, `{"code":"internal","error":"failed to get metric","trace_id":"abc123"}`)
			res := run("metrics", "get", "a_metric")
			require.ErrorContains(t, res.err, "failed to get metric")
			assert.Contains(t, res.err.Error(), "abc123")
		})
	})
}
