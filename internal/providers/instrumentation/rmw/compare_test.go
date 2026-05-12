package rmw_test

import (
	"testing"

	"github.com/grafana/gcx/internal/providers/instrumentation"
	"github.com/grafana/gcx/internal/providers/instrumentation/rmw"
	"github.com/stretchr/testify/assert"
)

func boolPtr(b bool) *bool { return &b } //nolint:modernize // new(bool) gives *false, not a pointer to the given value.

func TestAppEqual(t *testing.T) {
	tests := []struct {
		name      string
		a         instrumentation.App
		b         instrumentation.App
		wantEqual bool
		// wantDiff is checked as a substring of the diff summary when not equal.
		wantDiff string
	}{
		{
			name:      "identical apps are equal",
			a:         instrumentation.App{Name: "default", Autoinstrument: boolPtr(true), Tracing: boolPtr(true)}, //nolint:modernize
			b:         instrumentation.App{Name: "default", Autoinstrument: boolPtr(true), Tracing: boolPtr(true)}, //nolint:modernize
			wantEqual: true,
		},
		{
			name:      "both zero values are equal",
			a:         instrumentation.App{},
			b:         instrumentation.App{},
			wantEqual: true,
		},
		{
			name:      "name difference detected",
			a:         instrumentation.App{Name: "default"},
			b:         instrumentation.App{Name: "other"},
			wantEqual: false,
			wantDiff:  "name",
		},
		{
			name:      "autoinstrument true→false detected",
			a:         instrumentation.App{Name: "ns", Autoinstrument: boolPtr(true)},  //nolint:modernize
			b:         instrumentation.App{Name: "ns", Autoinstrument: boolPtr(false)}, //nolint:modernize
			wantEqual: false,
			wantDiff:  "autoinstrument: true → false",
		},
		{
			name:      "tracing nil→true detected",
			a:         instrumentation.App{Name: "ns"},
			b:         instrumentation.App{Name: "ns", Tracing: boolPtr(true)}, //nolint:modernize
			wantEqual: false,
			wantDiff:  "tracing: nil → true",
		},
		{
			name:      "logging true→nil detected",
			a:         instrumentation.App{Name: "ns", Logging: boolPtr(true)}, //nolint:modernize
			b:         instrumentation.App{Name: "ns"},
			wantEqual: false,
			wantDiff:  "logging: true → nil",
		},
		{
			name:      "processmetrics difference detected",
			a:         instrumentation.App{Name: "ns", ProcessMetrics: boolPtr(false)}, //nolint:modernize
			b:         instrumentation.App{Name: "ns", ProcessMetrics: boolPtr(true)},  //nolint:modernize
			wantEqual: false,
			wantDiff:  "processmetrics: false → true",
		},
		{
			name:      "extendedmetrics difference detected",
			a:         instrumentation.App{Name: "ns", ExtendedMetrics: boolPtr(true)},  //nolint:modernize
			b:         instrumentation.App{Name: "ns", ExtendedMetrics: boolPtr(false)}, //nolint:modernize
			wantEqual: false,
			wantDiff:  "extendedmetrics: true → false",
		},
		{
			name:      "profiling difference detected",
			a:         instrumentation.App{Name: "ns", Profiling: boolPtr(false)}, //nolint:modernize
			b:         instrumentation.App{Name: "ns", Profiling: boolPtr(true)},  //nolint:modernize
			wantEqual: false,
			wantDiff:  "profiling: false → true",
		},
		{
			// Apps[] with same elements in different order must be treated as equal.
			name: "apps overrides equal regardless of order",
			a: instrumentation.App{
				Name: "ns",
				Apps: []instrumentation.AppOverride{
					{Name: "frontend", Selection: "SELECTION_INCLUDED"},
					{Name: "backend", Selection: "SELECTION_EXCLUDED"},
				},
			},
			b: instrumentation.App{
				Name: "ns",
				Apps: []instrumentation.AppOverride{
					{Name: "backend", Selection: "SELECTION_EXCLUDED"},
					{Name: "frontend", Selection: "SELECTION_INCLUDED"},
				},
			},
			wantEqual: true,
		},
		{
			name: "added app override detected",
			a: instrumentation.App{
				Name: "ns",
				Apps: []instrumentation.AppOverride{
					{Name: "frontend", Selection: "SELECTION_INCLUDED"},
				},
			},
			b: instrumentation.App{
				Name: "ns",
				Apps: []instrumentation.AppOverride{
					{Name: "frontend", Selection: "SELECTION_INCLUDED"},
					{Name: "backend", Selection: "SELECTION_EXCLUDED"},
				},
			},
			wantEqual: false,
			wantDiff:  "added apps[]: backend",
		},
		{
			name: "removed app override detected",
			a: instrumentation.App{
				Name: "ns",
				Apps: []instrumentation.AppOverride{
					{Name: "frontend", Selection: "SELECTION_INCLUDED"},
					{Name: "backend", Selection: "SELECTION_EXCLUDED"},
				},
			},
			b: instrumentation.App{
				Name: "ns",
				Apps: []instrumentation.AppOverride{
					{Name: "frontend", Selection: "SELECTION_INCLUDED"},
				},
			},
			wantEqual: false,
			wantDiff:  "removed apps[]: backend",
		},
		{
			name: "changed app override selection detected",
			a: instrumentation.App{
				Name: "ns",
				Apps: []instrumentation.AppOverride{
					{Name: "frontend", Selection: "SELECTION_INCLUDED"},
				},
			},
			b: instrumentation.App{
				Name: "ns",
				Apps: []instrumentation.AppOverride{
					{Name: "frontend", Selection: "SELECTION_EXCLUDED"},
				},
			},
			wantEqual: false,
			wantDiff:  "apps[frontend].selection: SELECTION_INCLUDED → SELECTION_EXCLUDED",
		},
		{
			// Verify that AppEqual does not mutate the caller's slice (order preservation).
			name: "caller slices not mutated by sorting",
			a: instrumentation.App{
				Name: "ns",
				Apps: []instrumentation.AppOverride{
					{Name: "z-app", Selection: "SELECTION_INCLUDED"},
					{Name: "a-app", Selection: "SELECTION_INCLUDED"},
				},
			},
			b: instrumentation.App{
				Name: "ns",
				Apps: []instrumentation.AppOverride{
					{Name: "z-app", Selection: "SELECTION_INCLUDED"},
					{Name: "a-app", Selection: "SELECTION_INCLUDED"},
				},
			},
			wantEqual: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Save original slice order for mutation check.
			origAOrder := make([]string, len(tt.a.Apps))
			for i, o := range tt.a.Apps {
				origAOrder[i] = o.Name
			}
			origBOrder := make([]string, len(tt.b.Apps))
			for i, o := range tt.b.Apps {
				origBOrder[i] = o.Name
			}

			equal, diff := rmw.AppEqual(tt.a, tt.b)

			assert.Equal(t, tt.wantEqual, equal, "equality result")
			if !tt.wantEqual {
				assert.Contains(t, diff, tt.wantDiff, "diff should contain expected substring")
			} else {
				assert.Empty(t, diff, "diff should be empty when equal")
			}

			// Verify slices were not mutated.
			for i, o := range tt.a.Apps {
				assert.Equal(t, origAOrder[i], o.Name, "caller's slice a must not be mutated")
			}
			for i, o := range tt.b.Apps {
				assert.Equal(t, origBOrder[i], o.Name, "caller's slice b must not be mutated")
			}
		})
	}
}

func TestClusterEqual(t *testing.T) {
	tests := []struct {
		name      string
		a         instrumentation.Cluster
		b         instrumentation.Cluster
		wantEqual bool
		wantDiff  string
	}{
		{
			name:      "identical clusters are equal",
			a:         instrumentation.Cluster{Name: "prod-1", Selection: "SELECTION_INCLUDED", CostMetrics: boolPtr(true)}, //nolint:modernize
			b:         instrumentation.Cluster{Name: "prod-1", Selection: "SELECTION_INCLUDED", CostMetrics: boolPtr(true)}, //nolint:modernize
			wantEqual: true,
		},
		{
			name:      "both zero values are equal",
			a:         instrumentation.Cluster{},
			b:         instrumentation.Cluster{},
			wantEqual: true,
		},
		{
			name:      "name difference detected",
			a:         instrumentation.Cluster{Name: "prod-1"},
			b:         instrumentation.Cluster{Name: "prod-2"},
			wantEqual: false,
			wantDiff:  "name",
		},
		{
			name:      "selection difference detected",
			a:         instrumentation.Cluster{Name: "c1", Selection: "SELECTION_INCLUDED"},
			b:         instrumentation.Cluster{Name: "c1", Selection: "SELECTION_EXCLUDED"},
			wantEqual: false,
			wantDiff:  "selection",
		},
		{
			name:      "costmetrics false→true detected",
			a:         instrumentation.Cluster{Name: "c1", CostMetrics: boolPtr(false)}, //nolint:modernize
			b:         instrumentation.Cluster{Name: "c1", CostMetrics: boolPtr(true)},  //nolint:modernize
			wantEqual: false,
			wantDiff:  "costmetrics: false → true",
		},
		{
			name:      "energymetrics nil→true detected",
			a:         instrumentation.Cluster{Name: "c1"},
			b:         instrumentation.Cluster{Name: "c1", EnergyMetrics: boolPtr(true)}, //nolint:modernize
			wantEqual: false,
			wantDiff:  "energymetrics: nil → true",
		},
		{
			name:      "clusterevents true→false detected",
			a:         instrumentation.Cluster{Name: "c1", ClusterEvents: boolPtr(true)},  //nolint:modernize
			b:         instrumentation.Cluster{Name: "c1", ClusterEvents: boolPtr(false)}, //nolint:modernize
			wantEqual: false,
			wantDiff:  "clusterevents: true → false",
		},
		{
			name:      "nodelogs difference detected",
			a:         instrumentation.Cluster{Name: "c1", NodeLogs: boolPtr(true)},  //nolint:modernize
			b:         instrumentation.Cluster{Name: "c1", NodeLogs: boolPtr(false)}, //nolint:modernize
			wantEqual: false,
			wantDiff:  "nodelogs: true → false",
		},
		{
			name: "multiple fields differ — diff contains all",
			a: instrumentation.Cluster{
				Name:        "c1",
				CostMetrics: boolPtr(false), //nolint:modernize
				NodeLogs:    boolPtr(true),  //nolint:modernize
			},
			b: instrumentation.Cluster{
				Name:        "c1",
				CostMetrics: boolPtr(true),  //nolint:modernize
				NodeLogs:    boolPtr(false), //nolint:modernize
			},
			wantEqual: false,
			wantDiff:  "costmetrics",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			equal, diff := rmw.ClusterEqual(tt.a, tt.b)

			assert.Equal(t, tt.wantEqual, equal, "equality result")
			if !tt.wantEqual {
				assert.Contains(t, diff, tt.wantDiff, "diff should contain expected substring")
			} else {
				assert.Empty(t, diff, "diff should be empty when equal")
			}
		})
	}
}
