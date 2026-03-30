package logs

import (
	"sort"
)

// filterPatternsBySegment returns recommendations whose Segments map contains segmentID.
func filterPatternsBySegment(recs []LogRecommendation, segmentID string) []LogRecommendation {
	if segmentID == "" {
		return recs
	}
	var out []LogRecommendation
	for _, rec := range recs {
		if _, ok := rec.Segments[segmentID]; ok {
			out = append(out, rec)
		}
	}
	return out
}

// SegmentPatternStat is per-segment aggregated volume across all pattern recommendations.
type SegmentPatternStat struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Volume uint64 `json:"volume"`
}

// AggregateSegmentVolumes sums Segment.Volume per segment ID across recommendations and
// attaches names from the segment catalog. IDs not present in segments get name "(unknown)".
func AggregateSegmentVolumes(recs []LogRecommendation, segments []LogSegment) []SegmentPatternStat {
	sums := make(map[string]uint64)
	for _, rec := range recs {
		for id, seg := range rec.Segments {
			sums[id] += seg.Volume
		}
	}

	knownID := make(map[string]bool)
	nameByID := make(map[string]string)
	for _, s := range segments {
		if s.ID == "" {
			continue
		}
		knownID[s.ID] = true
		nameByID[s.ID] = s.Name
	}

	out := make([]SegmentPatternStat, 0, len(sums))
	for id, vol := range sums {
		name := nameByID[id]
		if !knownID[id] {
			name = "(unknown)"
		}
		out = append(out, SegmentPatternStat{
			ID:     id,
			Name:   name,
			Volume: vol,
		})
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].Volume != out[j].Volume {
			return out[i].Volume > out[j].Volume
		}
		return out[i].ID < out[j].ID
	})

	return out
}
