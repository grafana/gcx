package logs

import (
	"sort"
)

// defaultSegmentStatsKey is the aggregation bucket for recommendations that only have
// top-level Volume; the API often omits a per-segment map for the default segment.
const defaultSegmentStatsKey = "default"

// filterPatternsBySegment returns recommendations whose Segments map contains a key matching
// segmentRef. When catalog is non-nil, segmentRef may be a catalog LogSegment.ID or a selector
// string; matching keys from the catalog are tried so maps keyed only by selector still match
// when the user passes --segment <uuid>.
func filterPatternsBySegment(recs []LogRecommendation, segmentRef string, catalog []LogSegment) []LogRecommendation {
	if segmentRef == "" {
		return recs
	}
	matchKeys := []string{segmentRef}
	for _, s := range catalog {
		if s.ID == segmentRef && s.Selector != "" {
			matchKeys = append(matchKeys, s.Selector)
		}
		if s.Selector == segmentRef && s.ID != "" {
			matchKeys = append(matchKeys, s.ID)
		}
	}
	seen := make(map[string]bool)
	var keys []string
	for _, k := range matchKeys {
		if k == "" || seen[k] {
			continue
		}
		seen[k] = true
		keys = append(keys, k)
	}
	var out []LogRecommendation
	for _, rec := range recs {
		for _, k := range keys {
			if _, ok := rec.Segments[k]; ok {
				out = append(out, rec)
				break
			}
		}
	}
	return out
}

// SegmentPatternStat is per-segment aggregated volume across all pattern recommendations.
type SegmentPatternStat struct {
	// ID is the recommendation API map key (often a LogQL selector string).
	ID string `json:"id"`
	// SegmentID is the catalog LogSegment id when the key resolved to a known segment; use with
	// `gcx logs adaptive patterns show --segment <SegmentID>` when the API keys by selector.
	SegmentID string `json:"segment_id,omitempty"`
	Name      string `json:"name"`
	Volume    uint64 `json:"volume"`
}

// AggregateSegmentVolumes sums Segment.Volume per segment key across recommendations and
// attaches names from the segment catalog. Recommendation keys are often LogQL selectors
// that match LogSegment.Selector rather than LogSegment.ID; we resolve names by both ID and
// Selector. Keys with no catalog match get name "(unknown)".
func AggregateSegmentVolumes(recs []LogRecommendation, segments []LogSegment) []SegmentPatternStat {
	sums := make(map[string]uint64)
	for _, rec := range recs {
		if len(rec.Segments) > 0 {
			for id, seg := range rec.Segments {
				sums[id] += seg.Volume
			}
			continue
		}
		if rec.Volume > 0 {
			sums[defaultSegmentStatsKey] += rec.Volume
		}
	}

	known := make(map[string]bool)
	nameByKey := make(map[string]string)
	catalogIDByKey := make(map[string]string)
	for _, s := range segments {
		name := s.Name
		if s.ID != "" {
			known[s.ID] = true
			nameByKey[s.ID] = name
			catalogIDByKey[s.ID] = s.ID
		}
		if s.Selector != "" {
			known[s.Selector] = true
			nameByKey[s.Selector] = name
			if s.ID != "" {
				catalogIDByKey[s.Selector] = s.ID
			}
		}
	}

	out := make([]SegmentPatternStat, 0, len(sums))
	for id, vol := range sums {
		name := nameByKey[id]
		switch {
		case id == defaultSegmentStatsKey:
			name = "Default"
		case !known[id]:
			name = "(unknown)"
		}
		out = append(out, SegmentPatternStat{
			ID:        id,
			SegmentID: catalogIDByKey[id],
			Name:      name,
			Volume:    vol,
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
