package judge_test

import (
	"bytes"
	"testing"

	"github.com/grafana/gcx/internal/providers/sigil/eval"
	"github.com/grafana/gcx/internal/providers/sigil/eval/judge"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProvidersTableCodec_Encode(t *testing.T) {
	items := []eval.JudgeProvider{
		{ID: "openai", Name: "OpenAI", Type: "openai"},
		{ID: "anthropic", Name: "Anthropic", Type: "anthropic"},
	}

	codec := &judge.ProvidersTableCodec{}
	var buf bytes.Buffer
	require.NoError(t, codec.Encode(&buf, items))

	output := buf.String()
	for _, s := range []string{"ID", "NAME", "TYPE", "openai", "OpenAI", "anthropic", "Anthropic"} {
		assert.Contains(t, output, s)
	}
}

func TestProvidersTableCodec_WrongType(t *testing.T) {
	codec := &judge.ProvidersTableCodec{}
	var buf bytes.Buffer
	err := codec.Encode(&buf, "not-a-slice")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "expected []JudgeProvider")
}

func TestProvidersTableCodec_Format(t *testing.T) {
	codec := &judge.ProvidersTableCodec{}
	assert.Equal(t, "table", string(codec.Format()))
}

func TestProvidersTableCodec_DecodeUnsupported(t *testing.T) {
	codec := &judge.ProvidersTableCodec{}
	err := codec.Decode(nil, nil)
	require.Error(t, err)
}

func TestModelsTableCodec_Encode(t *testing.T) {
	items := []eval.JudgeModel{
		{ID: "gpt-4o", Name: "GPT-4o", Provider: "openai", ContextWindow: 128000},
		{ID: "claude-3", Name: "Claude 3", Provider: "anthropic", ContextWindow: 0},
	}

	tests := []struct {
		name string
		want []string
	}{
		{
			name: "headers and data",
			want: []string{"ID", "NAME", "PROVIDER", "CONTEXT WINDOW",
				"gpt-4o", "GPT-4o", "openai", "128000"},
		},
		{
			name: "zero context window shows dash",
			want: []string{"claude-3", "-"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			codec := &judge.ModelsTableCodec{}
			var buf bytes.Buffer
			require.NoError(t, codec.Encode(&buf, items))

			output := buf.String()
			for _, s := range tc.want {
				assert.Contains(t, output, s)
			}
		})
	}
}

func TestModelsTableCodec_WrongType(t *testing.T) {
	codec := &judge.ModelsTableCodec{}
	var buf bytes.Buffer
	err := codec.Encode(&buf, "not-a-slice")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "expected []JudgeModel")
}

func TestModelsTableCodec_Format(t *testing.T) {
	codec := &judge.ModelsTableCodec{}
	assert.Equal(t, "table", string(codec.Format()))
}

func TestModelsTableCodec_DecodeUnsupported(t *testing.T) {
	codec := &judge.ModelsTableCodec{}
	err := codec.Decode(nil, nil)
	require.Error(t, err)
}
