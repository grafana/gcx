package bigquery_test

import (
	"bytes"
	"testing"

	"github.com/grafana/gcx/internal/query/bigquery"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFormatStringList(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, bigquery.FormatStringList(&buf, []string{"GCS", "grafana_example_dataset"}, "DATASET"))
	out := buf.String()
	assert.Contains(t, out, "DATASET")
	assert.Contains(t, out, "GCS")
	assert.Contains(t, out, "grafana_example_dataset")
}

func TestFormatStringList_Empty(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, bigquery.FormatStringList(&buf, nil, "DATASET"))
	assert.Contains(t, buf.String(), "No data")
}

func TestFormatListTablesTable(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, bigquery.FormatListTablesTable(&buf, []bigquery.TableInfo{{Name: "WORLD_DATA", Type: "BASE TABLE"}}))
	out := buf.String()
	assert.Contains(t, out, "NAME")
	assert.Contains(t, out, "TYPE")
	assert.Contains(t, out, "WORLD_DATA")
	assert.Contains(t, out, "BASE TABLE")
}

func TestFormatListTablesTable_Empty(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, bigquery.FormatListTablesTable(&buf, nil))
	assert.Contains(t, buf.String(), "No data")
}

func TestFormatDescribeTableTable(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, bigquery.FormatDescribeTableTable(&buf, []bigquery.ColumnInfo{
		{Name: "birth_rate", Type: "INT64", Nullable: "YES"},
	}))
	out := buf.String()
	assert.Contains(t, out, "NAME")
	assert.Contains(t, out, "TYPE")
	assert.Contains(t, out, "NULLABLE")
	assert.Contains(t, out, "birth_rate")
	assert.Contains(t, out, "INT64")
}

func TestFormatDescribeTableTable_Empty(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, bigquery.FormatDescribeTableTable(&buf, nil))
	assert.Contains(t, buf.String(), "No data")
}
