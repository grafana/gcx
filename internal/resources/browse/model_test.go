package browse_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/grafana/gcx/internal/resources"
	"github.com/grafana/gcx/internal/resources/browse"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

//nolint:gochecknoglobals // immutable test fixtures
var (
	dashboards = resources.Descriptor{
		GroupVersion: schema.GroupVersion{Group: "dashboard.grafana.app", Version: "v1"},
		Kind:         "Dashboard", Singular: "dashboard", Plural: "dashboards",
	}
	folders = resources.Descriptor{
		GroupVersion: schema.GroupVersion{Group: "folder.grafana.app", Version: "v1"},
		Kind:         "Folder", Singular: "folder", Plural: "folders",
	}
)

func obj(kind, name, title, url string) unstructured.Unstructured {
	o := map[string]any{
		"apiVersion": "v1",
		"kind":       kind,
		"metadata":   map[string]any{"name": name},
		"spec":       map[string]any{"title": title},
	}
	if url != "" {
		o["url"] = url
	}
	return unstructured.Unstructured{Object: o}
}

// fakeSource records calls; lists and schemas are keyed by plural.
type fakeSource struct {
	mu          sync.Mutex
	types       resources.Descriptors
	typesErr    error
	lists       map[string][]unstructured.Unstructured
	listErrs    map[string]error
	schemas     map[string]map[string]any // keyed by plural
	schemasErr  error
	listCalls   map[string]int
	schemaCalls int
	// partial, when set for a plural, is returned alongside its items.
	partial map[string]error
	// gate, when set for a plural, blocks List until it is closed.
	gate map[string]chan struct{}
}

func newFakeSource() *fakeSource {
	return &fakeSource{
		types: resources.Descriptors{dashboards, folders},
		lists: map[string][]unstructured.Unstructured{
			"dashboards": {
				obj("Dashboard", "abc", "Alpha", "https://g.example/d/abc"),
				obj("Dashboard", "xyz", "Zulu", ""),
			},
			"folders": {obj("Folder", "f1", "Team", "")},
		},
		listErrs:  map[string]error{},
		schemas:   map[string]map[string]any{"dashboards": {"type": "object", "description": "dashboard spec"}},
		listCalls: map[string]int{},
		partial:   map[string]error{},
		gate:      map[string]chan struct{}{},
	}
}

func (f *fakeSource) Types(context.Context) (resources.Descriptors, error) {
	return f.types, f.typesErr
}

func (f *fakeSource) List(_ context.Context, d resources.Descriptor) ([]unstructured.Unstructured, error) {
	f.mu.Lock()
	f.listCalls[d.Plural]++
	gate := f.gate[d.Plural]
	f.mu.Unlock()
	if gate != nil {
		<-gate
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.listErrs[d.Plural]; err != nil {
		return nil, err
	}
	return f.lists[d.Plural], f.partial[d.Plural]
}

func (f *fakeSource) Schemas(_ context.Context, descs resources.Descriptors) (map[string]map[string]any, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.schemaCalls++
	out := map[string]map[string]any{}
	for _, d := range descs {
		if s, ok := f.schemas[d.Plural]; ok {
			out[browse.Selector(d)] = s
		}
	}
	return out, f.schemasErr
}

func (f *fakeSource) set(fn func()) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fn()
}

func (f *fakeSource) schemaCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.schemaCalls
}

func (f *fakeSource) dashboardLists() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.listCalls["dashboards"]
}

// harness drives a model the way tea.Program would: it runs commands and
// feeds the browser's own messages back in. Unrelated commands (cursor
// blinks, timers) are dropped after a short timeout.
type harness struct {
	t *testing.T
	m tea.Model
}

func newHarness(t *testing.T, src browse.Source, open browse.Opener) *harness {
	t.Helper()
	h := &harness{t: t, m: browse.New(context.Background(), src, open)}
	h.send(tea.WindowSizeMsg{Width: 120, Height: 30})
	h.run(h.m.Init())
	return h
}

func (h *harness) send(msg tea.Msg) {
	h.t.Helper()
	var cmd tea.Cmd
	h.m, cmd = h.m.Update(msg)
	h.run(cmd)
}

func (h *harness) run(cmd tea.Cmd) {
	h.t.Helper()
	for _, msg := range collect(cmd) {
		h.send(msg)
	}
}

// sendAsync updates the model but returns the command unexecuted, so a test
// can control when a background result arrives.
func (h *harness) sendAsync(msg tea.Msg) tea.Cmd {
	var cmd tea.Cmd
	h.m, cmd = h.m.Update(msg)
	return cmd
}

func collect(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	ch := make(chan tea.Msg, 1)
	go func() { ch <- cmd() }()
	var msg tea.Msg
	select {
	case msg = <-ch:
	case <-time.After(200 * time.Millisecond):
		return nil
	}
	if batch, ok := msg.(tea.BatchMsg); ok {
		var out []tea.Msg
		for _, c := range batch {
			out = append(out, collect(c)...)
		}
		return out
	}
	if browse.IsBrowserMsg(msg) {
		return []tea.Msg{msg}
	}
	return nil
}

// collectAll is collect without the browser-message filter, for asserting
// on framework messages such as tea.QuitMsg.
func collectAll(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	ch := make(chan tea.Msg, 1)
	go func() { ch <- cmd() }()
	select {
	case msg := <-ch:
		if batch, ok := msg.(tea.BatchMsg); ok {
			var out []tea.Msg
			for _, c := range batch {
				out = append(out, collectAll(c)...)
			}
			return out
		}
		return []tea.Msg{msg}
	case <-time.After(200 * time.Millisecond):
		return nil
	}
}

func (h *harness) view() string {
	return h.m.View().Content
}

func key(s string) tea.Msg {
	switch s {
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	case "tab":
		return tea.KeyPressMsg{Code: tea.KeyTab}
	case "down":
		return tea.KeyPressMsg{Code: tea.KeyDown}
	case "ctrl+c":
		return tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}
	}
	r := []rune(s)[0]
	return tea.KeyPressMsg{Code: r, Text: s}
}

func TestModel_TypesLoadAndSchemaPreview(t *testing.T) {
	src := newFakeSource()
	h := newHarness(t, src, nil)

	v := h.view()
	assert.Contains(t, v, "Dashboard")
	assert.Contains(t, v, "folders.v1.folder.grafana.app")
	assert.Contains(t, v, "dashboard spec", "schema of the highlighted type is previewed")

	// Moving the cursor must not fetch again: schemas are fetched once, in
	// one bounded request, for every type.
	h.send(key("down"))
	assert.Contains(t, h.view(), "no schema available")
	h.send(tea.KeyPressMsg{Code: tea.KeyUp})
	h.send(key("down"))
	assert.Equal(t, 1, src.schemaCount())
}

func TestModel_SchemaErrorRetriesOnCursorMove(t *testing.T) {
	src := newFakeSource()
	want := src.schemas
	src.schemas = map[string]map[string]any{}
	src.schemasErr = errors.New("openapi index unavailable")
	h := newHarness(t, src, nil)
	assert.Contains(t, h.view(), "openapi index unavailable")

	src.set(func() { src.schemas, src.schemasErr = want, nil })
	h.send(key("down"))
	h.send(tea.KeyPressMsg{Code: tea.KeyUp})
	assert.Contains(t, h.view(), "dashboard spec")
	assert.Equal(t, 2, src.schemaCount())
}

func TestModel_SchemaPartialResultShowsWhatResolved(t *testing.T) {
	// A failed server fetch can still carry provider-registered schemas.
	src := newFakeSource()
	src.schemasErr = errors.New("openapi index unavailable")
	h := newHarness(t, src, nil)
	h.send(key("down"))
	h.send(tea.KeyPressMsg{Code: tea.KeyUp})
	assert.Contains(t, h.view(), "dashboard spec")
}

func TestModel_TypesError(t *testing.T) {
	src := newFakeSource()
	src.typesErr = errors.New("discovery exploded")
	h := newHarness(t, src, nil)
	assert.Contains(t, h.view(), "discovery exploded")
}

func TestModel_DrillInBackAndCache(t *testing.T) {
	src := newFakeSource()
	h := newHarness(t, src, nil)

	h.send(key("enter"))
	v := h.view()
	assert.Contains(t, v, "abc")
	assert.Contains(t, v, "Alpha")
	assert.Contains(t, v, "name: abc", "highlighted object rendered as YAML")

	h.send(key("down"))
	assert.Contains(t, h.view(), "name: xyz", "preview follows the cursor")

	h.send(key("esc"))
	assert.Contains(t, h.view(), "dashboard spec", "esc returns to the type level")

	h.send(key("enter"))
	assert.Equal(t, 1, src.dashboardLists(), "second drill-in uses the cache")
}

func TestModel_ListErrorStaysUsable(t *testing.T) {
	src := newFakeSource()
	src.listErrs["dashboards"] = errors.New("403 forbidden")
	h := newHarness(t, src, nil)

	h.send(key("enter"))
	assert.Contains(t, h.view(), "403 forbidden")

	h.send(key("esc"))
	h.send(key("down"))
	h.send(key("enter"))
	assert.Contains(t, h.view(), "name: f1")
}

func TestModel_ListErrorIsRetried(t *testing.T) {
	src := newFakeSource()
	src.listErrs["dashboards"] = errors.New("connection reset")
	h := newHarness(t, src, nil)

	h.send(key("enter"))
	assert.Contains(t, h.view(), "connection reset")

	src.set(func() { delete(src.listErrs, "dashboards") })
	h.send(key("esc"))
	h.send(key("enter"))
	assert.Contains(t, h.view(), "name: abc", "a failed list is not cached")
	assert.Equal(t, 2, src.dashboardLists())

	h.send(key("esc"))
	h.send(key("enter"))
	assert.Equal(t, 2, src.dashboardLists(), "a successful list is cached")
}

func TestModel_PartialListShowsItemsAndWarning(t *testing.T) {
	src := newFakeSource()
	src.partial["dashboards"] = &browse.PartialError{Failed: 2, Err: errors.New("403 on v0alpha1")}
	h := newHarness(t, src, nil)

	h.send(key("enter"))
	v := h.view()
	assert.Contains(t, v, "name: abc")
	assert.Contains(t, v, "2 failed")
	assert.Contains(t, v, "403 on v0alpha1")

	h.send(key("esc"))
	assert.NotContains(t, h.view(), "2 failed", "the warning belongs to that type's object list")
}

func TestModel_StaleResultDoesNotReplaceCurrentType(t *testing.T) {
	src := newFakeSource()
	src.gate["dashboards"] = make(chan struct{})
	h := newHarness(t, src, nil)

	slow := h.sendAsync(key("enter")) // dashboards list blocked on the gate
	h.send(key("esc"))
	h.send(key("down"))
	h.send(key("enter")) // folders
	require.Contains(t, h.view(), "name: f1")

	close(src.gate["dashboards"])
	h.run(slow)
	v := h.view()
	assert.Contains(t, v, "name: f1", "late dashboards result must not replace folders")
	assert.NotContains(t, v, "name: abc")

	// The late result was cached, not dropped.
	h.send(key("esc"))
	h.send(tea.KeyPressMsg{Code: tea.KeyUp})
	h.send(key("enter"))
	assert.Contains(t, h.view(), "name: abc")
	assert.Equal(t, 1, src.dashboardLists())
}

func TestModel_Open(t *testing.T) {
	tests := []struct {
		name       string
		down       int
		wantURL    string
		wantStatus string
	}{
		{name: "object with url", down: 0, wantURL: "https://g.example/d/abc", wantStatus: "opened"},
		{name: "object without url", down: 1, wantURL: "", wantStatus: "no Grafana link"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var mu sync.Mutex
			var opened []string
			opener := func(url string) error {
				mu.Lock()
				defer mu.Unlock()
				opened = append(opened, url)
				return nil
			}
			h := newHarness(t, newFakeSource(), opener)
			h.send(key("enter"))
			for range tc.down {
				h.send(key("down"))
			}
			h.send(key("o"))

			mu.Lock()
			defer mu.Unlock()
			if tc.wantURL == "" {
				assert.Empty(t, opened)
			} else {
				assert.Equal(t, []string{tc.wantURL}, opened)
			}
			assert.Contains(t, h.view(), tc.wantStatus)
		})
	}
}

func TestModel_OpenError(t *testing.T) {
	opener := func(string) error { return errors.New("no browser") }
	h := newHarness(t, newFakeSource(), opener)
	h.send(key("enter"))
	h.send(key("o"))
	assert.Contains(t, h.view(), "no browser")
}

func TestModel_Quit(t *testing.T) {
	for _, k := range []string{"q", "ctrl+c"} {
		t.Run(k, func(t *testing.T) {
			h := newHarness(t, newFakeSource(), nil)
			_, cmd := h.m.Update(key(k))
			require.NotNil(t, cmd)
			_, ok := cmd().(tea.QuitMsg)
			assert.True(t, ok)
		})
	}
}

func TestModel_FilterCapturesKeys(t *testing.T) {
	h := newHarness(t, newFakeSource(), nil)
	h.send(key("/"))
	for _, k := range []string{"f", "o", "l", "q"} {
		cmd := h.sendAsync(key(k))
		for _, msg := range collectAll(cmd) {
			_, quit := msg.(tea.QuitMsg)
			require.False(t, quit, "%q while filtering must not quit", k)
			if browse.IsBrowserMsg(msg) {
				h.send(msg)
			}
		}
	}
	// "folq" matches nothing; backspace to "fol" leaves only folders.
	h.send(tea.KeyPressMsg{Code: tea.KeyBackspace})
	h.send(key("enter"))
	v := h.view()
	assert.Contains(t, v, "1/2")
	assert.Contains(t, v, "no schema available for Folder", "preview follows the filtered selection")

	// esc clears the applied filter before it navigates anywhere.
	h.send(key("esc"))
	assert.Contains(t, h.view(), "2 items")
}

func TestModel_FitsWindow(t *testing.T) {
	h := newHarness(t, newFakeSource(), nil)
	h.send(tea.WindowSizeMsg{Width: 80, Height: 20})
	h.send(key("enter"))
	lines := strings.Split(h.view(), "\n")
	assert.LessOrEqual(t, len(lines), 20)
}
