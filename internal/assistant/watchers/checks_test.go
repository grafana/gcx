package watchers_test

import (
	"testing"

	"github.com/grafana/gcx/internal/assistant/watchers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProjectChecksPreservesUnsupportedInformation(t *testing.T) {
	queries := []map[string]any{
		{
			"id": "known", "type": "promql", "datasourceUid": "source", "expr": "up", "comment": "Interpret uptime", "enabled": false,
			"role": "current_health", "thresholds": map[string]any{"comparator": "lt", "warning": float64(1), "futureBoundary": float64(2)},
			"goodWhen": "high", "logBaselineSeed": map[string]any{"patterns": []any{}}, "newWindow": map[string]any{"size": float64(42)},
		},
		{"id": "new", "type": "new_query_type", "expr": "expression", "newParameter": "value"},
		{"id": "default", "type": "alerts", "expr": "{}"},
	}
	checks := watchers.ProjectChecks(queries)
	require.Len(t, checks, 3)
	assert.Equal(t, "known", checks[0].ID)
	require.NotNil(t, checks[0].Enabled)
	assert.False(t, *checks[0].Enabled)
	assert.Equal(t, "source", checks[0].DatasourceUID)
	assert.Equal(t, "promql", checks[0].QueryType)
	assert.Equal(t, "up", checks[0].Expression)
	assert.Equal(t, "Interpret uptime", checks[0].Guidance)
	assert.Equal(t, "current_health", checks[0].Parameters["role"])
	assert.Equal(t, "high", checks[0].Parameters["goodWhen"])
	assert.Equal(t, queries[0]["logBaselineSeed"], checks[0].Parameters["logBaseline"])
	require.NotNil(t, checks[0].Unsupported)
	assert.False(t, checks[0].Unsupported.QueryType)
	assert.InDelta(t, float64(2), checks[0].Unsupported.Parameters["thresholds.futureBoundary"], 0)
	assert.Equal(t, queries[0]["newWindow"], checks[0].Unsupported.Parameters["newWindow"])
	require.NotNil(t, checks[1].Unsupported)
	assert.True(t, checks[1].Unsupported.QueryType)
	assert.Equal(t, "new_query_type", checks[1].QueryType)
	assert.Equal(t, "value", checks[1].Unsupported.Parameters["newParameter"])
	require.NotNil(t, checks[2].Enabled)
	assert.True(t, *checks[2].Enabled)
	assert.Nil(t, checks[2].Unsupported)
	assert.NotNil(t, checks[2].Parameters)
}

func TestProjectChecksSupportedQueryTypes(t *testing.T) {
	for _, queryType := range []string{"promql", "logql", "alerts", "bigquery", "traceql_metrics", "synthetic_monitoring", "pyroscope_series", "cloudwatch_metric"} {
		t.Run(queryType, func(t *testing.T) {
			checks := watchers.ProjectChecks([]map[string]any{{"type": queryType}})
			require.Len(t, checks, 1)
			assert.Equal(t, queryType, checks[0].QueryType)
			assert.Nil(t, checks[0].Unsupported)
		})
	}
	assert.NotNil(t, watchers.ProjectChecks(nil))
}

func TestProjectChecksMarksUnexpectedTypesAndNestedParameters(t *testing.T) {
	raw := map[string]any{
		"id": "example", "type": "logql", "enabled": "maybe", "datasourceUid": float64(17), "expr": "{service=\"example\"}",
		"role": "new_role", "goodWhen": false,
		"thresholds": map[string]any{"comparator": "new_comparator", "warning": "many"},
		"logBaselineSeed": map[string]any{
			"newSetting": "keep", "benignLineFilters": []any{"known", float64(42)},
			"patterns": []any{map[string]any{"pattern": "example", "classification": "new_class", "newKey": "keep", "reviewAfter": "later"}, "unexpected"},
		},
	}
	checks := watchers.ProjectChecks([]map[string]any{raw})
	require.Len(t, checks, 1)
	assert.Nil(t, checks[0].Enabled)
	require.NotNil(t, checks[0].Unsupported)
	for path, value := range map[string]any{
		"enabled": "maybe", "datasourceUid": float64(17), "role": "new_role", "goodWhen": false,
		"thresholds.comparator": "new_comparator", "thresholds.warning": "many",
		"logBaseline.newSetting": "keep", "logBaseline.benignLineFilters": []any{"known", float64(42)},
		"logBaseline.patterns[0].classification": "new_class", "logBaseline.patterns[0].newKey": "keep", "logBaseline.patterns[0].reviewAfter": "later",
		"logBaseline.patterns[1]": "unexpected",
	} {
		assert.Equal(t, value, checks[0].Unsupported.Parameters[path], path)
	}
	assert.Equal(t, raw["thresholds"], checks[0].Parameters["thresholds"])
	assert.Equal(t, raw["logBaselineSeed"], checks[0].Parameters["logBaseline"])
	assert.NotContains(t, checks[0].Parameters, "enabled")
}

func TestProjectChecksValidParameters(t *testing.T) {
	checks := watchers.ProjectChecks([]map[string]any{{
		"type": "logql", "role": "context", "goodWhen": "absent",
		"thresholds": map[string]any{"comparator": "gte", "warning": float64(1), "critical": float64(2), "source": "example"},
		"logBaselineSeed": map[string]any{
			"benignLineFilters": []any{"example"},
			"patterns":          []any{map[string]any{"pattern": "example", "level": "info", "classification": "watch", "reviewAfter": "2030-01-01T00:00:00Z"}},
		},
	}})
	require.Len(t, checks, 1)
	assert.Nil(t, checks[0].Unsupported)
}

func TestValidateIntervalSeconds(t *testing.T) {
	for _, tt := range []struct {
		seconds int64
		valid   bool
	}{{299, false}, {300, true}, {600, true}, {10800, true}, {10801, false}} {
		err := watchers.ValidateIntervalSeconds(tt.seconds)
		if tt.valid {
			assert.NoError(t, err)
		} else {
			assert.Error(t, err)
		}
	}
}
