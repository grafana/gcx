package kg_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/providers/kg"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"
)

const cypherResult = `{"columns":["name","count","missing","path"],"rows":[["checkout",9007199254740993,null,{"nodes":[{"kind":"node","ref":"n0","labels":["Service"],"properties":{"name":"checkout"}}],"relationships":[]}]],"stats":{"rowCount":1,"columnCount":4,"elapsedMs":5}}`

func TestGraphQueryCommandOutput(t *testing.T) {
	for _, tt := range []struct {
		name  string
		flags []string
		agent bool
		want  string
	}{
		{name: "json", flags: []string{"-o", "json"}, want: `9007199254740993`},
		{name: "yaml", flags: []string{"-o", "yaml"}, want: `9007199254740993`},
		{name: "table", flags: []string{"-o", "table"}, want: `9007199254740993`},
		{name: "rows preserve integers", flags: []string{"--json", "rows"}, want: `9007199254740993`},
		{name: "fields", flags: []string{"--json", "columns,stats"}, want: `"columnCount": 4`},
		{name: "jq", flags: []string{"--jq", ".rows[0][0]"}, want: `checkout`},
		{name: "agent", agent: true, want: `9007199254740993`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			pinHumanMode(t)
			forceNoColor(t)
			if tt.agent {
				setAgentMode(t)
			}
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				assert.Equal(t, http.MethodPost, r.Method)
				assert.Equal(t, "/api/plugins/grafana-asserts-app/resources/asserts/api-server/v1/query/cypher", r.URL.Path)
				var body map[string]any
				if !assert.NoError(t, json.NewDecoder(r.Body).Decode(&body)) {
					return
				}
				assert.Equal(t, map[string]any{"query": "RETURN 1 AS value LIMIT 1", "start": float64(1757000000000), "end": float64(1757003600000)}, body)
				_, _ = fmt.Fprint(w, cypherResult)
			}))
			defer server.Close()
			loader := &kg.FakeWriteLoader{Cfg: config.NamespacedRESTConfig{Config: rest.Config{Host: server.URL}}}
			args := append([]string{"RETURN 1 AS value LIMIT 1", "--from", "1757000000", "--to", "1757003600"}, tt.flags...)
			out, stderr, err := runKgCommand(t, func() *cobra.Command { return kg.NewGraphQueryCommand(loader) }, args, "")
			require.NoError(t, err)
			assert.Empty(t, stderr)
			assert.Equal(t, 1, calls)
			assert.Contains(t, out, tt.want)
			if tt.agent || tt.name == "json" {
				decodeSingleJSON(t, []byte(out))
				assert.Contains(t, out, `"nodes"`)
				assert.Contains(t, out, `null`)
			}
			if tt.name == "table" {
				assert.Contains(t, out, "name")
				assert.Contains(t, out, "checkout")
				assert.Contains(t, out, `"ref":"n0"`)
			}
		})
	}
}

func TestGraphQueryCommandCatalogWarning(t *testing.T) {
	const catalogResult = `{"columns":["from_type","to_type","n"],"rows":[["Schema","Schema",1],["Service","Pod",5]],"stats":{"rowCount":2,"columnCount":3,"elapsedMs":1}}`
	for _, tt := range []struct {
		name       string
		query      string
		flags      []string
		response   string
		want       string
		wantNot    string
		wantStderr string // "" means stderr stays empty
	}{
		{
			name:       "json gains a warning for catalog rows",
			query:      "MATCH (a)-[r:TYPE_A]->(b) RETURN labels(a)[0] AS from_type, labels(b)[0] AS to_type, count(r) AS n LIMIT 10",
			flags:      []string{"-o", "json"},
			response:   catalogResult,
			want:       `"warnings": [`,
			wantStderr: "warn: 1 of 2 rows holds the graph's own catalog records (label Schema)",
		},
		{
			name:       "yaml gains a warning for catalog rows",
			query:      "MATCH (a)-[r:TYPE_A]->(b) RETURN labels(a)[0] AS from_type, labels(b)[0] AS to_type, count(r) AS n LIMIT 10",
			flags:      []string{"-o", "yaml"},
			response:   catalogResult,
			want:       "warnings:",
			wantStderr: "warn: 1 of 2 rows holds the graph's own catalog records (label Schema)",
		},
		{
			name:       "table writes the warning to stderr, not after the rows",
			query:      "MATCH (a)-[r:TYPE_A]->(b) RETURN labels(a)[0] AS from_type, labels(b)[0] AS to_type, count(r) AS n LIMIT 10",
			flags:      []string{"-o", "table"},
			response:   catalogResult,
			want:       "Schema",
			wantNot:    "catalog records",
			wantStderr: "warn: 1 of 2 rows holds the graph's own catalog records (label Schema)",
		},
		{
			name:     "a query naming the catalog label gets no warning",
			query:    "MATCH (a:Schema)-[r]->(b) RETURN labels(a)[0] AS from_type, labels(b)[0] AS to_type, count(r) AS n LIMIT 10",
			flags:    []string{"-o", "json"},
			response: catalogResult,
			wantNot:  "warnings",
		},
		{
			name:     "observed entities get no warning",
			query:    "RETURN 1 AS value LIMIT 1",
			flags:    []string{"-o", "json"},
			response: cypherResult,
			wantNot:  "warnings",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			pinHumanMode(t)
			forceNoColor(t)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = fmt.Fprint(w, tt.response)
			}))
			defer server.Close()
			loader := &kg.FakeWriteLoader{Cfg: config.NamespacedRESTConfig{Config: rest.Config{Host: server.URL}}}
			args := append([]string{tt.query, "--from", "1757000000", "--to", "1757003600"}, tt.flags...)
			out, stderr, err := runKgCommand(t, func() *cobra.Command { return kg.NewGraphQueryCommand(loader) }, args, "")
			require.NoError(t, err)
			if tt.wantStderr == "" {
				assert.Empty(t, stderr)
			} else {
				assert.Contains(t, stderr, tt.wantStderr)
			}
			if tt.want != "" {
				assert.Contains(t, out, tt.want)
			}
			if tt.wantNot != "" {
				assert.NotContains(t, out, tt.wantNot)
			}
			if tt.flags[1] == "json" {
				decodeSingleJSON(t, []byte(out))
			}
		})
	}
}

func TestGraphQueryCommandTimeWindows(t *testing.T) {
	for _, tt := range []struct {
		name     string
		flags    []string
		duration time.Duration
	}{
		{name: "default hour", duration: time.Hour},
		{name: "since days", flags: []string{"--since", "7d"}, duration: 7 * 24 * time.Hour},
		{name: "relative", flags: []string{"--from", "now-2h", "--to", "now"}, duration: 2 * time.Hour},
	} {
		t.Run(tt.name, func(t *testing.T) {
			pinHumanMode(t)
			before := time.Now().UnixMilli()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var req kg.CypherQueryRequest
				if !assert.NoError(t, json.NewDecoder(r.Body).Decode(&req)) {
					return
				}
				assert.Equal(t, tt.duration.Milliseconds(), req.End-req.Start)
				assert.GreaterOrEqual(t, req.End, before)
				assert.LessOrEqual(t, req.End, time.Now().UnixMilli())
				_, _ = fmt.Fprint(w, `{"columns":["value"],"rows":[],"stats":{"columnCount":1,"rowCount":0,"elapsedMs":0}}`)
			}))
			defer server.Close()
			loader := &kg.FakeWriteLoader{Cfg: config.NamespacedRESTConfig{Config: rest.Config{Host: server.URL}}}
			args := append([]string{"RETURN 1 AS value LIMIT 0", "-o", "json"}, tt.flags...)
			out, stderr, err := runKgCommand(t, func() *cobra.Command { return kg.NewGraphQueryCommand(loader) }, args, "")
			require.NoError(t, err)
			assert.Empty(t, stderr)
			assert.Contains(t, out, `"rows": []`)
			assert.Contains(t, out, `"value"`)
		})
	}
}

func TestGraphQueryCommandInvalidInputsBeforeConfig(t *testing.T) {
	for _, tt := range []struct {
		name string
		args []string
		want string
	}{
		{"blank", []string{"  "}, "must not be blank"},
		{"empty since", []string{"RETURN 1 LIMIT 1", "--since="}, "--since must not be empty"},
		{"empty from", []string{"RETURN 1 LIMIT 1", "--from="}, "--from must not be empty"},
		{"empty to", []string{"RETURN 1 LIMIT 1", "--to="}, "--to must not be empty"},
		{"negative", []string{"RETURN 1 LIMIT 1", "--since=-1h"}, "must be positive"},
		{"zero", []string{"RETURN 1 LIMIT 1", "--since=0s"}, "must be positive"},
		{"bad duration", []string{"RETURN 1 LIMIT 1", "--since=1junkd"}, "invalid --since"},
		{"from only", []string{"RETURN 1 LIMIT 1", "--from=now-1h"}, "supplied together"},
		{"to only", []string{"RETURN 1 LIMIT 1", "--to=now"}, "supplied together"},
		{"conflict", []string{"RETURN 1 LIMIT 1", "--since=1h", "--from=now-1h", "--to=now"}, "mutually exclusive"},
		{"equal", []string{"RETURN 1 LIMIT 1", "--from=now", "--to=now"}, "start before end"},
		{"reverse", []string{"RETURN 1 LIMIT 1", "--from=now", "--to=now-1h"}, "start before end"},
		{"epoch zero", []string{"RETURN 1 LIMIT 1", "--from=1970-01-01T00:00:00Z", "--to=now"}, "positive epoch"},
		{"legacy page", []string{"RETURN 1 LIMIT 1", "--page=1"}, "unknown flag: --page"},
		{"legacy insights", []string{"RETURN 1 LIMIT 1", "--insights-only"}, "unknown flag: --insights-only"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			pinHumanMode(t)
			loader := &kg.FakeWriteLoader{CfgErr: errors.New("configuration should not be loaded")}
			out, _, err := runKgCommand(t, func() *cobra.Command { return kg.NewGraphQueryCommand(loader) }, tt.args, "")
			require.ErrorContains(t, err, tt.want)
			assert.Empty(t, out)
		})
	}
}

func TestCypherQueryErrors(t *testing.T) {
	for _, tt := range []struct {
		status int
		code   string
	}{
		{400, "CYPHER_INVALID_REQUEST"}, {404, "NOT_FOUND"}, {422, "CYPHER_LIMIT_REQUIRED"}, {429, "CYPHER_CONCURRENCY_LIMIT"}, {503, "CYPHER_BACKEND_UNAVAILABLE"},
	} {
		t.Run(tt.code, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				w.WriteHeader(tt.status)
				_, _ = fmt.Fprintf(w, `{"code":%q,"message":"query refused"}`, tt.code)
			}))
			defer server.Close()
			_, err := newTestClient(t, server).CypherQuery(t.Context(), kg.CypherQueryRequest{Query: "RETURN 1", Start: 1, End: 2})
			var apiErr *kg.APIError
			require.ErrorAs(t, err, &apiErr)
			assert.Equal(t, tt.status, apiErr.StatusCode)
			assert.Equal(t, tt.code, apiErr.Code)
			assert.Contains(t, apiErr.APIUserMessage(), tt.code)
			assert.Equal(t, 1, calls, "must not fall back to legacy endpoint")
		})
	}
}

func TestCypherRowsTableCodec(t *testing.T) {
	for _, tt := range []struct {
		name, body, want string
	}{
		{"empty", `{"columns":["name"],"rows":[]}`, "name"},
		{"duplicate columns", `{"columns":["x","x"],"rows":[[true,false],[null,0]]}`, "false"},
		{"nested", `{"columns":["list","map"],"rows":[[[1,2],{"a":null}]]}`, `{"a":null}`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			forceNoColor(t)
			var result kg.CypherQueryResponse
			require.NoError(t, json.Unmarshal([]byte(tt.body), &result))
			var out bytes.Buffer
			err := (&kg.CypherRowsTableCodec{}).Encode(&out, &result)
			require.NoError(t, err)
			assert.Contains(t, strings.TrimSpace(out.String()), tt.want)
		})
	}
}

func TestCypherQueryPreservesGraphValues(t *testing.T) {
	for _, tt := range []struct{ name, response string }{
		{"graph values", `{"columns":["node","edge","path","values"],"rows":[[{"kind":"node","ref":"n0","labels":["Service"],"properties":{"name":"cart","large":9223372036854775807}},{"kind":"relationship","ref":"e0","type":"CALLS","startRef":"n0","endRef":"n1","properties":{}},{"nodes":[{"kind":"node","ref":"n0","labels":["Service"],"properties":{"name":"cart"}},{"kind":"node","ref":"n1","labels":["Service"],"properties":{"name":"db"}}],"relationships":[{"kind":"relationship","ref":"e0","type":"CALLS","startRef":"n0","endRef":"n1","properties":{}}]},[true,null,0,1.25,{"message":"ok"}]]],"stats":{"columnCount":4,"rowCount":1,"elapsedMs":2}}`},
		{"empty rows retain columns", `{"columns":["name"],"rows":[],"stats":{"columnCount":1,"rowCount":0,"elapsedMs":0}}`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = fmt.Fprint(w, tt.response) }))
			defer server.Close()
			got, err := newTestClient(t, server).CypherQuery(t.Context(), kg.CypherQueryRequest{Query: "RETURN 1 LIMIT 1", Start: 1, End: 2})
			require.NoError(t, err)
			encoded, err := json.Marshal(got)
			require.NoError(t, err)
			var expected, actual any
			original := json.NewDecoder(strings.NewReader(tt.response))
			original.UseNumber()
			require.NoError(t, original.Decode(&expected))
			roundTrip := json.NewDecoder(bytes.NewReader(encoded))
			roundTrip.UseNumber()
			require.NoError(t, roundTrip.Decode(&actual))
			assert.Equal(t, expected, actual, "cell values and numeric precision must survive transport unchanged")
		})
	}
}

func TestGraphQueryHelpPathBounds(t *testing.T) {
	cmd := kg.NewGraphQueryCommand(&kg.FakeWriteLoader{})
	long := strings.Join(strings.Fields(cmd.Long), " ")
	assert.Contains(t, long, "upper bound of at most 5 hops", "help must state the hop maximum the endpoint enforces")
	bounds := regexp.MustCompile(`\*(\d*)\.\.(\d+)`).FindAllStringSubmatch(cmd.Long+cmd.Example, -1)
	require.NotEmpty(t, bounds, "help should show a bounded variable-length relationship")
	for _, b := range bounds {
		upper, err := strconv.Atoi(b[2])
		require.NoError(t, err)
		assert.LessOrEqual(t, upper, 5, "%s exceeds the hop maximum", b[0])
	}
}

func TestGraphQueryHelpUsesPlaceholders(t *testing.T) {
	cmd := kg.NewGraphQueryCommand(&kg.FakeWriteLoader{})
	// A plain, backtick-quoted or angle-bracket name.
	const name = "`[^`]*`|<[^>]*>|[A-Za-z_][A-Za-z0-9_]*"
	nameAt := regexp.MustCompile(`^\s*(` + name + `)`)
	placeholder := regexp.MustCompile(`^<[A-Za-z0-9]+>$`)
	// TYPE is the help's generic relationship type in its syntax notes.
	isType := func(s string) bool { return placeholder.MatchString(s) || s == "TYPE" }
	// The name and scope keys gcx itself matches on.
	isProperty := func(s string) bool {
		switch s {
		case "name", "env", "site", "namespace":
			return true
		}
		return placeholder.MatchString(s)
	}

	t.Run("pattern labels", func(t *testing.T) {
		// The labels after the colon of a node or relationship pattern, with
		// or without a variable, as in (e:<Type>, (n1:<Type>, [:<REL>*1..5]
		// or (a:<TypeA>|<TypeB>.
		pattern := regexp.MustCompile(`[(\[]\s*(?:[A-Za-z_][A-Za-z0-9_]*)?\s*:\s*((?:` + name + `)(?:\s*[|&:]\s*(?:` + name + `))*)`)
		matches := pattern.FindAllStringSubmatch(cmd.Long+cmd.Example, -1)
		require.NotEmpty(t, matches, "help should show patterns with a label or relationship type")
		for _, m := range matches {
			for _, label := range regexp.MustCompile(name).FindAllString(m[1], -1) {
				assert.True(t, isType(label), "%q names a type; help patterns use placeholders filled from 'gcx kg meta schema'", m[0])
			}
		}
	})

	t.Run("queries", func(t *testing.T) {
		examples := regexp.MustCompile(`"(MATCH [^"]*)"`).FindAllStringSubmatch(cmd.Example, -1)
		inLong := regexp.MustCompile(`'(MATCH [^']*)'`).FindAllStringSubmatch(cmd.Long, -1)
		queries := make([]string, 0, len(examples)+len(inLong))
		for _, m := range append(examples, inLong...) {
			queries = append(queries, m[1])
		}
		require.GreaterOrEqual(t, len(queries), 5, "the four examples and the Scope paragraph's query")
		for _, query := range queries {
			masked := regexp.MustCompile(`'[^']*'|"[^"]*"`).ReplaceAllString(query, "''")
			// Outside a map, a colon starts a label: in a pattern or a
			// predicate such as WHERE x:<Type>. Inside one, it ends a key.
			depth := 0
			for i, c := range masked {
				switch c {
				case '{':
					depth++
				case '}':
					depth--
				case ':':
					if depth == 0 {
						m := nameAt.FindStringSubmatch(masked[i+1:])
						require.NotNil(t, m, "no label after the colon at %d in %q", i, query)
						assert.True(t, isType(m[1]), "%q names the type %s; use a placeholder", query, m[1])
					}
				}
			}
			keys := regexp.MustCompile(`[{,]\s*(`+name+`)\s*:`).FindAllStringSubmatch(masked, -1)
			accesses := regexp.MustCompile(`[A-Za-z0-9_)\]]\s*\.\s*(`+name+`)`).FindAllStringSubmatch(masked, -1)
			for _, m := range append(keys, accesses...) {
				assert.True(t, isProperty(m[1]), "%q names the property %s; use a placeholder", query, m[1])
			}
		}
	})
}
