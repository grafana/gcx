package sql_test

import (
	"encoding/json"
	"fmt"
	"testing"

	querysql "github.com/grafana/gcx/internal/query/sql"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseTableConstraintsPreservesCompositeConstraintOrder(t *testing.T) {
	constraints := &querysql.QueryResponse{
		Columns: []querysql.Column{
			{Name: "constraint_name"},
			{Name: "constraint_type"},
			{Name: "ordinal_position"},
			{Name: "column_name"},
			{Name: "referenced_namespace"},
			{Name: "referenced_table"},
			{Name: "referenced_column"},
			{Name: "on_delete"},
			{Name: "on_update"},
		},
		// Deliberately return the second key position first. The parser must
		// use ordinal_position for both local and referenced columns.
		Rows: [][]any{
			{"fk_orders_customer", "FOREIGN KEY", float64(2), "region_id", "crm", "customers", "region", "CASCADE", "NO ACTION"},
			{"fk_orders_customer", "FOREIGN KEY", float64(1), "customer_id", "crm", "customers", "id", "CASCADE", "NO ACTION"},
			{"orders_pkey", "PRIMARY KEY", float64(1), "id", nil, nil, nil, nil, nil},
		},
	}

	desc, err := querysql.ParseTableConstraints("sales", "orders", constraints)
	require.NoError(t, err)
	assert.Equal(t, querysql.TableIdentity{Namespace: "sales", Name: "orders"}, desc.Table)
	require.Len(t, desc.Constraints, 2)

	fk := desc.Constraints[0]
	assert.Equal(t, "fk_orders_customer", fk.Name)
	assert.Equal(t, []string{"customer_id", "region_id"}, fk.Columns)
	require.NotNil(t, fk.Referenced)
	assert.Equal(t, "crm", fk.Referenced.Namespace)
	assert.Equal(t, "customers", fk.Referenced.Name)
	assert.Equal(t, []string{"id", "region"}, fk.Referenced.Columns)
	assert.Equal(t, "CASCADE", fk.OnDelete)
	assert.Equal(t, "NO ACTION", fk.OnUpdate)

	// Pin the JSON field names because this is an agent-facing
	// contract, not an implementation detail of the Go structs.
	encoded, err := json.Marshal(desc)
	require.NoError(t, err)
	assert.JSONEq(t, `{"table":{"namespace":"sales","name":"orders"},"constraints":[{"name":"fk_orders_customer","type":"FOREIGN KEY","columns":["customer_id","region_id"],"referencedTable":{"namespace":"crm","name":"customers","columns":["id","region"]},"onDelete":"CASCADE","onUpdate":"NO ACTION"},{"name":"orders_pkey","type":"PRIMARY KEY","columns":["id"]}]}`, string(encoded))
}

func TestParseTableConstraintsRejectsMalformedConstraintResponse(t *testing.T) {
	malformed := &querysql.QueryResponse{
		Columns: []querysql.Column{{Name: "unexpected"}},
		Rows:    [][]any{{"value"}},
	}
	_, err := querysql.ParseTableConstraints("public", "orders", malformed)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `missing column "constraint_name"`)
}

func TestParseTableConstraintsAcceptsNoDeclaredConstraints(t *testing.T) {
	desc, err := querysql.ParseTableConstraints("sales", "orders", &querysql.QueryResponse{})
	require.NoError(t, err)
	assert.Empty(t, desc.Constraints)

	encoded, err := json.Marshal(desc)
	require.NoError(t, err)
	assert.Contains(t, string(encoded), `"constraints":[]`)
}

func TestParseTableConstraintsSameNameDifferentTypes(t *testing.T) {
	for _, checkFirst := range []bool{false, true} {
		t.Run(fmt.Sprintf("checkFirst=%t", checkFirst), func(t *testing.T) {
			rows := [][]any{
				{"shared_name", "UNIQUE", 1, "id", nil, nil, nil, nil, nil},
				{"shared_name", "CHECK", nil, nil, nil, nil, nil, nil, nil},
			}
			if checkFirst {
				rows[0], rows[1] = rows[1], rows[0]
			}
			resp := &querysql.QueryResponse{Rows: rows}
			for _, name := range []string{"constraint_name", "constraint_type", "ordinal_position", "column_name", "referenced_namespace", "referenced_table", "referenced_column", "on_delete", "on_update"} {
				resp.Columns = append(resp.Columns, querysql.Column{Name: name})
			}
			desc, err := querysql.ParseTableConstraints("sales", "orders", resp)
			require.NoError(t, err)
			require.Len(t, desc.Constraints, 2)
			for _, constraint := range desc.Constraints {
				if constraint.Type == "UNIQUE" {
					assert.Equal(t, []string{"id"}, constraint.Columns)
				} else {
					assert.Empty(t, constraint.Columns)
				}
				assert.Nil(t, constraint.Referenced)
			}
		})
	}
}

func TestParseTableConstraintsRejectsInvalidOrdinals(t *testing.T) {
	for _, ordinal := range []any{-1, 1.5, "invalid", ^uint64(0)} {
		t.Run(fmt.Sprint(ordinal), func(t *testing.T) {
			resp := &querysql.QueryResponse{Rows: [][]any{{"key", "UNIQUE", ordinal, "id", nil, nil, nil, nil, nil}}}
			for _, name := range []string{"constraint_name", "constraint_type", "ordinal_position", "column_name", "referenced_namespace", "referenced_table", "referenced_column", "on_delete", "on_update"} {
				resp.Columns = append(resp.Columns, querysql.Column{Name: name})
			}
			_, err := querysql.ParseTableConstraints("sales", "orders", resp)
			require.ErrorContains(t, err, "ordinal position")
		})
	}
}
