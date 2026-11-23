package kg

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// catalogWarningCase is a case of catalogRowsWarning.
type catalogWarningCase struct {
	name    string
	query   string
	columns []string
	rows    [][]string
	want    []string // substrings of the warning; nil means no warning
	wantNot []string // strings the warning must not contain
}

func runCatalogWarningCases(t *testing.T, cases []catalogWarningCase) {
	t.Helper()
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			resp := &CypherQueryResponse{Columns: tt.columns, Rows: [][]json.RawMessage{}}
			for _, row := range tt.rows {
				cells := make([]json.RawMessage, len(row))
				for i, cell := range row {
					cells[i] = json.RawMessage(cell)
				}
				resp.Rows = append(resp.Rows, cells)
			}
			got := catalogRowsWarning(tt.query, resp)
			if tt.want == nil {
				assert.Empty(t, got)
				return
			}
			for _, want := range tt.want {
				assert.Contains(t, got, want)
			}
			for _, wantNot := range tt.wantNot {
				assert.NotContains(t, got, wantNot)
			}
		})
	}
}

const (
	schemaNode = `{"kind":"node","ref":"n0","labels":["Schema"],"properties":{"name":"name-type"}}`
	entityNode = `{"kind":"node","ref":"n1","labels":["TypeA"],"properties":{"name":"name-a"}}`
)

func TestCatalogRowsWarning(t *testing.T) {
	runCatalogWarningCases(t, []catalogWarningCase{
		{
			name:    "aliased label projections of a catalog group",
			query:   "MATCH (a)-[r:TYPE_A]-(b) RETURN labels(a)[0] AS from_type, labels(b)[0] AS to_type, count(r) AS n LIMIT 10",
			columns: []string{"from_type", "to_type", "n"},
			rows:    [][]string{{`"Schema"`, `"Schema"`, `1`}, {`"TypeA"`, `"TypeB"`, `5`}, {`"EntityType"`, `"EntityType"`, `1`}},
			want: []string{
				"2 of 3 rows hold the graph's own catalog records (labels EntityType, Schema)",
				"A label on each node leaves them out.",
				"For an unlabeled node n, filter its label after a WITH, as in WITH n, labels(n)[0] AS type WHERE type <> 'EntityType' AND type <> 'Schema', and list any other variable",
				"a WHERE on labels(n) right after MATCH can time out on a large graph.",
				"not observed entities; a node pattern without a label matches them too",
			},
		},
		{
			name:    "account records are catalog records",
			query:   "MATCH (a) RETURN labels(a)[0] AS t, count(a) AS n ORDER BY t LIMIT 100",
			columns: []string{"t", "n"},
			rows:    [][]string{{`"Account"`, `1`}, {`"EntityType"`, `2`}, {`"TypeB"`, `4`}},
			want: []string{
				"2 of 3 rows hold", "(labels Account, EntityType)",
				"WHERE type <> 'Account' AND type <> 'EntityType', and",
			},
		},
		{
			name:    "account nodes are catalog records",
			query:   "MATCH (a) RETURN a LIMIT 10",
			columns: []string{"a"},
			rows:    [][]string{{`{"kind":"node","ref":"n0","labels":["Account"],"properties":{"name":"acct-a"}}`}, {entityNode}},
			want:    []string{"1 of 2 rows holds", "(label Account)"},
		},
		{
			name:    "a filter that leaves out the other catalog labels still gets a warning for account records",
			query:   "MATCH (a) WHERE labels(a)[0] <> 'EntityType' AND labels(a)[0] <> 'Env' AND labels(a)[0] <> 'Schema' RETURN labels(a)[0] AS t, count(a) AS n ORDER BY t LIMIT 100",
			columns: []string{"t", "n"},
			rows:    [][]string{{`"Account"`, `1`}, {`"TypeB"`, `4`}},
			want:    []string{"1 of 2 rows holds", "(label Account)", "WHERE type <> 'Account', and"},
			wantNot: []string{"EntityType", "Schema"},
		},
		{
			name:    "returned nodes",
			query:   "MATCH (a)-[r:TYPE_A]->(b) RETURN a, r, b LIMIT 3",
			columns: []string{"a", "r", "b"},
			rows:    [][]string{{schemaNode, `{"kind":"relationship","ref":"e0","type":"TYPE_A","startRef":"n0","endRef":"n1"}`, schemaNode}, {entityNode, `{"kind":"relationship"}`, entityNode}},
			want:    []string{"1 of 2 rows", "(label Schema)"},
		},
		{
			name:    "nodes inside a path",
			query:   "MATCH p = (a)-[:TYPE_A]->(b) RETURN p LIMIT 1",
			columns: []string{"p"},
			rows:    [][]string{{`{"kind":"path","nodes":[` + schemaNode + `],"relationships":[]}`}},
			want:    []string{"1 of 1 row holds", "(label Schema)"},
		},
		{
			name:    "observed entities only",
			query:   "MATCH (a)-[r:TYPE_A]-(b) RETURN labels(a)[0] AS from_type, labels(b)[0] AS to_type, count(r) AS n LIMIT 10",
			columns: []string{"from_type", "to_type", "n"},
			rows:    [][]string{{`"TypeA"`, `"TypeB"`, `5`}},
		},
		{
			name:    "a property value equal to a catalog label is not a label column",
			query:   "MATCH (s:TypeA) RETURN s.name AS name, labels(s)[0] AS type LIMIT 10",
			columns: []string{"name", "type"},
			rows:    [][]string{{`"Schema"`, `"TypeA"`}},
		},
		{
			name:    "a node property named labels is not a node",
			query:   "MATCH (s:TypeA) RETURN properties(s) AS props LIMIT 1",
			columns: []string{"props"},
			rows:    [][]string{{`{"labels":["Schema"],"name":"name-a"}`}},
		},
		{
			name:    "a query that names a catalog label wants them",
			query:   "MATCH (a:Schema)-[r]->(b) RETURN labels(a)[0] AS t, count(r) AS n LIMIT 10",
			columns: []string{"t", "n"},
			rows:    [][]string{{`"Schema"`, `3`}},
		},
		{
			name:    "a query that filters a catalog label out names it",
			query:   "MATCH (a)-[r]->(b) WHERE labels(a)[0] <> 'Schema' RETURN labels(b)[0] AS t LIMIT 10",
			columns: []string{"t"},
			rows:    [][]string{{`"TypeA"`}},
		},
		{
			name:    "a query that filters one catalog label out still gets a warning for the others",
			query:   "MATCH (a)-[r]->(b) WHERE labels(a)[0] <> 'Schema' RETURN labels(a)[0] AS f, type(r) AS t, labels(b)[0] AS to, count(r) AS n LIMIT 100",
			columns: []string{"f", "t", "to", "n"},
			rows:    [][]string{{`"EntityType"`, `"TYPE_A"`, `"EntityType"`, `4`}, {`"TypeA"`, `"TYPE_B"`, `"TypeB"`, `5`}},
			want:    []string{"1 of 2 rows", "(label EntityType)", "WHERE type <> 'EntityType', and"},
			wantNot: []string{"Schema"},
		},
		{
			name:    "rows that carry only a named catalog label are not counted",
			query:   "MATCH (a)-[r:TYPE_A]->(b) WHERE labels(b)[0] <> 'Env' RETURN labels(a)[0] AS t, count(r) AS n LIMIT 10",
			columns: []string{"t", "n"},
			rows:    [][]string{{`"Schema"`, `1`}, {`"Env"`, `1`}, {`"TypeB"`, `2`}},
			want:    []string{"1 of 3 rows", "(label Schema)"},
			wantNot: []string{"Env"},
		},
		{
			name:    "a catalog label string in a property filter does not name it",
			query:   "MATCH (n {name: 'name-a'}) WHERE n.name <> 'Schema' AND n.name <> 'EntityType' RETURN labels(n)[0] AS t, n.name AS name LIMIT 10",
			columns: []string{"t", "name"},
			rows:    [][]string{{`"Schema"`, `"name-a"`}, {`"EntityType"`, `"name-a"`}},
			want:    []string{"2 of 2 rows hold", "(labels EntityType, Schema)"},
		},
		{
			name:    "a catalog label compared with a WITH alias of labels names it",
			query:   "MATCH (n) WITH n, labels(n)[0] AS type WHERE type <> 'Schema' RETURN type, count(n) AS c LIMIT 100",
			columns: []string{"type", "c"},
			rows:    [][]string{{`"Schema"`, `1`}, {`"Env"`, `2`}, {`"TypeA"`, `4`}},
			want:    []string{"1 of 3 rows holds", "(label Env)", "WHERE type <> 'Env', and"},
			wantNot: []string{"Schema"},
		},
		{
			name:    "a catalog label compared through a list predicate variable over labels names it",
			query:   "MATCH (a) WHERE any(l IN labels(a) WHERE l = 'Env') RETURN labels(a)[0] AS t LIMIT 5",
			columns: []string{"t"},
			rows:    [][]string{{`"Env"`}},
		},
		{
			name:    "catalog labels in a list predicate's list compared with labels name them",
			query:   "MATCH (a) WHERE any(l IN ['Schema', 'Env'] WHERE l IN labels(a)) RETURN labels(a)[0] AS t LIMIT 5",
			columns: []string{"t"},
			rows:    [][]string{{`"Schema"`}, {`"Env"`}},
		},
		{
			name:    "an alias equal to a catalog label does not name it",
			query:   "MATCH (n) RETURN labels(n)[0] AS EntityType, count(n) AS c ORDER BY EntityType LIMIT 50",
			columns: []string{"EntityType", "c"},
			rows:    [][]string{{`"EntityType"`, `2`}, {`"Env"`, `3`}, {`"Schema"`, `4`}, {`"TypeB"`, `5`}},
			want: []string{
				"3 of 4 rows", "(labels EntityType, Env, Schema)",
				"WHERE type <> 'EntityType' AND type <> 'Env' AND type <> 'Schema', and",
			},
		},
		{
			name:    "a backtick alias, a variable and a property key equal to catalog labels do not name them",
			query:   "MATCH (Env)-[r:TYPE_A]->(b) WHERE b.Schema IS NULL RETURN labels(Env)[0] AS `EntityType`, count(r) AS n LIMIT 10",
			columns: []string{"EntityType", "n"},
			rows:    [][]string{{`"EntityType"`, `1`}, {`"Env"`, `1`}, {`"Schema"`, `1`}},
			want:    []string{"3 of 3 rows", "(labels EntityType, Env, Schema)"},
		},
		{
			name:    "a label predicate and a label expression name their labels",
			query:   "MATCH (a:TypeA|Schema)-[r]->(b) WHERE NOT b:EntityType RETURN labels(a)[0] AS t, labels(b)[0] AS u LIMIT 10",
			columns: []string{"t", "u"},
			rows:    [][]string{{`"Schema"`, `"EntityType"`}, {`"TypeA"`, `"Env"`}},
			want:    []string{"1 of 2 rows", "(label Env)"},
			wantNot: []string{"Schema", "EntityType"},
		},
		{
			name:    "a catalog label in a line comment does not name it",
			query:   "MATCH (a)-[r:TYPE_A]->(b) RETURN labels(a)[0] AS t, count(r) AS n LIMIT 10 // EntityType",
			columns: []string{"t", "n"},
			rows:    [][]string{{`"EntityType"`, `1`}},
			want:    []string{"1 of 1 row holds", "(label EntityType)"},
		},
		{
			name:    "a catalog label in a block comment does not name it",
			query:   "MATCH (a)-[r:TYPE_A]->(b) /* not Schema */ RETURN labels(a)[0] AS t, count(r) AS n LIMIT 10",
			columns: []string{"t", "n"},
			rows:    [][]string{{`"Schema"`, `1`}},
			want:    []string{"1 of 1 row holds", "(label Schema)"},
		},
		{
			name:    "no rows",
			query:   "MATCH (a)-[r]->(b) RETURN labels(a)[0] AS t LIMIT 10",
			columns: []string{"t"},
		},
	})
}

// TestCatalogRowsWarningLabelColumns covers columns that project labels(...),
// directly or through aliases.
func TestCatalogRowsWarningLabelColumns(t *testing.T) {
	runCatalogWarningCases(t, []catalogWarningCase{
		{
			name:    "a WITH alias of labels renamed in RETURN",
			query:   "MATCH (a) WITH labels(a)[0] AS t, count(a) AS c RETURN t AS type, c ORDER BY type LIMIT 50",
			columns: []string{"type", "c"},
			rows:    [][]string{{`"Schema"`, `3`}, {`"TypeB"`, `4`}},
			want:    []string{"1 of 2 rows holds", "(label Schema)"},
		},
		{
			name:    "a WITH alias of a labels list indexed in RETURN",
			query:   "MATCH (a) WITH labels(a) AS ls, count(a) AS c RETURN ls[0] AS type, c ORDER BY type LIMIT 50",
			columns: []string{"type", "c"},
			rows:    [][]string{{`"EntityType"`, `2`}, {`"TypeB"`, `4`}},
			want:    []string{"1 of 2 rows holds", "(label EntityType)"},
		},
		{
			name:    "an unaliased column indexing a WITH alias of a labels list",
			query:   "MATCH (a) WITH labels(a) AS ls RETURN ls[0], count(*) LIMIT 50",
			columns: []string{"ls[0]", "count(*)"},
			rows:    [][]string{{`"Env"`, `1`}, {`"TypeB"`, `4`}},
			want:    []string{"1 of 2 rows holds", "(label Env)"},
		},
		{
			name:    "an alias chain through two WITH clauses and a backtick name",
			query:   "MATCH (a) WITH head(labels(a)) AS `first label`, a WITH `first label` AS t, count(a) AS c RETURN coalesce(t, 'none') AS type, c LIMIT 50",
			columns: []string{"type", "c"},
			rows:    [][]string{{`"Schema"`, `3`}, {`"TypeB"`, `4`}},
			want:    []string{"1 of 2 rows holds", "(label Schema)"},
		},
		{
			name:    "an alias given a new expression no longer projects labels",
			query:   "MATCH (a) WITH labels(a)[0] AS t, a WITH a.name AS t RETURN t LIMIT 10",
			columns: []string{"t"},
			rows:    [][]string{{`"Env"`}, {`"name-a"`}},
		},
		{
			name:    "a property key equal to an alias of labels does not refer to it",
			query:   "MATCH (a) WITH labels(a)[0] AS t, a RETURN a.t AS x, a. t AS y LIMIT 10",
			columns: []string{"x", "y"},
			rows:    [][]string{{`"Env"`, `"Schema"`}},
		},
		{
			name:    "labels of startNode and endNode",
			query:   "MATCH ()-[r]->() RETURN labels(startNode(r))[0] AS from_type, type(r) AS rel, labels(endNode(r))[0] AS to_type, count(r) AS n LIMIT 50",
			columns: []string{"from_type", "rel", "to_type", "n"},
			rows:    [][]string{{`"Schema"`, `"TYPE_A"`, `"Schema"`, `2`}},
			want:    []string{"1 of 1 row holds", "(label Schema)", "WHERE type <> 'Schema', and"},
		},
		{
			name:    "unaliased labels list column",
			query:   "MATCH (a)-[r:TYPE_A]->(b) RETURN labels(a), a.name, labels(b), b.name LIMIT 10",
			columns: []string{"labels(a)", "a.name", "labels(b)", "b.name"},
			rows:    [][]string{{`["Schema"]`, `"name-type"`, `["Schema"]`, `"name-b"`}},
			want:    []string{"1 of 1 row holds", "(label Schema)"},
		},
		{
			name:    "head of labels with a backtick alias",
			query:   "MATCH (a) RETURN head(labels(a)) AS `node type`, count(a) AS n LIMIT 10",
			columns: []string{"node type", "n"},
			rows:    [][]string{{`"Env"`, `1`}, {`"TypeB"`, `40`}},
			want:    []string{"1 of 2 rows holds", "(label Env)"},
		},
		{
			name:    "unaliased head of labels column",
			query:   "MATCH (a) RETURN head(labels(a)), count(a) LIMIT 10",
			columns: []string{"head(labels(a))", "count(a)"},
			rows:    [][]string{{`"Schema"`, `1`}, {`"TypeB"`, `4`}},
			want:    []string{"1 of 2 rows", "(label Schema)"},
		},
		{
			name:    "unaliased column of labels in another wrapper",
			query:   "MATCH (a) RETURN last(labels(a)), count(a) LIMIT 10",
			columns: []string{"last(labels(a))", "count(a)"},
			rows:    [][]string{{`"Schema"`, `1`}, {`"TypeB"`, `4`}},
			want:    []string{"1 of 2 rows", "(label Schema)"},
		},
		{
			name:    "unaliased column of indexed labels inside a call with more arguments",
			query:   "MATCH (a) RETURN coalesce(labels(a)[0], 'none'), count(a) LIMIT 10",
			columns: []string{"coalesce(labels(a)[0], 'none')", "count(a)"},
			rows:    [][]string{{`"EntityType"`, `1`}, {`"TypeB"`, `4`}},
			want:    []string{"1 of 2 rows", "(label EntityType)"},
		},
		{
			name:    "aliased labels nested in two calls",
			query:   "MATCH (a) RETURN coalesce(head(labels(a)), 'none') AS t, count(a) AS n LIMIT 10",
			columns: []string{"t", "n"},
			rows:    [][]string{{`"Env"`, `1`}, {`"TypeB"`, `4`}},
			want:    []string{"1 of 2 rows", "(label Env)"},
		},
		{
			name:    "aliased list of labels",
			query:   "MATCH (a)-[r:TYPE_A]->(b) RETURN [labels(a)[0], labels(b)[0]] AS pair, count(r) AS n LIMIT 10",
			columns: []string{"pair", "n"},
			rows:    [][]string{{`["Schema","TypeB"]`, `1`}, {`["TypeB","TypeB"]`, `4`}},
			want:    []string{"1 of 2 rows", "(label Schema)"},
		},
		{
			name:    "labels of unwound path nodes",
			query:   "MATCH p = (a)-[r]->(b) UNWIND nodes(p) AS x RETURN labels(x)[0] AS t, count(*) AS n LIMIT 10",
			columns: []string{"t", "n"},
			rows:    [][]string{{`"Schema"`, `1`}, {`"TypeB"`, `4`}},
			want:    []string{"1 of 2 rows", "(label Schema)"},
		},
		{
			name:    "labels aliased in WITH and returned by name",
			query:   "MATCH (a) WITH labels(a)[0] AS t, count(a) AS n WHERE n > 0 RETURN t, n LIMIT 10",
			columns: []string{"t", "n"},
			rows:    [][]string{{`"Schema"`, `1`}, {`"TypeB"`, `4`}},
			want:    []string{"1 of 2 rows", "(label Schema)"},
		},
		{
			name:    "sizes of labels, aliased or not, are not catalog labels",
			query:   "MATCH (a)-[r:TYPE_A]->(b) RETURN size(labels(a)) AS n, size(labels(b)), count(r) AS c LIMIT 10",
			columns: []string{"n", "size(labels(b))", "c"},
			rows:    [][]string{{`1`, `1`, `3`}},
		},
		{
			name:    "aliased labels of a doubly nested call",
			query:   "MATCH p = (a)-[r]->(b) RETURN DISTINCT labels(last(nodes(p)))[0] AS t LIMIT 50",
			columns: []string{"t"},
			rows:    [][]string{{`"Schema"`}, {`"TypeB"`}},
			want:    []string{"1 of 2 rows", "(label Schema)"},
		},
		{
			name:    "a map literal of labels",
			query:   "MATCH (n {name: 'name-a'}) RETURN {t: labels(n)[0]} AS m LIMIT 5",
			columns: []string{"m"},
			rows:    [][]string{{`{"t":"Schema"}`}, {`{"t":"EntityType"}`}},
			want:    []string{"2 of 2 rows hold", "(labels EntityType, Schema)"},
		},
		{
			name:    "an unaliased map literal column",
			query:   "MATCH (n) RETURN {t: labels(n)[0]} LIMIT 5",
			columns: []string{"{t: labels(n)[0]}"},
			rows:    [][]string{{`{"t":"Env"}`}, {`{"t":"TypeA"}`}},
			want:    []string{"1 of 2 rows holds", "(label Env)"},
		},
		{
			name:    "a map key that holds a property is not a label",
			query:   "MATCH (n) RETURN {t: labels(n)[0], name: n.name} AS m LIMIT 5",
			columns: []string{"m"},
			rows:    [][]string{{`{"t":"TypeA","name":"Schema"}`}},
		},
		{
			name:    "a nested map literal with a backtick key",
			query:   "MATCH (n) RETURN {a: {`node type`: labels(n)}} AS m LIMIT 5",
			columns: []string{"m"},
			rows:    [][]string{{`{"a":{"node type":["Schema"]}}`}, {`{"a":{"node type":["TypeA"]}}`}},
			want:    []string{"1 of 2 rows holds", "(label Schema)"},
		},
		{
			name:    "a map projection with a property selector",
			query:   "MATCH (n) RETURN n {.name, t: labels(n)[0]} AS m LIMIT 5",
			columns: []string{"m"},
			rows:    [][]string{{`{"name":"Schema","t":"Env"}`}},
			want:    []string{"1 of 1 row holds", "(label Env)"},
			wantNot: []string{"Schema"},
		},
		{
			name:    "a map projection of a WITH alias of labels",
			query:   "MATCH (n) WITH labels(n)[0] AS t, n RETURN n {.name, t} AS m LIMIT 5",
			columns: []string{"m"},
			rows:    [][]string{{`{"name":"name-a","t":"Account"}`}},
			want:    []string{"1 of 1 row holds", "(label Account)"},
		},
		{
			name:    "a node inside a map",
			query:   "MATCH (n) RETURN {node: n} AS m LIMIT 5",
			columns: []string{"m"},
			rows:    [][]string{{`{"node":` + schemaNode + `}`}, {`{"node":` + entityNode + `}`}},
			want:    []string{"1 of 2 rows holds", "(label Schema)"},
		},
		{
			name:    "a relationship alone gives no labels for its ends",
			query:   "MATCH (a)-[r:TYPE_A]->(b) RETURN r LIMIT 5",
			columns: []string{"r"},
			rows:    [][]string{{`{"kind":"relationship","ref":"e0","type":"TYPE_A","startRef":"n0","endRef":"n0","properties":{}}`}},
		},
		{
			name:    "aliased labels list of a doubly nested call",
			query:   "MATCH p = (a)-[r]->(b) RETURN labels(head(nodes(p))) AS t, count(r) AS n LIMIT 50",
			columns: []string{"t", "n"},
			rows:    [][]string{{`["EntityType"]`, `2`}},
			want:    []string{"1 of 1 row holds", "(label EntityType)"},
		},
	})
}

func TestCypherRefersTo(t *testing.T) {
	marked := map[string]bool{"t": true, "first label": true, "old": false}
	for _, tt := range []struct {
		name string
		expr string
		want bool
	}{
		{name: "the alias itself", expr: "t", want: true},
		{name: "an index of the alias", expr: "t[0]", want: true},
		{name: "an argument of a call", expr: "coalesce(t, 'none')", want: true},
		{name: "a backtick-quoted alias", expr: "`first label`", want: true},
		{name: "a slice bound", expr: "x[1..t]", want: true},
		{name: "a property key", expr: "a.t"},
		{name: "a property key after a space", expr: "a. t"},
		{name: "a backtick-quoted property key", expr: "a.`t`"},
		{name: "a longer word", expr: "type"},
		{name: "a word ending in the alias", expr: "at"},
		{name: "an alias that is not marked", expr: "old"},
		{name: "an alias that is not known", expr: "u"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, cypherRefersTo(tt.expr, func(name string) bool { return marked[name] }))
		})
	}
}

func TestCypherNamedLabels(t *testing.T) {
	for _, tt := range []struct {
		name  string
		query string
		want  []string
	}{
		{name: "node label", query: "MATCH (a:Schema) RETURN a LIMIT 1", want: []string{"Schema"}},
		{name: "backtick label with spaces", query: "MATCH (a: `Env`) RETURN a LIMIT 1", want: []string{"Env"}},
		{name: "label expression", query: "MATCH (a:TypeA|EntityType&Schema) RETURN a LIMIT 1", want: []string{"EntityType", "Schema"}},
		{name: "chained labels", query: "MATCH (a:TypeA:Env) RETURN a LIMIT 1", want: []string{"Env"}},
		{name: "label predicate", query: "MATCH (a) WHERE NOT a:Schema RETURN a LIMIT 1", want: []string{"Schema"}},
		{name: "single-quoted string", query: "MATCH (a) WHERE labels(a)[0] <> 'Env' RETURN a LIMIT 1", want: []string{"Env"}},
		{name: "double-quoted string", query: `MATCH (a) WHERE NOT "Schema" IN labels(a) RETURN a LIMIT 1`, want: []string{"Schema"}},
		{name: "alias", query: "MATCH (a) RETURN labels(a)[0] AS EntityType LIMIT 1"},
		{name: "backtick alias", query: "MATCH (a) RETURN labels(a)[0] AS `Schema` LIMIT 1"},
		{name: "variable and property key", query: "MATCH (Env) WHERE Env.Schema = 1 RETURN Env LIMIT 1"},
		{name: "map key", query: "MATCH (a {Schema: 1}) RETURN a LIMIT 1"},
		{name: "colon inside a string is not a label", query: "MATCH (a) WHERE a.url = 'x:y' RETURN a AS Env LIMIT 1"},
		{name: "relationship type is not a catalog label", query: "MATCH (a)-[r:TYPE_A]->(b) RETURN a LIMIT 1"},
		{name: "string compared with a property", query: "MATCH (a) WHERE a.name <> 'Schema' AND a.name <> 'EntityType' RETURN a LIMIT 1"},
		{name: "string in a property map", query: "MATCH (a {name: 'Schema'}) RETURN a LIMIT 1"},
		{name: "property IN a list of strings", query: "MATCH (a) WHERE a.name IN ['Env', 'Schema'] RETURN a LIMIT 1"},
		{name: "labels IN a list of strings", query: "MATCH (a) WHERE labels(a)[0] IN ['Env', 'Schema'] RETURN a LIMIT 1", want: []string{"Env", "Schema"}},
		{name: "string on the left of a labels comparison", query: "MATCH (a) WHERE 'Env' = head(labels(a)) RETURN a LIMIT 1", want: []string{"Env"}},
		{name: "labels comparison next to a property comparison", query: "MATCH (a) WHERE a.name = 'Schema' OR labels(a)[0] = 'Env' RETURN a LIMIT 1", want: []string{"Env"}},
		{name: "WITH alias of labels compared", query: "MATCH (a) WITH a, labels(a)[0] AS type WHERE type <> 'Account' AND type <> 'Env' RETURN type, count(a) AS c LIMIT 100", want: []string{"Account", "Env"}},
		{name: "WITH alias of a property compared", query: "MATCH (a) WITH a, a.name AS name WHERE name <> 'Env' RETURN a LIMIT 1"},
		{name: "STARTS WITH on labels", query: "MATCH (a) WHERE labels(a)[0] STARTS WITH 'Env' RETURN a LIMIT 1", want: []string{"Env"}},
		{name: "ENDS WITH on a property before a WITH clause", query: "MATCH (a) WHERE a.name ENDS WITH 'Env' WITH labels(a)[0] AS t RETURN t LIMIT 1"},
		{name: "property comparison before a WITH clause", query: "MATCH (a) WHERE a.name = 'Schema' WITH labels(a)[0] AS t RETURN t LIMIT 1"},
		{name: "CASE WHEN on labels", query: "MATCH (a) RETURN CASE WHEN labels(a)[0] = 'Schema' THEN 'x' ELSE 'y' END AS k LIMIT 1", want: []string{"Schema"}},
		{name: "default value of coalesce", query: "MATCH (a) RETURN coalesce(labels(a)[0], 'Env') AS t LIMIT 1"},
		{name: "list predicate variable over labels", query: "MATCH (a) WHERE any(l IN labels(a) WHERE l = 'Schema') RETURN a LIMIT 1", want: []string{"Schema"}},
		{name: "list predicate variable over a WITH alias of labels", query: "MATCH (a) WITH a, labels(a) AS ls WHERE none(l IN ls WHERE l IN ['Env', 'Account']) RETURN a LIMIT 1", want: []string{"Env", "Account"}},
		{name: "backtick list predicate variable with lower-case in and where", query: "MATCH (a) WHERE SINGLE(`v` in labels(a) where `v` = 'Env') RETURN a LIMIT 1", want: []string{"Env"}},
		{name: "strings of a list predicate's list compared with labels", query: "MATCH (a) WHERE all(l IN ['Env', 'Schema'] WHERE NOT l IN labels(a)) RETURN a LIMIT 1", want: []string{"Env", "Schema"}},
		{name: "property comparison inside a list predicate over labels", query: "MATCH (a) WHERE any(l IN labels(a) WHERE l = 'Schema' OR a.name = 'Env') RETURN a LIMIT 1", want: []string{"Schema"}},
		{name: "string next to a list predicate over labels", query: "MATCH (a) WHERE any(l IN labels(a) WHERE l = 'Schema') OR a.name = 'Env' RETURN a LIMIT 1", want: []string{"Schema"}},
		{name: "list predicate variable outside its call", query: "MATCH (a)-[r]->(l) WHERE any(l IN labels(a) WHERE l = 'Schema') AND l.name = 'Env' RETURN a LIMIT 1", want: []string{"Schema"}},
		{name: "list predicate variable over property keys", query: "MATCH (a) WHERE any(x IN keys(a) WHERE x = 'Schema') RETURN a LIMIT 1"},
		{name: "strings of a list predicate's list compared with a property", query: "MATCH (a) WHERE any(x IN ['Env'] WHERE x = a.name) RETURN a LIMIT 1"},
		{name: "list predicate variable hides a WITH alias of labels", query: "MATCH (a) WITH a, labels(a)[0] AS l WHERE any(l IN keys(a) WHERE l = 'Env') RETURN a LIMIT 1"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, labelAliases := cypherLabelAliases(maskCypherStrings(tt.query, nil))
			got := []string{}
			for label := range cypherNamedLabels(tt.query, labelAliases) {
				got = append(got, label)
			}
			assert.ElementsMatch(t, tt.want, got)
		})
	}
}

func TestCypherMapEntries(t *testing.T) {
	for _, tt := range []struct {
		name  string
		expr  string
		want  []cypherMapEntry
		isMap bool
	}{
		{
			name:  "map literal",
			expr:  "{t: labels(a)[0], name: a.name}",
			want:  []cypherMapEntry{{key: "t", expr: " labels(a)[0]"}, {key: "name", expr: " a.name"}},
			isMap: true,
		},
		{
			name:  "map projection with property, all-properties and variable selectors",
			expr:  "a {.name, .*, t: x, y}",
			want:  []cypherMapEntry{{key: "t", expr: " x"}, {key: "y", expr: "y"}},
			isMap: true,
		},
		{
			name:  "backtick key and a nested map",
			expr:  "{`node type`: {k: f(a, b)}}",
			want:  []cypherMapEntry{{key: "node type", expr: " {k: f(a, b)}"}},
			isMap: true,
		},
		{name: "empty map", expr: "{}", isMap: true},
		{name: "two maps joined", expr: "{a: 1} + {b: 2}"},
		{name: "map inside a call", expr: "f({a: 1})"},
		{name: "list", expr: "[1, 2]"},
		{name: "plain expression", expr: "labels(a)[0]"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, isMap := cypherMapEntries(tt.expr)
			assert.Equal(t, tt.isMap, isMap)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestCypherLabelShape(t *testing.T) {
	whole := &labelShape{whole: true}
	aliases := map[string]*labelShape{"t": whole, "m": {keys: map[string]*labelShape{"k": whole}}, "old": nil}
	for _, tt := range []struct {
		name string
		expr string
		want *labelShape
	}{
		{name: "labels call", expr: "labels(a)", want: whole},
		{name: "labels call under wrappers", expr: "coalesce(head(labels(a)), '    ')", want: whole},
		{name: "expression of a labels alias", expr: "t[0]", want: whole},
		{name: "bare alias keeps its shape", expr: "m", want: aliases["m"]},
		{name: "bare backtick alias", expr: "`t`", want: whole},
		{name: "alias without labels", expr: "old"},
		{name: "property", expr: "a.name"},
		{name: "property key equal to an alias", expr: "a.t"},
		{
			name: "map literal keeps the keys that hold labels",
			expr: "{t: labels(a)[0], name: a.name, n: {u: t}}",
			want: &labelShape{keys: map[string]*labelShape{"t": whole, "n": {keys: map[string]*labelShape{"u": whole}}}},
		},
		{name: "map literal without labels", expr: "{name: a.name}"},
		{name: "map projection", expr: "a {.name, k: labels(a)}", want: &labelShape{keys: map[string]*labelShape{"k": whole}}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, cypherLabelShape(tt.expr, aliases))
		})
	}
}

func TestCypherPredicate(t *testing.T) {
	for _, tt := range []struct {
		name    string
		query   string
		operand string // the first occurrence of it in query
		want    string
	}{
		{name: "between WHERE and AND", query: "MATCH (a) WHERE a.x <> 'v' AND a.y = 1 RETURN a", operand: "'v'", want: " a.x <> 'v' "},
		{name: "a list operand", query: "MATCH (a) WHERE labels(a)[0] IN ['v', 'w'] RETURN a", operand: "'w'", want: " labels(a)[0] IN ['v', 'w'] "},
		{name: "inside a map", query: "MATCH (a {k: 'v', j: 1}) RETURN a", operand: "'v'", want: "k: 'v'"},
		{name: "inside call arguments", query: "RETURN coalesce(a.x, 'v') AS c", operand: "'v'", want: " 'v'"},
		{name: "over calls and brackets", query: "WHERE coalesce(head(labels(a)), x)[0] = 'v' RETURN a", operand: "'v'", want: " coalesce(head(labels(a)), x)[0] = 'v' "},
		{name: "STARTS WITH is an operator", query: "WHERE a.x STARTS WITH 'v' WITH a RETURN a", operand: "'v'", want: " a.x STARTS WITH 'v' "},
		{name: "CASE keywords", query: "RETURN CASE WHEN a.x = 'v' THEN 1 END", operand: "'v'", want: " a.x = 'v' "},
		{name: "a property key named like a keyword", query: "WHERE a.limit = 'v' RETURN a", operand: "'v'", want: " a.limit = 'v' "},
	} {
		t.Run(tt.name, func(t *testing.T) {
			start := strings.Index(tt.query, tt.operand)
			require.GreaterOrEqual(t, start, 0)
			assert.Equal(t, tt.want, cypherPredicate(tt.query, start, start+len(tt.operand)))
		})
	}
}

func TestCypherListPredicateAt(t *testing.T) {
	for _, tt := range []struct {
		name      string
		masked    string // the first ( in it is the one read
		ok        bool
		variable  string
		list      string
		predicate string
	}{
		{name: "any with WHERE", masked: "WHERE any(l IN labels(a) WHERE l = 1) RETURN a", ok: true, variable: "l", list: " labels(a) ", predicate: " l = 1"},
		{name: "backtick variable with lower-case in and where", masked: "WHERE SINGLE(`v` in ls where `v` = 1)", ok: true, variable: "v", list: " ls ", predicate: " `v` = 1"},
		{name: "a list right after IN", masked: "WHERE none(x IN[' '] WHERE x = 1)", ok: true, variable: "x", list: "[' '] ", predicate: " x = 1"},
		{name: "no WHERE", masked: "WHERE all(x IN ls)", ok: true, variable: "x", list: " ls"},
		{name: "another function", masked: "RETURN coalesce(x IN ls) AS c"},
		{name: "no variable", masked: "WHERE any(labels(a))"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			call, ok := cypherListPredicateAt(tt.masked, strings.IndexByte(tt.masked, '('))
			require.Equal(t, tt.ok, ok)
			if !ok {
				return
			}
			assert.Equal(t, tt.variable, call.variable)
			assert.Equal(t, tt.list, tt.masked[call.list[0]:call.list[1]])
			assert.Equal(t, tt.predicate, tt.masked[call.predicate[0]:call.predicate[1]])
		})
	}
}

func TestCypherProjections(t *testing.T) {
	for _, tt := range []struct {
		name  string
		query string
		want  []cypherProjection
	}{
		{
			name:  "aliased items end at ORDER BY",
			query: "MATCH (a) RETURN labels(a)[0] AS t, count(a) AS n ORDER BY n LIMIT 5",
			want:  []cypherProjection{{expr: "labels(a)[0]", alias: "t"}, {expr: "count(a)", alias: "n"}},
		},
		{
			name:  "DISTINCT and unaliased items",
			query: "MATCH (a) RETURN DISTINCT head(labels(a)), a.name LIMIT 5",
			want:  []cypherProjection{{expr: "head(labels(a))"}, {expr: "a.name"}},
		},
		{
			name:  "commas inside calls, lists, maps and masked strings",
			query: "RETURN coalesce(a.x, 'y, z') AS c, [1, 2] AS l, {k: 1, m: 2} AS m LIMIT 1",
			want: []cypherProjection{
				{expr: "coalesce(a.x, '    ')", alias: "c"}, {expr: "[1, 2]", alias: "l"}, {expr: "{k: 1, m: 2}", alias: "m"},
			},
		},
		{
			name:  "WITH items end at WHERE, UNWIND has one item",
			query: "MATCH (a) WITH labels(a) AS ls, a WHERE size(ls) > 0 UNWIND ls AS l RETURN l LIMIT 5",
			want: []cypherProjection{
				{expr: "labels(a)", alias: "ls"}, {expr: "a"}, {expr: "ls", alias: "l"}, {expr: "l"},
			},
		},
		{
			name:  "STARTS WITH and ENDS WITH are operators",
			query: "MATCH (a) WHERE a.name ENDS WITH 'x' RETURN a.name STARTS WITH 'y' AS f, a.name AS n LIMIT 1",
			want:  []cypherProjection{{expr: "a.name STARTS WITH ' '", alias: "f"}, {expr: "a.name", alias: "n"}},
		},
		{
			name:  "backtick alias and properties named like keywords",
			query: "MATCH (a) RETURN a.limit AS `node type`, a.order LIMIT 1",
			want:  []cypherProjection{{expr: "a.limit", alias: "node type"}, {expr: "a.order"}},
		},
		{
			name:  "lower-case keywords",
			query: "match (a) return labels(a)[0] as t limit 1",
			want:  []cypherProjection{{expr: "labels(a)[0]", alias: "t"}},
		},
		{
			name:  "items of a nested clause end where its braces close",
			query: "CALL { MATCH (a) RETURN a AS x } RETURN x LIMIT 1",
			want:  []cypherProjection{{expr: "a", alias: "x"}, {expr: "x"}},
		},
		{
			name:  "no projection clause",
			query: "MATCH (a) LIMIT 1",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, cypherProjections(maskCypherStrings(tt.query, nil)))
		})
	}
}

func TestMaskCypherStrings(t *testing.T) {
	const query = "WHERE a.n = 'x,y' AND b.n = \"it\\\"s\" RETURN a AS `a'b` LIMIT 1"
	var literals []string
	got := maskCypherStrings(query, func(start, end int) {
		literals = append(literals, query[start:end])
	})
	assert.Equal(t, "WHERE a.n = '   ' AND b.n = \"     \" RETURN a AS `a'b` LIMIT 1", got)
	assert.Equal(t, []string{"x,y", "it\\\"s"}, literals)
}

func TestStripCypherComments(t *testing.T) {
	for _, tt := range []struct {
		name  string
		query string
		want  string
	}{
		{name: "no comments", query: "MATCH (a) RETURN a LIMIT 1", want: "MATCH (a) RETURN a LIMIT 1"},
		{name: "trailing line comment", query: "RETURN 1 LIMIT 1 // Schema", want: "RETURN 1 LIMIT 1 "},
		{name: "line comment keeps the newline", query: "MATCH (a) // Schema\nRETURN a LIMIT 1", want: "MATCH (a) \nRETURN a LIMIT 1"},
		{name: "block comment", query: "MATCH (a) /* Schema */RETURN a LIMIT 1", want: "MATCH (a)  RETURN a LIMIT 1"},
		{name: "unclosed block comment", query: "RETURN 1 LIMIT 1 /* Schema", want: "RETURN 1 LIMIT 1 "},
		{name: "markers inside a single-quoted string", query: "WHERE a.url = 'http://x/*y' RETURN a", want: "WHERE a.url = 'http://x/*y' RETURN a"},
		{name: "escaped quote inside a string", query: `WHERE a.n = 'it\'s // x' RETURN a // c`, want: `WHERE a.n = 'it\'s // x' RETURN a `},
		{name: "markers inside a double-quoted string and a backtick name", query: "RETURN \"//\" AS `a//b` // c", want: "RETURN \"//\" AS `a//b` "},
	} {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, stripCypherComments(tt.query))
		})
	}
}

func TestCypherRowsTableCodecLeavesWarningsOut(t *testing.T) {
	resp := &CypherQueryResponse{
		Columns:  []string{"t"},
		Rows:     [][]json.RawMessage{{json.RawMessage(`"Schema"`)}},
		Warnings: []string{"1 of 1 row holds the graph's own catalog records"},
	}
	var out bytes.Buffer
	require.NoError(t, (&CypherRowsTableCodec{}).Encode(&out, resp))
	assert.Contains(t, out.String(), "Schema")
	assert.NotContains(t, out.String(), "catalog records", "the table is rows only; warnings go to stderr")
}
