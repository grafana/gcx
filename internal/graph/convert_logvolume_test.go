package graph_test

import (
	"strconv"
	"testing"
	"time"

	"github.com/grafana/gcx/internal/graph"
	"github.com/grafana/gcx/internal/query/loki"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFromLokiLogVolumeResponse_BucketsByLevel(t *testing.T) {
	resp := &loki.QueryResponse{
		Data: loki.QueryResultData{
			Result: []loki.StreamEntry{
				{
					Stream: map[string]string{"app": "foo"},
					Values: []loki.LogEntry{
						{Timestamp: "1000000000", Line: "ok", StructuredMetadata: map[string]string{"detected_level": "info"}},
						{Timestamp: "2000000000", Line: "ERROR broke"},
						{Timestamp: "3000000000", Line: "ok again", StructuredMetadata: map[string]string{"detected_level": "info"}},
					},
				},
			},
		},
	}

	data, err := graph.FromLokiLogVolumeResponse(resp)
	require.NoError(t, err)
	require.Len(t, data.Series, 2)

	total := 0
	for _, series := range data.Series {
		for _, p := range series.Points {
			total += int(p.Value)
		}
		assert.NotNil(t, series.Color)
	}
	assert.Equal(t, 3, total)
}

// A rare level with a single occurrence must still produce a full,
// zero-filled series spanning the whole bucket range — not a single point —
// otherwise a line chart has nothing to draw a line through and the level
// silently disappears from the chart despite showing correctly in the legend.
func TestFromLokiLogVolumeResponse_SparseLevelIsZeroFilledNotSinglePoint(t *testing.T) {
	values := make([]loki.LogEntry, 0, 100)
	for i := range 99 {
		values = append(values, loki.LogEntry{
			Timestamp:          strconv.Itoa((i + 1) * int(time.Second)),
			Line:               "steady state",
			StructuredMetadata: map[string]string{"detected_level": "info"},
		})
	}
	values = append(values, loki.LogEntry{
		Timestamp:          strconv.Itoa(50 * int(time.Second)),
		Line:               "rare warning",
		StructuredMetadata: map[string]string{"detected_level": "warning"},
	})

	resp := &loki.QueryResponse{
		Data: loki.QueryResultData{
			Result: []loki.StreamEntry{{Stream: map[string]string{"app": "foo"}, Values: values}},
		},
	}

	data, err := graph.FromLokiLogVolumeResponse(resp)
	require.NoError(t, err)
	require.Len(t, data.Series, 2)

	var infoPoints, warningPoints int
	var warningHasNonZero bool
	for _, series := range data.Series {
		switch series.Name {
		case "info":
			infoPoints = len(series.Points)
		case "warning":
			warningPoints = len(series.Points)
			for _, p := range series.Points {
				if p.Value > 0 {
					warningHasNonZero = true
				}
			}
		}
	}

	require.NotZero(t, infoPoints)
	assert.Equal(t, infoPoints, warningPoints, "sparse level must span the same bucket range as a dense one, not just its own occurrences")
	assert.Greater(t, warningPoints, 1, "a single-point series can't be rendered as a line")
	assert.True(t, warningHasNonZero, "the warning bucket itself must still carry its real count")
}

func TestFromLokiLogVolumeResponse_EmptyResponse(t *testing.T) {
	data, err := graph.FromLokiLogVolumeResponse(&loki.QueryResponse{})
	require.NoError(t, err)
	assert.Empty(t, data.Series)

	data, err = graph.FromLokiLogVolumeResponse(nil)
	require.NoError(t, err)
	assert.Empty(t, data.Series)
}

func TestFromLokiLogVolumeResponse_SkipsUnparsableTimestamps(t *testing.T) {
	resp := &loki.QueryResponse{
		Data: loki.QueryResultData{
			Result: []loki.StreamEntry{
				{
					Stream: map[string]string{"app": "foo"},
					Values: []loki.LogEntry{
						{Timestamp: "not-a-number", Line: "bad entry"},
						{Timestamp: "1000000000", Line: "good entry"},
					},
				},
			},
		},
	}

	data, err := graph.FromLokiLogVolumeResponse(resp)
	require.NoError(t, err)
	require.Len(t, data.Series, 1)
	assert.InDelta(t, 1.0, data.Series[0].Points[0].Value, 0)
}
