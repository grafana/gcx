package eval_test

import (
	"testing"

	"github.com/grafana/gcx/internal/providers/sigil/eval"
	"github.com/grafana/gcx/internal/resources/adapter"
	"github.com/stretchr/testify/assert"
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
