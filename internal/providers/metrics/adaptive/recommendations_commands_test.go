package metrics_test

import (
	"fmt"
	"testing"

	"github.com/grafana/gcx/internal/providers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

//nolint:gochecknoglobals // Read-only fixtures shared by the recommendation tests.
var (
	recAddRule    = wireRule{Metric: "new_metric", MatchType: "exact", Action: "aggregate", Functions: []string{"sum"}, LabelPolicy: &wireLabelPolicy{Mode: "drop", Labels: []string{"pod"}}}
	recUpdateRule = wireRule{Metric: "a_metric", MatchType: "exact", Action: "aggregate", Functions: []string{"max"}, LabelPolicy: &wireLabelPolicy{Mode: "keep", Labels: []string{"job"}}}
)

func newRecommendationsFake(t *testing.T) *fakeRules {
	t.Helper()
	return seedRecommendations(newFakeRules(t))
}

func seedRecommendations(f *fakeRules) *fakeRules {
	return f.
		seed("default", ruleA, ruleB, ruleC).
		recommend("default", "new_metric", fakeRec{Action: "add", Rule: &recAddRule, CurrentSeries: 100, RecommendedSeries: 10}).
		recommend("default", "a_metric", fakeRec{Action: "update", Rule: &recUpdateRule, CurrentSeries: 50, RecommendedSeries: 5}).
		recommend("default", "b_metric", fakeRec{Action: "remove", CurrentSeries: 20}).
		recommend("default", "c_metric", fakeRec{Action: "keep"})
}

func TestRecommendationsDiff(t *testing.T) {
	t.Run("shows current and recommended rules per metric", func(t *testing.T) {
		forEachTransport(t, func(t *testing.T, tr rulesTransport, f *fakeRules, run func(...string) cmdResult) {
			t.Helper()
			seedRecommendations(f)
			res := run("recommendations", "diff", "new_metric", "a_metric", "b_metric", "-o", "json")
			require.NoError(t, res.err)
			assert.JSONEq(t, `[
				{"metric":"new_metric","action":"add","recommended_rule":{"metric":"new_metric","match_type":"exact","aggregations":["sum"],"drop_labels":["pod"]},"current_series":100,"recommended_series":10},
				{"metric":"a_metric","action":"update","current_rule":`+jsonA+`,"recommended_rule":{"metric":"a_metric","match_type":"exact","aggregations":["max"],"keep_labels":["job"]},"current_series":50,"recommended_series":5},
				{"metric":"b_metric","action":"remove","current_rule":`+jsonB+`,"current_series":20,"recommended_series":0}
			]`, res.stdout)
			assert.Empty(t, f.applied(tr.other(), ""))
			assert.Empty(t, f.requests("", "POST"))
			assert.Empty(t, f.requests("", "PUT"))
		})
	})
}

func TestRecommendationsDiff_Errors(t *testing.T) {
	f := newRecommendationsFake(t)
	loader := f.loader(t, fakeConfig{grafana: true})

	t.Run("metric without a recommendation", func(t *testing.T) {
		res := runAdaptiveCmd(t, loader, false, "recommendations", "diff", "nope")
		require.ErrorContains(t, res.err, "no recommendation found for nope")
	})

	t.Run("rule without a recommendation", func(t *testing.T) {
		g := newFakeRules(t).seed("default", ruleA)
		res := runAdaptiveCmd(t, g.loader(t, fakeConfig{grafana: true}), false, "recommendations", "diff", "a_metric")
		require.ErrorContains(t, res.err, "no recommendation found for a_metric")
	})

	t.Run("invalid metric sends nothing", func(t *testing.T) {
		g := newRecommendationsFake(t)
		res := runAdaptiveCmd(t, g.loader(t, fakeConfig{grafana: true}), false, "recommendations", "diff", "..")
		require.ErrorContains(t, res.err, "invalid metric")
		assert.Empty(t, g.requests("", ""))
	})

	t.Run("unknown segment", func(t *testing.T) {
		res := runAdaptiveCmd(t, loader, false, "recommendations", "diff", "a_metric", "--segment", "nope")
		require.ErrorContains(t, res.err, "segment not found: nope")
	})

	t.Run("requires a metric", func(t *testing.T) {
		res := runAdaptiveCmd(t, loader, false, "recommendations", "diff")
		require.ErrorContains(t, res.err, "at least one metric name")
	})
}

func TestRecommendationsApply_Selected(t *testing.T) {
	t.Run("applies in one write and reports per metric", func(t *testing.T) {
		forEachTransport(t, func(t *testing.T, tr rulesTransport, f *fakeRules, run func(...string) cmdResult) {
			t.Helper()
			seedRecommendations(f)
			res := run("recommendations", "apply", "new_metric", "a_metric", "b_metric", "c_metric", "--force", "-o", "json")
			require.NoError(t, res.err)

			assert.Contains(t, res.stderr, "Applied recommendation for new_metric.")
			assert.Contains(t, res.stderr, "Skipping c_metric: no change.")
			assert.Equal(t, []string{"a_metric", "c_metric", "new_metric"}, f.metrics("default"))
			stored, _ := f.stored("default", "a_metric")
			assert.Equal(t, []string{"max"}, stored.Functions)

			posts := f.applied(tr.via, "POST")
			require.Len(t, posts, 1)
			assert.Equal(t, "/v2/segments/default/rules:applyRecommendations", posts[0].Path)
			assert.JSONEq(t, `{"metrics":["new_metric","a_metric","b_metric","c_metric"]}`, posts[0].Body)
			assert.Empty(t, f.applied(tr.other(), ""))
			assert.Empty(t, f.requests("", "PUT"), "the server applies the recommendation, not the client")
			assert.Empty(t, f.requests("", "GET"), "--force needs no read")
		})
	})

	t.Run("custom segment", func(t *testing.T) {
		f := newFakeRules(t).seed("team-a", ruleB).recommend("team-a", "b_metric", fakeRec{Action: "remove"})
		res := runAdaptiveCmd(t, f.loader(t, fakeConfig{grafana: true}), false, "recommendations", "apply", "b_metric", "--segment", "team-a", "--force")
		require.NoError(t, res.err)
		assert.Empty(t, f.metrics("team-a"))
		posts := f.applied(viaProxy, "POST")
		require.Len(t, posts, 1)
		assert.Equal(t, "/v2/segments/team-a/rules:applyRecommendations", posts[0].Path)
	})

	t.Run("dry run previews with reads and writes nothing", func(t *testing.T) {
		forEachTransport(t, func(t *testing.T, tr rulesTransport, f *fakeRules, run func(...string) cmdResult) {
			t.Helper()
			seedRecommendations(f)
			res := run("recommendations", "apply", "new_metric", "c_metric", "--dry-run")
			require.NoError(t, res.err)
			assert.Contains(t, res.stderr, "new_metric: add (100 → 10 series)")
			assert.Contains(t, res.stderr, "c_metric: keep (no change)")
			assert.Empty(t, f.requests("", "POST"))
			assert.Empty(t, f.requests("", "PUT"))
			assert.Len(t, f.applied(tr.via, "GET"), 2)
		})
	})

	t.Run("declined confirmation writes nothing", func(t *testing.T) {
		f := newRecommendationsFake(t)
		res := runAdaptiveCmd(t, f.loader(t, fakeConfig{grafana: true}), false, "recommendations", "apply", "new_metric")
		require.Error(t, res.err)
		assert.Empty(t, f.requests("", "POST"))
	})

	t.Run("agent mode requires force", func(t *testing.T) {
		f := newRecommendationsFake(t)
		res := runAdaptiveCmd(t, f.loader(t, fakeConfig{grafana: true}), true, "recommendations", "apply", "new_metric")
		require.ErrorIs(t, res.err, providers.ErrAgentModeRequiresForce)
		assert.Empty(t, f.requests("", "POST"))
	})

	t.Run("nothing to change asks no confirmation", func(t *testing.T) {
		f := newRecommendationsFake(t)
		res := runAdaptiveCmd(t, f.loader(t, fakeConfig{grafana: true}), true, "recommendations", "apply", "c_metric")
		require.NoError(t, res.err)
		assert.Contains(t, res.stderr, "Skipping c_metric: no change.")
	})

	t.Run("a metric without a recommendation changes nothing", func(t *testing.T) {
		forEachTransport(t, func(t *testing.T, _ rulesTransport, f *fakeRules, run func(...string) cmdResult) {
			t.Helper()
			seedRecommendations(f)
			res := run("recommendations", "apply", "new_metric", "nope", "--force")
			require.ErrorContains(t, res.err, "no recommendation found for metrics: nope")
			assert.Equal(t, []string{"a_metric", "b_metric", "c_metric"}, f.metrics("default"))
		})
	})

	t.Run("rejects invalid input before any request", func(t *testing.T) {
		tooMany := make([]string, 101)
		for i := range tooMany {
			tooMany[i] = fmt.Sprintf("m%d", i)
		}
		for name, tc := range map[string]struct {
			args []string
			want string
		}{
			"more than 100 metrics": {append([]string{"recommendations", "apply", "--force"}, tooMany...), "at most 100"},
			"duplicate metric":      {[]string{"recommendations", "apply", "a", "a", "--force"}, "listed more than once"},
			"invalid metric":        {[]string{"recommendations", "apply", "..", "--force"}, "invalid metric"},
			"invalid segment":       {[]string{"recommendations", "apply", "a", "--segment", "..", "--force"}, "invalid segment"},
			"no metrics":            {[]string{"recommendations", "apply", "--force"}, "specify one or more metric names"},
			"all with metrics":      {[]string{"recommendations", "apply", "--all", "a", "--force"}, "mutually exclusive"},
		} {
			t.Run(name, func(t *testing.T) {
				f := newRecommendationsFake(t)
				res := runAdaptiveCmd(t, f.loader(t, fakeConfig{grafana: true, cloud: true}), false, tc.args...)
				require.ErrorContains(t, res.err, tc.want)
				assert.Empty(t, f.requests("", ""))
				assert.Zero(t, f.gcomCalls())
			})
		}
	})

	t.Run("exactly 100 metrics are accepted", func(t *testing.T) {
		f := newFakeRules(t)
		metrics := make([]string, 100)
		for i := range metrics {
			metrics[i] = fmt.Sprintf("m%d", i)
			f.recommend("default", metrics[i], fakeRec{Action: "keep"})
		}
		args := append([]string{"recommendations", "apply", "--force"}, metrics...)
		res := runAdaptiveCmd(t, f.loader(t, fakeConfig{grafana: true}), false, args...)
		require.NoError(t, res.err)
		assert.Len(t, f.applied(viaProxy, "POST"), 1)
	})
}

func TestRecommendationsApply_All(t *testing.T) {
	t.Run("applies the whole segment on the server", func(t *testing.T) {
		forEachTransport(t, func(t *testing.T, tr rulesTransport, f *fakeRules, run func(...string) cmdResult) {
			t.Helper()
			seedRecommendations(f)
			res := run("recommendations", "apply", "--all", "--force", "-o", "json")
			require.NoError(t, res.err)
			assert.Contains(t, res.stderr, "2 upserted, 1 deleted, 1 unchanged")
			assert.Equal(t, []string{"a_metric", "c_metric", "new_metric"}, f.metrics("default"))

			posts := f.applied(tr.via, "POST")
			require.Len(t, posts, 1)
			assert.JSONEq(t, `{"strategy":"all"}`, posts[0].Body)
			assert.Empty(t, f.requests("", "PUT"))
		})
	})

	t.Run("dry run counts from the summary and writes nothing", func(t *testing.T) {
		forEachTransport(t, func(t *testing.T, tr rulesTransport, f *fakeRules, run func(...string) cmdResult) {
			t.Helper()
			seedRecommendations(f)
			res := run("recommendations", "apply", "--all", "--dry-run")
			require.NoError(t, res.err)
			assert.Contains(t, res.stderr, "1 add, 1 update, 1 remove")
			assert.Empty(t, f.requests("", "POST"))
			assert.Len(t, f.applied(tr.via, "GET"), 1)
		})
	})

	t.Run("declined confirmation writes nothing", func(t *testing.T) {
		f := newRecommendationsFake(t)
		res := runAdaptiveCmd(t, f.loader(t, fakeConfig{grafana: true}), false, "recommendations", "apply", "--all")
		require.Error(t, res.err)
		assert.Empty(t, f.requests("", "POST"))
	})

	t.Run("skipped changes are a partial failure that names the reason", func(t *testing.T) {
		f := newRecommendationsFake(t)
		f.applyWarnings = []fakeApplyWarning{{Code: "rule_limit_reached", Skipped: 2}}
		res := runAdaptiveCmd(t, f.loader(t, fakeConfig{grafana: true}), false, "recommendations", "apply", "--all", "--force", "-o", "json")
		require.Error(t, res.err)
		assert.Contains(t, res.stderr, "rule_limit_reached: 2 recommendation(s) not applied")
		assert.Contains(t, res.stdout, "rule_limit_reached")
	})

	t.Run("server unavailable for apply is surfaced", func(t *testing.T) {
		f := newRecommendationsFake(t)
		f.fail(viaProxy, "POST", 503, `{"code":"internal","error":"segment-wide apply requires auto-apply to be configured"}`)
		res := runAdaptiveCmd(t, f.loader(t, fakeConfig{grafana: true}), false, "recommendations", "apply", "--all", "--force")
		require.ErrorContains(t, res.err, "requires auto-apply to be configured")
		assert.Empty(t, res.stdout)
	})
}
