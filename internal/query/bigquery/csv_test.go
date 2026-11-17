package bigquery_test

import (
	"bytes"
	"encoding/csv"
	"testing"

	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/query/bigquery"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStringListCSV(t *testing.T) {
	var out bytes.Buffer
	value := bigquery.StringList{Header: "CATALOG", Items: []string{"one,two", "three"}}
	require.NoError(t, (cmdio.CSV[cmdio.CSVMarshaler]{Records: cmdio.CSVMarshaler.CSVRecords}).Encode(&out, value))
	rows, err := csv.NewReader(&out).ReadAll()
	require.NoError(t, err)
	assert.Equal(t, [][]string{{"catalog"}, {"one,two"}, {"three"}}, rows)
}

func TestDiscoveryCSV(t *testing.T) {
	var out bytes.Buffer
	require.NoError(t, (cmdio.CSV[[]bigquery.TableInfo]{Records: bigquery.TableInfoCSVRecords}).Encode(&out, []bigquery.TableInfo{{Name: "long,table"}}))
	rows, err := csv.NewReader(&out).ReadAll()
	require.NoError(t, err)
	require.Len(t, rows, 2)
	assert.Contains(t, rows[1], "long,table")
	out.Reset()
	require.NoError(t, (cmdio.CSV[[]bigquery.ColumnInfo]{Records: bigquery.ColumnInfoCSVRecords}).Encode(&out, []bigquery.ColumnInfo{{Name: "column"}}))
	rows, err = csv.NewReader(&out).ReadAll()
	require.NoError(t, err)
	require.Len(t, rows, 2)
	assert.Equal(t, "column", rows[1][0])
}
