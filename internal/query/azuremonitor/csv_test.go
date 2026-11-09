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
	var frame testFrame
	frame.Schema.Fields = []testField{
		{Name: "time", Type: "time"},
		{Name: "Name", Type: "string"},
		{Name: "name", Type: "string"},
		{Name: "nil", Type: "string"},
		{Name: "nested", Type: "other"},
	}
	frame.Data.Values = []any{
		[]any{1700000000123, 1700000000987, 1700000001000},
		[]any{"UPPER,\nNAME", "second", "whole second"},
		[]any{"lower", "lower", "lower"},
		[]any{nil, nil, nil},
		[]any{map[string]any{"a": true}, nil, nil},
	}
	body := queryResultBody(t, testResultEntry{Frames: []testFrame{frame}})
	r, err := azuremonitor.ParseTableResponse(body, "logs")
	require.NoError(t, err)
	var out bytes.Buffer
	require.NoError(t, (cmdio.CSV[cmdio.CSVMarshaler]{Records: cmdio.CSVMarshaler.CSVRecords}).Encode(&out, r))
	rows, err := csv.NewReader(&out).ReadAll()
	require.NoError(t, err)
	assert.Equal(t, [][]string{
		{"time", "Name", "name", "nil", "nested"},
		{"2023-11-14T22:13:20.123Z", "UPPER,\nNAME", "lower", "", `{"a":true}`},
		{"2023-11-14T22:13:20.987Z", "second", "lower", "", ""},
		{"2023-11-14T22:13:21Z", "whole second", "lower", "", ""},
	}, rows)
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
