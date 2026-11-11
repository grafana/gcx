package style

import (
	"bytes"
	"strings"
	"testing"
)

// TestTableBuilder_CompactStyledHasNoBorders exercises renderStyled directly:
// tests run without a TTY, so Render always takes the plain path.
func TestTableBuilder_CompactStyledHasNoBorders(t *testing.T) {
	tb := NewTable("NAME", "STATUS").Compact()
	tb.Row("prod-eu", "OK")
	tb.Row("prod-us", "DEGRADED")

	var buf bytes.Buffer
	if err := tb.renderStyled(&buf); err != nil {
		t.Fatalf("renderStyled() error: %v", err)
	}
	out := buf.String()

	// Compact keeps only the rule under the header: no frame, column
	// separators, row separators, or junctions.
	for _, ch := range []string{"│", "┌", "┐", "└", "┘", "┼", "├", "┤", "┬", "┴"} {
		if strings.Contains(out, ch) {
			t.Errorf("compact table contains border char %q:\n%s", ch, out)
		}
	}
	for _, want := range []string{"NAME", "STATUS", "prod-eu", "DEGRADED"} {
		if !strings.Contains(out, want) {
			t.Errorf("compact table missing %q:\n%s", want, out)
		}
	}
	if got := strings.Count(strings.TrimRight(out, "\n"), "\n") + 1; got != 4 {
		t.Errorf("compact table has %d lines, want 4 (header, rule, 2 rows):\n%s", got, out)
	}
}
