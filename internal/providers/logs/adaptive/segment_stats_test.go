package logs_test

import (
	"testing"

	logs "github.com/grafana/gcx/internal/providers/logs/adaptive"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAggregateSegmentVolumes(t *testing.T) {
	t.Parallel()

	recs := []logs.LogRecommendation{
		{
			Segments: map[string]logs.Segment{
				"a": {Volume: 100},
				"b": {Volume: 50},
			},
		},
		{
			Segments: map[string]logs.Segment{
				"a": {Volume: 25},
				"c": {Volume: 10},
			},
		},
	}
	segments := []logs.LogSegment{
		{ID: "a", Name: "Alpha"},
		{ID: "b", Name: "Beta"},
		{ID: "c", Name: ""},
	}

	got := logs.AggregateSegmentVolumes(recs, segments)
	require.Len(t, got, 3)

	assert.Equal(t, "a", got[0].ID)
	assert.Equal(t, "a", got[0].SegmentID)
	assert.Equal(t, "Alpha", got[0].Name)
	assert.Equal(t, uint64(125), got[0].Volume)

	assert.Equal(t, "b", got[1].ID)
	assert.Equal(t, "b", got[1].SegmentID)
	assert.Equal(t, "Beta", got[1].Name)
	assert.Equal(t, uint64(50), got[1].Volume)

	assert.Equal(t, "c", got[2].ID)
	assert.Equal(t, "c", got[2].SegmentID)
	assert.Empty(t, got[2].Name)
	assert.Equal(t, uint64(10), got[2].Volume)
}

func TestAggregateSegmentVolumes_matchesLogSegmentSelector(t *testing.T) {
	t.Parallel()

	sel := `{namespace="prod"}`
	recs := []logs.LogRecommendation{
		{
			Segments: map[string]logs.Segment{
				sel: {Volume: 99},
			},
		},
	}
	segments := []logs.LogSegment{
		{ID: "uuid-1", Name: "Production", Selector: sel},
	}

	got := logs.AggregateSegmentVolumes(recs, segments)
	require.Len(t, got, 1)
	assert.Equal(t, sel, got[0].ID)
	assert.Equal(t, "uuid-1", got[0].SegmentID)
	assert.Equal(t, "Production", got[0].Name)
	assert.Equal(t, uint64(99), got[0].Volume)
}

func TestAggregateSegmentVolumes_unknownSegment(t *testing.T) {
	t.Parallel()

	recs := []logs.LogRecommendation{
		{
			Segments: map[string]logs.Segment{
				"orphan": {Volume: 42},
			},
		},
	}
	got := logs.AggregateSegmentVolumes(recs, nil)
	require.Len(t, got, 1)
	assert.Equal(t, "orphan", got[0].ID)
	assert.Empty(t, got[0].SegmentID)
	assert.Equal(t, "(unknown)", got[0].Name)
	assert.Equal(t, uint64(42), got[0].Volume)
}

func TestAggregateSegmentVolumes_empty(t *testing.T) {
	t.Parallel()

	assert.Empty(t, logs.AggregateSegmentVolumes(nil, nil))
	assert.Empty(t, logs.AggregateSegmentVolumes([]logs.LogRecommendation{{Pattern: "x"}}, nil))
}

func TestAggregateSegmentVolumes_topLevelVolumeAsDefaultSegment(t *testing.T) {
	t.Parallel()

	recs := []logs.LogRecommendation{
		{Volume: 100},
		{Volume: 40},
	}
	got := logs.AggregateSegmentVolumes(recs, nil)
	require.Len(t, got, 1)
	assert.Equal(t, "default", got[0].ID)
	assert.Empty(t, got[0].SegmentID)
	assert.Equal(t, "Default", got[0].Name)
	assert.Equal(t, uint64(140), got[0].Volume)
}

func TestAggregateSegmentVolumes_prefersPerSegmentMapOverTopLevelVolume(t *testing.T) {
	t.Parallel()

	recs := []logs.LogRecommendation{
		{Volume: 999, Segments: map[string]logs.Segment{"a": {Volume: 10}}},
	}
	got := logs.AggregateSegmentVolumes(recs, nil)
	require.Len(t, got, 1)
	assert.Equal(t, "a", got[0].ID)
	assert.Equal(t, uint64(10), got[0].Volume)
}
