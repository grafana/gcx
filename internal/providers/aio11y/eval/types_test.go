package eval_test

import (
	"encoding/json"
	"testing"

	"github.com/grafana/gcx/internal/providers/aio11y/eval"
	"github.com/grafana/gcx/internal/resources/adapter"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResourceIdentity(t *testing.T) {
	tests := []struct {
		name     string
		namer    adapter.ResourceNamer
		identity adapter.ResourceIdentity
		initial  string
		updated  string
	}{
		{
			name:     "EvaluatorDefinition",
			namer:    eval.EvaluatorDefinition{EvaluatorID: "eval-1"},
			identity: &eval.EvaluatorDefinition{EvaluatorID: "eval-1"},
			initial:  "eval-1",
			updated:  "eval-2",
		},
		{
			name:     "RuleDefinition",
			namer:    eval.RuleDefinition{RuleID: "rule-1"},
			identity: &eval.RuleDefinition{RuleID: "rule-1"},
			initial:  "rule-1",
			updated:  "rule-2",
		},
		{
			name:     "HookRuleDefinition",
			namer:    eval.HookRuleDefinition{RuleID: "hook-1"},
			identity: &eval.HookRuleDefinition{RuleID: "hook-1"},
			initial:  "hook-1",
			updated:  "hook-2",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name+"/GetResourceName", func(t *testing.T) {
			assert.Equal(t, tc.initial, tc.namer.GetResourceName())
		})

		t.Run(tc.name+"/SetResourceName", func(t *testing.T) {
			tc.identity.SetResourceName(tc.updated)
			assert.Equal(t, tc.updated, tc.identity.GetResourceName())
		})
	}
}

// TestRuleDefinition_MinIdleSeconds verifies that min_idle_seconds is serialized
// into the request body for conversation rules and omitted otherwise. This is the
// exact body the rule client sends on create/update.
func TestRuleDefinition_MinIdleSeconds(t *testing.T) {
	idle := 10

	t.Run("conversation rule includes min_idle_seconds", func(t *testing.T) {
		rule := eval.RuleDefinition{
			RuleID:         "online.conversation.report_compiler.response_quality",
			Enabled:        true,
			Selector:       "conversation",
			MinIdleSeconds: &idle,
			SampleRate:     1.0,
			EvaluatorIDs:   []string{"custom.response_quality.v1"},
		}

		body, err := json.Marshal(rule)
		require.NoError(t, err)
		assert.Contains(t, string(body), `"min_idle_seconds":10`)

		var decoded eval.RuleDefinition
		require.NoError(t, json.Unmarshal(body, &decoded))
		require.NotNil(t, decoded.MinIdleSeconds)
		assert.Equal(t, 10, *decoded.MinIdleSeconds)
	})

	t.Run("generation rule omits min_idle_seconds", func(t *testing.T) {
		rule := eval.RuleDefinition{
			RuleID:       "rule-1",
			Enabled:      true,
			Selector:     "user_visible_turn",
			SampleRate:   1.0,
			EvaluatorIDs: []string{"eval-1"},
		}

		body, err := json.Marshal(rule)
		require.NoError(t, err)
		assert.NotContains(t, string(body), "min_idle_seconds")
	})
}
