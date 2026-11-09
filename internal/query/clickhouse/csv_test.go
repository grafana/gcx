package clickhouse_test

import (
	"bytes"
	"encoding/csv"
	"testing"

	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/query/clickhouse"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDiscoveryCSV(t *testing.T) {
	var out bytes.Buffer
	require.NoError(t, (cmdio.CSV[[]clickhouse.TableInfo]{Records: clickhouse.TableInfoCSVRecords}).Encode(&out, []clickhouse.TableInfo{{Name: "long,table"}}))
	rows, err := csv.NewReader(&out).ReadAll()
	require.NoError(t, err)
	require.Len(t, rows, 2)
	assert.Contains(t, rows[1], "long,table")
	assert.Equal(t, []string{"", ""}, rows[1][3:])
	out.Reset()
	require.NoError(t, (cmdio.CSV[[]clickhouse.ColumnInfo]{Records: clickhouse.ColumnInfoCSVRecords}).Encode(&out, []clickhouse.ColumnInfo{{Name: "column"}}))
	rows, err = csv.NewReader(&out).ReadAll()
	require.NoError(t, err)
	require.Len(t, rows, 2)
	assert.Equal(t, "column", rows[1][0])
}
