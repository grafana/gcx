package graph

import (
	"errors"
	"time"

	"github.com/grafana/gcx/internal/query/influxdb"
)

// FromInfluxDBResponse converts an InfluxDB query response to ChartData.
// It looks for the first time column (millisecond-epoch) as the X axis and
// creates one series per remaining numeric column.
func FromInfluxDBResponse(resp *influxdb.QueryResponse) (*ChartData, error) {
	if resp == nil || len(resp.Rows) == 0 {
		return &ChartData{}, nil
	}

	// Find the first time column.
	timeColIdx := -1
	for idx := range resp.TimeColumns {
		if timeColIdx < 0 || idx < timeColIdx {
			timeColIdx = idx
		}
	}
	if timeColIdx < 0 {
		return nil, errors.New("graph output requires a time column; use -o table for tabular results")
	}

	data := &ChartData{
		Series: make([]Series, 0),
	}

	for colIdx, colName := range resp.Columns {
		if resp.TimeColumns[colIdx] {
			continue
		}

		series := Series{
			Name:   colName,
			Points: make([]Point, 0, len(resp.Rows)),
		}

		for _, row := range resp.Rows {
			if timeColIdx >= len(row) || colIdx >= len(row) {
				continue
			}

			var t time.Time
			switch v := row[timeColIdx].(type) {
			case float64:
				t = time.UnixMilli(int64(v)).UTC()
			case int64:
				t = time.UnixMilli(v).UTC()
			default:
				continue
			}

			var value float64
			switch v := row[colIdx].(type) {
			case float64:
				value = v
			case int64:
				value = float64(v)
			default:
				continue
			}

			series.Points = append(series.Points, Point{Time: t, Value: value})
		}

		if len(series.Points) > 0 {
			data.Series = append(data.Series, series)
		}
	}

	return data, nil
}
