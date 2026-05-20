package infinity_test

import (
	"bytes"
	"testing"

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
