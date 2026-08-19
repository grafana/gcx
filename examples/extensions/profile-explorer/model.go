package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// sinceOptions are the time ranges `t` cycles through.
var sinceOptions = []string{"15m", "1h", "3h", "6h", "24h"}

// maxNodes caps the flamegraph the server builds. A terminal can only show a
// few hundred cells per row, so the default 50000 is wasted bytes and latency.
const maxNodes = 8192

type stage int

const (
	stageDatasources stage = iota
	stageServices
	stageFlame
)

type overlay int

const (
	overlayNone overlay = iota
	overlayTypes
	overlayQuery
	overlayFilter
	overlaySearch
	overlayHelp
)

type model struct {
	client *gcxClient
	ctx    context.Context

	stage   stage
	overlay overlay

	width, height int

	datasources []datasource
	dsCursor    int
	ds          datasource

	types       []profileType
	typeCursor  int
	profileType profileType

	services  []string
	svcCursor int
	filter    string

	expr     string
	sinceIdx int

	fg      *flame
	nav     nav
	search  string
	showTop bool
	took    time.Duration

	input   textinput.Model
	spin    spinner.Model
	loading string
	reqID   int
	err     string

	// pendingQuery defers a start-up query until the profile types arrive: the
	// query needs the type's sample unit to label its values.
	pendingQuery bool
}

func newModel(ctx context.Context, client *gcxClient, start startArgs) model {
	in := textinput.New()
	in.Prompt = ""
	sp := spinner.New()
	sp.Spinner = spinner.Dot

	m := model{
		client:   client,
		ctx:      ctx,
		input:    in,
		spin:     sp,
		expr:     start.expr,
		sinceIdx: sinceIndex(start.since),
		width:    100,
		height:   30,
		nav:      nav{width: 100},
	}
	m.loading = "datasources"
	if start.datasource != "" {
		m.ds = datasource{UID: start.datasource, Name: start.datasource}
		m.stage = stageServices
		m.loading = "services"
		m.pendingQuery = start.expr != ""
	}
	if start.profileType != "" {
		m.profileType = profileType{ID: start.profileType}
	}
	return m
}

func sinceIndex(since string) int {
	for i, s := range sinceOptions {
		if s == since {
			return i
		}
	}
	return 1
}

func (m model) since() string { return sinceOptions[m.sinceIdx] }

// ── messages ─────────────────────────────────────────────────────────────────

type datasourcesMsg struct {
	items []datasource
	err   error
}

type typesMsg struct {
	items []profileType
	err   error
}

type servicesMsg struct {
	names []string
	err   error
}

type flameMsg struct {
	id   int
	fg   *flame
	took time.Duration
	err  error
}

func (m model) loadDatasources() tea.Cmd {
	return func() tea.Msg {
		items, err := m.client.profilingDatasources(m.ctx)
		return datasourcesMsg{items: items, err: err}
	}
}

func (m model) loadTypes(uid string) tea.Cmd {
	return func() tea.Msg {
		items, err := m.client.profileTypes(m.ctx, uid)
		return typesMsg{items: items, err: err}
	}
}

func (m model) loadServices(uid, since string) tea.Cmd {
	return func() tea.Msg {
		names, err := m.client.services(m.ctx, uid, since)
		return servicesMsg{names: names, err: err}
	}
}

func (m model) loadFlame() tea.Cmd {
	q := query{
		datasource:  m.ds.UID,
		expr:        m.expr,
		profileType: m.profileType,
		since:       m.since(),
		maxNodes:    maxNodes,
	}
	id := m.reqID
	return func() tea.Msg {
		started := time.Now()
		fg, err := m.client.flamegraph(m.ctx, q)
		return flameMsg{id: id, fg: fg, took: time.Since(started), err: err}
	}
}

func (m model) Init() tea.Cmd {
	if m.ds.UID != "" {
		return tea.Batch(m.spin.Tick, m.loadTypes(m.ds.UID), m.loadServices(m.ds.UID, m.since()))
	}
	return tea.Batch(m.spin.Tick, m.loadDatasources())
}

// ── update ───────────────────────────────────────────────────────────────────

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.nav.width = msg.Width
		return m, nil

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spin, cmd = m.spin.Update(msg)
		return m, cmd

	case datasourcesMsg:
		m.loading = ""
		if msg.err != nil {
			m.err = msg.err.Error()
			return m, nil
		}
		m.datasources = msg.items
		return m, nil

	case typesMsg:
		if msg.err != nil {
			m.err = msg.err.Error()
			return m, nil
		}
		m.types = msg.items
		m.typeCursor = defaultTypeIndex(msg.items, m.profileType.ID)
		m.profileType = msg.items[m.typeCursor]
		if m.pendingQuery {
			m.pendingQuery = false
			return m.startQuery()
		}
		return m, nil

	case servicesMsg:
		m.loading = ""
		if msg.err != nil {
			m.err = msg.err.Error()
			return m, nil
		}
		m.services = msg.names
		m.svcCursor = 0
		return m, nil

	case flameMsg:
		if msg.id != m.reqID {
			return m, nil
		}
		m.loading = ""
		m.took = msg.took
		if msg.err != nil {
			m.err = msg.err.Error()
			return m, nil
		}
		m.fg = msg.fg
		m.nav = nav{width: m.width}
		m.nav.moveDeeper(m.fg)
		m.stage = stageFlame
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

func defaultTypeIndex(types []profileType, want string) int {
	for i, t := range types {
		if t.ID == want {
			return i
		}
	}
	for i, t := range types {
		if strings.HasPrefix(t.ID, "process_cpu:cpu:") {
			return i
		}
	}
	return 0
}

func (m model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.Type == tea.KeyCtrlC {
		return m, tea.Quit
	}
	if m.overlay != overlayNone {
		return m.handleOverlayKey(msg)
	}

	switch m.stage {
	case stageDatasources:
		return m.handleDatasourceKey(msg)
	case stageServices:
		return m.handleServiceKey(msg)
	default:
		return m.handleFlameKey(msg)
	}
}

func (m model) handleOverlayKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.overlay {
	case overlayHelp:
		m.overlay = overlayNone
		return m, nil

	case overlayTypes:
		switch msg.String() {
		case "esc", "q", "p":
			m.overlay = overlayNone
		case "j", "down":
			m.typeCursor = clamp(m.typeCursor+1, 0, len(m.types)-1)
		case "k", "up":
			m.typeCursor = clamp(m.typeCursor-1, 0, len(m.types)-1)
		case "enter":
			m.overlay = overlayNone
			if m.typeCursor < len(m.types) {
				m.profileType = m.types[m.typeCursor]
			}
			if m.expr != "" {
				return m.startQuery()
			}
		}
		return m, nil
	}

	// The remaining overlays are text entry.
	switch msg.String() {
	case "esc":
		m.overlay = overlayNone
		m.input.Blur()
		return m, nil
	case "enter":
		value := m.input.Value()
		switch m.overlay {
		case overlayQuery:
			m.overlay = overlayNone
			m.input.Blur()
			m.expr = value
			return m.startQuery()
		case overlayFilter:
			m.overlay = overlayNone
			m.input.Blur()
			m.filter = value
			m.svcCursor = 0
			m.dsCursor = 0
		case overlaySearch:
			m.overlay = overlayNone
			m.input.Blur()
			m.search = value
			m.jumpToMatch(1)
		}
		return m, nil
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m model) handleDatasourceKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	items := m.visibleDatasources()
	switch msg.String() {
	case "q":
		return m, tea.Quit
	case "j", "down":
		m.dsCursor = clamp(m.dsCursor+1, 0, len(items)-1)
	case "k", "up":
		m.dsCursor = clamp(m.dsCursor-1, 0, len(items)-1)
	case "/":
		return m.openInput(overlayFilter, "filter datasources", m.filter)
	case "esc":
		if m.filter != "" {
			m.filter = ""
			m.dsCursor = 0
		}
	case "?":
		m.overlay = overlayHelp
	case "enter":
		if len(items) == 0 {
			return m, nil
		}
		m.ds = items[clamp(m.dsCursor, 0, len(items)-1)]
		m.stage = stageServices
		m.filter, m.svcCursor = "", 0
		m.loading = "services"
		m.err = ""
		return m, tea.Batch(m.loadTypes(m.ds.UID), m.loadServices(m.ds.UID, m.since()))
	}
	return m, nil
}

func (m model) handleServiceKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	items := m.visibleServices()
	switch msg.String() {
	case "q":
		return m, tea.Quit
	case "j", "down":
		m.svcCursor = clamp(m.svcCursor+1, 0, len(items)-1)
	case "k", "up":
		m.svcCursor = clamp(m.svcCursor-1, 0, len(items)-1)
	case "/":
		return m.openInput(overlayFilter, "filter services", m.filter)
	case "esc":
		if m.filter != "" {
			m.filter = ""
			m.svcCursor = 0
			return m, nil
		}
		m.stage = stageDatasources
		m.err = ""
	case "p":
		m.overlay = overlayTypes
	case "e":
		return m.openInput(overlayQuery, "label selector", m.exprOrSelected())
	case "t":
		m.sinceIdx = (m.sinceIdx + 1) % len(sinceOptions)
		m.loading = "services"
		return m, m.loadServices(m.ds.UID, m.since())
	case "?":
		m.overlay = overlayHelp
	case "enter":
		if len(items) == 0 {
			return m, nil
		}
		m.expr = fmt.Sprintf("{service_name=%q}", items[clamp(m.svcCursor, 0, len(items)-1)])
		return m.startQuery()
	}
	return m, nil
}

func (m model) handleFlameKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.fg == nil {
		if msg.String() == "q" {
			return m, tea.Quit
		}
		return m, nil
	}
	switch msg.String() {
	case "q":
		return m, tea.Quit
	case "esc":
		m.stage = stageServices
		m.err = ""
	case "h", "left":
		m.nav.moveLeft(m.fg)
	case "l", "right":
		m.nav.moveRight(m.fg)
	case "j", "down":
		m.nav.moveDeeper(m.fg)
	case "k", "up":
		m.nav.moveShallower(m.fg)
	case "enter", "z":
		m.nav.zoomIn(m.fg)
	case "backspace", "o":
		m.nav.zoomOut(m.fg)
	case "0":
		m.nav.reset()
		m.nav.moveDeeper(m.fg)
	case "/":
		return m.openInput(overlaySearch, "search frames", m.search)
	case "n":
		m.jumpToMatch(1)
	case "N":
		m.jumpToMatch(-1)
	case "T":
		m.showTop = !m.showTop
	case "p":
		m.overlay = overlayTypes
	case "e":
		return m.openInput(overlayQuery, "label selector", m.expr)
	case "t":
		m.sinceIdx = (m.sinceIdx + 1) % len(sinceOptions)
		return m.startQuery()
	case "r":
		return m.startQuery()
	case "?":
		m.overlay = overlayHelp
	}
	return m, nil
}

func (m model) openInput(o overlay, prompt, value string) (tea.Model, tea.Cmd) {
	m.overlay = o
	m.input.Placeholder = prompt
	m.input.SetValue(value)
	m.input.CursorEnd()
	m.input.Focus()
	return m, textinput.Blink
}

// startQuery fetches a flamegraph for the current selection. reqID rises on
// every request so a slow earlier response cannot overwrite a newer one.
func (m model) startQuery() (tea.Model, tea.Cmd) {
	if m.expr == "" || m.profileType.ID == "" {
		m.err = "pick a service and a profile type first"
		return m, nil
	}
	m.reqID++
	m.loading = "profile"
	m.err = ""
	return m, m.loadFlame()
}

func (m model) exprOrSelected() string {
	if m.expr != "" {
		return m.expr
	}
	items := m.visibleServices()
	if len(items) == 0 {
		return `{service_name=""}`
	}
	return fmt.Sprintf("{service_name=%q}", items[clamp(m.svcCursor, 0, len(items)-1)])
}

// jumpToMatch moves the selection to the next frame matching the search term,
// scanning level by level in the given direction.
func (m *model) jumpToMatch(dir int) {
	if m.fg == nil || m.search == "" {
		return
	}
	type pos struct{ lvl, idx int }
	var matches []pos
	term := strings.ToLower(m.search)
	rootX, rootW := m.nav.rootBounds(m.fg)
	for lvl := m.nav.rootLevel; lvl < len(m.fg.levels); lvl++ {
		for idx, fr := range m.fg.levels[lvl].frames {
			if fr.Total <= 0 || fr.X+fr.Total <= rootX || fr.X >= rootX+rootW {
				continue
			}
			if strings.Contains(strings.ToLower(m.fg.name(fr)), term) {
				matches = append(matches, pos{lvl, idx})
			}
		}
	}
	if len(matches) == 0 {
		return
	}
	current := -1
	for i, p := range matches {
		if p.lvl == m.nav.selLevel && p.idx == m.nav.selFrame {
			current = i
			break
		}
	}
	next := (current + dir + len(matches)*2) % len(matches)
	m.nav.selLevel, m.nav.selFrame = matches[next].lvl, matches[next].idx
}

func (m model) visibleDatasources() []datasource {
	if m.filter == "" {
		return m.datasources
	}
	out := make([]datasource, 0, len(m.datasources))
	for _, d := range m.datasources {
		if containsFold(d.Name, m.filter) || containsFold(d.UID, m.filter) {
			out = append(out, d)
		}
	}
	return out
}

func (m model) visibleServices() []string {
	if m.filter == "" {
		return m.services
	}
	out := make([]string, 0, len(m.services))
	for _, s := range m.services {
		if containsFold(s, m.filter) {
			out = append(out, s)
		}
	}
	return out
}

func containsFold(s, sub string) bool {
	return strings.Contains(strings.ToLower(s), strings.ToLower(sub))
}

// ── view ─────────────────────────────────────────────────────────────────────

var (
	titleStyle = lipgloss.NewStyle().Bold(true)
	mutedStyle = lipgloss.NewStyle().Faint(true)
	errStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("#ea6460"))
	selStyle   = lipgloss.NewStyle().Bold(true).Reverse(true)
	keyStyle   = lipgloss.NewStyle().Bold(true)
)

func (m model) View() string {
	var b strings.Builder
	b.WriteString(m.header())
	b.WriteString("\n")

	switch {
	case m.overlay == overlayHelp:
		b.WriteString(m.helpBody())
	case m.overlay == overlayTypes:
		b.WriteString(m.listBody(m.typeLabels(), m.typeCursor, "profile types"))
	case m.stage == stageDatasources:
		b.WriteString(m.listBody(m.datasourceLabels(), m.dsCursor, "Pyroscope datasources"))
	case m.stage == stageServices:
		b.WriteString(m.listBody(m.visibleServices(), m.svcCursor, "services"))
	default:
		b.WriteString(m.flameBody())
	}

	b.WriteString("\n")
	b.WriteString(m.footer())
	return b.String()
}

func (m model) header() string {
	ctx := m.client.context
	if ctx == "" {
		ctx = "current-context"
	}
	line := titleStyle.Render("gcx profile explorer") + mutedStyle.Render("  context="+ctx)
	if m.ds.UID != "" {
		line += mutedStyle.Render("  datasource=" + m.ds.Name)
	}
	if m.profileType.ID != "" {
		line += mutedStyle.Render("  " + m.profileType.label())
	}
	line += mutedStyle.Render("  since=" + m.since())

	second := ""
	if m.expr != "" {
		second = mutedStyle.Render("query ") + m.expr
	}
	if m.fg != nil {
		second += mutedStyle.Render(fmt.Sprintf("  total=%s  in %dms",
			formatValue(m.fg.total, m.fg.unit), m.took.Milliseconds()))
	}
	if second == "" {
		return line
	}
	return line + "\n" + second
}

func (m model) footer() string {
	if m.err != "" {
		return errStyle.Render("error: " + m.err)
	}
	if m.loading != "" {
		return m.spin.View() + " loading " + m.loading + "…"
	}
	switch {
	case m.overlay == overlayTypes:
		return keys("j/k", "move", "enter", "select", "esc", "cancel")
	case m.overlay != overlayNone && m.overlay != overlayHelp:
		return keys("enter", "apply", "esc", "cancel")
	case m.stage == stageDatasources:
		return keys("j/k", "move", "enter", "open", "/", "filter", "?", "help", "q", "quit")
	case m.stage == stageServices:
		return keys("enter", "profile", "p", "type", "e", "query", "t", "range", "/", "filter", "?", "help")
	default:
		return keys("hjkl", "move", "z", "zoom in", "o", "zoom out", "0", "reset", "/", "search", "T", "top", "?", "help")
	}
}

func keys(pairs ...string) string {
	var parts []string
	for i := 0; i+1 < len(pairs); i += 2 {
		parts = append(parts, keyStyle.Render(pairs[i])+" "+mutedStyle.Render(pairs[i+1]))
	}
	return strings.Join(parts, mutedStyle.Render(" · "))
}

// bodyHeight is the rows left for the body once the header, footer, and the
// input line are accounted for.
func (m model) bodyHeight() int {
	used := 4
	if m.expr != "" || m.fg != nil {
		used++
	}
	if h := m.height - used; h > 1 {
		return h
	}
	return 1
}

func (m model) datasourceLabels() []string {
	items := m.visibleDatasources()
	out := make([]string, 0, len(items))
	for _, d := range items {
		out = append(out, fmt.Sprintf("%-45s %s", truncate(d.Name, 45), mutedStyle.Render(d.UID)))
	}
	return out
}

func (m model) typeLabels() []string {
	out := make([]string, 0, len(m.types))
	for _, t := range m.types {
		out = append(out, t.ID)
	}
	return out
}

// listBody renders a scrolling list with the cursor kept in view.
func (m model) listBody(items []string, cursor int, what string) string {
	height := m.bodyHeight()
	if m.overlay != overlayNone && m.overlay != overlayTypes {
		height--
	}
	var b strings.Builder
	if len(items) == 0 && m.loading == "" {
		b.WriteString(mutedStyle.Render("no " + what))
	}
	start := 0
	if cursor >= height {
		start = cursor - height + 1
	}
	for i := start; i < len(items) && i-start < height; i++ {
		line := truncate(items[i], m.width-2)
		if i == cursor {
			b.WriteString(selStyle.Render("› " + line))
		} else {
			b.WriteString("  " + line)
		}
		b.WriteString("\n")
	}
	body := strings.TrimRight(b.String(), "\n")
	if m.overlay == overlayFilter || m.overlay == overlayQuery || m.overlay == overlaySearch {
		body += "\n" + m.inputLine()
	}
	return body
}

func (m model) inputLine() string {
	return keyStyle.Render(m.input.Placeholder+": ") + m.input.View()
}

func (m model) flameBody() string {
	if m.fg == nil {
		return mutedStyle.Render("no profile loaded")
	}
	height := m.bodyHeight() - 1 // selected-frame line
	if m.showTop {
		return m.topBody(height) + "\n" + m.selectionLine()
	}
	body := renderIcicle(m.fg, &m.nav, m.width, height, m.search)
	if m.overlay == overlaySearch {
		return body + "\n" + m.inputLine()
	}
	return body + "\n" + m.selectionLine()
}

func (m model) selectionLine() string {
	sel, ok := m.nav.selection(m.fg)
	if !ok {
		return mutedStyle.Render("nothing selected")
	}
	_, rootW := m.nav.rootBounds(m.fg)
	line := fmt.Sprintf("%s  total %s (%.1f%%)  self %s (%.1f%%)",
		truncate(m.fg.name(sel), m.width/2),
		formatValue(sel.Total, m.fg.unit), percent(sel.Total, rootW),
		formatValue(sel.Self, m.fg.unit), percent(sel.Self, rootW))
	if m.nav.rootLevel > 0 {
		line += mutedStyle.Render(fmt.Sprintf("  [zoomed: %s]", formatValue(rootW, m.fg.unit)))
	}
	if m.search != "" {
		line += mutedStyle.Render(fmt.Sprintf("  [%d match %q]", countMatches(m.fg, &m.nav, m.search), m.search))
	}
	return line
}

func (m model) topBody(height int) string {
	_, rootW := m.nav.rootBounds(m.fg)
	rows := topFunctions(m.fg, &m.nav, height-1)
	var b strings.Builder
	b.WriteString(mutedStyle.Render(fmt.Sprintf("%-11s %-7s %-11s  %s", "self", "self%", "total", "function")))
	b.WriteString("\n")
	for _, r := range rows {
		fmt.Fprintf(&b, "%-11s %-6.2f%% %-11s  %s\n",
			formatValue(r.Self, m.fg.unit), percent(r.Self, rootW),
			formatValue(r.Total, m.fg.unit), truncate(r.Name, m.width-34))
	}
	return strings.TrimRight(b.String(), "\n")
}

func (m model) helpBody() string {
	sections := []struct {
		title string
		rows  [][2]string
	}{
		{"lists", [][2]string{
			{"j / k", "move"},
			{"enter", "open datasource / load profile"},
			{"/", "filter"},
			{"esc", "clear filter, or go back"},
		}},
		{"query", [][2]string{
			{"p", "choose profile type"},
			{"e", "edit the label selector"},
			{"t", "cycle time range (" + strings.Join(sinceOptions, ", ") + ")"},
			{"r", "re-run the query"},
		}},
		{"flamegraph", [][2]string{
			{"h / l", "previous / next sibling frame"},
			{"j / k", "callee / caller"},
			{"z or enter", "zoom into the selected frame"},
			{"o or backspace", "zoom out one level"},
			{"0", "reset zoom"},
			{"/ then n / N", "search frames, cycle matches"},
			{"T", "toggle the top-functions table"},
		}},
	}
	var b strings.Builder
	for _, s := range sections {
		b.WriteString(titleStyle.Render(s.title) + "\n")
		for _, r := range s.rows {
			fmt.Fprintf(&b, "  %-16s %s\n", keyStyle.Render(r[0]), mutedStyle.Render(r[1]))
		}
	}
	b.WriteString(mutedStyle.Render("\nany key closes this help"))
	return b.String()
}

func truncate(s string, width int) string {
	r := []rune(s)
	if width <= 0 || len(r) <= width {
		return s
	}
	if width <= 2 {
		return string(r[:width])
	}
	return string(r[:width-2]) + ".."
}
