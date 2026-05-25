package athena_test

import (
	"bytes"
	"testing"

	"github.com/grafana/gcx/internal/query/athena"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFormatTable(t *testing.T) {
	t.Run("renders columns and rows", func(t *testing.T) {
		resp := &athena.QueryResponse{
			Columns: []athena.Column{{Name: "id", Type: "number"}, {Name: "name", Type: "string"}},
			Rows:    [][]any{{float64(1), "alice"}, {float64(2), "bob"}},
		}
		var buf bytes.Buffer
		err := athena.FormatTable(&buf, resp)
		require.NoError(t, err)
		out := buf.String()
		assert.Contains(t, out, "ID")
		assert.Contains(t, out, "NAME")
		assert.Contains(t, out, "alice")
		assert.Contains(t, out, "bob")
	})

	t.Run("formats time columns as RFC3339", func(t *testing.T) {
		resp := &athena.QueryResponse{
			Columns: []athena.Column{{Name: "timestamp", Type: "time"}, {Name: "service", Type: "string"}},
			Rows:    [][]any{{float64(1778601661000), "frontend"}, {float64(1778601721000), "backend"}},
		}
		var buf bytes.Buffer
		err := athena.FormatTable(&buf, resp)
		require.NoError(t, err)
		out := buf.String()
		assert.Contains(t, out, "2026-05-12T")
		assert.NotContains(t, out, "1778601661000")
	})

	t.Run("empty result", func(t *testing.T) {
		resp := &athena.QueryResponse{}
		var buf bytes.Buffer
		err := athena.FormatTable(&buf, resp)
		require.NoError(t, err)
		assert.Contains(t, buf.String(), "No data")
	})
}

func TestFormatStringList(t *testing.T) {
	t.Run("renders items", func(t *testing.T) {
		var buf bytes.Buffer
		err := athena.FormatStringList(&buf, []string{"alpha", "beta", "gamma"}, "CATALOG")
		require.NoError(t, err)
		out := buf.String()
		assert.Contains(t, out, "CATALOG")
		assert.Contains(t, out, "alpha")
		assert.Contains(t, out, "gamma")
	})

	t.Run("empty result", func(t *testing.T) {
		var buf bytes.Buffer
		err := athena.FormatStringList(&buf, []string{}, "EMPTY")
		require.NoError(t, err)
		assert.Contains(t, buf.String(), "No data")
	})
}
