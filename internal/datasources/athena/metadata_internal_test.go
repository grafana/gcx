package athena

import (
	"bytes"
	"testing"

	"github.com/grafana/gcx/internal/agent"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFilterAndLimit(t *testing.T) {
	items := []string{"a1", "a2", "a3", "b1"}

	t.Run("limit 0 is unbounded and silent", func(t *testing.T) {
		var warn bytes.Buffer
		assert.Equal(t, items, filterAndLimit(&warn, items, "", 0, "tables"))
		assert.Empty(t, warn.String())
	})

	t.Run("under the limit is silent", func(t *testing.T) {
		var warn bytes.Buffer
		assert.Equal(t, items, filterAndLimit(&warn, items, "", 4, "tables"))
		assert.Empty(t, warn.String())
	})

	t.Run("filter applies before the limit", func(t *testing.T) {
		var warn bytes.Buffer
		got := filterAndLimit(&warn, items, "A", 3, "tables")
		assert.Equal(t, []string{"a1", "a2", "a3"}, got)
		assert.Empty(t, warn.String(), "exactly limit matches is not truncation")
	})

	t.Run("truncation warns and names both escape hatches", func(t *testing.T) {
		var warn bytes.Buffer
		got := filterAndLimit(&warn, items, "", 2, "tables")
		assert.Equal(t, []string{"a1", "a2"}, got)
		out := warn.String()
		assert.Contains(t, out, "showing the first 2 tables; more tables match")
		assert.Contains(t, out, "--filter SUBSTRING")
		assert.Contains(t, out, "--limit 0")
	})

	t.Run("agent mode warning is JSONL", func(t *testing.T) {
		prev := agent.IsAgentMode()
		agent.SetFlag(true)
		t.Cleanup(func() { agent.SetFlag(prev) })
		var warn bytes.Buffer
		filterAndLimit(&warn, items, "", 1, "columns")
		lines := bytes.Split(bytes.TrimSpace(warn.Bytes()), []byte("\n"))
		require.Len(t, lines, 2)
		assert.Contains(t, string(lines[0]), `"class":"warning"`)
		assert.Contains(t, string(lines[1]), `"class":"hint"`)
	})
}
