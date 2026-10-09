package query_test

import (
	"bytes"
	"strings"
	"testing"

	dsquery "github.com/grafana/gcx/internal/datasources/query"
	querysql "github.com/grafana/gcx/internal/query/sql"
	"github.com/stretchr/testify/assert"
)

func rows(n int) [][]any {
	out := make([][]any, n)
	for i := range out {
		out[i] = []any{i}
	}
	return out
}

func TestSurfaceRowLimits(t *testing.T) {
	t.Run("capped: drops sentinel row and warns", func(t *testing.T) {
		resp := &querysql.QueryResponse{Rows: rows(101)}
		var buf bytes.Buffer
		dsquery.SurfaceRowLimits(&buf, resp, true, 100, 1000)

		assert.Len(t, resp.Rows, 100, "sentinel row dropped before output")
		assert.Contains(t, buf.String(), "showing the first 100 rows")
	})

	t.Run("capped but within limit: no warning", func(t *testing.T) {
		resp := &querysql.QueryResponse{Rows: rows(42)}
		var buf bytes.Buffer
		dsquery.SurfaceRowLimits(&buf, resp, true, 100, 1000)

		assert.Len(t, resp.Rows, 42)
		assert.Empty(t, buf.String())
	})

	t.Run("not capped: no truncation, no gcx warning", func(t *testing.T) {
		resp := &querysql.QueryResponse{Rows: rows(500)}
		var buf bytes.Buffer
		dsquery.SurfaceRowLimits(&buf, resp, false, 100, 1000)

		assert.Len(t, resp.Rows, 500, "user-controlled result left intact")
		assert.NotContains(t, buf.String(), "showing the first")
	})

	t.Run("server-side notices surfaced verbatim", func(t *testing.T) {
		resp := &querysql.QueryResponse{
			Rows:    rows(100),
			Notices: []string{"Results have been limited to 100 because the SQL row limit was reached"},
		}
		var buf bytes.Buffer
		dsquery.SurfaceRowLimits(&buf, resp, false, 100, 1000)

		assert.Contains(t, buf.String(), "Results have been limited to 100")
	})

	t.Run("both gcx truncation and server notice fire", func(t *testing.T) {
		resp := &querysql.QueryResponse{
			Rows:    rows(101),
			Notices: []string{"server notice"},
		}
		var buf bytes.Buffer
		dsquery.SurfaceRowLimits(&buf, resp, true, 100, 1000)

		assert.Len(t, resp.Rows, 100)
		// One emitted line per source (gcx truncation + server notice), in both
		// TTY and agent-JSON modes since each EmitHint prints exactly one line.
		lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
		assert.Len(t, lines, 2, "one hint line per source")
	})
}
