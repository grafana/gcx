package influxdb_test

import (
	"testing"
	"time"

	"github.com/grafana/gcx/internal/query/influxdb"
	"github.com/stretchr/testify/assert"
)

func TestQueryRequest_IsRange(t *testing.T) {
	tests := []struct {
		name string
		req  influxdb.QueryRequest
		want bool
	}{
		{
			name: "zero Start and End returns false",
			req:  influxdb.QueryRequest{Query: "SELECT * FROM cpu"},
			want: false,
		},
		{
			name: "non-zero Start and End returns true",
			req: influxdb.QueryRequest{
				Query: "SELECT * FROM cpu",
				Start: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
				End:   time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC),
			},
			want: true,
		},
		{
			name: "only Start set returns false",
			req: influxdb.QueryRequest{
				Query: "SELECT * FROM cpu",
				Start: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
			},
			want: false,
		},
		{
			name: "only End set returns false",
			req: influxdb.QueryRequest{
				Query: "SELECT * FROM cpu",
				End:   time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC),
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.req.IsRange()
			assert.Equal(t, tt.want, got)
		})
	}
}
