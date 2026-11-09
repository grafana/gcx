package athena_test

import (
	"bytes"
	"encoding/csv"
	"testing"

	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/query/athena"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStringListCSV(t *testing.T) {
	var out bytes.Buffer
	value := athena.StringList{Header: "CATALOG", Items: []string{"one,two", "three"}}
	require.NoError(t, (cmdio.CSV[cmdio.CSVMarshaler]{Records: cmdio.CSVMarshaler.CSVRecords}).Encode(&out, value))
	rows, err := csv.NewReader(&out).ReadAll()
	require.NoError(t, err)
	assert.Equal(t, [][]string{{"catalog"}, {"one,two"}, {"three"}}, rows)
}
