package root_test

import (
	"bytes"
	"encoding/json"
	"os"
	"slices"
	"testing"

	"github.com/grafana/gcx/cmd/gcx/root"
	"github.com/grafana/gcx/internal/agent"
	"github.com/stretchr/testify/require"
)

func TestCommandSearchRelevance(t *testing.T) {
	t.Setenv("GCX_NO_UPDATE_NOTIFIER", "1")
	t.Setenv("GCX_CONFIG", t.TempDir()+"/absent.yaml")
	data, err := os.ReadFile("testdata/command_search.json")
	require.NoError(t, err)
	var cases []struct {
		Query    string   `json:"query"`
		Accepted []string `json:"accepted"`
	}
	require.NoError(t, json.Unmarshal(data, &cases))
	for _, tc := range cases {
		t.Run(tc.Query, func(t *testing.T) {
			cmd := buildRootCmd()
			cmd.Use = "gcx"
			var out, stderr bytes.Buffer
			cmd.SetOut(&out)
			cmd.SetErr(&stderr)
			cmd.SetArgs([]string{"commands", "search", tc.Query, "-o", "json"})
			require.NoError(t, cmd.Execute())
			var result struct {
				Items []struct {
					Path string `json:"full_path"`
				} `json:"items"`
			}
			require.NoError(t, json.Unmarshal(out.Bytes(), &result))
			paths := make([]string, 0, len(result.Items))
			for _, item := range result.Items {
				paths = append(paths, item.Path)
			}
			require.True(t, slices.ContainsFunc(paths, func(path string) bool {
				return slices.Contains(tc.Accepted, path)
			}), "expected one of %v in top five; got %v", tc.Accepted, paths)
		})
	}
}

func TestCommandSearchTelemetry(t *testing.T) {
	t.Setenv("GCX_NO_UPDATE_NOTIFIER", "1")
	cmd := buildRootCmd()
	cmd.Use = "gcx"
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"commands", "search", "private-customer-identifier", "-o", "json"})
	require.NoError(t, cmd.Execute())
	info := root.CurrentTelemetryInfo()
	require.NotNil(t, info)
	require.Equal(t, &root.TelemetryInfo{Command: "commands search", Flags: "output", OutputFormat: "json"}, info)
}

func TestCommandSearchContinuation(t *testing.T) {
	stdout, code := runGcx(t, "commands", "search", "list dashboards", "--limit", "1", "-o", "json")
	require.Zero(t, code)
	var result struct {
		Items []map[string]any `json:"items"`
		Meta  struct {
			Truncated bool   `json:"truncated"`
			Returned  int    `json:"returned"`
			Total     int    `json:"total"`
			Continue  string `json:"continue"`
		} `json:"list_meta"`
	}
	require.NoError(t, json.Unmarshal([]byte(stdout), &result))
	require.Len(t, result.Items, 1)
	require.Equal(t, "create-dashboard,manage-dashboards", result.Items[0]["skill"])
	require.True(t, result.Meta.Truncated)
	require.Equal(t, 1, result.Meta.Returned)
	require.Greater(t, result.Meta.Total, 1)
	require.Contains(t, result.Meta.Continue, "commands search 'list dashboards'")
	require.Contains(t, result.Meta.Continue, "-o json")
	require.Contains(t, result.Meta.Continue, "--limit 0")
}

func BenchmarkCommandSearch(b *testing.B) {
	b.Setenv("GCX_NO_UPDATE_NOTIFIER", "1")
	cmd := buildRootCmd()
	cmd.Use = "gcx"
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"commands", "--flat", "-o", "json"})
	if err := cmd.Execute(); err != nil {
		b.Fatal(err)
	}
	var catalog struct {
		Commands []struct {
			Path        string `json:"full_path"`
			Description string `json:"description"`
			Long        string `json:"long"`
		} `json:"commands"`
	}
	if err := json.Unmarshal(out.Bytes(), &catalog); err != nil {
		b.Fatal(err)
	}
	documents := make([]agent.SearchDocument, 0, len(catalog.Commands))
	for _, c := range catalog.Commands {
		documents = append(documents, agent.SearchDocument{Path: c.Path, Summary: c.Description, Description: c.Long})
	}
	b.ReportAllocs()
	for b.Loop() {
		agent.SearchCommands(documents, "create an uptime check")
	}
}

// Aggregate gates keep exploratory paraphrases distinct from hard correctness
// contracts. The holdout was frozen before tuning and must not drive vocabulary.
func TestCommandSearchExpandedRelevance(t *testing.T) {
	t.Setenv("GCX_NO_UPDATE_NOTIFIER", "1")
	for _, filename := range []string{"command_search_expanded.json", "command_search_holdout.json"} {
		t.Run(filename, func(t *testing.T) {
			data, err := os.ReadFile("testdata/" + filename)
			require.NoError(t, err)
			var cases []struct {
				Query    string   `json:"query"`
				Category string   `json:"category"`
				Accepted []string `json:"accepted"`
			}
			require.NoError(t, json.Unmarshal(data, &cases))
			type counts struct{ total, first, found int }
			totals := map[string]*counts{}
			for _, tc := range cases {
				category := tc.Category
				if category == "" {
					category = "holdout"
				}
				if totals[category] == nil {
					totals[category] = &counts{}
				}
				stats := totals[category]
				stats.total++
				cmd := buildRootCmd()
				cmd.Use = "gcx"
				var out, stderr bytes.Buffer
				cmd.SetOut(&out)
				cmd.SetErr(&stderr)
				cmd.SetArgs([]string{"commands", "search", tc.Query, "-o", "json"})
				require.NoError(t, cmd.Execute(), tc.Query)
				var result struct {
					Items []struct {
						Path       string `json:"full_path"`
						Invocation string `json:"invocation"`
					} `json:"items"`
				}
				require.NoError(t, json.Unmarshal(out.Bytes(), &result))
				if category == "negative" || category == "vague" {
					require.Empty(t, result.Items, tc.Query)
					continue
				}
				rank := 0
				for i, item := range result.Items {
					path := item.Path
					if item.Invocation != "" {
						path = item.Invocation
					}
					if slices.Contains(tc.Accepted, path) {
						rank = i + 1
						break
					}
				}
				if rank == 1 {
					stats.first++
				}
				if rank > 0 {
					stats.found++
				} else {
					t.Logf("miss: %s", tc.Query)
				}
			}
			for category, stats := range totals {
				t.Logf("%s: top1 %d/%d; top5 %d/%d", category, stats.first, stats.total, stats.found, stats.total)
				switch category {
				case "canonical":
					require.Equal(t, stats.total, stats.first)
				case "original":
					require.Equal(t, stats.total, stats.found)
				case "paraphrase":
					require.GreaterOrEqual(t, stats.first*100, stats.total*75)
					require.GreaterOrEqual(t, stats.found*100, stats.total*90)
				case "transposition":
					require.GreaterOrEqual(t, stats.found*100, stats.total*95)
				case "holdout":
					require.GreaterOrEqual(t, stats.found*100, stats.total*85)
				}
			}
		})
	}
}

func TestCommandSearchWorkflows(t *testing.T) {
	t.Setenv("GCX_NO_UPDATE_NOTIFIER", "1")
	for _, tc := range []struct{ query, skill string }{
		{"investigate high CPU usage", "debug-with-grafana"},
		{"move dashboards to another Grafana instance", "manage-dashboards"},
		{"why is my alert rule firing", "investigate-alert"},
		{"triage oncall pages", "oncall-triage"},
		{"why is my SLO breaching", "slo-investigate"},
		{"investigate failing synthetic checks", "synth-investigate-check"},
	} {
		t.Run(tc.skill, func(t *testing.T) {
			cmd := buildRootCmd()
			cmd.Use = "gcx"
			var out, stderr bytes.Buffer
			cmd.SetOut(&out)
			cmd.SetErr(&stderr)
			cmd.SetArgs([]string{"commands", "search", tc.query, "-o", "json"})
			require.NoError(t, cmd.Execute())
			var result struct {
				Items []struct {
					Path        string `json:"full_path"`
					Kind        string `json:"kind"`
					Skill       string `json:"skill"`
					Invocation  string `json:"invocation"`
					Description string `json:"description"`
				} `json:"items"`
			}
			require.NoError(t, json.Unmarshal(out.Bytes(), &result))
			require.NotEmpty(t, result.Items)
			first := result.Items[0]
			require.Equal(t, "workflow", first.Kind)
			require.Equal(t, tc.skill, first.Skill)
			require.Equal(t, "gcx agent skills get", first.Path)
			require.Equal(t, "gcx agent skills get "+tc.skill, first.Invocation)
			require.Contains(t, first.Description, "Open a guide")
			seen := map[string]bool{}
			for _, item := range result.Items {
				key := item.Path
				if item.Invocation != "" {
					key = item.Invocation
				}
				require.False(t, seen[key])
				seen[key] = true
			}
			// The suggested invocation is real and reads the bundled guide offline.
			guide := buildRootCmd()
			guide.Use = "gcx"
			out.Reset()
			guide.SetOut(&out)
			guide.SetErr(&stderr)
			guide.SetArgs([]string{"agent", "skills", "get", tc.skill, "-o", "text"})
			require.NoError(t, guide.Execute())
			require.Contains(t, out.String(), "name: "+tc.skill)
		})
	}
}
