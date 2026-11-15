package kg_test

import (
	"encoding/json"
	"testing"

	"github.com/grafana/gcx/internal/providers/kg"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSummarizeEntityCounts(t *testing.T) {
	tests := []struct {
		name   string
		counts map[string]int64
		want   kg.EntitiesSummary
	}{
		{
			name:   "no entities",
			counts: map[string]int64{},
			want:   kg.EntitiesSummary{ByType: map[string]int64{}},
		},
		{
			name:   "observed entities only",
			counts: map[string]int64{"Service": 3, "Pod": 7},
			want:   kg.EntitiesSummary{Total: 10, ByType: map[string]int64{"Service": 3, "Pod": 7}},
		},
		{
			name: "catalog records are split out of the total",
			counts: map[string]int64{
				"Service": 3, "Pod": 7,
				"Account": 1, "EntityType": 10, "Env": 1, "Schema": 32,
			},
			want: kg.EntitiesSummary{
				Total:  10,
				ByType: map[string]int64{"Service": 3, "Pod": 7},
				Internal: &kg.EntityCount{
					Total:  44,
					ByType: map[string]int64{"Account": 1, "EntityType": 10, "Env": 1, "Schema": 32},
				},
			},
		},
		{
			name:   "only catalog records leaves an empty observed total",
			counts: map[string]int64{"Account": 1, "EntityType": 4, "Schema": 32},
			want: kg.EntitiesSummary{
				ByType: map[string]int64{},
				Internal: &kg.EntityCount{
					Total:  37,
					ByType: map[string]int64{"Account": 1, "EntityType": 4, "Schema": 32},
				},
			},
		},
		{
			name:   "zero counts are dropped",
			counts: map[string]int64{"Service": 0, "Pod": 2, "Schema": 0},
			want:   kg.EntitiesSummary{Total: 2, ByType: map[string]int64{"Pod": 2}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, kg.SummarizeEntityCounts(tt.counts))
		})
	}
}

func TestEntitiesSummary_JSONOmitsEmptyInternal(t *testing.T) {
	tests := []struct {
		name   string
		counts map[string]int64
		want   string
	}{
		{
			name:   "no catalog records",
			counts: map[string]int64{"Pod": 2},
			want:   `{"total":2,"byType":{"Pod":2}}`,
		},
		{
			name:   "with catalog records",
			counts: map[string]int64{"Pod": 2, "Schema": 5},
			want:   `{"total":2,"byType":{"Pod":2},"internal":{"total":5,"byType":{"Schema":5}}}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b, err := json.Marshal(kg.SummarizeEntityCounts(tt.counts))
			require.NoError(t, err)
			assert.JSONEq(t, tt.want, string(b))
		})
	}
}
