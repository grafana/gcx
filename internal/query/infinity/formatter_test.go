package infinity_test

import (
	"bytes"
	"testing"
	"time"

	"github.com/grafana/gcx/internal/arrowtable"
	"github.com/grafana/gcx/internal/query/infinity"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFormatTable(t *testing.T) {
	tests := []struct {
		name       string
		resp       *infinity.QueryResponse
		wantSubstr []string
		wantExact  string
	}{
		{
			name: "empty response prints no data",
			resp: &infinity.QueryResponse{
				Columns: nil,
				Rows:    nil,
			},
			wantExact: "No data\n",
		},
		{
			name: "single column response renders table with header",
			resp: &infinity.QueryResponse{
				Columns: []infinity.Column{
					{Name: "name", Type: "string"},
				},
				Rows: [][]any{
					{"Alice"},
					{"Bob"},
				},
			},
			wantSubstr: []string{"NAME", "Alice", "Bob"},
		},
		{
			name: "multi-column response renders all columns and rows",
			resp: &infinity.QueryResponse{
				Columns: []infinity.Column{
					{Name: "host", Type: "string"},
					{Name: "status", Type: "number"},
					{Name: "message", Type: "string"},
				},
				Rows: [][]any{
					{"server-1", float64(200), "ok"},
					{"server-2", float64(500), "error"},
				},
			},
			wantSubstr: []string{
				"HOST", "STATUS", "MESSAGE",
				"server-1", "200", "ok",
				"server-2", "500", "error",
			},
		},
		{
			name: "values with various types render correctly",
			resp: &infinity.QueryResponse{
				Columns: []infinity.Column{
					{Name: "label", Type: "string"},
					{Name: "count", Type: "number"},
					{Name: "note", Type: "string"},
				},
				Rows: [][]any{
					{"active", float64(42.5), nil},
					{"idle", float64(0), "no activity"},
				},
			},
			wantSubstr: []string{
				"LABEL", "COUNT", "NOTE",
				"active", "42.5",
				"idle", "0", "no activity",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			err := infinity.FormatTable(&buf, tt.resp)
			require.NoError(t, err)

			out := buf.String()

			if tt.wantExact != "" {
				assert.Equal(t, tt.wantExact, out)
				return
			}

			for _, substr := range tt.wantSubstr {
				assert.Contains(t, out, substr,
					"expected output to contain %q", substr)
			}
		})
	}
}

func TestFormatArrow(t *testing.T) {
	resp := &infinity.QueryResponse{
		Columns: []infinity.Column{
			{Name: "host", Type: "string"},
			{Name: "status", Type: "number"},
			{Name: "up", Type: "boolean"},
			{Name: "checked_at", Type: "time"},
		},
		Rows: [][]any{
			{"server-1", float64(200), true, float64(1700000000000)},
			{"server-2", float64(500), false, float64(1700000060000)},
		},
	}

	var buf bytes.Buffer
	require.NoError(t, infinity.FormatArrow(&buf, resp))

	headers, rows, err := arrowtable.ReadStream(&buf)
	require.NoError(t, err)
	assert.Equal(t, []string{"HOST", "STATUS", "UP", "CHECKED_AT"}, headers)
	require.Len(t, rows, 2)
	assert.Equal(t, "server-1", rows[0][0])
	assert.InEpsilon(t, 200.0, rows[0][1], 0.0001)
	assert.Equal(t, true, rows[0][2])
	assert.Equal(t, time.UnixMilli(1700000000000).UTC(), rows[0][3])
}

func TestFormatArrow_RowShorterThanColumns(t *testing.T) {
	resp := &infinity.QueryResponse{
		Columns: []infinity.Column{
			{Name: "host", Type: "string"},
			{Name: "status", Type: "number"},
		},
		Rows: [][]any{
			{"server-1"}, // missing the trailing "status" cell
		},
	}

	var buf bytes.Buffer
	require.NoError(t, infinity.FormatArrow(&buf, resp))

	_, rows, err := arrowtable.ReadStream(&buf)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, "server-1", rows[0][0])
	assert.Nil(t, rows[0][1])
}

func TestFormatArrow_NoData(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, infinity.FormatArrow(&buf, &infinity.QueryResponse{}))
	assert.Empty(t, buf.String())
}
