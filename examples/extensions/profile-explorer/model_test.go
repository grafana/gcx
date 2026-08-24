package main

import (
	"context"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

func newTestModel(t *testing.T) model {
	t.Helper()
	m := newModel(context.Background(), &gcxClient{bin: "gcx", context: "dev"}, startArgs{since: "1h", top: 10})
	next, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	return next.(model)
}

func key(s string) tea.KeyMsg {
	switch s {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "backspace":
		return tea.KeyMsg{Type: tea.KeyBackspace}
	case "up":
		return tea.KeyMsg{Type: tea.KeyUp}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	default:
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
	}
}

// send applies a sequence of key presses, discarding the commands they return.
func send(m model, keys ...string) model {
	for _, k := range keys {
		next, _ := m.Update(key(k))
		m = next.(model)
	}
	return m
}

func withData(t *testing.T, m model) model {
	t.Helper()
	next, _ := m.Update(datasourcesMsg{items: []datasource{
		{UID: "uid-a", Name: "profiles-a", Type: "grafana-pyroscope-datasource"},
		{UID: "uid-b", Name: "profiles-b", Type: "grafana-pyroscope-datasource"},
	}})
	m = next.(model)
	next, _ = m.Update(typesMsg{items: []profileType{
		{ID: "memory:inuse_space:bytes:space:bytes", Name: "memory", SampleType: "inuse_space", SampleUnit: "bytes"},
		{ID: "process_cpu:cpu:nanoseconds:cpu:nanoseconds", Name: "process_cpu", SampleType: "cpu", SampleUnit: "nanoseconds"},
	}})
	m = next.(model)
	next, _ = m.Update(servicesMsg{names: []string{"checkout", "frontend", "payments"}})
	return next.(model)
}

func withFlame(t *testing.T, m model) model {
	t.Helper()
	f, err := parseFlamegraph([]byte(testPayload), "nanoseconds")
	if err != nil {
		t.Fatalf("parseFlamegraph: %v", err)
	}
	m.reqID = 1
	next, _ := m.Update(flameMsg{id: 1, fg: f, took: 12 * time.Millisecond})
	return next.(model)
}

func TestWindowSizeDrivesTheRenderWidth(t *testing.T) {
	m := newTestModel(t)
	if m.nav.width != 120 {
		t.Errorf("nav width: got %d, want 120", m.nav.width)
	}
}

func TestDatasourceSelectionAdvancesToServices(t *testing.T) {
	m := withData(t, newTestModel(t))
	m = send(m, "down", "enter")

	if m.stage != stageServices {
		t.Fatalf("stage: got %v, want services", m.stage)
	}
	if m.ds.UID != "uid-b" {
		t.Errorf("datasource: got %q, want uid-b", m.ds.UID)
	}
	if m.loading != "services" {
		t.Errorf("loading: got %q, want services", m.loading)
	}
}

func TestDefaultProfileTypePrefersCPU(t *testing.T) {
	m := withData(t, newTestModel(t))
	if m.profileType.ID != "process_cpu:cpu:nanoseconds:cpu:nanoseconds" {
		t.Errorf("profile type: got %q, want the CPU one", m.profileType.ID)
	}
}

func TestFilterNarrowsTheServiceList(t *testing.T) {
	m := withData(t, newTestModel(t))
	m = send(m, "enter") // open the first datasource
	m = send(m, "p", "a", "y")

	got := m.visibleServices()
	if len(got) != 1 || got[0] != "payments" {
		t.Errorf("filtered services: got %v, want [payments]", got)
	}
	if m.overlay != overlayNone {
		t.Errorf("typing should not open an overlay, got %v", m.overlay)
	}

	m = send(m, "backspace")
	if m.filter != "pa" || len(m.visibleServices()) != 1 {
		t.Errorf("backspace should trim the filter, got %q with %d services", m.filter, len(m.visibleServices()))
	}

	m = send(m, "esc")
	if m.filter != "" || len(m.visibleServices()) != 3 {
		t.Errorf("esc should clear the filter, got %q with %d services", m.filter, len(m.visibleServices()))
	}
}

// The keys that used to move or quit are filter input on a list now.
func TestListLettersFilterRatherThanActing(t *testing.T) {
	m := withData(t, newTestModel(t))
	m = send(m, "q", "k")

	if m.filter != "qk" {
		t.Errorf("filter: got %q, want qk", m.filter)
	}
	if m.dsCursor != 0 {
		t.Errorf("cursor: got %d, want 0", m.dsCursor)
	}
}

func TestFilterResetsTheCursorAndSurvivesSelection(t *testing.T) {
	m := withData(t, newTestModel(t))
	m = send(m, "down", "b")

	if m.dsCursor != 0 {
		t.Errorf("cursor after filtering: got %d, want 0", m.dsCursor)
	}
	items := m.visibleDatasources()
	if len(items) != 1 || items[0].UID != "uid-b" {
		t.Fatalf("filtered datasources: got %v, want [uid-b]", items)
	}

	m = send(m, "enter")
	if m.ds.UID != "uid-b" {
		t.Errorf("datasource: got %q, want uid-b", m.ds.UID)
	}
	if m.filter != "" {
		t.Errorf("opening a datasource should clear the filter, got %q", m.filter)
	}
}

func TestSelectingAServiceBuildsTheQuery(t *testing.T) {
	m := withData(t, newTestModel(t))
	m = send(m, "enter", "down", "enter")

	if want := `{service_name="frontend"}`; m.expr != want {
		t.Errorf("expr: got %q, want %q", m.expr, want)
	}
	if m.loading != "profile" {
		t.Errorf("loading: got %q, want profile", m.loading)
	}
	if m.reqID != 1 {
		t.Errorf("reqID: got %d, want 1", m.reqID)
	}
}

// A slow response from an abandoned query must not replace the current one.
func TestStaleFlameResponseIsIgnored(t *testing.T) {
	m := withFlame(t, withData(t, newTestModel(t)))
	m.reqID = 2

	next, _ := m.Update(flameMsg{id: 1, err: context.DeadlineExceeded})
	m = next.(model)

	if m.err != "" {
		t.Errorf("stale error surfaced: %q", m.err)
	}
	if m.fg == nil {
		t.Error("stale response cleared the loaded profile")
	}
}

func TestFlameZoomKeys(t *testing.T) {
	m := withFlame(t, withData(t, newTestModel(t)))
	if m.stage != stageFlame {
		t.Fatalf("stage: got %v, want flame", m.stage)
	}
	// A loaded profile starts on the first frame below the root.
	if m.nav.selLevel != 1 {
		t.Fatalf("initial selection level: got %d, want 1", m.nav.selLevel)
	}

	m = send(m, "z")
	if m.nav.rootLevel != 1 || m.nav.rootFrame != 0 {
		t.Errorf("after z: root (%d,%d), want (1,0)", m.nav.rootLevel, m.nav.rootFrame)
	}
	m = send(m, "o")
	if m.nav.rootLevel != 0 {
		t.Errorf("after o: root level %d, want 0", m.nav.rootLevel)
	}

	m = send(m, "l")
	if m.nav.selFrame != 1 {
		t.Errorf("after l: selected frame %d, want 1", m.nav.selFrame)
	}
	m = send(m, "h")
	if m.nav.selFrame != 0 {
		t.Errorf("after h: selected frame %d, want 0", m.nav.selFrame)
	}
}

func TestSearchSelectsTheMatchingFrame(t *testing.T) {
	m := withFlame(t, withData(t, newTestModel(t)))
	m = send(m, "/", "e", "enter")

	if m.search != "e" {
		t.Fatalf("search term: got %q, want e", m.search)
	}
	sel, ok := m.nav.selection(m.fg)
	if !ok {
		t.Fatal("no selection after search")
	}
	if got := m.fg.name(sel); got != "e" {
		t.Errorf("selection: got %q, want e", got)
	}
}

func TestTopViewToggle(t *testing.T) {
	m := withFlame(t, withData(t, newTestModel(t)))
	m = send(m, "T")
	if !m.showTop {
		t.Fatal("T should switch to the top-functions table")
	}
	if !strings.Contains(m.View(), "function") {
		t.Error("top view should render its header")
	}
	m = send(m, "T")
	if m.showTop {
		t.Error("T should toggle back to the flamegraph")
	}
}

func TestProfileTypeOverlayRequeries(t *testing.T) {
	m := withFlame(t, withData(t, newTestModel(t)))
	m.expr = `{service_name="frontend"}`
	before := m.reqID

	m = send(m, "p")
	if m.overlay != overlayTypes {
		t.Fatalf("overlay: got %v, want types", m.overlay)
	}
	m = send(m, "k", "enter") // memory is above the CPU default
	if m.profileType.SampleUnit != "bytes" {
		t.Errorf("profile type: got %q, want the memory one", m.profileType.ID)
	}
	if m.reqID != before+1 {
		t.Errorf("changing profile type should re-query: reqID %d, want %d", m.reqID, before+1)
	}
}

func TestHelpOverlayClosesOnAnyKey(t *testing.T) {
	m := withData(t, newTestModel(t))
	m = send(m, "?")
	if m.overlay != overlayHelp {
		t.Fatalf("overlay: got %v, want help", m.overlay)
	}
	if !strings.Contains(m.View(), "zoom into the selected frame") {
		t.Error("help should list the flamegraph keys")
	}
	m = send(m, "x")
	if m.overlay != overlayNone {
		t.Error("any key should close help")
	}
}

func TestViewRendersWithoutData(t *testing.T) {
	m := newTestModel(t)
	if !strings.Contains(m.View(), "gcx profile explorer") {
		t.Error("header missing")
	}
	if !strings.Contains(m.View(), "loading datasources") {
		t.Error("first frame should say it is loading, not that there is nothing")
	}
}

// -d with --expr should land on the flamegraph without asking anything, but it
// still has to wait for the profile types to know the sample unit.
func TestStartArgsQueryWaitsForProfileTypes(t *testing.T) {
	m := newModel(context.Background(), &gcxClient{bin: "gcx"}, startArgs{
		datasource: "uid-a",
		expr:       `{service_name="frontend"}`,
		since:      "1h",
	})
	if m.stage != stageServices {
		t.Errorf("stage: got %v, want services (the datasource is already known)", m.stage)
	}
	if !m.pendingQuery {
		t.Fatal("a start-up expr should leave a query pending")
	}

	next, cmd := m.Update(typesMsg{items: []profileType{
		{ID: "process_cpu:cpu:nanoseconds:cpu:nanoseconds", SampleUnit: "nanoseconds"},
	}})
	m = next.(model)
	if cmd == nil {
		t.Error("the pending query should be issued once types arrive")
	}
	if m.pendingQuery {
		t.Error("pendingQuery should be cleared")
	}
	if m.loading != "profile" {
		t.Errorf("loading: got %q, want profile", m.loading)
	}
}
