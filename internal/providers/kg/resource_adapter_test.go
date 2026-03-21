package kg_test

import (
	"testing"

	"github.com/grafana/grafanactl/internal/providers/kg"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRuleToResource_RoundTrip(t *testing.T) {
	original := kg.Rule{
		Name:   "my-rule",
		Expr:   "sum(rate(http_requests_total[5m])) by (service)",
		Record: "service:http_requests:rate5m",
		Labels: map[string]string{"team": "platform"},
	}

	res, err := kg.RuleToResource(original, "stack-123")
	require.NoError(t, err)

	assert.Equal(t, kg.APIVersion, res.GroupVersionKind().Group+"/"+res.GroupVersionKind().Version)
	assert.Equal(t, kg.Kind, res.GroupVersionKind().Kind)
	assert.Equal(t, "my-rule", res.Raw.GetName())
	assert.Equal(t, "stack-123", res.Raw.GetNamespace())

	// Round-trip back to Rule.
	rule, err := kg.RuleFromResource(res)
	require.NoError(t, err)
	assert.Equal(t, original.Name, rule.Name)
	assert.Equal(t, original.Expr, rule.Expr)
	assert.Equal(t, original.Record, rule.Record)
	assert.Equal(t, original.Labels, rule.Labels)
}

func TestRuleToResource_AlertRule(t *testing.T) {
	original := kg.Rule{
		Name:        "alert-rule",
		Alert:       "HighErrorRate",
		Expr:        "error_rate > 0.5",
		Labels:      map[string]string{"severity": "critical"},
		Annotations: map[string]string{"summary": "High error rate detected"},
	}

	res, err := kg.RuleToResource(original, "stack-456")
	require.NoError(t, err)

	rule, err := kg.RuleFromResource(res)
	require.NoError(t, err)
	assert.Equal(t, "alert-rule", rule.Name)
	assert.Equal(t, "HighErrorRate", rule.Alert)
	assert.Equal(t, original.Annotations, rule.Annotations)
}
