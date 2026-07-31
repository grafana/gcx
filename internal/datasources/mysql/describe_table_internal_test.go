package mysql

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSplitTableArg(t *testing.T) {
	tests := []struct {
		name         string
		arg          string
		databaseFlag string
		wantDatabase string
		wantTable    string
		wantErr      string
	}{
		{name: "bare table", arg: "orders", wantDatabase: "", wantTable: "orders"},
		{name: "bare table with database flag", arg: "orders", databaseFlag: "mydb", wantDatabase: "mydb", wantTable: "orders"},
		{name: "qualified table", arg: "mydb.orders", wantDatabase: "mydb", wantTable: "orders"},
		{name: "qualified table with matching flag", arg: "mydb.orders", databaseFlag: "mydb", wantDatabase: "mydb", wantTable: "orders"},
		{name: "qualified table with conflicting flag", arg: "archive.orders", databaseFlag: "mydb", wantErr: "conflicts with --database"},
		{name: "too many dots", arg: "a.b.c", wantErr: "use TABLE or DATABASE.TABLE"},
		{name: "empty database part", arg: ".orders", wantErr: "use TABLE or DATABASE.TABLE"},
		{name: "empty table part", arg: "mydb.", wantErr: "use TABLE or DATABASE.TABLE"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			database, table, err := splitTableArg(tt.arg, tt.databaseFlag)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantDatabase, database)
			assert.Equal(t, tt.wantTable, table)
		})
	}
}
