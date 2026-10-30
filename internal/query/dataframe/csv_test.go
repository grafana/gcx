package dataframe_test

import (
	"math"
	"testing"
	"time"

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

func TestCSVSeriesRecords(t *testing.T) {
	value := 1.25
	timestamp := time.Unix(1700000000, 123456789)
	columns, rows, err := dataframe.CSVSeriesRecords([]dataframe.CSVSeries{
		{Name: "one", Unit: "s", Labels: map[string]string{"value": "label", "Name": "upper"}, Timestamps: []time.Time{time.Unix(1700000000, 0).In(time.FixedZone("UTC+1", 3600)), timestamp}, Values: []*float64{&value, nil}},
		{Name: "two", Labels: map[string]string{"name": "lower"}, Timestamps: []time.Time{time.UnixMilli(1700000000123)}, Values: []*float64{&value}},
	}, true)
	require.NoError(t, err)
	assert.Equal(t, []string{"timestamp_rfc3339", "value", "series", "unit", "label.Name", "label.name", "label.value"}, columns)
	assert.Equal(t, [][]string{
		{"2023-11-14T22:13:20.000000000Z", "1.25", "one", "s", "upper", "", "label"},
		{"2023-11-14T22:13:20.123456789Z", "", "one", "s", "upper", "", "label"},
		{"2023-11-14T22:13:20.123000000Z", "1.25", "two", "", "", "lower", ""},
	}, rows)
	_, _, err = dataframe.CSVSeriesRecords([]dataframe.CSVSeries{{Timestamps: []time.Time{timestamp}}}, false)
	require.Error(t, err)
}
