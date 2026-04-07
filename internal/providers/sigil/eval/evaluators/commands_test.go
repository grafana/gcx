package evaluators_test

import (
	"bytes"
	"testing"
	"time"

	"github.com/grafana/gcx/internal/providers/sigil/eval"
	"github.com/grafana/gcx/internal/providers/sigil/eval/evaluators"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTableCodec_Encode(t *testing.T) {
	items := []eval.EvaluatorDefinition{
		{EvaluatorID: "eval-1", Version: "1.0", Kind: "llm_judge", Description: "Quality check",
			OutputKeys: []eval.OutputKey{{Key: "score"}}, CreatedBy: "admin",
			CreatedAt: time.Date(2026, 4, 1, 10, 0, 0, 0, time.UTC)},
		{EvaluatorID: "eval-2", Version: "2.0", Kind: "regex", Description: ""},
	}

	tests := []struct {
		name string
		wide bool
		want []string
	}{
		{
			name: "table format",
			wide: false,
			want: []string{"ID", "VERSION", "KIND", "DESCRIPTION", "eval-1", "1.0", "llm_judge", "Quality check"},
		},
		{
			name: "wide includes OUTPUTS and CREATED BY",
			wide: true,
			want: []string{"OUTPUTS", "CREATED BY", "1", "admin", "2026-04-01 10:00"},
		},
		{
			name: "empty description shows dash",
			wide: false,
			want: []string{"eval-2", "-"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			codec := &evaluators.TableCodec{Wide: tc.wide}
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
	codec := &evaluators.TableCodec{}
	var buf bytes.Buffer
	err := codec.Encode(&buf, "not-a-slice")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "expected []EvaluatorDefinition")
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
		codec := &evaluators.TableCodec{Wide: tc.wide}
		assert.Equal(t, tc.expect, string(codec.Format()))
	}
}

func TestTableCodec_DecodeUnsupported(t *testing.T) {
	codec := &evaluators.TableCodec{}
	err := codec.Decode(nil, nil)
	require.Error(t, err)
}
