package browse

import (
	"fmt"
	"slices"
	"strings"
	"unicode"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/grafana/gcx/internal/style"
)

// pickerItem is one selectable row: a title, a muted description, and the
// text the filter matches against.
type pickerItem interface {
	Title() string
	Description() string
	FilterValue() string
}

// picker is a minimal fzf-style list: one row per item, "/" to filter with
// fuzzy subsequence matching, and a cursor that scrolls the visible window.
// It exists instead of bubbles/list because that package imports
// github.com/sahilm/fuzzy, a module gcx does not otherwise depend on.
type picker struct {
	title     string
	items     []pickerItem
	matches   []int // indices into items, in display order
	cursor    int   // index into matches
	offset    int   // first visible match
	filter    textinput.Model
	filtering bool // the filter input has focus
	width     int
	height    int
}

func newPicker(title string) picker {
	ti := textinput.New()
	ti.Prompt = "/"
	return picker{title: title, filter: ti}
}

// setItems replaces the items, re-applies the current filter, and moves the
// cursor to the top.
func (p *picker) setItems(items []pickerItem) {
	p.items = items
	p.refilter()
}

func (p *picker) setSize(width, height int) {
	p.width, p.height = width, height
	p.filter.SetWidth(max(width-2, 1))
	p.clampOffset()
}

func (p *picker) selected() (pickerItem, bool) {
	if p.cursor < 0 || p.cursor >= len(p.matches) {
		return nil, false
	}
	return p.items[p.matches[p.cursor]], true
}

func (p *picker) settingFilter() bool { return p.filtering }

func (p *picker) filterApplied() bool { return !p.filtering && p.filter.Value() != "" }

func (p *picker) resetFilter() {
	p.filtering = false
	p.filter.Blur()
	p.filter.SetValue("")
	p.refilter()
}

// update handles key presses; anything else (cursor blinks) goes to the
// filter input while it has focus.
func (p *picker) update(msg tea.Msg) tea.Cmd {
	km, ok := msg.(tea.KeyPressMsg)
	if !ok {
		if p.filtering {
			var cmd tea.Cmd
			p.filter, cmd = p.filter.Update(msg)
			return cmd
		}
		return nil
	}

	if p.filtering {
		switch km.String() {
		case "esc":
			p.resetFilter()
			return nil
		case "enter":
			p.filtering = false
			p.filter.Blur()
			return nil
		case "up", "ctrl+p":
			p.move(-1)
			return nil
		case "down", "ctrl+n":
			p.move(1)
			return nil
		}
		before := p.filter.Value()
		var cmd tea.Cmd
		p.filter, cmd = p.filter.Update(km)
		if p.filter.Value() != before {
			p.refilter()
		}
		return cmd
	}

	switch km.String() {
	case "/":
		p.filtering = true
		return p.filter.Focus()
	case "esc":
		p.resetFilter()
	case "up", "k", "ctrl+p":
		p.move(-1)
	case "down", "j", "ctrl+n":
		p.move(1)
	case "pgup", "ctrl+u":
		p.move(-p.rows())
	case "pgdown", "ctrl+d":
		p.move(p.rows())
	case "home", "g":
		p.move(-len(p.matches))
	case "end", "G":
		p.move(len(p.matches))
	}
	return nil
}

func (p *picker) move(delta int) {
	p.cursor = max(0, min(p.cursor+delta, len(p.matches)-1))
	p.clampOffset()
}

// rows is the number of item rows that fit below the title and info lines.
func (p *picker) rows() int { return max(p.height-2, 1) }

func (p *picker) clampOffset() {
	if p.cursor < p.offset {
		p.offset = p.cursor
	}
	if p.cursor >= p.offset+p.rows() {
		p.offset = p.cursor - p.rows() + 1
	}
	p.offset = max(0, p.offset)
}

func (p *picker) refilter() {
	pattern := p.filter.Value()
	type scored struct{ idx, score int }
	var hits []scored
	for i, it := range p.items {
		if s, ok := fuzzyMatch(pattern, it.FilterValue()); ok {
			hits = append(hits, scored{i, s})
		}
	}
	if pattern != "" {
		slices.SortStableFunc(hits, func(a, b scored) int { return b.score - a.score })
	}
	p.matches = p.matches[:0]
	for _, h := range hits {
		p.matches = append(p.matches, h.idx)
	}
	p.cursor, p.offset = 0, 0
}

func (p *picker) view() string {
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(style.ColorPrimary)
	muted := lipgloss.NewStyle().Foreground(style.ColorMuted)
	cursorStyle := lipgloss.NewStyle().Bold(true).Foreground(style.ColorPrimary)
	line := lipgloss.NewStyle().MaxWidth(p.width)

	lines := make([]string, 0, p.height)
	lines = append(lines, line.Render(titleStyle.Render(p.title)))

	var info string
	switch {
	case p.filtering:
		// The count goes first: the input pads itself to the full width.
		info = muted.Render(fmt.Sprintf("%d/%d ", len(p.matches), len(p.items))) + p.filter.View()
	case p.filterApplied():
		info = muted.Render(fmt.Sprintf("%d/%d  filter: %s  (esc clears)", len(p.matches), len(p.items), p.filter.Value()))
	default:
		info = muted.Render(fmt.Sprintf("%d items", len(p.items)))
	}
	lines = append(lines, line.Render(info))

	end := min(p.offset+p.rows(), len(p.matches))
	for i := p.offset; i < end; i++ {
		it := p.items[p.matches[i]]
		prefix, title := "  ", it.Title()
		if i == p.cursor {
			prefix, title = cursorStyle.Render("> "), cursorStyle.Render(title)
		}
		row := prefix + title
		if d := it.Description(); d != "" {
			row += "  " + muted.Render(d)
		}
		lines = append(lines, line.Render(row))
	}
	return strings.Join(lines, "\n")
}

// fuzzyMatch reports whether every rune of pattern appears in target in
// order, ignoring case, and scores the match: consecutive runs and runs that
// start at word boundaries score higher, so "dash" ranks "dashboards" above
// "d-a-s-h". An empty pattern matches everything with score 0.
func fuzzyMatch(pattern, target string) (int, bool) {
	p := []rune(strings.ToLower(pattern))
	if len(p) == 0 {
		return 0, true
	}
	t := []rune(strings.ToLower(target))
	score, pi, prev, first := 0, 0, -2, -1
	for ti, r := range t {
		if pi == len(p) {
			break
		}
		if r != p[pi] {
			continue
		}
		if first < 0 {
			first = ti
		}
		switch {
		case ti == prev+1:
			score += 5
		case ti == 0 || isBoundary(t[ti-1]):
			score += 3
		default:
			score++
		}
		prev = ti
		pi++
	}
	if pi < len(p) {
		return 0, false
	}
	return score*10 - first, true
}

func isBoundary(r rune) bool {
	return !unicode.IsLetter(r) && !unicode.IsDigit(r)
}
