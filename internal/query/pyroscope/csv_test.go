package pyroscope_test

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"strconv"
	"testing"

	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/query/pyroscope"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCSVExportsAllFunctions(t *testing.T) {
	fg := &pyroscope.Flamegraph{Total: 100}
	for i := range 25 {
		fg.Names = append(fg.Names, fmt.Sprintf("long.function.name.%d", i))
		fg.Levels = append(fg.Levels, pyroscope.Level{Values: []string{"0", "1", "1", strconv.Itoa(i)}})
	}
	var out bytes.Buffer
	require.NoError(t, (cmdio.CSV[cmdio.CSVMarshaler]{Records: cmdio.CSVMarshaler.CSVRecords}).Encode(&out, &pyroscope.QueryResponse{Flamegraph: fg}))
	rows, err := csv.NewReader(&out).ReadAll()
	require.NoError(t, err)
	require.Len(t, rows, 26)
	assert.Equal(t, []string{"function", "self", "total", "self_percentage"}, rows[0])
	assert.Equal(t, []string{"long.function.name.0", "1", "1", "1"}, rows[1])
}
