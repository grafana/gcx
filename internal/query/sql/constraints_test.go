package sql_test

import (
	"encoding/json"
	"testing"

	querysql "github.com/grafana/gcx/internal/query/sql"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseTableDescriptionPreservesCompositeConstraintOrder(t *testing.T) {
	columns := &querysql.QueryResponse{
		Columns: []querysql.Column{
			{Name: "name"}, {Name: "type"}, {Name: "nullable"}, {Name: "default"},
		},
		Rows: [][]any{
			{"id", "bigint", "NO", nil},
			{"tenant_id", "bigint", "NO", "0"},
		},
	}
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

	desc, err := querysql.ParseTableDescription("sales", "orders", columns, constraints)
	require.NoError(t, err)
	assert.Equal(t, querysql.TableIdentity{Namespace: "sales", Name: "orders"}, desc.Table)
	require.Len(t, desc.Columns, 2)
	assert.Equal(t, "tenant_id", desc.Columns[1].Name)
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

	// Pin the JSON field names because this is an agent-facing opt-in
	// contract, not an implementation detail of the Go structs.
	encoded, err := json.Marshal(desc)
	require.NoError(t, err)
	assert.JSONEq(t, `{"table":{"namespace":"sales","name":"orders"},"columns":[{"name":"id","type":"bigint","nullable":"NO","default":null},{"name":"tenant_id","type":"bigint","nullable":"NO","default":"0"}],"constraints":[{"name":"fk_orders_customer","type":"FOREIGN KEY","columns":["customer_id","region_id"],"referencedTable":{"namespace":"crm","name":"customers","columns":["id","region"]},"onDelete":"CASCADE","onUpdate":"NO ACTION"},{"name":"orders_pkey","type":"PRIMARY KEY","columns":["id"]}]}`, string(encoded))
}

func TestParseTableDescriptionRejectsMalformedConstraintResponse(t *testing.T) {
	columns := &querysql.QueryResponse{Columns: []querysql.Column{{Name: "name"}, {Name: "type"}, {Name: "nullable"}, {Name: "default"}}, Rows: [][]any{{"id", "integer", "NO", nil}}}
	_, err := querysql.ParseTableDescription("public", "orders", columns, &querysql.QueryResponse{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), `missing column "constraint_name"`)
}
