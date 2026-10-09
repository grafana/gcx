package browse

import (
	"context"
	"errors"
	"fmt"
	"maps"

	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/grafana/gcx/internal/resources"
	"github.com/grafana/gcx/internal/style"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

const helpLine = "enter select · esc back · / filter · tab scroll preview · o open in Grafana · q quit"

type level int

const (
	levelTypes level = iota
	levelObjects
)

type focus int

const (
	focusList focus = iota
	focusPreview
)

// Background results. Each carries the selector it was fetched for so a late
// result is cached under its own type instead of replacing the current view.
type (
	typesLoadedMsg struct {
		types resources.Descriptors
		err   error
	}
	objectsLoadedMsg struct {
		key   string
		items []unstructured.Unstructured
		err   error
	}
	schemasLoadedMsg struct {
		schemas map[string]map[string]any
		err     error
	}
	openedMsg struct {
		url string
		err error
	}
)

// objectsEntry is a cached List result. Only results with objects are
// cached; a failed List is kept in objErr instead so the next drill-in
// retries it.
type objectsEntry struct {
	items []unstructured.Unstructured
	warn  string // set when some objects could not be fetched
}

type typeItem struct{ desc resources.Descriptor }

func (i typeItem) Title() string       { return i.desc.Kind }
func (i typeItem) Description() string { return Selector(i.desc) }
func (i typeItem) FilterValue() string { return i.desc.Kind + " " + Selector(i.desc) }

type objectItem struct{ obj unstructured.Unstructured }

func (i objectItem) Title() string { return i.obj.GetName() }

func (i objectItem) Description() string {
	if t := objectTitle(i.obj); t != "" {
		return t
	}
	return i.obj.GetKind()
}

func (i objectItem) FilterValue() string { return i.obj.GetName() + " " + objectTitle(i.obj) }

func objectTitle(o unstructured.Unstructured) string {
	t, _, _ := unstructured.NestedString(o.Object, "spec", "title")
	return t
}

// Model is the bubbletea model for the resource browser.
type Model struct {
	ctx  context.Context //nolint:containedctx // bubbletea commands outlive Update; the program's ctx is their only cancellation path.
	src  Source
	open Opener

	width, height int
	level         level
	focus         focus

	types   picker
	objects picker
	preview viewport.Model

	typesLoaded bool
	typesErr    error
	allTypes    resources.Descriptors
	current     resources.Descriptor

	objCache   map[string]objectsEntry
	objErr     map[string]error
	objPending map[string]bool

	// Schemas for every type are fetched in one request. After a failure,
	// the next fetch waits until the cursor leaves schemaErrKey, so a broken
	// endpoint costs one request per cursor move, never a retry loop.
	schemas        map[string]map[string]any
	schemasErr     error
	schemasPending bool
	schemaErrKey   string

	// previewKey identifies what the preview currently shows, so unchanged
	// content keeps its scroll position across updates.
	previewKey string
	status     string
}

// New returns a browser model that reads from src. open may be nil, in which
// case opening deep links is reported as unsupported.
func New(ctx context.Context, src Source, open Opener) *Model {
	pv := viewport.New()
	pv.SoftWrap = true
	return &Model{
		ctx:        ctx,
		src:        src,
		open:       open,
		types:      newPicker("Resource types"),
		objects:    newPicker(""),
		preview:    pv,
		objCache:   map[string]objectsEntry{},
		objErr:     map[string]error{},
		objPending: map[string]bool{},
		schemas:    map[string]map[string]any{},
	}
}

func (m *Model) Init() tea.Cmd {
	ctx, src := m.ctx, m.src
	return func() tea.Msg {
		t, err := src.Types(ctx)
		return typesLoadedMsg{types: t, err: err}
	}
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.resize()
		return m, nil

	case typesLoadedMsg:
		m.typesLoaded, m.typesErr, m.allTypes = true, msg.err, msg.types
		items := make([]pickerItem, 0, len(msg.types))
		for _, d := range msg.types {
			items = append(items, typeItem{desc: d})
		}
		m.types.setItems(items)
		return m, tea.Batch(m.fetchSchemas(), m.refreshPreview())

	case objectsLoadedMsg:
		delete(m.objPending, msg.key)
		var partial *PartialError
		switch {
		case msg.err == nil:
			m.objCache[msg.key] = objectsEntry{items: msg.items}
		case errors.As(msg.err, &partial) && len(msg.items) > 0:
			m.objCache[msg.key] = objectsEntry{items: msg.items, warn: msg.err.Error()}
		default:
			m.objErr[msg.key] = msg.err
		}
		if m.level != levelObjects || msg.key != Selector(m.current) {
			return m, nil
		}
		m.objects.setItems(objectItems(m.objCache[msg.key].items))
		return m, m.refreshPreview()

	case schemasLoadedMsg:
		m.schemasPending = false
		maps.Copy(m.schemas, msg.schemas)
		m.schemasErr = msg.err
		if it, ok := m.selectedType(); ok && msg.err != nil {
			m.schemaErrKey = Selector(it.desc)
		}
		return m, m.refreshPreview()

	case openedMsg:
		if msg.err != nil {
			m.status = "open failed: " + msg.err.Error()
		} else {
			m.status = "opened " + msg.url
		}
		return m, nil

	case tea.KeyPressMsg:
		return m.handleKey(msg)
	}

	// Anything else (cursor blinks) belongs to the active list.
	return m.updateList(msg)
}

func (m *Model) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	k := msg.String()
	if k == "ctrl+c" {
		return m, tea.Quit
	}

	// While typing a filter every key is filter input.
	if m.activeList().settingFilter() {
		return m.updateList(msg)
	}

	if m.focus == focusPreview {
		switch k {
		case "tab", "esc":
			m.focus = focusList
			return m, nil
		case "q":
			return m, tea.Quit
		case "o":
			return m.openSelected()
		}
		var cmd tea.Cmd
		m.preview, cmd = m.preview.Update(msg)
		return m, cmd
	}

	switch k {
	case "q":
		return m, tea.Quit
	case "tab":
		m.focus = focusPreview
		return m, nil
	case "enter":
		if m.level == levelTypes {
			return m.drillIn()
		}
	case "esc":
		if m.activeList().filterApplied() {
			return m.updateList(msg)
		}
		if m.level == levelObjects {
			m.level = levelTypes
			m.status = ""
			return m, m.refreshPreview()
		}
		return m, nil
	case "o":
		return m.openSelected()
	}
	return m.updateList(msg)
}

func (m *Model) drillIn() (tea.Model, tea.Cmd) {
	it, ok := m.selectedType()
	if !ok {
		return m, nil
	}
	m.level, m.focus, m.current, m.status = levelObjects, focusList, it.desc, ""
	m.objects.title = it.desc.Kind + " · " + Selector(it.desc)
	m.objects.resetFilter()

	key := Selector(it.desc)
	var cmds []tea.Cmd
	if e, cached := m.objCache[key]; cached {
		m.objects.setItems(objectItems(e.items))
	} else {
		m.objects.setItems(nil)
		if !m.objPending[key] {
			m.objPending[key] = true
			delete(m.objErr, key)
			ctx, src, desc := m.ctx, m.src, it.desc
			cmds = append(cmds, func() tea.Msg {
				items, err := src.List(ctx, desc)
				return objectsLoadedMsg{key: key, items: items, err: err}
			})
		}
	}
	cmds = append(cmds, m.refreshPreview())
	return m, tea.Batch(cmds...)
}

func (m *Model) openSelected() (tea.Model, tea.Cmd) {
	if m.level != levelObjects {
		return m, nil
	}
	it, ok := m.selectedObject()
	if !ok {
		return m, nil
	}
	url, _ := it.obj.Object["url"].(string)
	if url == "" {
		m.status = "no Grafana link for " + it.obj.GetName()
		return m, nil
	}
	if m.open == nil {
		m.status = "opening links is not supported here"
		return m, nil
	}
	open := m.open
	return m, func() tea.Msg { return openedMsg{url: url, err: open(url)} }
}

func (m *Model) updateList(msg tea.Msg) (tea.Model, tea.Cmd) {
	cmd := m.activeList().update(msg)
	return m, tea.Batch(cmd, m.refreshPreview())
}

func (m *Model) selectedType() (typeItem, bool) {
	it, ok := m.types.selected()
	if !ok {
		return typeItem{}, false
	}
	ti, ok := it.(typeItem)
	return ti, ok
}

func (m *Model) selectedObject() (objectItem, bool) {
	it, ok := m.objects.selected()
	if !ok {
		return objectItem{}, false
	}
	oi, ok := it.(objectItem)
	return oi, ok
}

func (m *Model) activeList() *picker {
	if m.level == levelTypes {
		return &m.types
	}
	return &m.objects
}

// refreshPreview recomputes the preview for the highlighted entry, returning
// a command when schemas need (re)fetching.
func (m *Model) refreshPreview() tea.Cmd {
	key, content, cmd := m.previewContent()
	if key != m.previewKey {
		m.previewKey = key
		m.preview.SetContent(content)
		m.preview.GotoTop()
	}
	return cmd
}

// fetchSchemas starts the single schema request for every type, unless one
// is already in flight.
func (m *Model) fetchSchemas() tea.Cmd {
	if m.schemasPending || len(m.allTypes) == 0 {
		return nil
	}
	m.schemasPending = true
	ctx, src, descs := m.ctx, m.src, m.allTypes
	return func() tea.Msg {
		s, err := src.Schemas(ctx, descs)
		return schemasLoadedMsg{schemas: s, err: err}
	}
}

func (m *Model) previewContent() (string, string, tea.Cmd) {
	if m.level == levelObjects {
		return m.objectPreview()
	}
	switch {
	case !m.typesLoaded:
		return "types:loading", "Loading resource types…", nil
	case m.typesErr != nil:
		return "types:error", "Error loading resource types: " + m.typesErr.Error(), nil
	}
	it, ok := m.selectedType()
	if !ok {
		return "types:none", "No resource types match.", nil
	}
	key := Selector(it.desc)
	if sch, found := m.schemas[key]; found {
		body, err := renderYAML(sch)
		if err != nil {
			return "schema:" + key, "Error rendering schema: " + err.Error(), nil
		}
		return "schema:" + key, fmt.Sprintf("# %s spec schema\n%s", it.desc.Kind, body), nil
	}
	switch {
	case m.schemasPending:
		return "schema:loading:" + key, "Loading schemas…", nil
	case m.schemasErr != nil:
		var cmd tea.Cmd
		if key != m.schemaErrKey {
			cmd = m.fetchSchemas()
		}
		return "schema:error:" + key, "Error loading schemas: " + m.schemasErr.Error(), cmd
	}
	return "schema:none:" + key, "no schema available for " + it.desc.Kind, nil
}

func (m *Model) objectPreview() (string, string, tea.Cmd) {
	key := Selector(m.current)
	e, cached := m.objCache[key]
	if !cached {
		if err := m.objErr[key]; err != nil && !m.objPending[key] {
			return "objects:error:" + key, "Error listing " + m.current.Plural + ": " + err.Error() + "\n\nPress esc, then enter to retry.", nil
		}
		return "objects:loading:" + key, "Loading " + m.current.Plural + "…", nil
	}
	if len(e.items) == 0 {
		return "objects:empty:" + key, "No " + m.current.Plural + " found.", nil
	}
	it, ok := m.selectedObject()
	if !ok {
		return "objects:none:" + key, "No matches.", nil
	}
	pk := "object:" + key + "/" + it.obj.GetName()
	body, err := RenderObject(it.obj)
	if err != nil {
		return pk, "Error rendering object: " + err.Error(), nil
	}
	return pk, body, nil
}

func objectItems(objs []unstructured.Unstructured) []pickerItem {
	items := make([]pickerItem, 0, len(objs))
	for _, o := range objs {
		items = append(items, objectItem{obj: o})
	}
	return items
}

func (m *Model) layout() (int, int, int) {
	leftW := m.width * 2 / 5
	return leftW, m.width - leftW, max(m.height-1, 0)
}

func (m *Model) resize() {
	leftW, rightW, bodyH := m.layout()
	m.types.setSize(leftW, bodyH)
	m.objects.setSize(leftW, bodyH)
	m.preview.SetWidth(max(rightW-2, 0))
	m.preview.SetHeight(max(bodyH-2, 0))
}

func (m *Model) View() tea.View {
	if m.width == 0 || m.height == 0 {
		return tea.NewView("")
	}
	leftW, rightW, bodyH := m.layout()

	left := lipgloss.NewStyle().Width(leftW).Height(bodyH).MaxHeight(bodyH).
		Render(m.activeList().view())

	border := style.ColorBorder
	if m.focus == focusPreview {
		border = style.ColorPrimary
	}
	right := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).BorderForeground(border).
		Width(rightW).Height(bodyH).MaxHeight(bodyH).
		Render(m.preview.View())

	footer := helpLine
	if m.status != "" {
		footer = m.status + "  ·  " + helpLine
	}
	if m.level == levelObjects {
		if w := m.objCache[Selector(m.current)].warn; w != "" {
			footer = "warning: " + w + "  ·  " + footer
		}
	}
	footer = lipgloss.NewStyle().Foreground(style.ColorMuted).Width(m.width).MaxWidth(m.width).MaxHeight(1).Render(footer)

	v := tea.NewView(lipgloss.JoinVertical(lipgloss.Left,
		lipgloss.JoinHorizontal(lipgloss.Top, left, right),
		footer,
	))
	v.AltScreen = true
	return v
}
