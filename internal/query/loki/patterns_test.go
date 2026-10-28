package loki_test

import (
	"slices"
	"testing"

	"github.com/grafana/gcx/internal/query/loki"
	"github.com/stretchr/testify/assert"
)

func TestSummarizePatterns(t *testing.T) {
	tests := []struct {
		name string
		in   []loki.Pattern
		want []loki.Pattern
	}{
		{name: "nil"},
		{name: "empty", in: []loki.Pattern{}, want: []loki.Pattern{}},
		{
			name: "sum samples and preserve level",
			in:   []loki.Pattern{{Pattern: "request <_>", Level: "info", Samples: [][]int64{{1, 10}, {2, 15}}}},
			want: []loki.Pattern{{Pattern: "request <_>", Level: "info", Samples: [][]int64{{1, 10}, {2, 15}}, TotalCount: 25}},
		},
		{
			name: "ignore malformed samples",
			in:   []loki.Pattern{{Pattern: "request <_>", Samples: [][]int64{nil, {}, {1}, {2, 7}, {3, 4, 99}}}},
			want: []loki.Pattern{{Pattern: "request <_>", Samples: [][]int64{nil, {}, {1}, {2, 7}, {3, 4, 99}}, TotalCount: 11}},
		},
		{
			name: "no samples resets stale total",
			in:   []loki.Pattern{{Pattern: "request <_>", TotalCount: 99}},
			want: []loki.Pattern{{Pattern: "request <_>"}},
		},
		{
			name: "int64 total",
			in:   []loki.Pattern{{Pattern: "request <_>", Samples: [][]int64{{1, 1 << 31}, {2, 1}}}},
			want: []loki.Pattern{{Pattern: "request <_>", Samples: [][]int64{{1, 1 << 31}, {2, 1}}, TotalCount: 1<<31 + 1}},
		},
		{
			name: "sort descending with alphabetical ties",
			in: []loki.Pattern{
				{Pattern: "rare", Samples: [][]int64{{1, 2}}},
				{Pattern: "zebra", Samples: [][]int64{{1, 5}}},
				{Pattern: "alpha", Samples: [][]int64{{1, 2}, {2, 3}}},
			},
			want: []loki.Pattern{
				{Pattern: "alpha", Samples: [][]int64{{1, 2}, {2, 3}}, TotalCount: 5},
				{Pattern: "zebra", Samples: [][]int64{{1, 5}}, TotalCount: 5},
				{Pattern: "rare", Samples: [][]int64{{1, 2}}, TotalCount: 2},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			original := slices.Clone(tt.in)
			got := loki.SummarizePatterns(tt.in)
			assert.Equal(t, tt.want, got)
			assert.Equal(t, original, tt.in, "input order and totals must remain unchanged")
			assert.Equal(t, tt.want, loki.SummarizePatterns(got), "totals must not accumulate on repeated calls")
		})
	}
}
