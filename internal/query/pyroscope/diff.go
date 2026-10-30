package pyroscope

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strconv"
	"time"

	"github.com/grafana/gcx/internal/style"
)

// ProfileSelection identifies one side of a comparison. Times are resolved once,
// before I/O, and shared by the RPC, result, and links.
type ProfileSelection struct {
	Selector string    `json:"selector"`
	From     time.Time `json:"from"`
	To       time.Time `json:"to"`
}

type DiffRequest struct {
	Before      ProfileSelection
	After       ProfileSelection
	ProfileType string
	MaxNodes    int64
}

// DiffFlamegraph uses SEVEN values per node, unlike a single flamegraph's four:
// left offset/total/self, right offset/total/self, name index. Offsets are delta
// encoded independently on each side. Omitted proto-JSON tick counts mean zero.
type DiffFlamegraph struct {
	Names      []string `json:"names"`
	Levels     []Level  `json:"levels"`
	LeftTicks  int64    `json:"leftTicks,string"`
	RightTicks int64    `json:"rightTicks,string"`
}

type DiffResponse struct {
	Flamegraph *DiffFlamegraph `json:"flamegraph"`
}

// Diff executes Pyroscope's public, server-side tree comparison. The same
// profile type and node budget apply to both sides; costs are not normalized.
func (c *Client) Diff(ctx context.Context, datasourceUID string, req DiffRequest) (*DiffResponse, error) {
	selection := func(s ProfileSelection) map[string]any {
		return map[string]any{
			"labelSelector": s.Selector,
			"profileTypeID": req.ProfileType,
			"start":         strconv.FormatInt(s.From.UnixMilli(), 10),
			"end":           strconv.FormatInt(s.To.UnixMilli(), 10),
			"maxNodes":      strconv.FormatInt(req.MaxNodes, 10),
		}
	}
	body, err := json.Marshal(map[string]any{"left": selection(req.Before), "right": selection(req.After)})
	if err != nil {
		return nil, fmt.Errorf("failed to marshal profile diff request: %w", err)
	}
	var result DiffResponse
	if err := c.postQuery(ctx, datasourceUID, "querier.v1.QuerierService/Diff", "profile diff", body, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// CostDiff holds raw sample units, not durations normalized by window length or
// request count. ChangePercent is undefined for zero baselines or empty sides.
type CostDiff struct {
	Before        int64    `json:"before"`
	After         int64    `json:"after"`
	Delta         int64    `json:"delta"`
	ChangePercent *float64 `json:"changePercent"`
}

type SymbolDiff struct {
	Name  string   `json:"name"`
	Self  CostDiff `json:"self"`
	Total CostDiff `json:"total"`
}

type DiffResult struct {
	Type          string            `json:"type"`
	SchemaVersion int               `json:"schema_version"`
	ProfileType   string            `json:"profileType"`
	Unit          string            `json:"unit"`
	Before        ProfileSelection  `json:"before"`
	After         ProfileSelection  `json:"after"`
	Total         CostDiff          `json:"total"`
	Omitted       CostDiff          `json:"omitted"`
	Complete      bool              `json:"complete"`
	MaxNodes      int64             `json:"maxNodes"`
	Symbols       []SymbolDiff      `json:"symbols"`
	Warnings      []string          `json:"warnings"`
	Links         map[string]string `json:"links"`
}

func costDiff(before, after int64, bothPresent bool) CostDiff {
	cost := CostDiff{Before: before, After: after, Delta: after - before}
	if bothPresent && before > 0 {
		percent := float64(cost.Delta) / float64(before) * 100
		cost.ChangePercent = &percent
	}
	return cost
}

type diffSpan struct {
	start, end, self int64
}

type diffFrame struct {
	name          string
	before, after diffSpan
	parent        *diffFrame
}

func decodeDiffSpan(offset, total, self, previousEnd, ticks int64) (diffSpan, error) {
	if offset > ticks-previousEnd {
		return diffSpan{}, errors.New("profile diff offset exceeds tick counts")
	}
	start := previousEnd + offset
	if total > ticks-start {
		return diffSpan{}, errors.New("profile diff node exceeds tick counts")
	}
	return diffSpan{start: start, end: start + total, self: self}, nil
}

func decodeDiffNode(values []string, fg *DiffFlamegraph, leftEnd, rightEnd int64) (*diffFrame, error) {
	var v [7]int64
	for i := range v {
		n, err := strconv.ParseInt(values[i], 10, 64)
		if err != nil || n < 0 {
			return nil, errors.New("invalid profile diff value")
		}
		v[i] = n
	}
	if v[6] >= int64(len(fg.Names)) || v[2] > v[1] || v[5] > v[4] {
		return nil, errors.New("invalid profile diff node")
	}
	before, err := decodeDiffSpan(v[0], v[1], v[2], leftEnd, fg.LeftTicks)
	if err != nil {
		return nil, err
	}
	after, err := decodeDiffSpan(v[3], v[4], v[5], rightEnd, fg.RightTicks)
	if err != nil {
		return nil, err
	}
	return &diffFrame{name: fg.Names[v[6]], before: before, after: after}, nil
}

func attachDiffParent(frame *diffFrame, previous []*diffFrame, index int) (int, error) {
	if frame.before.end == frame.before.start && frame.after.end == frame.after.start {
		return index, nil
	}
	// The aligned trees have the same topology, including zero-width nodes
	// on the side where a stack is absent. Compare both coordinates instead
	// of adding them, which could overflow for two large int64 tick counts.
	for index < len(previous) && previous[index].before.end <= frame.before.start && previous[index].after.end <= frame.after.start {
		index++
	}
	if index >= len(previous) {
		return index, errors.New("profile diff node has no parent")
	}
	parent := previous[index]
	if parent.before.start > frame.before.start || parent.before.end < frame.before.end ||
		parent.after.start > frame.after.start || parent.after.end < frame.after.end {
		return index, errors.New("profile diff node has no parent")
	}
	frame.parent = parent
	return index, nil
}

func decodeDiffFrames(fg *DiffFlamegraph) ([]*diffFrame, error) {
	var previous, all []*diffFrame
	for depth, level := range fg.Levels {
		if len(level.Values)%7 != 0 {
			return nil, fmt.Errorf("invalid profile diff level %d: expected seven values per node", depth)
		}
		frames := make([]*diffFrame, 0, len(level.Values)/7)
		var leftEnd, rightEnd int64
		parentIndex := 0
		for i := 0; i < len(level.Values); i += 7 {
			frame, err := decodeDiffNode(level.Values[i:i+7], fg, leftEnd, rightEnd)
			if err != nil {
				return nil, fmt.Errorf("level %d node %d: %w", depth, i/7, err)
			}
			leftEnd, rightEnd = frame.before.end, frame.after.end
			frames = append(frames, frame)
			if depth == 0 {
				if len(level.Values) != 7 || leftEnd != fg.LeftTicks || rightEnd != fg.RightTicks || frame.before.self != 0 || frame.after.self != 0 {
					return nil, errors.New("invalid profile diff root")
				}
				continue
			}
			parentIndex, err = attachDiffParent(frame, previous, parentIndex)
			if err != nil {
				return nil, fmt.Errorf("level %d: %w", depth, err)
			}
			all = append(all, frame)
		}
		previous = frames
	}
	return all, nil
}

// BuildDiffResult reduces only the bounded graph returned by the backend. Self
// costs are additive. Inclusive costs count only the outermost occurrence of a
// symbol on each stack, so recursion does not count the same samples twice.
func BuildDiffResult(resp *DiffResponse, req DiffRequest) (*DiffResult, error) {
	if resp == nil || resp.Flamegraph == nil {
		return nil, errors.New("profile diff response is missing its flamegraph")
	}
	fg := resp.Flamegraph
	if fg.LeftTicks < 0 || fg.RightTicks < 0 {
		return nil, errors.New("profile diff contains negative tick counts")
	}
	bothPresent := fg.LeftTicks > 0 && fg.RightTicks > 0
	result := &DiffResult{
		Type: "profile_diff", SchemaVersion: 1,
		ProfileType: req.ProfileType, Unit: sampleUnitFromProfileType(req.ProfileType),
		Before: req.Before, After: req.After, MaxNodes: req.MaxNodes,
		Total:    costDiff(fg.LeftTicks, fg.RightTicks, bothPresent),
		Complete: true, Symbols: []SymbolDiff{}, Warnings: []string{}, Links: map[string]string{},
	}
	stats := map[string]*SymbolDiff{}
	frames, err := decodeDiffFrames(fg)
	if err != nil {
		return nil, err
	}
	var omittedBefore, omittedAfter, selfBefore, selfAfter int64
	for _, frame := range frames {
		if frame.before.self > fg.LeftTicks-selfBefore || frame.after.self > fg.RightTicks-selfAfter {
			return nil, errors.New("profile diff self costs exceed tick counts")
		}
		selfBefore, selfAfter = selfBefore+frame.before.self, selfAfter+frame.after.self
		if frame.name == "other" {
			omittedBefore += frame.before.self
			omittedAfter += frame.after.self
			continue
		}
		stat := stats[frame.name]
		if stat == nil {
			stat = &SymbolDiff{Name: frame.name}
			stats[frame.name] = stat
		}
		stat.Self.Before += frame.before.self
		stat.Self.After += frame.after.self
		recursive := false
		for parent := frame.parent; parent != nil; parent = parent.parent {
			if parent.parent != nil && parent.name == frame.name {
				recursive = true
				break
			}
		}
		if !recursive {
			stat.Total.Before += frame.before.end - frame.before.start
			stat.Total.After += frame.after.end - frame.after.start
		}
	}
	if selfBefore != fg.LeftTicks || selfAfter != fg.RightTicks {
		return nil, errors.New("profile diff self costs do not match tick counts")
	}
	for _, stat := range stats {
		stat.Self = costDiff(stat.Self.Before, stat.Self.After, bothPresent)
		stat.Total = costDiff(stat.Total.Before, stat.Total.After, bothPresent)
		result.Symbols = append(result.Symbols, *stat)
	}
	slices.SortFunc(result.Symbols, func(a, b SymbolDiff) int {
		// Deltas of two non-negative int64 costs cannot be math.MinInt64.
		abs := func(n int64) int64 {
			if n < 0 {
				return -n
			}
			return n
		}
		if order := cmp.Compare(abs(b.Self.Delta), abs(a.Self.Delta)); order != 0 {
			return order
		}
		return cmp.Compare(a.Name, b.Name)
	})
	result.Omitted = costDiff(omittedBefore, omittedAfter, bothPresent)
	result.Complete = omittedBefore == 0 && omittedAfter == 0
	if !result.Complete {
		result.Warnings = append(result.Warnings, "backend pruned frames into 'other'; symbol costs are partial, increase --max-nodes or narrow the selectors")
	}
	if fg.LeftTicks == 0 || fg.RightTicks == 0 {
		result.Warnings = append(result.Warnings, "at least one profile has no positive samples; missing data does not establish zero cost or an improvement")
	}
	if req.Before.To.Sub(req.Before.From) != req.After.To.Sub(req.After.From) {
		result.Warnings = append(result.Warnings, "time windows have different lengths; costs are raw sums, not normalized by duration")
	}
	return result, nil
}

// FormatDiffTable keeps the human projection short. Machine codecs retain all
// symbols in the fetched graph; no format changes what is queried or reduced.
func FormatDiffTable(w io.Writer, result *DiffResult) error {
	if _, err := fmt.Fprintf(w, "Unit: %s | before: %d | after: %d | delta: %+d (after - before)\n", result.Unit, result.Total.Before, result.Total.After, result.Total.Delta); err != nil {
		return err
	}
	t := style.NewTable("SYMBOL", "BEFORE SELF", "AFTER SELF", "SELF DELTA", "SELF %", "BEFORE TOTAL", "AFTER TOTAL", "TOTAL DELTA")
	limit := min(20, len(result.Symbols))
	for _, s := range result.Symbols[:limit] {
		percent := "n/a"
		if s.Self.ChangePercent != nil {
			percent = fmt.Sprintf("%+.1f%%", *s.Self.ChangePercent)
		}
		t.Row(s.Name, strconv.FormatInt(s.Self.Before, 10), strconv.FormatInt(s.Self.After, 10), fmt.Sprintf("%+d", s.Self.Delta), percent,
			strconv.FormatInt(s.Total.Before, 10), strconv.FormatInt(s.Total.After, 10), fmt.Sprintf("%+d", s.Total.Delta))
	}
	if err := t.Render(w); err != nil {
		return err
	}
	if len(result.Symbols) > limit {
		_, err := fmt.Fprintf(w, "Showing %d of %d symbols by absolute self delta; use -o json for all returned symbols\n", limit, len(result.Symbols))
		return err
	}
	return nil
}
