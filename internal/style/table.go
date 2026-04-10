package style

import (
	"fmt"
	"io"
	"text/tabwriter"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/table"
	"github.com/grafana/gcx/internal/terminal"
)

// TableBuilder constructs styled tables that degrade gracefully to plain
// tabwriter output when styling is disabled (piped, agent mode, --no-color).
type TableBuilder struct {
	headers []string
	rows    [][]string
}

// NewTable creates a new table with the given column headers.
func NewTable(headers ...string) *TableBuilder {
	return &TableBuilder{
		headers: headers,
	}
}

// Row appends a data row. The number of values should match the header count.
func (tb *TableBuilder) Row(vals ...string) *TableBuilder {
	tb.rows = append(tb.rows, vals)
	return tb
}

// Render writes the table to w. When styling is enabled, it uses lipgloss/table
// with the Grafana Neon Dark palette. Otherwise, it falls back to text/tabwriter
// with the exact same formatting as the legacy code (minwidth=0, tabwidth=4, padding=2).
func (tb *TableBuilder) Render(w io.Writer) error {
	if !IsStylingEnabled() {
		return tb.renderPlain(w)
	}
	return tb.renderStyled(w)
}

func (tb *TableBuilder) renderPlain(w io.Writer) error {
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', tabwriter.TabIndent|tabwriter.DiscardEmptyColumns)
	if len(tb.headers) > 0 {
		for i, h := range tb.headers {
			if i > 0 {
				fmt.Fprint(tw, "\t")
			}
			fmt.Fprint(tw, h)
		}
		fmt.Fprintln(tw)
	}
	for _, row := range tb.rows {
		for i, v := range row {
			if i > 0 {
				fmt.Fprint(tw, "\t")
			}
			fmt.Fprint(tw, v)
		}
		fmt.Fprintln(tw)
	}
	return tw.Flush()
}

func (tb *TableBuilder) renderStyled(w io.Writer) error {
	width := terminalWidth()

	headerStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(ColorPrimary).
		Padding(0, 1)

	cellStyle := lipgloss.NewStyle().
		Padding(0, 1)

	evenRowStyle := cellStyle.Foreground(lipgloss.Color("#CCCCCC"))
	oddRowStyle := cellStyle.Foreground(lipgloss.Color("#999999"))

	rows := make([][]string, len(tb.rows))
	copy(rows, tb.rows)

	t := table.New().
		Border(lipgloss.NormalBorder()).
		BorderStyle(lipgloss.NewStyle().Foreground(ColorBorder)).
		Headers(tb.headers...).
		Rows(rows...).
		Width(width).
		StyleFunc(func(row, col int) lipgloss.Style {
			if row == table.HeaderRow {
				return headerStyle
			}
			if row%2 == 0 {
				return evenRowStyle
			}
			return oddRowStyle
		})

	_, err := fmt.Fprintln(w, t)
	return err
}

func terminalWidth() int {
	if w := terminal.StdoutWidth(); w > 0 {
		return w
	}
	return 80
}
