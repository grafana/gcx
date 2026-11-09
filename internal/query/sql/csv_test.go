package sql_test

import (
	"bytes"
	"encoding/csv"
	"testing"

	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/query/sql"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCSVRecords(t *testing.T) {
	r := &sql.QueryResponse{Columns: []sql.Column{{Name: "time", Type: "time"}, {Name: "Name"}, {Name: "name"}, {Name: "nil"}, {Name: "nested"}}, Rows: [][]any{{1700000000123.0, "UPPER,\nNAME", "lower", nil, map[string]any{"a": true}}}}
	var out bytes.Buffer
	require.NoError(t, (cmdio.CSV[cmdio.CSVMarshaler]{Records: cmdio.CSVMarshaler.CSVRecords}).Encode(&out, r))
	rows, err := csv.NewReader(&out).ReadAll()
	require.NoError(t, err)
	assert.Equal(t, [][]string{{"time", "Name", "name", "nil", "nested"}, {"1700000000123", "UPPER,\nNAME", "lower", "", `{"a":true}`}}, rows)
	r.Rows = nil
	columns, rows, err := r.CSVRecords()
	require.NoError(t, err)
	assert.Len(t, columns, 5)
	assert.Empty(t, rows)
}
