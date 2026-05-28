package kg_test

import (
	"testing"

	"github.com/grafana/gcx/internal/providers/kg"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRuleToResource_RoundTrip(t *testing.T) {
	original := kg.Rule{
		Name: "my-rule",
		Groups: []kg.RuleGroup{
			{
				Name:     "primary",
				Interval: "30s",
				Rules: []kg.PromRule{
					{
						Record: "service:http_requests:rate5m",
						Expr:   "sum(rate(http_requests_total[5m])) by (service)",
						Labels: map[string]string{"team": "platform"},
					},
				},
			},
		},
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
	require.Len(t, rule.Groups, 1)
	assert.Equal(t, original.Groups[0].Name, rule.Groups[0].Name)
	assert.Equal(t, original.Groups[0].Rules[0].Expr, rule.Groups[0].Rules[0].Expr)
	assert.Equal(t, original.Groups[0].Rules[0].Record, rule.Groups[0].Rules[0].Record)
	assert.Equal(t, original.Groups[0].Rules[0].Labels, rule.Groups[0].Rules[0].Labels)
}

func TestRuleToResource_AlertRule(t *testing.T) {
	original := kg.Rule{
		Name: "alert-rule",
		Groups: []kg.RuleGroup{
			{
				Name: "alerts",
				Rules: []kg.PromRule{
					{
						Alert:       "HighErrorRate",
						Expr:        "error_rate > 0.5",
						Labels:      map[string]string{"severity": "critical"},
						Annotations: map[string]string{"summary": "High error rate detected"},
					},
				},
			},
		},
	}

	res, err := kg.RuleToResource(original, "stack-456")
	require.NoError(t, err)

	rule, err := kg.RuleFromResource(res)
	require.NoError(t, err)
	assert.Equal(t, "alert-rule", rule.Name)
	require.Len(t, rule.Groups, 1)
	require.Len(t, rule.Groups[0].Rules, 1)
	assert.Equal(t, "HighErrorRate", rule.Groups[0].Rules[0].Alert)
	assert.Equal(t, original.Groups[0].Rules[0].Annotations, rule.Groups[0].Rules[0].Annotations)
}
