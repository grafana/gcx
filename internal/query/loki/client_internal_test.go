package loki

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSummarizePatterns(t *testing.T) {
	tests := []struct {
		name string
		in   []Pattern
		want []Pattern
	}{
		{name: "nil"},
		{name: "empty", in: []Pattern{}, want: []Pattern{}},
		{
			name: "ignore malformed samples",
			in:   []Pattern{{Pattern: "request <_>", Samples: [][]int64{nil, {}, {1}, {2, 7}, {3, 4, 99}}}},
			want: []Pattern{{Pattern: "request <_>", Samples: [][]int64{nil, {}, {1}, {2, 7}, {3, 4, 99}}, TotalCount: 11}},
		},
		{
			name: "int64 total",
			in:   []Pattern{{Pattern: "request <_>", Samples: [][]int64{{1, 1 << 31}, {2, 1}}}},
			want: []Pattern{{Pattern: "request <_>", Samples: [][]int64{{1, 1 << 31}, {2, 1}}, TotalCount: 1<<31 + 1}},
		},
		{
			name: "sort descending with alphabetical ties",
			in: []Pattern{
				{Pattern: "rare", Samples: [][]int64{{1, 2}}},
				{Pattern: "zebra", Samples: [][]int64{{1, 5}}},
				{Pattern: "alpha", Samples: [][]int64{{1, 2}, {2, 3}}},
			},
			want: []Pattern{
				{Pattern: "alpha", Samples: [][]int64{{1, 2}, {2, 3}}, TotalCount: 5},
				{Pattern: "zebra", Samples: [][]int64{{1, 5}}, TotalCount: 5},
				{Pattern: "rare", Samples: [][]int64{{1, 2}}, TotalCount: 2},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			summarizePatterns(tt.in)
			assert.Equal(t, tt.want, tt.in)
		})
	}
}
