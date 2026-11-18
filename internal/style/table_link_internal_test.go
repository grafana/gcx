package style

import (
	"strings"
	"testing"
)

func TestTableBuilder_StyledHyperlink(t *testing.T) {
	tb := NewTable("NAME", "LINK").ColumnWidths([]int{0, 6})
	tb.Row("checkout", "open")
	tb.Link(1, "https://example.grafana.net/explore")

	out := tb.styledTable()
	if !strings.Contains(out, "\x1b]8;;https://example.grafana.net/explore\a") || !strings.Contains(out, "open") {
		t.Fatalf("styled link cell = %q", out)
	}
}
