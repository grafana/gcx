package logs //nolint:testpackage // Tests unexported table codecs and filterPatternsBySegment.

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPatternsTableCodec_TopNAndRollup(t *testing.T) {
	t.Parallel()

	recs := []LogRecommendation{
		{Pattern: "low", Volume: 10},
		{Pattern: "mid", Volume: 100},
		{Pattern: "high", Volume: 1000},
		{Pattern: "top", Volume: 10000},
	}

	opts := &patternsShowOpts{TopN: 2}
	codec := &patternsTableCodec{wide: false, opts: opts}

	var buf bytes.Buffer
	require.NoError(t, codec.Encode(&buf, recs))

	out := buf.String()
	assert.Contains(t, out, "top")
	assert.Contains(t, out, "high")
	assert.NotContains(t, out, "mid\t")
	assert.NotContains(t, out, "low\t")
	assert.Contains(t, out, "Everything else (2 patterns)")
	assert.Contains(t, out, "110 B") // 100+10
}

func TestPatternsTableCodec_TopZeroShowsAll(t *testing.T) {
	t.Parallel()

	recs := []LogRecommendation{
		{Pattern: "a", Volume: 1},
		{Pattern: "b", Volume: 2},
	}
	opts := &patternsShowOpts{TopN: 0}
	codec := &patternsTableCodec{wide: false, opts: opts}

	var buf bytes.Buffer
	require.NoError(t, codec.Encode(&buf, recs))

	out := buf.String()
	assert.Contains(t, out, "b")
	assert.Contains(t, out, "a")
	assert.NotContains(t, strings.ToLower(out), "everything else")
}

func TestFilterPatternsBySegment(t *testing.T) {
	t.Parallel()

	recs := []LogRecommendation{
		{Pattern: "p1", Segments: map[string]Segment{"a": {}, "b": {}}},
		{Pattern: "p2", Segments: map[string]Segment{"b": {}}},
	}
	assert.Len(t, filterPatternsBySegment(recs, "a", nil), 1)
	assert.Len(t, filterPatternsBySegment(recs, "b", nil), 2)
	assert.Len(t, filterPatternsBySegment(recs, "", nil), 2)
	assert.Empty(t, filterPatternsBySegment(recs, "none", nil))
}

func TestFilterPatternsBySegment_resolvesCatalogIDToSelectorKey(t *testing.T) {
	t.Parallel()

	sel := `{namespace="prod"}`
	recs := []LogRecommendation{
		{Pattern: "p1", Segments: map[string]Segment{sel: {Volume: 1}}},
	}
	catalog := []LogSegment{{ID: "uuid-42", Name: "Prod", Selector: sel}}
	assert.Len(t, filterPatternsBySegment(recs, "uuid-42", catalog), 1)
	assert.Empty(t, filterPatternsBySegment(recs, "uuid-99", catalog))
}

func TestSegmentStatsTableCodec(t *testing.T) {
	t.Parallel()

	stats := []SegmentPatternStat{
		{ID: "s1", SegmentID: "seg-1", Name: "One", Volume: 1024},
	}
	codec := &segmentStatsTableCodec{wide: false}

	var buf bytes.Buffer
	require.NoError(t, codec.Encode(&buf, stats))

	out := buf.String()
	lines := strings.Split(strings.TrimSpace(out), "\n")
	require.NotEmpty(t, lines)
	hdr := lines[0]
	assert.Contains(t, hdr, "ID")
	assert.Contains(t, hdr, "NAME")
	assert.Contains(t, hdr, "SEGMENT")
	assert.Contains(t, hdr, "VOLUME")
	assert.Contains(t, out, "seg-1")
	assert.Contains(t, out, "s1")
	assert.Contains(t, out, "One")
	assert.Contains(t, out, "1.00 KB")
}

func TestPatternsTableCodec_RecommendedRateAsterisk(t *testing.T) {
	t.Parallel()

	recs := []LogRecommendation{
		{Pattern: "match", Volume: 100, ConfiguredDropRate: 0, RecommendedDropRate: 50, IngestedLines: 10, QueriedLines: 1},
	}

	opts := &patternsShowOpts{TopN: 0}
	codec := &patternsTableCodec{wide: false, opts: opts}

	var buf bytes.Buffer
	require.NoError(t, codec.Encode(&buf, recs))

	out := buf.String()
	assert.Contains(t, out, "50.00 *")
	assert.Contains(t, out, "* Recommended rate differs from drop rate by more than 10 percentage points.")
}
