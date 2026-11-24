package kg

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
)

const (
	schemaNode = `{"kind":"node","ref":"n0","labels":["Schema"],"properties":{"name":"name-type"}}`
	entityNode = `{"kind":"node","ref":"n1","labels":["TypeA"],"properties":{"name":"name-a"}}`
)

func TestCatalogRowsWarning(t *testing.T) {
	tests := []struct {
		name    string
		query   string
		rows    [][]string
		want    []string // substrings of the warning; nil means no warning
		wantNot []string
	}{
		{
			name:  "label columns of catalog groups",
			query: "MATCH (a)-[r:TYPE_A]-(b) RETURN labels(a)[0] AS from_type, labels(b)[0] AS to_type, count(r) AS n LIMIT 10",
			rows:  [][]string{{`"Schema"`, `"Schema"`, `1`}, {`"TypeA"`, `"TypeB"`, `5`}, {`"EntityType"`, `"EntityType"`, `1`}},
			want: []string{
				"2 of 3 rows hold the graph's own catalog records (labels EntityType, Schema), not observed entities; a node pattern without a label matches them too",
				"A label on each node leaves them out.",
				"WITH n, labels(n)[0] AS type WHERE type <> 'EntityType' AND type <> 'Schema', and list any other variable",
				"a WHERE on labels(n) right after MATCH can time out on a large graph.",
			},
		},
		{
			name:  "account records are catalog records",
			query: "MATCH (a) RETURN labels(a)[0] AS t, count(a) AS n ORDER BY t LIMIT 100",
			rows:  [][]string{{`"Account"`, `1`}, {`"TypeB"`, `4`}},
			want:  []string{"1 of 2 rows holds", "(label Account)", "WHERE type <> 'Account', and"},
		},
		{
			name:  "returned nodes",
			query: "MATCH (a)-[r:TYPE_A]->(b) RETURN a, r, b LIMIT 3",
			rows:  [][]string{{schemaNode, `{"kind":"relationship","ref":"e0","type":"TYPE_A","startRef":"n0","endRef":"n1"}`, schemaNode}, {entityNode, `{"kind":"relationship"}`, entityNode}},
			want:  []string{"1 of 2 rows holds", "(label Schema)"},
		},
		{
			name:  "nodes inside a path",
			query: "MATCH p = (a)-[:TYPE_A]->(b) RETURN p LIMIT 1",
			rows:  [][]string{{`{"kind":"path","nodes":[` + schemaNode + `],"relationships":[]}`}},
			want:  []string{"1 of 1 row holds", "(label Schema)"},
		},
		{
			name:  "labels under a map key",
			query: "MATCH (n) RETURN {t: labels(n)[0]} AS m LIMIT 5",
			rows:  [][]string{{`{"t":"Env"}`}},
			want:  []string{"(label Env)"},
		},
		{
			name:  "observed entities only",
			query: "MATCH (a)-[r:TYPE_A]-(b) RETURN labels(a)[0] AS from_type, labels(b)[0] AS to_type LIMIT 10",
			rows:  [][]string{{`"TypeA"`, `"TypeB"`}},
		},
		{
			name:  "a property value equal to a catalog label in a query without labels()",
			query: "MATCH (s:TypeA) RETURN s.name AS name LIMIT 10",
			rows:  [][]string{{`"Schema"`}},
		},
		{
			name:  "node properties are not labels",
			query: "MATCH (s) RETURN labels(s)[0] AS t, s LIMIT 1",
			rows:  [][]string{{`"TypeA"`, `{"kind":"node","labels":["TypeA"],"properties":{"name":"Schema"}}`}},
		},
		{
			name:  "a node pattern label names it",
			query: "MATCH (a:Schema)-[r]->(b) RETURN labels(a)[0] AS t, count(r) AS n LIMIT 10",
			rows:  [][]string{{`"Schema"`, `3`}},
		},
		{
			name:  "a label expression names each label",
			query: "MATCH (a:TypeA|Schema) RETURN a LIMIT 10",
			rows:  [][]string{{schemaNode}},
		},
		{
			name:    "a filter on one catalog label still warns about the others",
			query:   "MATCH (n) WITH n, labels(n)[0] AS type WHERE type <> 'Schema' RETURN type, count(n) AS c LIMIT 100",
			rows:    [][]string{{`"EntityType"`, `2`}, {`"TypeA"`, `4`}},
			want:    []string{"1 of 2 rows holds", "(label EntityType)"},
			wantNot: []string{"Schema"},
		},
		{
			name:  "an alias equal to a catalog label does not name it",
			query: "MATCH (a) RETURN labels(a)[0] AS EntityType LIMIT 10",
			rows:  [][]string{{`"EntityType"`}},
			want:  []string{"(label EntityType)"},
		},
		{
			name:  "no rows",
			query: "MATCH (a) RETURN labels(a)[0] AS t LIMIT 1",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := &CypherQueryResponse{}
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
