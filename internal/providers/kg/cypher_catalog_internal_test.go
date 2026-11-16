package kg

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCatalogRowsWarning(t *testing.T) {
	const schemaNode = `{"kind":"node","ref":"n0","labels":["Schema"],"properties":{"name":"svc-type"}}`
	const serviceNode = `{"kind":"node","ref":"n1","labels":["Service"],"properties":{"name":"svc-a"}}`
	for _, tt := range []struct {
		name    string
		query   string
		columns []string
		rows    [][]string
		want    []string // substrings of the warning; nil means no warning
		wantNot []string // strings the warning must not contain
	}{
		{
			name:    "aliased label projections of a catalog group",
			query:   "MATCH (a)-[r:TYPE_A]-(b) RETURN labels(a)[0] AS from_type, labels(b)[0] AS to_type, count(r) AS n LIMIT 10",
			columns: []string{"from_type", "to_type", "n"},
			rows:    [][]string{{`"Schema"`, `"Schema"`, `1`}, {`"Service"`, `"Pod"`, `5`}, {`"EntityType"`, `"EntityType"`, `1`}},
			want: []string{
				"2 of 3 rows hold the graph's own catalog records (labels EntityType, Schema)",
				"WHERE labels(n)[0] <> 'EntityType' AND labels(n)[0] <> 'Schema' on an unlabeled node n",
				"the relationship types between them and its env values, not observed entities",
			},
		},
		{
			name:    "labels of startNode and endNode",
			query:   "MATCH ()-[r]->() RETURN labels(startNode(r))[0] AS from_type, type(r) AS rel, labels(endNode(r))[0] AS to_type, count(r) AS n LIMIT 50",
			columns: []string{"from_type", "rel", "to_type", "n"},
			rows:    [][]string{{`"Schema"`, `"TYPE_A"`, `"Schema"`, `2`}},
			want:    []string{"1 of 1 rows", "(label Schema)", "WHERE labels(n)[0] <> 'Schema' on"},
		},
		{
			name:    "unaliased labels list column",
			query:   "MATCH (a)-[r:TYPE_A]->(b) RETURN labels(a), a.name, labels(b), b.name LIMIT 10",
			columns: []string{"labels(a)", "a.name", "labels(b)", "b.name"},
			rows:    [][]string{{`["Schema"]`, `"svc-type"`, `["Schema"]`, `"pod-type"`}},
			want:    []string{"1 of 1 rows", "(label Schema)"},
		},
		{
			name:    "head of labels with a backtick alias",
			query:   "MATCH (a) RETURN head(labels(a)) AS `node type`, count(a) AS n LIMIT 10",
			columns: []string{"node type", "n"},
			rows:    [][]string{{`"Env"`, `1`}, {`"Pod"`, `40`}},
			want:    []string{"1 of 2 rows", "(label Env)"},
		},
		{
			name:    "unaliased head of labels column",
			query:   "MATCH (a) RETURN head(labels(a)), count(a) LIMIT 10",
			columns: []string{"head(labels(a))", "count(a)"},
			rows:    [][]string{{`"Schema"`, `1`}, {`"Pod"`, `4`}},
			want:    []string{"1 of 2 rows", "(label Schema)"},
		},
		{
			name:    "aliased labels of a doubly nested call",
			query:   "MATCH p = (a)-[r]->(b) RETURN DISTINCT labels(last(nodes(p)))[0] AS t LIMIT 50",
			columns: []string{"t"},
			rows:    [][]string{{`"Schema"`}, {`"Pod"`}},
			want:    []string{"1 of 2 rows", "(label Schema)"},
		},
		{
			name:    "aliased labels list of a doubly nested call",
			query:   "MATCH p = (a)-[r]->(b) RETURN labels(head(nodes(p))) AS t, count(r) AS n LIMIT 50",
			columns: []string{"t", "n"},
			rows:    [][]string{{`["EntityType"]`, `2`}},
			want:    []string{"1 of 1 rows", "(label EntityType)"},
		},
		{
			name:    "returned nodes",
			query:   "MATCH (a)-[r:TYPE_A]->(b) RETURN a, r, b LIMIT 3",
			columns: []string{"a", "r", "b"},
			rows:    [][]string{{schemaNode, `{"kind":"relationship","ref":"e0","type":"TYPE_A","startRef":"n0","endRef":"n1"}`, schemaNode}, {serviceNode, `{"kind":"relationship"}`, serviceNode}},
			want:    []string{"1 of 2 rows", "(label Schema)"},
		},
		{
			name:    "nodes inside a path",
			query:   "MATCH p = (a)-[:TYPE_A]->(b) RETURN p LIMIT 1",
			columns: []string{"p"},
			rows:    [][]string{{`{"kind":"path","nodes":[` + schemaNode + `],"relationships":[]}`}},
			want:    []string{"1 of 1 rows", "(label Schema)"},
		},
		{
			name:    "observed entities only",
			query:   "MATCH (a)-[r:TYPE_A]-(b) RETURN labels(a)[0] AS from_type, labels(b)[0] AS to_type, count(r) AS n LIMIT 10",
			columns: []string{"from_type", "to_type", "n"},
			rows:    [][]string{{`"Service"`, `"Pod"`, `5`}},
		},
		{
			name:    "a property value equal to a catalog label is not a label column",
			query:   "MATCH (s:Service) RETURN s.name AS name, labels(s)[0] AS type LIMIT 10",
			columns: []string{"name", "type"},
			rows:    [][]string{{`"Schema"`, `"Service"`}},
		},
		{
			name:    "a node property named labels is not a node",
			query:   "MATCH (s:Service) RETURN properties(s) AS props LIMIT 1",
			columns: []string{"props"},
			rows:    [][]string{{`{"labels":["Schema"],"name":"svc-a"}`}},
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
			rows:    [][]string{{`"Service"`}},
		},
		{
			name:    "a query that filters one catalog label out still gets a warning for the others",
			query:   "MATCH (a)-[r]->(b) WHERE labels(a)[0] <> 'Schema' RETURN labels(a)[0] AS f, type(r) AS t, labels(b)[0] AS to, count(r) AS n LIMIT 100",
			columns: []string{"f", "t", "to", "n"},
			rows:    [][]string{{`"EntityType"`, `"TYPE_A"`, `"EntityType"`, `4`}, {`"Service"`, `"TYPE_B"`, `"Pod"`, `5`}},
			want:    []string{"1 of 2 rows", "(label EntityType)", "WHERE labels(n)[0] <> 'EntityType' on"},
			wantNot: []string{"Schema"},
		},
		{
			name:    "rows that carry only a named catalog label are not counted",
			query:   "MATCH (a)-[r:TYPE_A]->(b) WHERE a.name <> 'Env' RETURN labels(a)[0] AS t, count(r) AS n LIMIT 10",
			columns: []string{"t", "n"},
			rows:    [][]string{{`"Schema"`, `1`}, {`"Env"`, `1`}, {`"Pod"`, `2`}},
			want:    []string{"1 of 3 rows", "(label Schema)"},
			wantNot: []string{"Env"},
		},
		{
			name:    "an alias equal to a catalog label does not name it",
			query:   "MATCH (n) RETURN labels(n)[0] AS EntityType, count(n) AS c ORDER BY EntityType LIMIT 50",
			columns: []string{"EntityType", "c"},
			rows:    [][]string{{`"EntityType"`, `11`}, {`"Env"`, `1`}, {`"Schema"`, `37`}, {`"Pod"`, `9`}},
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
			query:   "MATCH (a:Service|Schema)-[r]->(b) WHERE NOT b:EntityType RETURN labels(a)[0] AS t, labels(b)[0] AS u LIMIT 10",
			columns: []string{"t", "u"},
			rows:    [][]string{{`"Schema"`, `"EntityType"`}, {`"Service"`, `"Env"`}},
			want:    []string{"1 of 2 rows", "(label Env)"},
			wantNot: []string{"Schema", "EntityType"},
		},
		{
			name:    "a catalog label in a line comment does not name it",
			query:   "MATCH (a)-[r:TYPE_A]->(b) RETURN labels(a)[0] AS t, count(r) AS n LIMIT 10 // EntityType",
			columns: []string{"t", "n"},
			rows:    [][]string{{`"EntityType"`, `1`}},
			want:    []string{"1 of 1 rows", "(label EntityType)"},
		},
		{
			name:    "a catalog label in a block comment does not name it",
			query:   "MATCH (a)-[r:TYPE_A]->(b) /* not Schema */ RETURN labels(a)[0] AS t, count(r) AS n LIMIT 10",
			columns: []string{"t", "n"},
			rows:    [][]string{{`"Schema"`, `1`}},
			want:    []string{"1 of 1 rows", "(label Schema)"},
		},
		{
			name:    "no rows",
			query:   "MATCH (a)-[r]->(b) RETURN labels(a)[0] AS t LIMIT 10",
			columns: []string{"t"},
		},
	} {
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

func TestCypherNamedLabels(t *testing.T) {
	for _, tt := range []struct {
		name  string
		query string
		want  []string
	}{
		{name: "node label", query: "MATCH (a:Schema) RETURN a LIMIT 1", want: []string{"Schema"}},
		{name: "backtick label with spaces", query: "MATCH (a: `Env`) RETURN a LIMIT 1", want: []string{"Env"}},
		{name: "label expression", query: "MATCH (a:Service|EntityType&Schema) RETURN a LIMIT 1", want: []string{"EntityType", "Schema"}},
		{name: "chained labels", query: "MATCH (a:Service:Env) RETURN a LIMIT 1", want: []string{"Env"}},
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
		Warnings: []string{"1 of 1 rows hold the graph's own catalog records"},
	}
	var out bytes.Buffer
	require.NoError(t, (&CypherRowsTableCodec{}).Encode(&out, resp))
	assert.Contains(t, out.String(), "Schema")
	assert.NotContains(t, out.String(), "catalog records", "the table is rows only; warnings go to stderr")
}
