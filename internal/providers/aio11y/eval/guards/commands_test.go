package guards_test

import (
	"bytes"
	"testing"
	"time"

	"github.com/grafana/gcx/internal/providers/aio11y/eval"
	"github.com/grafana/gcx/internal/providers/aio11y/eval/guards"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTableCodec_Encode(t *testing.T) {
	items := []eval.HookRuleDefinition{
		{
			RuleID:       "guard-1",
			Enabled:      true,
			Phase:        "preflight",
			Priority:     10,
			Selector:     "all",
			ActionOnFail: "deny",
			EvaluatorIDs: []string{"eval-1", "eval-2"},
			Transform:    &eval.TransformConfig{Patterns: []eval.TransformPattern{{Regex: "secret", Replacement: "[redacted]"}}},
			ToolFilter:   &eval.ToolFilterConfig{BlockedNames: []string{"bad-tool"}},
			CreatedBy:    "admin",
			CreatedAt:    time.Date(2026, 4, 1, 10, 0, 0, 0, time.UTC),
		},
		{
			RuleID:       "guard-2",
			Enabled:      false,
			Phase:        "postflight",
			Selector:     "user_visible_turn",
			ActionOnFail: "warn",
		},
	}

	tests := []struct {
		name string
		wide bool
		want []string
	}{
		{
			name: "table format",
			wide: false,
			want: []string{"ID", "ENABLED", "PHASE", "PRIORITY", "SELECTOR", "ACTION",
				"guard-1", "yes", "preflight", "10", "all", "deny"},
		},
		{
			name: "wide includes evaluators and transform",
			wide: true,
			want: []string{"EVALUATORS", "TRANSFORM", "TOOL FILTER", "CREATED BY", "CREATED AT",
				"eval-1, eval-2", "yes", "admin", "2026-04-01 10:00"},
		},
		{
			name: "disabled row shows no",
			wide: false,
			want: []string{"guard-2", "no", "postflight"},
		},
		{
			name: "wide row without transform/filter shows no",
			wide: true,
			want: []string{"guard-2", "no", "-"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			codec := &guards.TableCodec{Wide: tc.wide}
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
	codec := &guards.TableCodec{}
	var buf bytes.Buffer
	err := codec.Encode(&buf, "not-a-slice")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "expected []HookRuleDefinition")
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
		codec := &guards.TableCodec{Wide: tc.wide}
		assert.Equal(t, tc.expect, string(codec.Format()))
	}
}

func TestTableCodec_DecodeUnsupported(t *testing.T) {
	codec := &guards.TableCodec{}
	err := codec.Decode(nil, nil)
	require.Error(t, err)
}
