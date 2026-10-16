package dataframe_test

import (
	"math"
	"testing"

	"github.com/grafana/gcx/internal/query/dataframe"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCSVRows(t *testing.T) {
	columns := []string{"time", "text", "null", "number", "nested", "bool"}
	headers, rows, err := dataframe.CSVRows(columns, [][]any{{1700000000123.0, "raw,\ntext", nil, math.Inf(1), map[string]any{"a": 1}, true}})
	require.NoError(t, err)
	assert.Equal(t, columns, headers)
	assert.Equal(t, [][]string{{"1700000000123", "raw,\ntext", "", "+Inf", `{"a":1}`, "true"}}, rows)
	_, _, err = dataframe.CSVRows([]string{"unsupported"}, [][]any{{make(chan int)}})
	require.Error(t, err)
}
