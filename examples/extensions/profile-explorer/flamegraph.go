package main

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// flexInt decodes a JSON number that may arrive quoted: gcx's flamebearer
// payload emits sample counts as strings.
type flexInt int64

func (v *flexInt) UnmarshalJSON(b []byte) error {
	s := strings.Trim(strings.TrimSpace(string(b)), `"`)
	if s == "" || s == "null" {
		*v = 0
		return nil
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		f, ferr := strconv.ParseFloat(s, 64)
		if ferr != nil {
			return fmt.Errorf("decoding %q as a number: %w", s, err)
		}
		n = int64(f)
	}
	*v = flexInt(n)
	return nil
}

// flamebearerResponse is the subset of `gcx datasources pyroscope query
// --output json` this extension reads.
type flamebearerResponse struct {
	Flamegraph struct {
		Names  []string `json:"names"`
		Levels []struct {
			Values []flexInt `json:"values"`
		} `json:"levels"`
		Total   flexInt `json:"total"`
		MaxSelf flexInt `json:"maxSelf"`
	} `json:"flamegraph"`
}

// frame is one function occurrence in the profile. X and Total are in sample
// units; Name indexes flame.names.
type frame struct {
	X     int64
	Total int64
	Self  int64
	Name  int
}

type level struct {
	frames []frame
}

// flame is a decoded flamebearer: levels are ordered root-first, and within a
// level frames are ordered left to right with absolute X offsets.
type flame struct {
	levels  []level
	names   []string
	total   int64
	maxSelf int64
	unit    string
}

// parseFlamegraph decodes a flamebearer payload. Pyroscope encodes each level
// as groups of four values, [gap, total, self, nameIndex], where gap is the
// distance from the previous frame's right edge rather than an absolute
// position; this resolves the gaps into absolute X offsets once so that
// navigation and rendering can compare ranges directly.
func parseFlamegraph(data []byte, unit string) (*flame, error) {
	var resp flamebearerResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("decoding flamegraph: %w", err)
	}
	fb := resp.Flamegraph
	if len(fb.Levels) == 0 || len(fb.Names) == 0 {
		return nil, errNoProfileData
	}

	f := &flame{
		names:   fb.Names,
		total:   int64(fb.Total),
		maxSelf: int64(fb.MaxSelf),
		unit:    unit,
		levels:  make([]level, 0, len(fb.Levels)),
	}
	for _, lv := range fb.Levels {
		frames := make([]frame, 0, len(lv.Values)/4)
		var x int64
		for i := 0; i+3 < len(lv.Values); i += 4 {
			x += int64(lv.Values[i])
			name := int(lv.Values[i+3])
			if name < 0 || name >= len(fb.Names) {
				name = 0
			}
			frames = append(frames, frame{
				X:     x,
				Total: int64(lv.Values[i+1]),
				Self:  int64(lv.Values[i+2]),
				Name:  name,
			})
			x += int64(lv.Values[i+1])
		}
		f.levels = append(f.levels, level{frames: frames})
	}
	if f.total == 0 && len(f.levels[0].frames) > 0 {
		f.total = f.levels[0].frames[0].Total
	}
	return f, nil
}

func (f *flame) name(fr frame) string {
	if fr.Name < len(f.names) {
		return f.names[fr.Name]
	}
	return "?"
}

func (f *flame) frameAt(lvl, idx int) (frame, bool) {
	if lvl < 0 || lvl >= len(f.levels) {
		return frame{}, false
	}
	if idx < 0 || idx >= len(f.levels[lvl].frames) {
		return frame{}, false
	}
	return f.levels[lvl].frames[idx], true
}

// nav is the zoom and selection state. rootLevel/rootFrame pick the frame that
// fills the viewport; selLevel/selFrame is the highlighted frame.
type nav struct {
	rootLevel int
	rootFrame int
	selLevel  int
	selFrame  int
	// width is the render width in cells, used to skip frames that would round
	// down to zero cells and so cannot be seen.
	width int
}

func (n *nav) reset() {
	n.rootLevel, n.rootFrame, n.selLevel, n.selFrame = 0, 0, 0, 0
}

// rootBounds returns the X offset and width in samples of the zoom root.
func (n *nav) rootBounds(f *flame) (int64, int64) {
	if fr, ok := f.frameAt(n.rootLevel, n.rootFrame); ok && fr.Total > 0 {
		return fr.X, fr.Total
	}
	return 0, f.total
}

func (n *nav) selection(f *flame) (frame, bool) {
	return f.frameAt(n.selLevel, n.selFrame)
}

// minWidth is the narrowest frame that still occupies a cell.
func (n *nav) minWidth(rootWidth int64) int64 {
	if n.width <= 0 {
		return 0
	}
	return rootWidth / int64(n.width)
}

func (n *nav) visible(f *flame, lvl, idx int) bool {
	fr, ok := f.frameAt(lvl, idx)
	if !ok {
		return false
	}
	rootX, rootW := n.rootBounds(f)
	return fr.Total > 0 && fr.X+fr.Total > rootX && fr.X < rootX+rootW
}

func (n *nav) moveRight(f *flame) {
	if n.selLevel >= len(f.levels) {
		return
	}
	_, rootW := n.rootBounds(f)
	min := n.minWidth(rootW)
	for i := n.selFrame + 1; i < len(f.levels[n.selLevel].frames); i++ {
		if n.visible(f, n.selLevel, i) && f.levels[n.selLevel].frames[i].Total > min {
			n.selFrame = i
			return
		}
	}
}

func (n *nav) moveLeft(f *flame) {
	if n.selLevel >= len(f.levels) {
		return
	}
	_, rootW := n.rootBounds(f)
	min := n.minWidth(rootW)
	for i := n.selFrame - 1; i >= 0; i-- {
		if n.visible(f, n.selLevel, i) && f.levels[n.selLevel].frames[i].Total > min {
			n.selFrame = i
			return
		}
	}
}

// moveDeeper selects the widest callee of the current selection, so holding j
// walks down the hot path rather than down the leftmost edge of the profile.
func (n *nav) moveDeeper(f *flame) {
	next := n.selLevel + 1
	if next >= len(f.levels) {
		return
	}
	sel, ok := n.selection(f)
	if !ok {
		return
	}
	_, rootW := n.rootBounds(f)
	min := n.minWidth(rootW)
	best, bestWidth := -1, int64(0)
	for i, fr := range f.levels[next].frames {
		if fr.Total > min && fr.X+fr.Total > sel.X && fr.X < sel.X+sel.Total && fr.Total > bestWidth {
			best, bestWidth = i, fr.Total
		}
	}
	if best >= 0 {
		n.selLevel, n.selFrame = next, best
	}
}

// moveShallower selects the caller of the current selection, stopping at the
// zoom root.
func (n *nav) moveShallower(f *flame) {
	if n.selLevel == 0 || n.selLevel <= n.rootLevel {
		return
	}
	prev := n.selLevel - 1
	sel, ok := n.selection(f)
	if !ok {
		return
	}
	_, rootW := n.rootBounds(f)
	min := n.minWidth(rootW)
	for i, fr := range f.levels[prev].frames {
		if n.visible(f, prev, i) && fr.Total > min && fr.X <= sel.X && fr.X+fr.Total >= sel.X+sel.Total {
			n.selLevel, n.selFrame = prev, i
			return
		}
	}
	for i := range f.levels[prev].frames {
		if n.visible(f, prev, i) && f.levels[prev].frames[i].Total > min {
			n.selLevel, n.selFrame = prev, i
			return
		}
	}
}

// zoomIn makes the selected frame fill the viewport.
func (n *nav) zoomIn(f *flame) {
	if _, ok := n.selection(f); !ok {
		return
	}
	n.rootLevel, n.rootFrame = n.selLevel, n.selFrame
	n.moveDeeper(f)
}

// zoomOut widens the viewport by one level, keeping the selection inside it.
func (n *nav) zoomOut(f *flame) {
	if n.rootLevel == 0 {
		n.reset()
		return
	}
	parent := n.rootLevel - 1
	root, ok := f.frameAt(n.rootLevel, n.rootFrame)
	n.rootLevel = parent
	n.rootFrame = 0
	if ok {
		for i, fr := range f.levels[parent].frames {
			if fr.X <= root.X && fr.X+fr.Total >= root.X+root.Total {
				n.rootFrame = i
				break
			}
		}
	}
	if n.selLevel < n.rootLevel {
		n.selLevel, n.selFrame = n.rootLevel, n.rootFrame
	}
}

// topRow is one line of the top-functions table.
type topRow struct {
	Name  string
	Self  int64
	Total int64
}

// topFunctions aggregates self time by function within the current zoom
// viewport, heaviest first.
func topFunctions(f *flame, n *nav, limit int) []topRow {
	rootX, rootW := n.rootBounds(f)
	self := map[int]int64{}
	total := map[int]int64{}
	for lvl := n.rootLevel; lvl < len(f.levels); lvl++ {
		for _, fr := range f.levels[lvl].frames {
			if fr.Total <= 0 || fr.X+fr.Total <= rootX || fr.X >= rootX+rootW {
				continue
			}
			self[fr.Name] += fr.Self
			total[fr.Name] += fr.Total
		}
	}
	rows := make([]topRow, 0, len(self))
	for name, s := range self {
		if s == 0 {
			continue
		}
		rows = append(rows, topRow{Name: f.names[name], Self: s, Total: total[name]})
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Self != rows[j].Self {
			return rows[i].Self > rows[j].Self
		}
		return rows[i].Name < rows[j].Name
	})
	if limit > 0 && len(rows) > limit {
		rows = rows[:limit]
	}
	return rows
}

// countMatches reports how many visible frames contain the search term.
func countMatches(f *flame, n *nav, term string) int {
	if term == "" {
		return 0
	}
	term = strings.ToLower(term)
	rootX, rootW := n.rootBounds(f)
	count := 0
	for lvl := n.rootLevel; lvl < len(f.levels); lvl++ {
		for _, fr := range f.levels[lvl].frames {
			if fr.Total <= 0 || fr.X+fr.Total <= rootX || fr.X >= rootX+rootW {
				continue
			}
			if strings.Contains(strings.ToLower(f.name(fr)), term) {
				count++
			}
		}
	}
	return count
}
