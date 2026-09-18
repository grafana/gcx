// Package logs provides an interactive, color-coded terminal viewer for raw
// Loki log-line query results, for quickly eyeballing the real data behind
// an AI-generated summary (logs-drilldown#2081) without leaving the CLI.
package logs

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/grafana/gcx/internal/graph"
	"github.com/grafana/gcx/internal/query/loki"
)

// levelFieldWidth is wide enough for the longest canonical level word
// ("CRITICAL"), so the level column stays aligned across lines.
const levelFieldWidth = 8

// timestampWidth is the fixed rendered width of a time.RFC3339-formatted UTC
// timestamp (e.g. "2026-09-17T21:23:35Z"), computed once rather than
// hardcoded so it stays correct if the format ever changes.
var timestampWidth = len(time.Unix(0, 0).UTC().Format(time.RFC3339)) //nolint:gochecknoglobals

// prefixWidth is the total visual width of the timestamp + level columns
// (plus their separating spaces) that precede the message body on every
// line. Wrapped continuation lines are indented by this much so the message
// column lines up under itself instead of running back to column zero.
var prefixWidth = timestampWidth + 2 + levelFieldWidth + 2 //nolint:gochecknoglobals

// leadingTimestampPattern matches a bracketed ISO-8601-ish timestamp at the
// start of a message body (e.g. "[2026-09-17T21:11:08.106Z] \"GET ...\"").
// Many access-log-style lines repeat their own timestamp inside the body;
// since the TUI already shows Loki's own timestamp as its first column,
// this redundant copy is stripped before display.
var leadingTimestampPattern = regexp.MustCompile(`^\[\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?Z?\]\s*`)

// entry is one flattened, time-sorted log line ready for display. message
// reuses the same body-parsing gcx's table output already does (see
// loki.NewDisplayLogEntry), so the TUI shows a clean extracted message
// instead of the full raw line. level is detected via graph.DetectLevel —
// the same function the log-volume histogram uses — so the TUI's level
// column and the histogram's bucketing never disagree about the same line.
type entry struct {
	t       time.Time
	level   graph.LogLevel
	message string
}

// Model is the bubbletea model for the interactive log viewer.
type Model struct {
	vp        viewport.Model
	entries   []entry
	width     int
	ready     bool
	startWrap bool
}

// Option configures a Model returned by New.
type Option func(*Model)

// WithWrap sets whether long lines soft-wrap (with the message column
// hanging-indented under itself) instead of being clipped at the terminal
// width. Toggle live with 'w' regardless of this initial setting.
func WithWrap(wrap bool) Option {
	return func(m *Model) { m.startWrap = wrap }
}

// New builds a Model from a Loki query response, flattening all streams'
// log lines into a single time-sorted list.
func New(resp *loki.QueryResponse, opts ...Option) Model {
	m := Model{entries: flattenEntries(resp)}
	for _, opt := range opts {
		opt(&m)
	}
	return m
}

func flattenEntries(resp *loki.QueryResponse) []entry {
	if resp == nil {
		return nil
	}

	entries := make([]entry, 0)
	for _, stream := range resp.Data.Result {
		for _, e := range stream.Values {
			t, err := loki.ParseTimestamp(e.Timestamp)
			if err != nil {
				continue
			}
			entries = append(entries, entry{
				t:       t,
				level:   graph.DetectLevel(stream.Stream, e),
				message: cleanMessage(loki.NewDisplayLogEntry(stream.Stream, e).Message),
			})
		}
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].t.Before(entries[j].t) })

	return entries
}

// Init satisfies tea.Model. The viewport is sized lazily on the first
// WindowSizeMsg, so there's no initial command.
func (m Model) Init() tea.Cmd {
	return nil
}

// Update satisfies tea.Model.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.vp = viewport.New(viewport.WithWidth(msg.Width), viewport.WithHeight(msg.Height-1))
		m.vp.SoftWrap = m.startWrap
		m.vp.SetContent(m.renderLines(m.width, m.vp.SoftWrap))
		m.vp.GotoBottom()
		m.ready = true
		return m, nil
	case tea.KeyPressMsg:
		switch msg.String() {
		case "q", "ctrl+c", "esc":
			return m, tea.Quit
		case "w":
			if m.ready {
				m.vp.SoftWrap = !m.vp.SoftWrap
				m.vp.SetContent(m.renderLines(m.width, m.vp.SoftWrap))
			}
			return m, nil
		}
	}

	if !m.ready {
		return m, nil
	}

	var cmd tea.Cmd
	m.vp, cmd = m.vp.Update(msg)
	return m, cmd
}

// View satisfies tea.Model.
func (m Model) View() tea.View {
	if !m.ready {
		return tea.NewView("")
	}

	wrapHint := "wrap off"
	if m.vp.SoftWrap {
		wrapHint = "wrap on"
	}
	header := lipgloss.NewStyle().Bold(true).
		Render(fmt.Sprintf("%d log lines — ↑/↓ or j/k to scroll, ←/→ or h/l to pan, w to toggle wrap (%s), q to quit", len(m.entries), wrapHint))

	return tea.NewView(header + "\n" + m.vp.View())
}

// renderLines builds the viewport content. When wrap is true, each message
// is word-wrapped to fit within width (accounting for the timestamp+level
// prefix), and continuation lines are indented by prefixWidth so the message
// column lines up under itself rather than wrapping back to column zero.
func (m Model) renderLines(width int, wrap bool) string {
	if len(m.entries) == 0 {
		return "No log lines"
	}

	messageWidth := width - prefixWidth

	var sb strings.Builder
	for _, e := range m.entries {
		sb.WriteString(e.t.UTC().Format(time.RFC3339))
		sb.WriteString("  ")
		sb.WriteString(formatLevelField(e.level))
		sb.WriteString("  ")

		message := e.message
		if wrap {
			message = indentContinuationLines(ansi.Wrap(message, messageWidth, ""), prefixWidth)
		}
		sb.WriteString(message)
		sb.WriteString("\n")
	}
	return sb.String()
}

// indentContinuationLines pads every line after the first in a wrapped
// string with indent spaces, so it hangs under the column it started in.
func indentContinuationLines(wrapped string, indent int) string {
	if !strings.Contains(wrapped, "\n") {
		return wrapped
	}
	pad := strings.Repeat(" ", indent)
	lines := strings.Split(wrapped, "\n")
	for i := 1; i < len(lines); i++ {
		lines[i] = pad + lines[i]
	}
	return strings.Join(lines, "\n")
}

// formatLevelField renders the level column, uppercased and padded for
// alignment. Only this field is colored — timestamp and message are left
// unstyled so they render in the terminal's own default foreground (a
// readable white or gray depending on the user's light/dark theme) rather
// than a hardcoded color. A line with no detectable level renders as blank
// space rather than the word "UNKNOWN" on every line lacking structure.
func formatLevelField(level graph.LogLevel) string {
	if level == graph.LogLevelUnknown {
		return strings.Repeat(" ", levelFieldWidth)
	}
	text := fmt.Sprintf("%-*s", levelFieldWidth, strings.ToUpper(string(level)))
	return lipgloss.NewStyle().Foreground(graph.LevelColor(level)).Render(text)
}

// cleanMessage strips a redundant leading bracketed timestamp (see
// leadingTimestampPattern) and substitutes a placeholder for a genuinely
// empty line, so the message column is never blank in a way that looks
// like missing data.
func cleanMessage(message string) string {
	if message == "" {
		return "(empty line)"
	}
	return leadingTimestampPattern.ReplaceAllString(message, "")
}

// Run launches the interactive log viewer for resp on the current terminal,
// blocking until the user quits.
func Run(resp *loki.QueryResponse, opts ...Option) error {
	_, err := tea.NewProgram(New(resp, opts...)).Run()
	return err
}
