package kg

import (
	"bytes"
	"encoding/json"
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
				"WHERE labels(n)[0] <> 'EntityType' AND labels(n)[0] <> 'Schema' on an unlabeled node n",
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
				"WHERE labels(n)[0] <> 'Account' AND labels(n)[0] <> 'EntityType' on",
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
			want:    []string{"1 of 2 rows holds", "(label Account)", "WHERE labels(n)[0] <> 'Account' on"},
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
			want:    []string{"1 of 2 rows", "(label EntityType)", "WHERE labels(n)[0] <> 'EntityType' on"},
			wantNot: []string{"Schema"},
		},
		{
			name:    "rows that carry only a named catalog label are not counted",
			query:   "MATCH (a)-[r:TYPE_A]->(b) WHERE a.name <> 'Env' RETURN labels(a)[0] AS t, count(r) AS n LIMIT 10",
			columns: []string{"t", "n"},
			rows:    [][]string{{`"Schema"`, `1`}, {`"Env"`, `1`}, {`"TypeB"`, `2`}},
			want:    []string{"1 of 3 rows", "(label Schema)"},
			wantNot: []string{"Env"},
		},
		{
			name:    "an alias equal to a catalog label does not name it",
			query:   "MATCH (n) RETURN labels(n)[0] AS EntityType, count(n) AS c ORDER BY EntityType LIMIT 50",
			columns: []string{"EntityType", "c"},
			rows:    [][]string{{`"EntityType"`, `2`}, {`"Env"`, `3`}, {`"Schema"`, `4`}, {`"TypeB"`, `5`}},
			want: []string{
				"3 of 4 rows", "(labels EntityType, Env, Schema)",
				"WHERE labels(n)[0] <> 'EntityType' AND labels(n)[0] <> 'Env' AND labels(n)[0] <> 'Schema' on",
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
			want:    []string{"1 of 1 row holds", "(label Schema)", "WHERE labels(n)[0] <> 'Schema' on"},
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
			assert.Equal(t, tt.want, cypherRefersTo(tt.expr, marked))
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
	} {
		t.Run(tt.name, func(t *testing.T) {
			got := []string{}
			for label := range cypherNamedLabels(tt.query) {
				got = append(got, label)
			}
			assert.ElementsMatch(t, tt.want, got)
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
	var literals []string
	got := maskCypherStrings("WHERE a.n = 'x,y' AND b.n = \"it\\\"s\" RETURN a AS `a'b` LIMIT 1", func(literal string) {
		literals = append(literals, literal)
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
