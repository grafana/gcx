package main

import (
	"errors"
	"testing"
)

// testPayload is a four-level profile shaped like this (widths in samples):
//
//	total 100
//	├─ a 60 (self 10)
//	│  ├─ c 30 (self 30)
//	│  └─ d 30 (self 20)
//	│     └─ e 10 (self 10)
//	└─ b 40 (self 40)
//
// Offsets are written the way Pyroscope sends them: gaps from the previous
// frame's right edge, not absolute positions.
const testPayload = `{
  "flamegraph": {
    "names": ["total", "a", "b", "c", "d", "e"],
    "levels": [
      {"values": ["0", "100", "0", "0"]},
      {"values": ["0", "60", "10", "1", "0", "40", "40", "2"]},
      {"values": ["0", "30", "30", "3", "0", "30", "20", "4"]},
      {"values": ["30", "10", "10", "5"]}
    ],
    "total": "100",
    "maxSelf": "40"
  }
}`

func testFlame(t *testing.T) *flame {
	t.Helper()
	f, err := parseFlamegraph([]byte(testPayload), "nanoseconds")
	if err != nil {
		t.Fatalf("parseFlamegraph: %v", err)
	}
	return f
}

func TestParseFlamegraphResolvesGapsToOffsets(t *testing.T) {
	f := testFlame(t)

	tests := []struct {
		level int
		want  []frame
	}{
		{0, []frame{{X: 0, Total: 100, Self: 0, Name: 0}}},
		{1, []frame{{X: 0, Total: 60, Self: 10, Name: 1}, {X: 60, Total: 40, Self: 40, Name: 2}}},
		{2, []frame{{X: 0, Total: 30, Self: 30, Name: 3}, {X: 30, Total: 30, Self: 20, Name: 4}}},
		{3, []frame{{X: 30, Total: 10, Self: 10, Name: 5}}},
	}
	for _, tc := range tests {
		got := f.levels[tc.level].frames
		if len(got) != len(tc.want) {
			t.Fatalf("level %d: got %d frames, want %d", tc.level, len(got), len(tc.want))
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Errorf("level %d frame %d: got %+v, want %+v", tc.level, i, got[i], tc.want[i])
			}
		}
	}
	if f.total != 100 {
		t.Errorf("total: got %d, want 100", f.total)
	}
}

func TestParseFlamegraphErrors(t *testing.T) {
	tests := []struct {
		name    string
		payload string
		want    error
	}{
		{"empty flamegraph", `{"flamegraph": {"names": [], "levels": []}}`, errNoProfileData},
		{"no flamegraph key", `{"other": 1}`, errNoProfileData},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := parseFlamegraph([]byte(tc.payload), ""); !errors.Is(err, tc.want) {
				t.Errorf("got %v, want %v", err, tc.want)
			}
		})
	}
}

func TestParseFlamegraphAcceptsBareNumbers(t *testing.T) {
	payload := `{"flamegraph": {"names": ["total", "a"], "levels": [
		{"values": [0, 100, 0, 0]}, {"values": [0, 100, 100, 1]}], "total": 100}}`
	f, err := parseFlamegraph([]byte(payload), "")
	if err != nil {
		t.Fatalf("parseFlamegraph: %v", err)
	}
	if got := f.levels[1].frames[0].Self; got != 100 {
		t.Errorf("self: got %d, want 100", got)
	}
}

func TestNavMovement(t *testing.T) {
	f := testFlame(t)

	tests := []struct {
		name      string
		steps     []func(*nav)
		wantLevel int
		wantFrame int
	}{
		{"deeper from root selects the first child", []func(*nav){
			func(n *nav) { n.moveDeeper(f) },
		}, 1, 0},
		{"right moves to the next sibling", []func(*nav){
			func(n *nav) { n.moveDeeper(f) },
			func(n *nav) { n.moveRight(f) },
		}, 1, 1},
		{"right stops at the last sibling", []func(*nav){
			func(n *nav) { n.moveDeeper(f) },
			func(n *nav) { n.moveRight(f) },
			func(n *nav) { n.moveRight(f) },
		}, 1, 1},
		{"left returns to the previous sibling", []func(*nav){
			func(n *nav) { n.moveDeeper(f) },
			func(n *nav) { n.moveRight(f) },
			func(n *nav) { n.moveLeft(f) },
		}, 1, 0},
		{"deeper follows the selected subtree", []func(*nav){
			func(n *nav) { n.moveDeeper(f) },
			func(n *nav) { n.moveDeeper(f) },
		}, 2, 0},
		{"shallower selects the containing caller", []func(*nav){
			func(n *nav) { n.moveDeeper(f) },
			func(n *nav) { n.moveDeeper(f) },
			func(n *nav) { n.moveRight(f) },
			func(n *nav) { n.moveShallower(f) },
		}, 1, 0},
		{"shallower stops at the root level", []func(*nav){
			func(n *nav) { n.moveShallower(f) },
		}, 0, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			n := &nav{width: 100}
			for _, step := range tc.steps {
				step(n)
			}
			if n.selLevel != tc.wantLevel || n.selFrame != tc.wantFrame {
				t.Errorf("selection: got (%d,%d), want (%d,%d)", n.selLevel, n.selFrame, tc.wantLevel, tc.wantFrame)
			}
		})
	}
}

func TestNavSkipsFramesNarrowerThanACell(t *testing.T) {
	// b is 40% of the profile but only 1 sample wide once the viewport is 200
	// cells: a 1-sample frame rounds to zero cells, so navigation must skip it.
	payload := `{"flamegraph": {"names": ["total", "a", "b", "c"], "levels": [
		{"values": ["0", "1000", "0", "0"]},
		{"values": ["0", "600", "600", "1", "0", "1", "1", "2", "0", "399", "399", "3"]}],
		"total": "1000"}}`
	f, err := parseFlamegraph([]byte(payload), "")
	if err != nil {
		t.Fatalf("parseFlamegraph: %v", err)
	}
	n := &nav{width: 200}
	n.moveDeeper(f)
	n.moveRight(f)
	if got := f.name(mustSelection(t, n, f)); got != "c" {
		t.Errorf("after moveRight: selected %q, want %q", got, "c")
	}
}

func TestNavZoom(t *testing.T) {
	f := testFlame(t)
	n := &nav{width: 100}
	n.moveDeeper(f) // a

	n.zoomIn(f)
	if x, w := n.rootBounds(f); x != 0 || w != 60 {
		t.Errorf("zoomed root bounds: got (%d,%d), want (0,60)", x, w)
	}
	if n.selLevel != 2 || n.selFrame != 0 {
		t.Errorf("zoomIn should select the first callee, got (%d,%d)", n.selLevel, n.selFrame)
	}
	// b sits outside a's range, so it is not reachable while zoomed in.
	if n.visible(f, 1, 1) {
		t.Error("frame b should not be visible inside a's zoom")
	}

	n.zoomOut(f)
	if n.rootLevel != 0 || n.rootFrame != 0 {
		t.Errorf("zoomOut: got root (%d,%d), want (0,0)", n.rootLevel, n.rootFrame)
	}
	if x, w := n.rootBounds(f); x != 0 || w != 100 {
		t.Errorf("root bounds after zoomOut: got (%d,%d), want (0,100)", x, w)
	}
}

func TestNavZoomOutPicksTheContainingParent(t *testing.T) {
	f := testFlame(t)
	n := &nav{width: 100}
	n.moveDeeper(f) // a
	n.moveDeeper(f) // c
	n.moveRight(f)  // d
	n.zoomIn(f)     // root = d, level 2
	n.zoomOut(f)    // root should become a, not level 1 frame 0 by luck

	if n.rootLevel != 1 || n.rootFrame != 0 {
		t.Fatalf("root after zoomOut: got (%d,%d), want (1,0)", n.rootLevel, n.rootFrame)
	}
	if x, w := n.rootBounds(f); x != 0 || w != 60 {
		t.Errorf("root bounds: got (%d,%d), want (0,60)", x, w)
	}
}

func TestTopFunctions(t *testing.T) {
	f := testFlame(t)

	tests := []struct {
		name  string
		setup func(*nav)
		limit int
		want  []string
	}{
		{"whole profile, heaviest self first", func(*nav) {}, 0, []string{"b", "c", "d", "a", "e"}},
		{"limit applies after sorting", func(*nav) {}, 2, []string{"b", "c"}},
		{"zoomed viewport drops other subtrees", func(n *nav) {
			n.moveDeeper(f)
			n.zoomIn(f)
		}, 0, []string{"c", "d", "a", "e"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			n := &nav{width: 100}
			tc.setup(n)
			rows := topFunctions(f, n, tc.limit)
			if len(rows) != len(tc.want) {
				t.Fatalf("got %d rows %v, want %d %v", len(rows), names(rows), len(tc.want), tc.want)
			}
			for i, want := range tc.want {
				if rows[i].Name != want {
					t.Errorf("row %d: got %q, want %q", i, rows[i].Name, want)
				}
			}
		})
	}
}

func TestCountMatches(t *testing.T) {
	f := testFlame(t)
	n := &nav{width: 100}

	tests := []struct {
		term string
		want int
	}{
		{"", 0},
		{"c", 1},
		{"total", 1},
		{"zzz", 0},
	}
	for _, tc := range tests {
		if got := countMatches(f, n, tc.term); got != tc.want {
			t.Errorf("countMatches(%q): got %d, want %d", tc.term, got, tc.want)
		}
	}
}

func mustSelection(t *testing.T, n *nav, f *flame) frame {
	t.Helper()
	fr, ok := n.selection(f)
	if !ok {
		t.Fatal("no selection")
	}
	return fr
}

func names(rows []topRow) []string {
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.Name)
	}
	return out
}

// Descending should follow the hot path: the widest callee, not the leftmost.
func TestMoveDeeperPicksTheWidestCallee(t *testing.T) {
	payload := `{"flamegraph": {"names": ["total", "a", "narrow", "wide"], "levels": [
		{"values": ["0", "100", "0", "0"]},
		{"values": ["0", "100", "0", "1"]},
		{"values": ["0", "20", "20", "2", "0", "80", "80", "3"]}],
		"total": "100"}}`
	f, err := parseFlamegraph([]byte(payload), "")
	if err != nil {
		t.Fatalf("parseFlamegraph: %v", err)
	}
	n := &nav{width: 100}
	n.moveDeeper(f)
	n.moveDeeper(f)
	if got := f.name(mustSelection(t, n, f)); got != "wide" {
		t.Errorf("selected %q, want %q", got, "wide")
	}
}
