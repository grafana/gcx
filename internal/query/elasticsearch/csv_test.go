package elasticsearch_test

import (
	"bytes"
	"encoding/csv"
	"testing"
	"time"

	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/query/elasticsearch"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMetricCSVRecords(t *testing.T) {
	value := 2.5
	r := &elasticsearch.MetricsResponse{Series: []elasticsearch.MetricSeries{{Name: "metric", Timestamps: []time.Time{time.UnixMilli(1700000000123), time.UnixMilli(1700000000456)}, Values: []*float64{&value, nil}}}}
	var out bytes.Buffer
	require.NoError(t, (cmdio.CSV[cmdio.CSVMarshaler]{Records: cmdio.CSVMarshaler.CSVRecords}).Encode(&out, r))
	rows, err := csv.NewReader(&out).ReadAll()
	require.NoError(t, err)
	require.Len(t, rows, 3)
	assert.Equal(t, []string{"2023-11-14T22:13:20.123Z", "2.5", "metric"}, rows[1][:3])
	assert.Empty(t, rows[2][1])
}
