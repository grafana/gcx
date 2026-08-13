package style_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/style"
)

// TestTableBuilder_AgentModeRendersPlain documents the invariant that
// TableBuilder uses plain-text (no box chars, no ANSI) in agent mode
// regardless of whether -o wide or another format was chosen.
// Non-format presentation properties are suppressed uniformly in agent mode
// across all output formats.
func TestTableBuilder_AgentModeRendersPlain(t *testing.T) {
	t.Setenv("CLAUDECODE", "1")
	agent.ResetForTesting()
	t.Cleanup(func() { agent.ResetForTesting() })

	tb := style.NewTable("NAME", "STATUS")
	tb.Row("prod-eu", "OK")

	var buf bytes.Buffer
	if err := tb.Render(&buf); err != nil {
		t.Fatalf("Render() error: %v", err)
	}
	out := buf.String()

	// Plain tabwriter output must not contain lipgloss border box chars.
	for _, ch := range []string{"│", "┌", "┐", "└", "┘", "─", "┼"} {
		if strings.Contains(out, ch) {
			t.Errorf("agent-mode table output contains box char %q:\n%s", ch, out)
		}
	}
	// Must still contain the data.
	if !strings.Contains(out, "prod-eu") {
		t.Errorf("agent-mode table output missing data:\n%s", out)
	}
}

func TestTableBuilder_RenderCSV(t *testing.T) {
	tb := style.NewTable("NAME", "NOTE")
	tb.Row("prod-eu", "ok")
	tb.Row("has,comma", `has "quote"`+"\nand newline")

	var buf bytes.Buffer
	if err := tb.RenderCSV(&buf); err != nil {
		t.Fatalf("RenderCSV() error: %v", err)
	}

	want := "NAME,NOTE\nprod-eu,ok\n\"has,comma\",\"has \"\"quote\"\"\nand newline\"\n"
	if buf.String() != want {
		t.Errorf("RenderCSV() = %q, want %q", buf.String(), want)
	}
}

func TestTableBuilder_RenderCSV_NoRows(t *testing.T) {
	tb := style.NewTable("NAME", "NOTE")

	var buf bytes.Buffer
	if err := tb.RenderCSV(&buf); err != nil {
		t.Fatalf("RenderCSV() error: %v", err)
	}

	if want := "NAME,NOTE\n"; buf.String() != want {
		t.Errorf("RenderCSV() = %q, want %q", buf.String(), want)
	}
}
