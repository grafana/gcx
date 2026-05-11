package kg_test

import (
	"testing"

	"github.com/grafana/gcx/internal/providers/kg"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildInsightSearchRequest(t *testing.T) {
	scope := &kg.ScopeCriteria{NameAndValues: map[string][]string{"env": {"prod"}}}

	t.Run("single name rule maps op", func(t *testing.T) {
		req, err := kg.BuildInsightSearchRequest("Service", []string{"contains=Saturation"}, nil, nil, 100, 200)
		require.NoError(t, err)
		assert.Equal(t, "Service", req.EntityType)
		require.Len(t, req.SearchCriteria, 1)
		assert.Equal(t, []kg.LabelMatcher{
			{Name: "asserts_assertion_name", Op: "CONTAINS", Value: "Saturation"},
		}, req.SearchCriteria[0].LabelMatchers)
		assert.Equal(t, &kg.TimeCriteria{Start: 100, End: 200}, req.TimeCriteria)
		assert.Nil(t, req.ScopeCriteria)
	})

	t.Run("ops are case-insensitive and cover all three", func(t *testing.T) {
		req, err := kg.BuildInsightSearchRequest("Service", []string{
			"CONTAINS=foo",
			"starts-with=bar",
			"Equals=baz",
		}, nil, nil, 0, 0)
		require.NoError(t, err)
		require.Len(t, req.SearchCriteria, 3)
		assert.Equal(t, "CONTAINS", req.SearchCriteria[0].LabelMatchers[0].Op)
		assert.Equal(t, "STARTS WITH", req.SearchCriteria[1].LabelMatchers[0].Op)
		assert.Equal(t, "=", req.SearchCriteria[2].LabelMatchers[0].Op)
	})

	t.Run("severity matchers AND into every rule group", func(t *testing.T) {
		req, err := kg.BuildInsightSearchRequest("Service",
			[]string{"contains=foo", "equals=bar"},
			[]string{"critical", "warning"},
			nil, 0, 0)
		require.NoError(t, err)
		require.Len(t, req.SearchCriteria, 2)
		for _, group := range req.SearchCriteria {
			require.Len(t, group.LabelMatchers, 3)
			assert.Equal(t, "asserts_assertion_name", group.LabelMatchers[0].Name)
			assert.Equal(t, kg.LabelMatcher{Name: "asserts_severity", Op: "=", Value: "critical"}, group.LabelMatchers[1])
			assert.Equal(t, kg.LabelMatcher{Name: "asserts_severity", Op: "=", Value: "warning"}, group.LabelMatchers[2])
		}
	})

	t.Run("severity-only call seeds IS NOT NULL name matcher", func(t *testing.T) {
		req, err := kg.BuildInsightSearchRequest("Service", nil, []string{"critical"}, nil, 0, 0)
		require.NoError(t, err)
		require.Len(t, req.SearchCriteria, 1)
		assert.Equal(t, []kg.LabelMatcher{
			{Name: "asserts_assertion_name", Op: "IS NOT NULL"},
			{Name: "asserts_severity", Op: "=", Value: "critical"},
		}, req.SearchCriteria[0].LabelMatchers)
	})

	t.Run("blank severities are skipped", func(t *testing.T) {
		req, err := kg.BuildInsightSearchRequest("Service",
			[]string{"contains=foo"},
			[]string{"", " ", "critical"},
			nil, 0, 0)
		require.NoError(t, err)
		require.Len(t, req.SearchCriteria, 1)
		require.Len(t, req.SearchCriteria[0].LabelMatchers, 2)
		assert.Equal(t, "critical", req.SearchCriteria[0].LabelMatchers[1].Value)
	})

	t.Run("scope and time propagate through", func(t *testing.T) {
		req, err := kg.BuildInsightSearchRequest("Namespace",
			[]string{"contains=foo"}, nil, scope, 1000, 2000)
		require.NoError(t, err)
		assert.Equal(t, "Namespace", req.EntityType)
		assert.Equal(t, scope, req.ScopeCriteria)
		assert.Equal(t, &kg.TimeCriteria{Start: 1000, End: 2000}, req.TimeCriteria)
	})

	t.Run("missing = returns error", func(t *testing.T) {
		_, err := kg.BuildInsightSearchRequest("Service", []string{"Saturation"}, nil, nil, 0, 0)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "expected op=value")
	})

	t.Run("unknown op returns error", func(t *testing.T) {
		_, err := kg.BuildInsightSearchRequest("Service", []string{"matches=foo"}, nil, nil, 0, 0)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "must be one of contains, starts-with, equals")
	})
}
