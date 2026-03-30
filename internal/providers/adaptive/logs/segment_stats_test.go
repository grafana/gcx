package logs_test

import (
	"testing"

	"github.com/grafana/gcx/internal/providers/adaptive/logs"
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
	assert.Equal(t, "Alpha", got[0].Name)
	assert.Equal(t, uint64(125), got[0].Volume)

	assert.Equal(t, "b", got[1].ID)
	assert.Equal(t, "Beta", got[1].Name)
	assert.Equal(t, uint64(50), got[1].Volume)

	assert.Equal(t, "c", got[2].ID)
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
	assert.Equal(t, "(unknown)", got[0].Name)
	assert.Equal(t, uint64(42), got[0].Volume)
}

func TestAggregateSegmentVolumes_empty(t *testing.T) {
	t.Parallel()

	assert.Empty(t, logs.AggregateSegmentVolumes(nil, nil))
	assert.Empty(t, logs.AggregateSegmentVolumes([]logs.LogRecommendation{{Pattern: "x"}}, nil))
}
