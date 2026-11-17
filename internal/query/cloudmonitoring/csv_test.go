package cloudmonitoring_test

import (
	"bytes"
	"encoding/csv"
	"testing"
	"time"

	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/query/cloudmonitoring"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMetricCSVRecords(t *testing.T) {
	value := 2.5
	r := &cloudmonitoring.QueryResponse{Frames: []cloudmonitoring.Frame{{Name: "metric", Labels: map[string]string{"region": "west"}, Timestamps: []time.Time{time.UnixMilli(1700000000123), time.Unix(1700000001, 0)}, Values: []*float64{&value, nil}}}}
	var out bytes.Buffer
	require.NoError(t, (cmdio.CSV[cmdio.CSVMarshaler]{Records: cmdio.CSVMarshaler.CSVRecords}).Encode(&out, r))
	rows, err := csv.NewReader(&out).ReadAll()
	require.NoError(t, err)
	assert.Equal(t, [][]string{
		{"timestamp_rfc3339", "value", "series", "label.region"},
		{"2023-11-14T22:13:20.123000000Z", "2.5", "metric", "west"},
		{"2023-11-14T22:13:21.000000000Z", "", "metric", "west"},
	}, rows)
}
