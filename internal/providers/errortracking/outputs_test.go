package errortracking_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/providers/errortracking"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/yaml"
)

type outputCase struct {
	name    string
	args    []string
	calls   []string
	query   map[string]string
	body    string
	json    string
	exact   bool
	table   []string
	narrow  []string
	wide    []string
	text    string
	jq      []string
	noTable bool
}

func assertJSONSubset(t *testing.T, want, got any, path string) {
	t.Helper()
	switch w := want.(type) {
	case map[string]any:
		g, ok := got.(map[string]any)
		require.True(t, ok, "%s: want an object, got %v", path, got)
		for k, v := range w {
			gv, ok := g[k]
			require.True(t, ok, "%s.%s is missing", path, k)
			assertJSONSubset(t, v, gv, path+"."+k)
		}
	case []any:
		g, ok := got.([]any)
		require.True(t, ok, "%s: want an array, got %v", path, got)
		require.Len(t, g, len(w), path)
		for i := range w {
			assertJSONSubset(t, w[i], g[i], path)
		}
	default:
		assert.Equal(t, want, got, path)
	}
}

func assertOutputJSON(t *testing.T, tc outputCase, out []byte) {
	t.Helper()
	if tc.exact {
		assert.JSONEq(t, tc.json, string(out))
		return
	}
	var want, got any
	require.NoError(t, json.Unmarshal([]byte(tc.json), &want))
	require.NoError(t, json.Unmarshal(out, &got), "%s", out)
	assertJSONSubset(t, want, got, "$")
}

func outputFake(t *testing.T) *fakeET {
	t.Helper()
	f := newFakeET(t)
	for route, body := range map[string]string{
		"GET projects":                               `{"projects":[{"id":314,"name":"Checkout API","ingestKey":"k314","dsn":"https://k314@ingest.example.com/314","createdAt":"2026-11-01T09:30:00Z","stats":{"unresolvedErrors":3,"eventsLast24h":40,"lastEventAt":"2026-11-17T08:15:42Z","dailyEvents":[]}}]}`,
		"GET projects/314":                           `{"id":314,"name":"Checkout API","ingestKey":"k314","dsn":"https://k314@ingest.example.com/314","previousIngestKeyUntil":"2099-11-18T09:00:00Z","stats":{"unresolvedErrors":3,"eventsLast24h":40,"dailyEvents":[]}}`,
		"GET projects/314/timeline":                  `{"bucket":"1h","buckets":[{"bucket":"2026-11-17T08:00:00Z","eventCount":4}]}`,
		"POST projects":                              `{"id":314,"name":"Checkout API","stats":{"dailyEvents":[]}}`,
		"PATCH projects/314":                         `{"id":314,"name":"Checkout API","stats":{"dailyEvents":[]}}`,
		"DELETE projects/314":                        `{"status":"deleted"}`,
		"POST projects/314/ingestKey/rotate":         `{"id":314,"name":"Checkout API","ingestKey":"k-new","dsn":"https://k-new@ingest.example.com/314","stats":{"dailyEvents":[]}}`,
		"POST projects/314/ingestKey/revokePrevious": `{"id":314,"name":"Checkout API","ingestKey":"k-new","stats":{"dailyEvents":[]}}`,
	} {
		f.reply(route, http.StatusOK, body)
	}
	return f
}

func outputCases() []outputCase {
	const ns = `"namespace":"stacks-123"`
	const project = `"project":{"id":314,"name":"Checkout API"}`
	mutation := func(action, target, rest string) string {
		return `{"type":"gcx.mutation","schema_version":"1","action":"` + action + `","changed":true,"target":{` + target + `,` + ns + `}` + rest + `}`
	}
	return []outputCase{
		{name: "projects list", calls: []string{"GET projects"}, args: []string{"projects", "list"},
			exact:  true,
			json:   `{"items":[{"kind":"Project","apiVersion":"error-tracking.ext.grafana.app/v0alpha1","metadata":{"name":"checkout-api-314",` + ns + `},"spec":{"id":314,"name":"Checkout API","description":"","sourceRepoUrl":"","createdAt":"2026-11-01T09:30:00Z","stats":{"unresolvedErrors":3,"eventsLast24h":40,"lastEventAt":"2026-11-17T08:15:42Z","dailyEvents":[]}}}]}`,
			narrow: []string{" ago"}, table: []string{"NAME", "UNRESOLVED", "EVENTS 24H", "LAST EVENT", "checkout-api-314"}, wide: []string{"CREATED", "2026-11-01 09:30:00Z", "2026-11-17 08:15:42Z"},
			jq: []string{".items[0].spec.id", "314"}},
		{name: "projects get", calls: []string{"GET projects/314"}, args: []string{"projects", "get", "314"},
			json:  `{"apiVersion":"error-tracking.ext.grafana.app/v0alpha1","kind":"Project","metadata":{"name":"checkout-api-314",` + ns + `},"spec":{"id":314,"ingestKey":"k314","dsn":"https://k314@ingest.example.com/314"}}`,
			table: []string{"NAME", "checkout-api-314"}, wide: []string{"DSN", "https://k314@ingest.example.com/314"}, jq: []string{".spec.dsn", `"https://k314@ingest.example.com/314"`}},
		{name: "projects timeline", calls: []string{"GET projects/314/timeline"}, query: map[string]string{"from": "*", "to": "*"}, args: []string{"projects", "timeline", "314"}, exact: true,
			json:  `{"bucket":"1h","buckets":[{"bucket":"2026-11-17T08:00:00Z","eventCount":4}]}`,
			table: []string{"BUCKET", "EVENTS", "2026-11-17 08:00Z", "4"}, jq: []string{".buckets[0].eventCount", "4"}},
		{name: "projects create", calls: []string{"POST projects"}, body: `{"name":"Checkout API"}`, args: []string{"projects", "create", "--name", "Checkout API"}, exact: true, noTable: true,
			json: mutation("created", `"kind":"Project","id":"314","name":"Checkout API"`, ""), text: "Created project Checkout API (ID 314)"},
		{name: "projects update", calls: []string{"PATCH projects/314"}, body: `{"description":"x"}`, args: []string{"projects", "update", "314", "--description", "x"}, exact: true, noTable: true,
			json: mutation("updated", `"kind":"Project","id":"314","name":"Checkout API"`, ""), text: "Changed the description of project Checkout API (ID 314)"},
		{name: "projects delete", calls: []string{"GET projects/314", "DELETE projects/314"}, args: []string{"projects", "delete", "314", "--force"}, noTable: true,
			json: mutation("deleted", `"kind":"Project","id":"314","name":"Checkout API"`, ""), text: "Deleted project Checkout API (ID 314)"},
		{name: "projects ingest-keys rotate", calls: []string{"POST projects/314/ingestKey/rotate"}, args: []string{"projects", "ingest-keys", "rotate", "314", "--force"}, exact: true, noTable: true,
			json: mutation("rotated", `"kind":"IngestKey"`, `,`+project),
			text: "Rotated the ingest key of project Checkout API (ID 314); the old key stopped working at once"},
		{name: "projects ingest-keys delete-previous", calls: []string{"GET projects/314", "POST projects/314/ingestKey/revokePrevious"}, args: []string{"projects", "ingest-keys", "delete-previous", "314", "--force"}, exact: true, noTable: true,
			json: mutation("deleted", `"kind":"IngestKey"`, `,`+project), text: "Stopped the previous ingest key of project Checkout API (ID 314)"},
	}
}

func TestOutputFormats(t *testing.T) {
	f := outputFake(t)
	cases := outputCases()

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			run := func(t *testing.T, args ...string) string {
				t.Helper()
				out, err := f.run(append(append([]string{}, tc.args...), args...)...)
				require.NoError(t, err)
				calls := f.takeCalls()
				require.Equal(t, tc.calls, routesOf(calls), "the requests behind %v", args)
				last := calls[len(calls)-1]
				got := map[string]string{}
				for k := range last.Query {
					got[k] = last.Query.Get(k)
					if tc.query[k] == "*" {
						got[k] = "*"
					}
				}
				want := tc.query
				if want == nil {
					want = map[string]string{}
				}
				assert.Equal(t, want, got, "the query of %s", last.Route)
				if tc.body != "" {
					assert.JSONEq(t, tc.body, last.Body)
				}
				return out
			}
			t.Run("json", func(t *testing.T) { assertOutputJSON(t, tc, []byte(run(t, "-o", "json"))) })
			t.Run("yaml", func(t *testing.T) {
				out, err := yaml.YAMLToJSON([]byte(run(t, "-o", "yaml")))
				require.NoError(t, err)
				assertOutputJSON(t, tc, out)
			})
			t.Run("agents", func(t *testing.T) { assertOutputJSON(t, tc, []byte(run(t, "-o", "agents"))) })
			if tc.noTable {
				var want struct {
					Action string `json:"action"`
				}
				require.NoError(t, json.Unmarshal([]byte(tc.json), &want))
				t.Run("json fields", func(t *testing.T) {
					assert.JSONEq(t, `{"action":"`+want.Action+`"}`, run(t, "--json", "action"))
				})
				t.Run("jq", func(t *testing.T) {
					assert.Equal(t, `"`+want.Action+`"`, strings.TrimSpace(run(t, "--jq", ".action")))
				})
				t.Run("text by default", func(t *testing.T) {
					assert.Contains(t, run(t), tc.text)
					assert.Contains(t, run(t, "-o", "text"), tc.text)
				})
				return
			}
			t.Run("table by default", func(t *testing.T) {
				for _, out := range []string{run(t), run(t, "-o", "table")} {
					for _, s := range append(append([]string{}, tc.table...), tc.narrow...) {
						assert.Contains(t, out, s)
					}
					for _, s := range tc.wide {
						if !strings.Contains(strings.Join(tc.table, " "), s) {
							assert.NotContains(t, out, s, "only -o wide shows it")
						}
					}
				}
			})
			if tc.wide != nil {
				t.Run("wide", func(t *testing.T) {
					out := run(t, "-o", "wide")
					for _, s := range append(append([]string{}, tc.table...), tc.wide...) {
						assert.Contains(t, out, s)
					}
					for _, s := range tc.narrow {
						assert.NotContains(t, out, s, "only the narrow table shows it")
					}
				})
			}
			if tc.jq != nil {
				t.Run("jq", func(t *testing.T) { assert.Equal(t, tc.jq[1], strings.TrimSpace(run(t, "--jq", tc.jq[0]))) })
			}
		})
	}
}

func TestEveryLeafTeachesAgents(t *testing.T) {
	var walk func(c *cobra.Command)
	walk = func(c *cobra.Command) {
		if !c.HasSubCommands() {
			assert.NotEmpty(t, c.Annotations[agent.AnnotationLLMHint], "%s has an llm_hint", c.CommandPath())
			assert.Contains(t, c.Example, "As an agent", "%s has an agent example", c.CommandPath())
		}
		for _, s := range c.Commands() {
			walk(s)
		}
	}
	walk((&errortracking.Provider{}).Commands()[0])
}
