package metrics_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/providers"
	metrics "github.com/grafana/gcx/internal/providers/metrics/adaptive"
	"github.com/grafana/gcx/internal/resources/adapter"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

const (
	routeDeniedBody   = `{"message":"plugin proxy route access denied"}`
	pluginMissingBody = `{"message":"Plugin not found, no installed plugin with that id"}`
)

//nolint:gochecknoglobals // Read-only fixtures shared by the rules tests.
var (
	ruleA = wireRule{Metric: "a_metric", MatchType: "exact", Action: "aggregate", Functions: []string{"sum", "count"}, LabelPolicy: &wireLabelPolicy{Mode: "drop", Labels: []string{"pod"}}}
	ruleB = wireRule{Metric: "b_metric", MatchType: "exact", Action: "drop"}
	ruleC = wireRule{Metric: "c_metric", MatchType: "prefix", Action: "aggregate", Functions: []string{"max"}, Interval: "1m", Delay: "5m", LabelPolicy: &wireLabelPolicy{Mode: "keep", Labels: []string{"job", "instance"}}}
)

const (
	jsonA = `{"metric":"a_metric","match_type":"exact","aggregations":["sum","count"],"drop_labels":["pod"]}`
	jsonB = `{"metric":"b_metric","match_type":"exact","drop":true}`
	jsonC = `{"metric":"c_metric","match_type":"prefix","keep_labels":["job","instance"],"aggregations":["max"],"aggregation_interval":"1m","aggregation_delay":"5m"}`
)

type rulesTransport struct {
	name    string
	cfg     fakeConfig
	prepare func(*fakeRules)
	via     string
}

func rulesTransports() []rulesTransport {
	both := fakeConfig{grafana: true, cloud: true}
	return []rulesTransport{
		{name: "proxy", cfg: both, via: viaProxy},
		{name: "proxy without cloud config", cfg: fakeConfig{grafana: true}, via: viaProxy},
		{name: "direct without grafana config", cfg: fakeConfig{cloud: true}, via: viaDirect},
		{name: "direct after route denied", cfg: both, via: viaDirect, prepare: func(f *fakeRules) { f.fail(viaProxy, "", 403, routeDeniedBody) }},
		{name: "direct after plugin missing", cfg: both, via: viaDirect, prepare: func(f *fakeRules) { f.fail(viaProxy, "", 404, pluginMissingBody) }},
	}
}

func (tr rulesTransport) other() string {
	if tr.via == viaProxy {
		return viaDirect
	}
	return viaProxy
}

// forEachTransport runs fn once per transport with a fresh fake, so every
// command is exercised through each way of reaching the API.
func forEachTransport(t *testing.T, fn func(t *testing.T, tr rulesTransport, f *fakeRules, run func(args ...string) cmdResult)) {
	t.Helper()
	for _, tr := range rulesTransports() {
		t.Run(tr.name, func(t *testing.T) {
			f := newFakeRules(t)
			if tr.prepare != nil {
				tr.prepare(f)
			}
			loader := f.loader(t, tr.cfg)
			fn(t, tr, f, func(args ...string) cmdResult {
				return runAdaptiveCmd(t, loader, false, args...)
			})
		})
	}
}

func TestRulesList(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		forEachTransport(t, func(t *testing.T, _ rulesTransport, _ *fakeRules, run func(...string) cmdResult) {
			t.Helper()
			res := run("rules", "list", "-o", "json")
			require.NoError(t, res.err)
			assert.JSONEq(t, `[]`, res.stdout)
			assert.Contains(t, res.stderr, "0 rule(s)")
		})
	})

	t.Run("converts v2 rules to the existing output shape", func(t *testing.T) {
		forEachTransport(t, func(t *testing.T, tr rulesTransport, f *fakeRules, run func(...string) cmdResult) {
			t.Helper()
			f.seed("default", ruleC, ruleA, ruleB)
			res := run("rules", "list", "-o", "json")
			require.NoError(t, res.err)
			assert.JSONEq(t, "["+jsonA+","+jsonB+","+jsonC+"]", res.stdout)
			assert.Contains(t, res.stderr, "3 rule(s)")
			assert.NotContains(t, res.stderr, "showing first")
			reqs := f.applied(tr.via, "GET")
			require.Len(t, reqs, 1)
			assert.Equal(t, "51", reqs[0].Query.Get("page_size"))
		})
	})

	t.Run("limit truncates and hints", func(t *testing.T) {
		forEachTransport(t, func(t *testing.T, tr rulesTransport, f *fakeRules, run func(...string) cmdResult) {
			t.Helper()
			f.seed("default", ruleA, ruleB, ruleC)
			f.pageSize = 1
			res := run("rules", "list", "--limit", "2", "-o", "json")
			require.NoError(t, res.err)
			assert.JSONEq(t, "["+jsonA+","+jsonB+"]", res.stdout)
			assert.Contains(t, res.stderr, "showing first 2")
			assert.Contains(t, res.stderr, "2 rule(s)")
			assert.Len(t, f.applied(tr.via, "GET"), 3)
		})
	})

	t.Run("limit equal to the rule count does not hint", func(t *testing.T) {
		forEachTransport(t, func(t *testing.T, _ rulesTransport, f *fakeRules, run func(...string) cmdResult) {
			t.Helper()
			f.seed("default", ruleA, ruleB)
			res := run("rules", "list", "--limit", "2", "-o", "json")
			require.NoError(t, res.err)
			assert.JSONEq(t, "["+jsonA+","+jsonB+"]", res.stdout)
			assert.NotContains(t, res.stderr, "showing first")
		})
	})

	t.Run("negative limit is rejected before any request", func(t *testing.T) {
		f := newFakeRules(t)
		res := runAdaptiveCmd(t, f.loader(t, fakeConfig{grafana: true, cloud: true}), false, "rules", "list", "--limit", "-1")
		require.ErrorContains(t, res.err, "invalid --limit -1")
		assert.Empty(t, f.requests("", ""))
	})

	t.Run("limit zero follows every page", func(t *testing.T) {
		forEachTransport(t, func(t *testing.T, tr rulesTransport, f *fakeRules, run func(...string) cmdResult) {
			t.Helper()
			for _, m := range []string{"m1", "m2", "m3", "m4", "m5"} {
				f.seed("default", wireRule{Metric: m, Action: "drop"})
			}
			f.pageSize = 2
			res := run("rules", "list", "--limit", "0", "-o", "json")
			require.NoError(t, res.err)
			var got []map[string]any
			require.NoError(t, json.Unmarshal([]byte(res.stdout), &got))
			assert.Len(t, got, 5)
			assert.NotContains(t, res.stderr, "showing first")
			reqs := f.applied(tr.via, "GET")
			require.Len(t, reqs, 3)
			assert.Empty(t, reqs[0].Query.Get("page_token"))
			assert.Equal(t, "m2", reqs[1].Query.Get("page_token"))
			assert.Equal(t, "m4", reqs[2].Query.Get("page_token"))
		})
	})

	t.Run("segment is escaped into the path", func(t *testing.T) {
		forEachTransport(t, func(t *testing.T, tr rulesTransport, f *fakeRules, run func(...string) cmdResult) {
			t.Helper()
			f.seed("team a/b", ruleB)
			res := run("rules", "list", "--segment", "team a/b", "-o", "json")
			require.NoError(t, res.err)
			assert.JSONEq(t, "["+jsonB+"]", res.stdout)
			reqs := f.applied(tr.via, "GET")
			require.Len(t, reqs, 1)
			assert.Contains(t, reqs[0].Path, "/segments/team%20a%2Fb/rules")
		})
	})

	t.Run("unknown segment", func(t *testing.T) {
		forEachTransport(t, func(t *testing.T, _ rulesTransport, _ *fakeRules, run func(...string) cmdResult) {
			t.Helper()
			res := run("rules", "list", "--segment", "nope")
			require.ErrorContains(t, res.err, "segment not found: nope")
			assert.Empty(t, res.stdout)
		})
	})

	t.Run("invalid segment sends nothing", func(t *testing.T) {
		forEachTransport(t, func(t *testing.T, _ rulesTransport, f *fakeRules, run func(...string) cmdResult) {
			t.Helper()
			res := run("rules", "list", "--segment", "..")
			require.ErrorContains(t, res.err, "invalid segment")
			assert.Empty(t, f.requests("", ""))
			assert.Zero(t, f.gcomCalls())
		})
	})

	t.Run("server error is surfaced", func(t *testing.T) {
		forEachTransport(t, func(t *testing.T, tr rulesTransport, f *fakeRules, run func(...string) cmdResult) {
			t.Helper()
			f.fail(tr.via, "GET", 500, `{"code":"internal","error":"failed to list rules","trace_id":"abc123"}`)
			res := run("rules", "list")
			require.ErrorContains(t, res.err, "failed to list rules")
			assert.Contains(t, res.err.Error(), "abc123")
		})
	})

	t.Run("repeated page token is rejected", func(t *testing.T) {
		f := newFakeRules(t)
		f.seed("default", ruleA, ruleB)
		f.pageSize = 1
		f.fail(viaDirect, "", 200, `{"rules":[{"metric":"x","action":"drop"}],"next_page_token":"same"}`)
		res := runAdaptiveCmd(t, f.loader(t, fakeConfig{cloud: true}), false, "rules", "list", "--limit", "0")
		require.ErrorContains(t, res.err, "repeated a page token")
	})

	t.Run("table, wide and yaml output", func(t *testing.T) {
		forEachTransport(t, func(t *testing.T, _ rulesTransport, f *fakeRules, run func(...string) cmdResult) {
			t.Helper()
			f.seed("default", ruleA, ruleC)

			table := run("rules", "list")
			require.NoError(t, table.err)
			assert.Contains(t, table.stdout, "METRIC")
			assert.Contains(t, table.stdout, "a_metric")
			assert.Contains(t, table.stdout, "sum,count")
			assert.NotContains(t, table.stdout, "INTERVAL")

			wide := run("rules", "list", "-o", "wide")
			require.NoError(t, wide.err)
			assert.Contains(t, wide.stdout, "INTERVAL")
			assert.NotContains(t, wide.stdout, "MANAGED BY")
			assert.Contains(t, wide.stdout, "1m")

			yml := run("rules", "list", "-o", "yaml")
			require.NoError(t, yml.err)
			assert.Contains(t, yml.stdout, "metric: a_metric")
			assert.Contains(t, yml.stdout, "drop_labels:")
		})
	})
}

func TestRulesGet(t *testing.T) {
	for name, tc := range map[string]struct {
		rule wireRule
		want string
	}{"aggregate dropping labels": {ruleA, jsonA}, "drop": {ruleB, jsonB}, "aggregate keeping labels": {ruleC, jsonC}} {
		t.Run(name, func(t *testing.T) {
			forEachTransport(t, func(t *testing.T, tr rulesTransport, f *fakeRules, run func(...string) cmdResult) {
				t.Helper()
				f.seed("default", tc.rule)
				res := run("rules", "get", tc.rule.Metric)
				require.NoError(t, res.err)
				assert.JSONEq(t, tc.want, res.stdout)
				reqs := f.applied(tr.via, "GET")
				require.Len(t, reqs, 1)
				assert.Equal(t, "/v2/segments/default/rules/"+tc.rule.Metric, reqs[0].Path)
			})
		})
	}

	t.Run("custom segment and yaml", func(t *testing.T) {
		forEachTransport(t, func(t *testing.T, _ rulesTransport, f *fakeRules, run func(...string) cmdResult) {
			t.Helper()
			f.seed("team-a", ruleA)
			res := run("rules", "get", "a_metric", "--segment", "team-a", "-o", "yaml")
			require.NoError(t, res.err)
			assert.Contains(t, res.stdout, "metric: a_metric")
		})
	})

	t.Run("metric is escaped into the path", func(t *testing.T) {
		forEachTransport(t, func(t *testing.T, tr rulesTransport, f *fakeRules, run func(...string) cmdResult) {
			t.Helper()
			f.seed("default", wireRule{Metric: "job:http/requests", Action: "drop"})
			res := run("rules", "get", "job:http/requests")
			require.NoError(t, res.err)
			reqs := f.applied(tr.via, "GET")
			require.Len(t, reqs, 1)
			assert.Contains(t, reqs[0].Path, "/rules/job:http%2Frequests")
		})
	})

	t.Run("missing rule is not found", func(t *testing.T) {
		forEachTransport(t, func(t *testing.T, _ rulesTransport, _ *fakeRules, run func(...string) cmdResult) {
			t.Helper()
			res := run("rules", "get", "nope")
			require.ErrorIs(t, res.err, metrics.ErrRuleNotFound)
			require.ErrorIs(t, res.err, adapter.ErrNotFound)
		})
	})

	t.Run("missing segment is not reported as a missing rule", func(t *testing.T) {
		forEachTransport(t, func(t *testing.T, _ rulesTransport, _ *fakeRules, run func(...string) cmdResult) {
			t.Helper()
			res := run("rules", "get", "a_metric", "--segment", "nope")
			require.ErrorContains(t, res.err, "segment not found: nope")
			assert.NotErrorIs(t, res.err, metrics.ErrRuleNotFound)
		})
	})

	for _, metric := range []string{"..", ".", " "} {
		t.Run("invalid metric "+metric+" sends nothing", func(t *testing.T) {
			forEachTransport(t, func(t *testing.T, _ rulesTransport, f *fakeRules, run func(...string) cmdResult) {
				t.Helper()
				res := run("rules", "get", metric)
				require.ErrorContains(t, res.err, "invalid metric")
				assert.Empty(t, f.requests("", ""))
			})
		})
	}

	t.Run("requires one argument", func(t *testing.T) {
		forEachTransport(t, func(t *testing.T, _ rulesTransport, f *fakeRules, run func(...string) cmdResult) {
			t.Helper()
			require.Error(t, run("rules", "get").err)
			require.Error(t, run("rules", "get", "a", "b").err)
			assert.Empty(t, f.requests("", ""))
		})
	})
}

func TestRulesCreate(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		seed     []wireRule
		wantWire *wireRule
		wantJSON string
		wantErr  string
		noReqs   bool
	}{
		{
			name:     "aggregate dropping labels",
			args:     []string{"--metric", "m", "--aggregations", "sum,count", "--drop-labels", "pod,instance"},
			wantWire: &wireRule{Metric: "m", MatchType: "exact", Action: "aggregate", Functions: []string{"sum", "count"}, LabelPolicy: &wireLabelPolicy{Mode: "drop", Labels: []string{"pod", "instance"}}},
			wantJSON: `{"metric":"m","match_type":"exact","aggregations":["sum","count"],"drop_labels":["pod","instance"]}`,
		},
		{
			name:     "aggregate keeping labels with options",
			args:     []string{"--metric", "m", "--match-type", "prefix", "--aggregations", "max", "--keep-labels", "job", "--aggregation-interval", "1m", "--aggregation-delay", "5m"},
			wantWire: &wireRule{Metric: "m", MatchType: "prefix", Action: "aggregate", Functions: []string{"max"}, Interval: "1m", Delay: "5m", LabelPolicy: &wireLabelPolicy{Mode: "keep", Labels: []string{"job"}}},
			wantJSON: `{"metric":"m","match_type":"prefix","keep_labels":["job"],"aggregations":["max"],"aggregation_interval":"1m","aggregation_delay":"5m"}`,
		},
		{
			name:     "drop",
			args:     []string{"--metric", "m", "--drop"},
			wantWire: &wireRule{Metric: "m", MatchType: "exact", Action: "drop"},
			wantJSON: `{"metric":"m","match_type":"exact","drop":true}`,
		},
		{name: "fails when the rule exists", args: []string{"--metric", "a_metric", "--drop"}, seed: []wireRule{ruleA}, wantErr: "a rule for a_metric already exists"},
		{name: "both label kinds", args: []string{"--metric", "m", "--aggregations", "sum", "--keep-labels", "a", "--drop-labels", "b"}, wantErr: "both dropped and kept labels", noReqs: true},
		{name: "no labels", args: []string{"--metric", "m", "--aggregations", "sum"}, wantErr: "no labels to aggregate on", noReqs: true},
		{name: "drop with aggregations", args: []string{"--metric", "m", "--drop", "--aggregations", "sum"}, wantErr: "drop and aggregate", noReqs: true},
		{name: "invalid metric", args: []string{"--metric", ".."}, wantErr: "invalid metric", noReqs: true},
		{name: "missing metric flag", args: []string{"--drop"}, wantErr: "metric", noReqs: true},
		{name: "no aggregations", args: []string{"--metric", "m", "--drop-labels", "pod"}, wantErr: "at least one aggregation", noReqs: true},
		{name: "drop with interval", args: []string{"--metric", "m", "--drop", "--aggregation-interval", "1m"}, wantErr: "interval or delay", noReqs: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			forEachTransport(t, func(t *testing.T, tr rulesTransport, f *fakeRules, run func(...string) cmdResult) {
				t.Helper()
				f.seed("default", tt.seed...)
				res := run(append([]string{"rules", "create"}, tt.args...)...)

				if tt.wantErr != "" {
					require.ErrorContains(t, res.err, tt.wantErr)
					assert.Empty(t, res.stdout)
					assert.Empty(t, f.applied("", "PUT"), "a failed create must not write")
					if tt.noReqs {
						assert.Empty(t, f.requests("", ""))
						assert.Zero(t, f.gcomCalls())
					}
					return
				}

				require.NoError(t, res.err)
				assert.JSONEq(t, tt.wantJSON, res.stdout)
				assert.Contains(t, res.stderr, "Created rule for "+tt.wantWire.Metric+".")
				stored, ok := f.stored("default", tt.wantWire.Metric)
				require.True(t, ok)
				assert.Equal(t, *tt.wantWire, stored)
				assert.Len(t, f.requests(tr.via, "GET"), 1)

				puts := f.applied(tr.via, "PUT")
				require.Len(t, puts, 1)
				assert.Equal(t, "/v2/segments/default/rules/"+tt.wantWire.Metric, puts[0].Path)
				assert.Empty(t, f.applied(tr.other(), "PUT"))
				var sent wireRule
				require.NoError(t, json.Unmarshal([]byte(puts[0].Body), &sent))
				assert.Equal(t, *tt.wantWire, sent)
			})
		})
	}

	t.Run("custom segment", func(t *testing.T) {
		forEachTransport(t, func(t *testing.T, _ rulesTransport, f *fakeRules, run func(...string) cmdResult) {
			t.Helper()
			f.seed("team-a")
			res := run("rules", "create", "--metric", "m", "--drop", "--segment", "team-a")
			require.NoError(t, res.err)
			assert.Contains(t, res.stderr, "Created rule for m in segment team-a.")
			_, ok := f.stored("team-a", "m")
			assert.True(t, ok)
			_, ok = f.stored("default", "m")
			assert.False(t, ok)
		})
	})

	t.Run("unknown segment", func(t *testing.T) {
		forEachTransport(t, func(t *testing.T, _ rulesTransport, _ *fakeRules, run func(...string) cmdResult) {
			t.Helper()
			res := run("rules", "create", "--metric", "m", "--drop", "--segment", "nope")
			require.ErrorContains(t, res.err, "segment not found: nope")
		})
	})

	t.Run("yaml output", func(t *testing.T) {
		forEachTransport(t, func(t *testing.T, _ rulesTransport, _ *fakeRules, run func(...string) cmdResult) {
			t.Helper()
			res := run("rules", "create", "--metric", "m", "--drop", "-o", "yaml")
			require.NoError(t, res.err)
			assert.Contains(t, res.stdout, "drop: true")
		})
	})
}

func TestRulesUpdate(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		seed     []wireRule
		wantWire *wireRule
		wantErr  string
		noWrites bool
		noReqs   bool
	}{
		{
			name:     "changes functions and keeps labels",
			args:     []string{"a_metric", "--aggregations", "max"},
			seed:     []wireRule{ruleA},
			wantWire: &wireRule{Metric: "a_metric", MatchType: "exact", Action: "aggregate", Functions: []string{"max"}, LabelPolicy: &wireLabelPolicy{Mode: "drop", Labels: []string{"pod"}}},
		},
		{
			name:     "replaces drop labels",
			args:     []string{"a_metric", "--drop-labels", "pod,instance"},
			seed:     []wireRule{ruleA},
			wantWire: &wireRule{Metric: "a_metric", MatchType: "exact", Action: "aggregate", Functions: []string{"sum", "count"}, LabelPolicy: &wireLabelPolicy{Mode: "drop", Labels: []string{"pod", "instance"}}},
		},
		{
			name:     "changes interval and keeps the rest",
			args:     []string{"c_metric", "--aggregation-interval", "2m"},
			seed:     []wireRule{ruleC},
			wantWire: &wireRule{Metric: "c_metric", MatchType: "prefix", Action: "aggregate", Functions: []string{"max"}, Interval: "2m", Delay: "5m", LabelPolicy: &wireLabelPolicy{Mode: "keep", Labels: []string{"job", "instance"}}},
		},
		{
			name:     "changes match type",
			args:     []string{"b_metric", "--match-type", "suffix"},
			seed:     []wireRule{ruleB},
			wantWire: &wireRule{Metric: "b_metric", MatchType: "suffix", Action: "drop"},
		},
		{
			name:     "adding keep labels to a drop-labels rule is rejected",
			args:     []string{"a_metric", "--keep-labels", "job"},
			seed:     []wireRule{ruleA},
			wantErr:  "both dropped and kept labels",
			noWrites: true,
		},
		{
			name:     "dropping an aggregate rule is rejected",
			args:     []string{"a_metric", "--drop"},
			seed:     []wireRule{ruleA},
			wantErr:  "drop and aggregate",
			noWrites: true,
		},
		{
			name:     "missing rule",
			args:     []string{"nope", "--aggregations", "sum"},
			wantErr:  "failed to fetch existing rule for merge",
			noWrites: true,
		},
		{name: "no flags", args: []string{"a_metric"}, seed: []wireRule{ruleA}, wantErr: "specify at least one flag", noWrites: true, noReqs: true},
		{name: "invalid metric", args: []string{"..", "--drop"}, wantErr: "invalid metric", noWrites: true, noReqs: true},
		{name: "emptied aggregations", args: []string{"a_metric", "--aggregations", ""}, seed: []wireRule{ruleA}, wantErr: "at least one aggregation", noWrites: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			forEachTransport(t, func(t *testing.T, tr rulesTransport, f *fakeRules, run func(...string) cmdResult) {
				t.Helper()
				f.seed("default", tt.seed...)
				res := run(append([]string{"rules", "update"}, tt.args...)...)

				if tt.wantErr != "" {
					require.ErrorContains(t, res.err, tt.wantErr)
					assert.Empty(t, res.stdout)
					if tt.noWrites {
						assert.Empty(t, f.applied("", "PUT"))
					}
					if tt.noReqs {
						assert.Empty(t, f.requests("", ""))
					}
					return
				}

				require.NoError(t, res.err)
				assert.Contains(t, res.stderr, "Updated rule for "+tt.wantWire.Metric+".")
				stored, ok := f.stored("default", tt.wantWire.Metric)
				require.True(t, ok)
				assert.Equal(t, *tt.wantWire, stored)
				assert.Len(t, f.applied(tr.via, "GET"), 1)
				assert.Len(t, f.applied(tr.via, "PUT"), 1)
				assert.Empty(t, f.applied(tr.other(), "PUT"))
				var out map[string]any
				require.NoError(t, json.Unmarshal([]byte(res.stdout), &out))
				assert.Equal(t, tt.wantWire.Metric, out["metric"])
			})
		})
	}

	t.Run("custom segment", func(t *testing.T) {
		forEachTransport(t, func(t *testing.T, _ rulesTransport, f *fakeRules, run func(...string) cmdResult) {
			t.Helper()
			f.seed("team-a", ruleA)
			res := run("rules", "update", "a_metric", "--segment", "team-a", "--aggregations", "min")
			require.NoError(t, res.err)
			assert.Contains(t, res.stderr, "in segment team-a")
			stored, _ := f.stored("team-a", "a_metric")
			assert.Equal(t, []string{"min"}, stored.Functions)
		})
	})
}

func TestRulesDelete(t *testing.T) {
	t.Run("deletes the rule", func(t *testing.T) {
		forEachTransport(t, func(t *testing.T, tr rulesTransport, f *fakeRules, run func(...string) cmdResult) {
			t.Helper()
			f.seed("default", ruleA, ruleB)
			res := run("rules", "delete", "a_metric", "--force")
			require.NoError(t, res.err)
			assert.Empty(t, res.stdout)
			assert.Contains(t, res.stderr, "Deleted rule for a_metric.")
			assert.Equal(t, []string{"b_metric"}, f.metrics("default"))
			dels := f.applied(tr.via, "DELETE")
			require.Len(t, dels, 1)
			assert.Equal(t, "/v2/segments/default/rules/a_metric", dels[0].Path)
			assert.Empty(t, f.applied(tr.other(), "DELETE"))
		})
	})

	t.Run("a missing rule fails without sending a delete", func(t *testing.T) {
		forEachTransport(t, func(t *testing.T, _ rulesTransport, f *fakeRules, run func(...string) cmdResult) {
			t.Helper()
			res := run("rules", "delete", "nope", "--force", "-o", "json")
			require.ErrorIs(t, res.err, metrics.ErrRuleNotFound)
			require.ErrorContains(t, res.err, "delete rule nope")
			assert.Empty(t, res.stdout)
			assert.NotContains(t, res.stderr, "Deleted rule")
			assert.Empty(t, f.requests("", "DELETE"))
		})
	})

	t.Run("json mutation document", func(t *testing.T) {
		forEachTransport(t, func(t *testing.T, _ rulesTransport, f *fakeRules, run func(...string) cmdResult) {
			t.Helper()
			f.seed("team-a", ruleA)
			res := run("rules", "delete", "a_metric", "--force", "--segment", "team-a", "-o", "json")
			require.NoError(t, res.err)
			doc, ok := decodeOneJSONValue(t, res.stdout).(map[string]any)
			require.True(t, ok)
			assert.Equal(t, "deleted", doc["action"])
			assert.Equal(t, true, doc["changed"])
			target, _ := doc["target"].(map[string]any)
			assert.Equal(t, "rule", target["kind"])
			assert.Equal(t, "a_metric", target["name"])
			assert.Contains(t, res.stderr, "in segment team-a")
			assert.Empty(t, f.metrics("team-a"))
		})
	})

	t.Run("declined confirmation sends nothing", func(t *testing.T) {
		forEachTransport(t, func(t *testing.T, _ rulesTransport, f *fakeRules, run func(...string) cmdResult) {
			t.Helper()
			f.seed("default", ruleA)
			res := run("rules", "delete", "a_metric")
			require.Error(t, res.err)
			assert.Equal(t, []string{"a_metric"}, f.metrics("default"))
			assert.Empty(t, f.requests("", ""))
			assert.Zero(t, f.gcomCalls())
		})
	})

	t.Run("agent mode requires force", func(t *testing.T) {
		f := newFakeRules(t).seed("default", ruleA)
		res := runAdaptiveCmd(t, f.loader(t, fakeConfig{grafana: true}), true, "rules", "delete", "a_metric")
		require.ErrorIs(t, res.err, providers.ErrAgentModeRequiresForce)
		assert.Empty(t, f.requests("", ""))
	})

	t.Run("invalid input sends nothing", func(t *testing.T) {
		forEachTransport(t, func(t *testing.T, _ rulesTransport, f *fakeRules, run func(...string) cmdResult) {
			t.Helper()
			require.ErrorContains(t, run("rules", "delete", "..", "--force").err, "invalid metric")
			require.ErrorContains(t, run("rules", "delete", "m", "--force", "--segment", "..").err, "invalid segment")
			assert.Empty(t, f.requests("", ""))
		})
	})

	t.Run("unknown segment", func(t *testing.T) {
		forEachTransport(t, func(t *testing.T, _ rulesTransport, _ *fakeRules, run func(...string) cmdResult) {
			t.Helper()
			res := run("rules", "delete", "m", "--force", "--segment", "nope")
			require.ErrorContains(t, res.err, "segment not found: nope")
		})
	})

	t.Run("server error is surfaced", func(t *testing.T) {
		forEachTransport(t, func(t *testing.T, tr rulesTransport, f *fakeRules, run func(...string) cmdResult) {
			t.Helper()
			f.seed("default", ruleA)
			f.fail(tr.via, "DELETE", 500, `{"code":"internal","error":"failed to write rules"}`)
			res := run("rules", "delete", "a_metric", "--force")
			require.ErrorContains(t, res.err, "failed to write rules")
			assert.Equal(t, []string{"a_metric"}, f.metrics("default"))
		})
	})
}

func TestRulesTransport(t *testing.T) {
	t.Run("proxy uses the Grafana credential and never contacts GCOM", func(t *testing.T) {
		f := newFakeRules(t).seed("default", ruleA)
		res := runAdaptiveCmd(t, f.loader(t, fakeConfig{grafana: true, cloud: true}), false, "rules", "list")
		require.NoError(t, res.err)
		reqs := f.requests("", "")
		require.Len(t, reqs, 1)
		assert.Equal(t, viaProxy, reqs[0].Via)
		assert.Equal(t, "Bearer "+fakeGrafanaToken, reqs[0].Auth)
		assert.Zero(t, f.gcomCalls())
	})

	t.Run("direct uses basic auth and resolves GCOM once", func(t *testing.T) {
		f := newFakeRules(t).seed("default", ruleA)
		res := runAdaptiveCmd(t, f.loader(t, fakeConfig{cloud: true}), false, "rules", "update", "a_metric", "--aggregations", "min")
		require.NoError(t, res.err)
		reqs := f.requests("", "")
		require.Len(t, reqs, 2)
		for _, r := range reqs {
			assert.Equal(t, viaDirect, r.Via)
			assert.True(t, strings.HasPrefix(r.Auth, "Basic "))
		}
		assert.Equal(t, 1, f.gcomCalls())
	})

	t.Run("a refused write is applied once, on the direct path", func(t *testing.T) {
		f := newFakeRules(t).seed("default", ruleA)
		f.fail(viaProxy, "PUT", 403, routeDeniedBody)
		res := runAdaptiveCmd(t, f.loader(t, fakeConfig{grafana: true, cloud: true}), false, "rules", "update", "a_metric", "--aggregations", "min")
		require.NoError(t, res.err)
		assert.Empty(t, f.applied(viaProxy, "PUT"))
		assert.Len(t, f.applied(viaDirect, "PUT"), 1)
		assert.Len(t, f.applied(viaProxy, "GET"), 1, "reads keep using the proxy")
		stored, _ := f.stored("default", "a_metric")
		assert.Equal(t, []string{"min"}, stored.Functions)
	})

	t.Run("refusal is remembered for the rest of the command", func(t *testing.T) {
		f := newFakeRules(t)
		for _, m := range []string{"m1", "m2", "m3"} {
			f.seed("default", wireRule{Metric: m, Action: "drop"})
		}
		f.pageSize = 1
		f.fail(viaProxy, "GET", 403, routeDeniedBody)
		res := runAdaptiveCmd(t, f.loader(t, fakeConfig{grafana: true, cloud: true}), false, "rules", "list", "--limit", "0", "-o", "json")
		require.NoError(t, res.err)
		assert.Len(t, f.requests(viaProxy, "GET"), 1)
		assert.Len(t, f.applied(viaDirect, "GET"), 3)
	})

	for name, tc := range map[string]struct {
		status int
		body   string
	}{
		"plugin not installed":     {404, `{"message":"Plugin not found, no installed plugin with that id"}`},
		"plugin not configured":    {404, `{"message":"Plugin setting not found"}`},
		"plugin disabled":          {404, `{"message":"Plugin is not enabled"}`},
		"plugin without the route": {404, `{"message":"plugin route match not found"}`},
		"route access denied":      {403, routeDeniedBody},
		"plugin access denied":     {403, `{"title":"Access denied","message":"You'll need additional permissions to perform this action. Permissions needed: plugins.app:access"}`},
	} {
		t.Run("falls back when "+name, func(t *testing.T) {
			f := newFakeRules(t).seed("default", ruleA)
			f.fail(viaProxy, "", tc.status, tc.body)
			res := runAdaptiveCmd(t, f.loader(t, fakeConfig{grafana: true, cloud: true}), false, "rules", "get", "a_metric")
			require.NoError(t, res.err)
			assert.JSONEq(t, jsonA, res.stdout)
			assert.Len(t, f.applied(viaDirect, "GET"), 1)
		})
	}

	for name, tc := range map[string]struct {
		status int
		body   string
	}{
		"backend forbidden":    {403, `{"code":"forbidden","error":"tenant is read-only"}`},
		"backend not found":    {404, `{"code":"not_found","error":"segment not found: x"}`},
		"grafana server error": {500, `{"message":"Failed to fetch plugin settings"}`},
	} {
		t.Run("does not fall back on "+name, func(t *testing.T) {
			f := newFakeRules(t)
			f.fail(viaProxy, "", tc.status, tc.body)
			res := runAdaptiveCmd(t, f.loader(t, fakeConfig{grafana: true, cloud: true}), false, "rules", "list")
			require.Error(t, res.err)
			assert.Empty(t, f.requests(viaDirect, ""))
			assert.Zero(t, f.gcomCalls())
		})
	}

	t.Run("does not fall back when the proxy connection fails", func(t *testing.T) {
		f := newFakeRules(t)
		f.fail(viaProxy, "PUT", 0, "")
		res := runAdaptiveCmd(t, f.loader(t, fakeConfig{grafana: true, cloud: true}), false, "rules", "create", "--metric", "m", "--drop")
		require.Error(t, res.err)
		assert.NotEmpty(t, f.requests(viaProxy, "PUT"))
		assert.Empty(t, f.requests(viaDirect, ""))
		assert.Zero(t, f.gcomCalls())
	})

	t.Run("direct auth failure without grafana config is reported as is", func(t *testing.T) {
		f := newFakeRules(t)
		f.fail(viaDirect, "", 403, `{"error":"forbidden"}`)
		res := runAdaptiveCmd(t, f.loader(t, fakeConfig{cloud: true}), false, "rules", "list")
		require.ErrorContains(t, res.err, "status 403: forbidden")
		assert.NotContains(t, res.err.Error(), "grafana config")
		var invalid config.ValidationError
		assert.NotErrorAs(t, res.err, &invalid)
	})

	t.Run("route denied without direct access names the missing action", func(t *testing.T) {
		for verb, tc := range map[string]struct {
			args   []string
			method string
			action string
			role   string
		}{
			"read":   {[]string{"rules", "list"}, "", "grafana-adaptive-metrics-app.rules:read", "Rules Reader"},
			"write":  {[]string{"rules", "create", "--metric", "m", "--drop"}, "PUT", "grafana-adaptive-metrics-app.rules:write", "Rules Editor"},
			"delete": {[]string{"rules", "delete", "a_metric", "--force"}, "DELETE", "grafana-adaptive-metrics-app.rules:delete", "Rules Editor"},
		} {
			t.Run(verb, func(t *testing.T) {
				f := newFakeRules(t).seed("default", ruleA)
				f.fail(viaProxy, tc.method, 403, routeDeniedBody)
				res := runAdaptiveCmd(t, f.loader(t, fakeConfig{grafana: true}), false, tc.args...)
				require.Error(t, res.err)
				var denied *providers.PluginRouteDeniedError
				require.ErrorAs(t, res.err, &denied)
				assert.Equal(t, tc.action, denied.Action)
				assert.Equal(t, tc.role, denied.Role)
				assert.Contains(t, res.err.Error(), "direct access unavailable")
			})
		}
	})

	t.Run("app access denied without direct access names the missing permission", func(t *testing.T) {
		f := newFakeRules(t)
		f.fail(viaProxy, "", 403, `{"title":"Access denied","message":"You'll need additional permissions to perform this action. Permissions needed: plugins.app:access"}`)
		res := runAdaptiveCmd(t, f.loader(t, fakeConfig{grafana: true}), false, "rules", "list")
		require.ErrorContains(t, res.err, "plugins.app:access")
		var denied *providers.PluginRouteDeniedError
		require.NotErrorAs(t, res.err, &denied)
	})

	t.Run("both transports rejected reports both", func(t *testing.T) {
		f := newFakeRules(t)
		f.fail(viaProxy, "", 403, routeDeniedBody)
		f.fail(viaDirect, "", 401, `{"error":"invalid token"}`)
		res := runAdaptiveCmd(t, f.loader(t, fakeConfig{grafana: true, cloud: true}), false, "rules", "list")
		require.Error(t, res.err)
		assert.Contains(t, res.err.Error(), "plugin proxy refused")
		assert.Contains(t, res.err.Error(), "direct access also failed")
		var denied *providers.PluginRouteDeniedError
		assert.ErrorAs(t, res.err, &denied)
	})

	t.Run("a direct backend error after fallback is returned as is", func(t *testing.T) {
		f := newFakeRules(t)
		f.fail(viaProxy, "", 403, routeDeniedBody)
		res := runAdaptiveCmd(t, f.loader(t, fakeConfig{grafana: true, cloud: true}), false, "rules", "get", "nope")
		require.ErrorIs(t, res.err, metrics.ErrRuleNotFound)
	})

	t.Run("no credentials at all", func(t *testing.T) {
		f := newFakeRules(t)
		res := runAdaptiveCmd(t, f.loader(t, fakeConfig{}), false, "rules", "list")
		require.Error(t, res.err)
		assert.Contains(t, res.err.Error(), "plugin proxy is unavailable")
		var invalid config.ValidationError
		assert.NotErrorAs(t, res.err, &invalid, "the direct failure must lead, not the Grafana config error")
		assert.Empty(t, f.requests("", ""))
	})
}

func TestRuleAdapter_DryRun(t *testing.T) {
	manifest := func(spec map[string]any) *unstructured.Unstructured {
		return &unstructured.Unstructured{Object: map[string]any{
			"apiVersion": metrics.RuleAPIVersion,
			"kind":       metrics.RuleKind,
			"metadata":   map[string]any{"name": spec["metric"]},
			"spec":       spec,
		}}
	}
	tests := []struct {
		name    string
		spec    map[string]any
		wantErr string
	}{
		{"valid rule is unverified", map[string]any{"metric": "m", "aggregations": []any{"sum"}, "drop_labels": []any{"pod"}}, ""},
		{"both label kinds", map[string]any{"metric": "m", "aggregations": []any{"sum"}, "drop_labels": []any{"pod"}, "keep_labels": []any{"job"}}, "both dropped and kept labels"},
		{"drop with interval", map[string]any{"metric": "m", "drop": true, "aggregation_interval": "1m"}, "interval or delay"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFakeRules(t)
			a, err := metrics.NewRuleAdapterFactory(f.loader(t, fakeConfig{grafana: true, cloud: true}))(t.Context())
			require.NoError(t, err)

			_, createErr := a.Create(t.Context(), manifest(tt.spec), metav1.CreateOptions{DryRun: []string{metav1.DryRunAll}})
			_, updateErr := a.Update(t.Context(), manifest(tt.spec), metav1.UpdateOptions{DryRun: []string{metav1.DryRunAll}})
			for _, err := range []error{createErr, updateErr} {
				if tt.wantErr == "" {
					require.ErrorIs(t, err, adapter.ErrDryRunUnverified)
				} else {
					require.ErrorContains(t, err, tt.wantErr)
					require.NotErrorIs(t, err, adapter.ErrDryRunUnverified)
				}
			}
			assert.Empty(t, f.requests("", ""))
		})
	}
}

func TestRuleExample_IsValid(t *testing.T) {
	f := newFakeRules(t)
	a, err := metrics.NewRuleAdapterFactory(f.loader(t, fakeConfig{grafana: true}))(t.Context())
	require.NoError(t, err)
	var obj unstructured.Unstructured
	require.NoError(t, json.Unmarshal(metrics.RuleExample(), &obj.Object))
	_, err = a.Create(t.Context(), &obj, metav1.CreateOptions{DryRun: []string{metav1.DryRunAll}})
	require.ErrorIs(t, err, adapter.ErrDryRunUnverified)
}

func TestRuleSchema_IncludesManagedBy(t *testing.T) {
	assert.Contains(t, string(metrics.RuleSchema()), "managed_by")
	assert.Contains(t, string(metrics.RuleSchema()), "aggregation_delay")
}
