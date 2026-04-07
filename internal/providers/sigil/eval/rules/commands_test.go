package rules_test

import (
	"bytes"
	"testing"
	"time"

	"github.com/grafana/gcx/internal/providers/sigil/eval"
	"github.com/grafana/gcx/internal/providers/sigil/eval/rules"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTableCodec_Encode(t *testing.T) {
	items := []eval.RuleDefinition{
		{RuleID: "rule-1", Enabled: true, Selector: "user_visible_turn", SampleRate: 1.0,
			EvaluatorIDs: []string{"eval-1", "eval-2"}, CreatedBy: "admin",
			CreatedAt: time.Date(2026, 4, 1, 10, 0, 0, 0, time.UTC)},
		{RuleID: "rule-2", Enabled: false, Selector: "all_assistant_generations", SampleRate: 0.5,
			EvaluatorIDs: nil},
	}

	tests := []struct {
		name string
		wide bool
		want []string
	}{
		{
			name: "table format",
			wide: false,
			want: []string{"ID", "ENABLED", "SELECTOR", "SAMPLE RATE", "EVALUATORS",
				"rule-1", "yes", "user_visible_turn", "1", "eval-1, eval-2"},
		},
		{
			name: "wide includes CREATED BY",
			wide: true,
			want: []string{"CREATED BY", "CREATED AT", "admin", "2026-04-01 10:00"},
		},
		{
			name: "disabled shows no",
			wide: false,
			want: []string{"rule-2", "no"},
		},
		{
			name: "nil evaluators shows dash",
			wide: false,
			want: []string{"-"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			codec := &rules.TableCodec{Wide: tc.wide}
			var buf bytes.Buffer
			require.NoError(t, codec.Encode(&buf, items))

			output := buf.String()
			for _, s := range tc.want {
				assert.Contains(t, output, s)
			}
		})
	}
}

func TestTableCodec_WrongType(t *testing.T) {
	codec := &rules.TableCodec{}
	var buf bytes.Buffer
	err := codec.Encode(&buf, "not-a-slice")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "expected []RuleDefinition")
}

func TestTableCodec_Format(t *testing.T) {
	tests := []struct {
		wide   bool
		expect string
	}{
		{false, "table"},
		{true, "wide"},
	}
	for _, tc := range tests {
		codec := &rules.TableCodec{Wide: tc.wide}
		assert.Equal(t, tc.expect, string(codec.Format()))
	}
}

func TestTableCodec_DecodeUnsupported(t *testing.T) {
	codec := &rules.TableCodec{}
	err := codec.Decode(nil, nil)
	require.Error(t, err)
}
