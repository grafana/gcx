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
	r := &azuremonitor.QueryResponse{Frames: []azuremonitor.Frame{{Name: "metric", Unit: "Percent", Labels: map[string]string{"region": "west"}, Timestamps: []time.Time{time.UnixMilli(1700000000123), time.Unix(1700000001, 0)}, Values: []*float64{&value, nil}}}}
	var out bytes.Buffer
	require.NoError(t, (cmdio.CSV[cmdio.CSVMarshaler]{Records: cmdio.CSVMarshaler.CSVRecords}).Encode(&out, r))
	rows, err := csv.NewReader(&out).ReadAll()
	require.NoError(t, err)
	assert.Equal(t, [][]string{
		{"timestamp_rfc3339", "value", "series", "unit", "label.region"},
		{"2023-11-14T22:13:20.123000000Z", "2.5", "metric", "Percent", "west"},
		{"2023-11-14T22:13:21.000000000Z", "", "metric", "Percent", "west"},
	}, rows)
}
