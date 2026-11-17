package pyroscope_test

import (
	"strconv"
	"testing"

	"github.com/grafana/gcx/internal/query/pyroscope"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExtractTopFunctionsRecursion(t *testing.T) {
	type testNode struct {
		name                string
		offset, total, self int
	}
	node := func(name string, offset, total, self int) testNode {
		return testNode{name: name, offset: offset, total: total, self: self}
	}
	tests := []struct {
		name   string
		levels [][]testNode
		want   map[string][2]int64 // self, cumulative
	}{
		{"direct", [][]testNode{{node("total", 0, 10, 0)}, {node("A", 0, 10, 0)}, {node("A", 0, 10, 0)}, {node("leaf", 0, 10, 10)}}, map[string][2]int64{"total": {0, 10}, "A": {0, 10}, "leaf": {10, 10}}},
		{"indirect", [][]testNode{{node("total", 0, 10, 0)}, {node("A", 0, 10, 0)}, {node("B", 0, 10, 0)}, {node("A", 0, 10, 10)}}, map[string][2]int64{"total": {0, 10}, "A": {10, 10}, "B": {0, 10}}},
		{"self at multiple depths", [][]testNode{{node("total", 0, 10, 0)}, {node("A", 0, 10, 3)}, {node("A", 3, 7, 7)}}, map[string][2]int64{"total": {0, 10}, "A": {10, 10}}},
		{"independent branches and relative offsets", [][]testNode{
			{node("total", 0, 30, 0)},
			{node("B", 0, 10, 0), node("leaf", 5, 15, 0)},
			{node("A", 0, 10, 4), node("A", 5, 15, 6)},
			{node("A", 4, 6, 6), node("A", 6, 9, 9)},
		}, map[string][2]int64{"total": {0, 30}, "A": {25, 25}, "B": {0, 10}, "leaf": {0, 15}}},
		{"other preserves offsets", [][]testNode{{node("total", 0, 30, 0)}, {node("other", 0, 10, 10), node("A", 5, 15, 5)}, {node("A", 20, 10, 10)}}, map[string][2]int64{"total": {0, 30}, "A": {15, 15}}},
		{"non recursive", [][]testNode{{node("total", 0, 30, 0)}, {node("A", 0, 10, 10), node("B", 0, 20, 20)}}, map[string][2]int64{"total": {0, 30}, "A": {10, 10}, "B": {20, 20}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fg := &pyroscope.Flamegraph{Names: []string{"total", "A", "B", "leaf", "other"}, Total: int64(tt.levels[0][0].total)}
			for _, values := range tt.levels {
				level := pyroscope.Level{}
				for _, n := range values {
					nameIndex := 0
					for fg.Names[nameIndex] != n.name {
						nameIndex++
					}
					for _, v := range []int{n.offset, n.total, n.self, nameIndex} {
						level.Values = append(level.Values, strconv.Itoa(v))
					}
				}
				fg.Levels = append(fg.Levels, level)
			}
			got := make(map[string][2]int64)
			samples, err := pyroscope.ExtractTopFunctions(fg, len(fg.Names))
			require.NoError(t, err)
			for i, s := range samples {
				got[s.Name] = [2]int64{s.Self, s.Total}
				assert.InDelta(t, float64(s.Self)/float64(fg.Total)*100, s.Percentage, 0.0001)
				if i > 0 {
					assert.GreaterOrEqual(t, samples[i-1].Self, s.Self)
				}
			}
			assert.Equal(t, tt.want, got)
			limited, err := pyroscope.ExtractTopFunctions(fg, 1)
			require.NoError(t, err)
			assert.Len(t, limited, 1)
		})
	}
}
