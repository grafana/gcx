package kg_test

import (
	"sort"
	"testing"

	"github.com/grafana/gcx/internal/providers/kg"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// EntityOverview / ScopeSummary builders
// ---------------------------------------------------------------------------

func TestBuildEntityOverview_ExcludesMetaTypes(t *testing.T) {
	in := kg.OrientationInput{
		EntityCounts: map[string]int64{
			"Service":    10,
			"Pod":        50,
			"EntityType": 18, // meta — excluded
			"Schema":     30, // meta — excluded
			"Env":        3,  // meta — excluded
		},
		TracesTargetInfoSeries: 4,
	}

	overview := kg.BuildEntityOverview(in)

	assert.Equal(t, int64(60), overview.Total,
		"Total should exclude meta types (10 + 50, not 10+50+18+30+3)")
	assert.Equal(t, int64(10), overview.TotalServiceCount)
	assert.Equal(t, int64(4), overview.TracedServiceCount)
	assert.NotContains(t, overview.ByType, "EntityType")
	assert.NotContains(t, overview.ByType, "Schema")
	assert.NotContains(t, overview.ByType, "Env")
	assert.Contains(t, overview.ByType, "Service")
	assert.Contains(t, overview.ByType, "Pod")
}

func TestBuildScopeSummary(t *testing.T) {
	tests := []struct {
		name          string
		scope         kg.ScopeFlags
		in            kg.OrientationInput
		wantFilterSet bool
		wantNoneBkt   bool
	}{
		{
			name:          "no filter, no none bucket",
			scope:         kg.NewTestScopeFlags("", "", ""),
			in:            kg.OrientationInput{Scopes: map[string][]string{"env": {"prod", "staging"}}},
			wantFilterSet: false,
			wantNoneBkt:   false,
		},
		{
			name:          "env filter set",
			scope:         kg.NewTestScopeFlags("prod", "", ""),
			in:            kg.OrientationInput{Scopes: map[string][]string{"env": {"prod"}}},
			wantFilterSet: true,
			wantNoneBkt:   false,
		},
		{
			name:          "namespace filter set",
			scope:         kg.NewTestScopeFlags("", "", "team-a"),
			in:            kg.OrientationInput{Scopes: map[string][]string{"namespace": {"team-a"}}},
			wantFilterSet: true,
			wantNoneBkt:   false,
		},
		{
			name:  "none bucket present and populated",
			scope: kg.NewTestScopeFlags("", "", ""),
			in: kg.OrientationInput{
				Scopes:                map[string][]string{"env": {"prod", "none"}},
				NoneBucketHasEntities: true,
			},
			wantFilterSet: false,
			wantNoneBkt:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := kg.BuildScopeSummary(tt.in, &tt.scope)
			assert.Equal(t, tt.wantFilterSet, got.FilterSet, "FilterSet mismatch")
			assert.Equal(t, tt.wantNoneBkt, got.NoneBucketPresent, "NoneBucketPresent mismatch")
		})
	}
}

// ---------------------------------------------------------------------------
// Scenario detectors
// ---------------------------------------------------------------------------

func TestDetectNoEntities(t *testing.T) {
	tests := []struct {
		name      string
		in        kg.OrientationInput
		overview  kg.EntityOverview
		wantMatch bool
	}{
		{
			name:      "stack disabled",
			in:        kg.OrientationInput{StackEnabled: false, EntityCounts: map[string]int64{"Service": 5}},
			overview:  kg.EntityOverview{Total: 5},
			wantMatch: true,
		},
		{
			name:      "stack enabled, zero entities",
			in:        kg.OrientationInput{StackEnabled: true, EntityCounts: map[string]int64{}},
			overview:  kg.EntityOverview{Total: 0},
			wantMatch: true,
		},
		{
			name:      "stack enabled, entities present",
			in:        kg.OrientationInput{StackEnabled: true, EntityCounts: map[string]int64{"Service": 5}},
			overview:  kg.EntityOverview{Total: 5},
			wantMatch: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := kg.DetectNoEntities(tt.in, tt.overview)
			if tt.wantMatch {
				require.NotNil(t, got, "expected scenario match")
				assert.Equal(t, kg.ScenarioNoEntities, got.ID)
				assert.Equal(t, "I see no entities at all", got.Label)
				assert.Equal(t, kg.ConfidenceHigh, got.Confidence)
			} else {
				assert.Nil(t, got)
			}
		})
	}
}

func TestDetectEntitiesNoEdges(t *testing.T) {
	tests := []struct {
		name      string
		in        kg.OrientationInput
		overview  kg.EntityOverview
		scope     kg.ScopeFlags
		wantMatch bool
	}{
		{
			name: "workloads in scope, calls = 0",
			in: kg.OrientationInput{
				AssertsMixinWorkloadJobSeries: 100,
				AssertsRelationCallsSeries:    0,
			},
			scope:     kg.NewTestScopeFlags("", "", ""),
			wantMatch: true,
		},
		{
			name: "workloads in scope, calls > 0",
			in: kg.OrientationInput{
				AssertsMixinWorkloadJobSeries: 100,
				AssertsRelationCallsSeries:    20,
			},
			scope:     kg.NewTestScopeFlags("", "", ""),
			wantMatch: false,
		},
		{
			name: "no workloads in scope — detector silent",
			in: kg.OrientationInput{
				AssertsMixinWorkloadJobSeries: 0,
				AssertsRelationCallsSeries:    0,
			},
			scope:     kg.NewTestScopeFlags("", "", ""),
			wantMatch: false,
		},
		{
			name: "env scope echoed in reasoning",
			in: kg.OrientationInput{
				AssertsMixinWorkloadJobSeries: 100,
				AssertsRelationCallsSeries:    0,
			},
			scope:     kg.NewTestScopeFlags("production", "", ""),
			wantMatch: true,
		},
		{
			// Regression: previously this fired HIGH-confidence on an
			// empty scope because the gate was the stack-wide
			// TotalServiceCount. With the scoped workload gate, an
			// empty scope must defer to detectMissingEntities.
			name: "scope with stack-wide services but no scoped workloads — silent",
			in: kg.OrientationInput{
				AssertsMixinWorkloadJobSeries: 0,
				AssertsRelationCallsSeries:    0,
			},
			overview:  kg.EntityOverview{TotalServiceCount: 6427},
			scope:     kg.NewTestScopeFlags("azure-westeurope-1", "", ""),
			wantMatch: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := kg.DetectEntitiesNoEdges(tt.in, tt.overview, &tt.scope)
			if tt.wantMatch {
				require.NotNil(t, got, "expected scenario match")
				assert.Equal(t, kg.ScenarioEntitiesNoEdges, got.ID)
				assert.Equal(t, "I see entities with no edges", got.Label)
				assert.Equal(t, kg.ConfidenceHigh, got.Confidence)
			} else {
				assert.Nil(t, got)
			}
		})
	}
}

func TestDetectCantFilter(t *testing.T) {
	tests := []struct {
		name      string
		in        kg.OrientationInput
		scope     kg.ScopeFlags
		wantMatch bool
	}{
		{
			name:      "none bucket has entities",
			in:        kg.OrientationInput{NoneBucketHasEntities: true},
			scope:     kg.NewTestScopeFlags("", "", ""),
			wantMatch: true,
		},
		{
			name: "user gave bogus env",
			in: kg.OrientationInput{
				Scopes: map[string][]string{"env": {"prod", "staging"}},
			},
			scope:     kg.NewTestScopeFlags("doesnotexist", "", ""),
			wantMatch: true,
		},
		{
			name: "user gave valid env, no none bucket",
			in: kg.OrientationInput{
				Scopes: map[string][]string{"env": {"prod", "staging"}},
			},
			scope:     kg.NewTestScopeFlags("prod", "", ""),
			wantMatch: false,
		},
		{
			name:      "no filter, no none bucket — silent",
			in:        kg.OrientationInput{},
			scope:     kg.NewTestScopeFlags("", "", ""),
			wantMatch: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := kg.DetectCantFilter(tt.in, &tt.scope)
			if tt.wantMatch {
				require.NotNil(t, got, "expected scenario match")
				assert.Equal(t, kg.ScenarioCantFilter, got.ID)
				assert.Equal(t, "I can't filter to the entities I want", got.Label)
				assert.Equal(t, kg.ConfidenceHigh, got.Confidence)
			} else {
				assert.Nil(t, got)
			}
		})
	}
}

func TestDetectMissingEntities(t *testing.T) {
	tests := []struct {
		name      string
		in        kg.OrientationInput
		scope     kg.ScopeFlags
		wantMatch bool
	}{
		{
			name:      "no filter — silent regardless of data",
			in:        kg.OrientationInput{AssertsMixinWorkloadJobSeries: 0},
			scope:     kg.NewTestScopeFlags("", "", ""),
			wantMatch: false,
		},
		{
			name:      "filter set, no workloads in scope",
			in:        kg.OrientationInput{AssertsMixinWorkloadJobSeries: 0},
			scope:     kg.NewTestScopeFlags("prod", "", ""),
			wantMatch: true,
		},
		{
			name:      "filter set, workloads present",
			in:        kg.OrientationInput{AssertsMixinWorkloadJobSeries: 100},
			scope:     kg.NewTestScopeFlags("prod", "", ""),
			wantMatch: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := kg.DetectMissingEntities(tt.in, &tt.scope)
			if tt.wantMatch {
				require.NotNil(t, got, "expected scenario match")
				assert.Equal(t, kg.ScenarioMissingEntities, got.ID)
				assert.Equal(t, "Some expected entities are missing", got.Label)
				assert.Equal(t, kg.ConfidenceMedium, got.Confidence)
			} else {
				assert.Nil(t, got)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// ComputeOrientation — end-to-end composition
// ---------------------------------------------------------------------------

func TestComputeOrientation_HealthyUnscoped(t *testing.T) {
	in := kg.OrientationInput{
		StackEnabled:                  true,
		EntityCounts:                  map[string]int64{"Service": 100, "Pod": 200, "EntityType": 18, "Schema": 30, "Env": 3},
		Scopes:                        map[string][]string{"env": {"prod"}, "namespace": {"team-a", "team-b"}},
		TracesTargetInfoSeries:        20,
		AssertsRelationCallsSeries:    20,
		AssertsMixinWorkloadJobSeries: 100,
	}
	scope := kg.NewTestScopeFlags("", "", "")

	got := kg.ComputeOrientation(in, &scope)

	assert.Empty(t, got.MatchedScenarios, "healthy stack should match no scenarios")
	assert.NotEmpty(t, got.StartingPoints, "unscoped run should suggest starting points")
	assert.Len(t, got.StartingPoints, 3)
	assert.Equal(t, int64(300), got.EntityOverview.Total, "total excludes meta types")
	assert.Equal(t, int64(20), got.EntityOverview.TracedServiceCount)
	assert.False(t, got.Scope.FilterSet)
}

func TestComputeOrientation_FilterSet_SuppressesStartingPoints(t *testing.T) {
	in := kg.OrientationInput{
		StackEnabled:                  true,
		EntityCounts:                  map[string]int64{"Service": 10},
		Scopes:                        map[string][]string{"env": {"prod"}},
		AssertsRelationCallsSeries:    5,
		AssertsMixinWorkloadJobSeries: 10,
	}
	scope := kg.NewTestScopeFlags("prod", "", "")

	got := kg.ComputeOrientation(in, &scope)

	assert.Empty(t, got.StartingPoints, "starting points must be suppressed when any scope flag is set")
	assert.True(t, got.Scope.FilterSet)
}

func TestComputeOrientation_MultipleScenarios_OrderByConfidence(t *testing.T) {
	// Simulates: user supplied an env value the stack does not know about.
	// Expect: cant-filter (high, via scopeValueUnknown) AND missing-entities
	// (medium, via filter-set with zero scoped workloads) both match, with
	// high-confidence first.
	in := kg.OrientationInput{
		StackEnabled:                  true,
		EntityCounts:                  map[string]int64{"Service": 100},
		Scopes:                        map[string][]string{"env": {"prod", "staging"}},
		AssertsRelationCallsSeries:    0,
		AssertsMixinWorkloadJobSeries: 0,
	}
	scope := kg.NewTestScopeFlags("doesnotexist", "", "")

	got := kg.ComputeOrientation(in, &scope)

	require.GreaterOrEqual(t, len(got.MatchedScenarios), 2,
		"expected both entities-no-edges and missing-entities to match")

	// First match must be high-confidence.
	assert.Equal(t, kg.ConfidenceHigh, got.MatchedScenarios[0].Confidence,
		"high-confidence scenarios should appear first")

	// All high-confidence entries must come before any medium-confidence ones.
	seenMedium := false
	for _, s := range got.MatchedScenarios {
		if s.Confidence == kg.ConfidenceMedium {
			seenMedium = true
		}
		if seenMedium {
			assert.NotEqual(t, kg.ConfidenceHigh, s.Confidence,
				"no high-confidence entry may follow a medium-confidence entry")
		}
	}
}

func TestComputeOrientation_NoEntities_StackDisabled(t *testing.T) {
	in := kg.OrientationInput{
		StackEnabled: false,
		EntityCounts: map[string]int64{},
	}
	scope := kg.NewTestScopeFlags("", "", "")

	got := kg.ComputeOrientation(in, &scope)

	require.Len(t, got.MatchedScenarios, 1)
	assert.Equal(t, kg.ScenarioNoEntities, got.MatchedScenarios[0].ID)
}

// ---------------------------------------------------------------------------
// Starting points
// ---------------------------------------------------------------------------

func TestBuildStartingPoints_AlwaysThreeModes(t *testing.T) {
	scope := kg.ScopeSummary{NamespacesKnown: []string{"a", "b", "c"}}
	got := kg.BuildStartingPoints(scope)

	require.Len(t, got, 3)
	ids := []string{string(got[0].ID), string(got[1].ID), string(got[2].ID)}
	sort.Strings(ids)
	assert.Equal(t, []string{
		string(kg.StartingPointByNamePattern),
		string(kg.StartingPointByNamespace),
		string(kg.StartingPointBySingleService),
	}, ids)
}

func TestBuildStartingPoints_NamespaceContextUsesRuntimeCount(t *testing.T) {
	scope := kg.ScopeSummary{NamespacesKnown: []string{"a", "b", "c", "d"}}
	got := kg.BuildStartingPoints(scope)

	var nsPoint kg.StartingPoint
	for _, p := range got {
		if p.ID == kg.StartingPointByNamespace {
			nsPoint = p
		}
	}
	require.Equal(t, kg.StartingPointByNamespace, nsPoint.ID,
		"by-namespace starting point must be present")
	assert.Contains(t, nsPoint.Context, "4 namespace",
		"namespace context should report the actual count")
	// Context must NOT make a judgment about whether the count is too many.
	assert.NotContains(t, nsPoint.Context, "many")
	assert.NotContains(t, nsPoint.Context, "too")
}

func TestBuildStartingPoints_NoNamespacesKnown_NoContextSet(t *testing.T) {
	scope := kg.ScopeSummary{}
	got := kg.BuildStartingPoints(scope)

	for _, p := range got {
		if p.ID == kg.StartingPointByNamespace {
			assert.Empty(t, p.Context,
				"namespace context should be empty when no namespaces are known")
		}
	}
}
