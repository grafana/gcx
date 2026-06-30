package sql_test

import (
	"bytes"
	"testing"

	"github.com/grafana/gcx/internal/query/sql"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFormatTable(t *testing.T) {
	t.Run("renders columns and rows", func(t *testing.T) {
		resp := &sql.QueryResponse{
			Columns: []sql.Column{{Name: "id", Type: "number"}, {Name: "name", Type: "string"}},
			Rows:    [][]any{{float64(1), "alice"}, {float64(2), "bob"}},
		}
		var buf bytes.Buffer
		err := sql.FormatTable(&buf, resp)
		require.NoError(t, err)
		out := buf.String()
		assert.Contains(t, out, "ID")
		assert.Contains(t, out, "NAME")
		assert.Contains(t, out, "alice")
		assert.Contains(t, out, "bob")
	})

	t.Run("formats time columns as RFC3339", func(t *testing.T) {
		resp := &sql.QueryResponse{
			Columns: []sql.Column{{Name: "timestamp", Type: "time"}, {Name: "service", Type: "string"}},
			Rows:    [][]any{{float64(1778601661000), "frontend"}, {float64(1778601721000), "backend"}},
		}
		var buf bytes.Buffer
		err := sql.FormatTable(&buf, resp)
		require.NoError(t, err)
		out := buf.String()
		assert.Contains(t, out, "2026-05-12T")
		assert.NotContains(t, out, "1778601661000")
	})

	t.Run("empty result", func(t *testing.T) {
		resp := &sql.QueryResponse{}
		var buf bytes.Buffer
		err := sql.FormatTable(&buf, resp)
		require.NoError(t, err)
		assert.Contains(t, buf.String(), "No data")
	})
}

func TestFormatWideTable(t *testing.T) {
	resp := &sql.QueryResponse{
		Columns: []sql.Column{{Name: "id", Type: "number"}},
		Rows:    [][]any{{float64(1)}},
	}
	var buf bytes.Buffer
	err := sql.FormatWideTable(&buf, resp)
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "ID")
}
