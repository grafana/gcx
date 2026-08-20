package postgres

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSplitTableArg(t *testing.T) {
	tests := []struct {
		name       string
		arg        string
		schemaFlag string
		wantSchema string
		wantTable  string
		wantErr    string
	}{
		{name: "bare table", arg: "orders", wantSchema: "", wantTable: "orders"},
		{name: "bare table with schema flag", arg: "orders", schemaFlag: "public", wantSchema: "public", wantTable: "orders"},
		{name: "qualified table", arg: "public.orders", wantSchema: "public", wantTable: "orders"},
		{name: "qualified table with matching flag", arg: "public.orders", schemaFlag: "public", wantSchema: "public", wantTable: "orders"},
		{name: "qualified table with conflicting flag", arg: "archive.orders", schemaFlag: "public", wantErr: "conflicts with --schema"},
		{name: "too many dots", arg: "db.public.orders", wantErr: "use TABLE or SCHEMA.TABLE"},
		{name: "empty schema part", arg: ".orders", wantErr: "use TABLE or SCHEMA.TABLE"},
		{name: "empty table part", arg: "public.", wantErr: "use TABLE or SCHEMA.TABLE"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			schema, table, err := splitTableArg(tt.arg, tt.schemaFlag)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantSchema, schema)
			assert.Equal(t, tt.wantTable, table)
		})
	}
}
