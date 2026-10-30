package pyroscope_test

import (
	"strconv"
	"testing"

	"github.com/grafana/gcx/internal/query/pyroscope"
	"github.com/stretchr/testify/assert"
)

func TestExtractTopFunctionsRecursion(t *testing.T) {
	tests := []struct {
		name   string
		levels [][]int
		want   map[string][2]int64 // self, cumulative
	}{
		{"direct", [][]int{{0, 10, 0, 0}, {0, 10, 0, 1}, {0, 10, 0, 1}, {0, 10, 10, 3}}, map[string][2]int64{"total": {0, 10}, "A": {0, 10}, "leaf": {10, 10}}},
		{"indirect", [][]int{{0, 10, 0, 0}, {0, 10, 0, 1}, {0, 10, 0, 2}, {0, 10, 10, 1}}, map[string][2]int64{"total": {0, 10}, "A": {10, 10}, "B": {0, 10}}},
		{"self at multiple depths", [][]int{{0, 10, 0, 0}, {0, 10, 3, 1}, {3, 7, 7, 1}}, map[string][2]int64{"total": {0, 10}, "A": {10, 10}}},
		{"independent branches and relative offsets", [][]int{{0, 30, 0, 0}, {0, 10, 0, 2, 5, 15, 0, 3}, {0, 10, 4, 1, 5, 15, 6, 1}, {4, 6, 6, 1, 6, 9, 9, 1}}, map[string][2]int64{"total": {0, 30}, "A": {25, 25}, "B": {0, 10}, "leaf": {0, 15}}},
		{"other preserves offsets", [][]int{{0, 30, 0, 0}, {0, 10, 10, 4, 5, 15, 5, 1}, {20, 10, 10, 1}}, map[string][2]int64{"total": {0, 30}, "A": {15, 15}}},
		{"non recursive", [][]int{{0, 30, 0, 0}, {0, 10, 10, 1, 0, 20, 20, 2}}, map[string][2]int64{"total": {0, 30}, "A": {10, 10}, "B": {20, 20}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fg := &pyroscope.Flamegraph{Names: []string{"total", "A", "B", "leaf", "other"}, Total: int64(tt.levels[0][1])}
			for _, values := range tt.levels {
				level := pyroscope.Level{}
				for _, v := range values {
					level.Values = append(level.Values, strconv.Itoa(v))
				}
				fg.Levels = append(fg.Levels, level)
			}
			got := make(map[string][2]int64)
			samples := pyroscope.ExtractTopFunctions(fg, len(fg.Names))
			for i, s := range samples {
				got[s.Name] = [2]int64{s.Self, s.Total}
				assert.InDelta(t, float64(s.Self)/float64(fg.Total)*100, s.Percentage, 0.0001)
				if i > 0 {
					assert.GreaterOrEqual(t, samples[i-1].Self, s.Self)
				}
			}
			assert.Equal(t, tt.want, got)
			assert.Len(t, pyroscope.ExtractTopFunctions(fg, 1), 1)
		})
	}
}
