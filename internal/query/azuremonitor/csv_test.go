package azuremonitor_test

import (
	"bytes"
	"encoding/csv"
	"testing"
	"time"

	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/query/azuremonitor"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCSVRecords(t *testing.T) {
	r := &azuremonitor.TableResponse{Columns: []azuremonitor.Column{{Name: "time", Type: "time"}, {Name: "Name"}, {Name: "name"}, {Name: "nil"}, {Name: "nested"}}, Rows: [][]any{{1700000000123.0, "UPPER,\nNAME", "lower", nil, map[string]any{"a": true}}}}
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

func TestMetricCSVRecords(t *testing.T) {
	value := 2.5
	r := &azuremonitor.QueryResponse{Frames: []azuremonitor.Frame{{Name: "metric", Timestamps: []time.Time{time.UnixMilli(1700000000123), time.UnixMilli(1700000000456)}, Values: []*float64{&value, nil}}}}
	var out bytes.Buffer
	require.NoError(t, (cmdio.CSV[cmdio.CSVMarshaler]{Records: cmdio.CSVMarshaler.CSVRecords}).Encode(&out, r))
	rows, err := csv.NewReader(&out).ReadAll()
	require.NoError(t, err)
	require.Len(t, rows, 3)
	assert.Equal(t, []string{"2023-11-14T22:13:20.123Z", "2.5", "metric"}, rows[1][:3])
	assert.Empty(t, rows[2][1])
}
