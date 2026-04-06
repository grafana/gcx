package evaluators_test

import (
	"bytes"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

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

func TestTestTableCodec_Encode(t *testing.T) {
	passed := true
	failed := false
	resp := &eval.EvalTestResponse{
		GenerationID:    "gen-abc",
		ConversationID:  "conv-1",
		ExecutionTimeMs: 250,
		Scores: []eval.EvalTestScore{
			{Key: "quality", Type: "number", Value: 0.9, Passed: &passed, Explanation: "Good quality"},
			{Key: "safety", Type: "boolean", Value: true, Passed: &failed, Explanation: ""},
		},
	}

	codec := &evaluators.TestTableCodec{}
	var buf bytes.Buffer
	require.NoError(t, codec.Encode(&buf, resp))

	output := buf.String()
	assert.Contains(t, output, "KEY")
	assert.Contains(t, output, "quality")
	assert.Contains(t, output, "yes")
	assert.Contains(t, output, "safety")
	assert.Contains(t, output, "no")
	assert.Contains(t, output, "gen-abc")
	assert.Contains(t, output, "250ms")
}

func TestTestTableCodec_UTF8Truncation(t *testing.T) {
	// Issue 4: Byte-based truncation can split multi-byte UTF-8 characters.
	// Use a string of 2-byte runes (e.g., Cyrillic) that exceeds 60 runes.
	longExplanation := strings.Repeat("Ж", 65) // each Ж is 2 bytes
	resp := &eval.EvalTestResponse{
		GenerationID: "gen-utf8",
		Scores: []eval.EvalTestScore{
			{Key: "q", Type: "string", Value: "ok", Explanation: longExplanation},
		},
	}

	codec := &evaluators.TestTableCodec{}
	var buf bytes.Buffer
	require.NoError(t, codec.Encode(&buf, resp))

	output := buf.String()
	// The output must be valid UTF-8 (no mid-rune slice).
	assert.True(t, utf8.ValidString(output), "output must be valid UTF-8")
	// The explanation should be truncated with "..." suffix.
	assert.Contains(t, output, "...")
}

func TestTestTableCodec_NilPassed(t *testing.T) {
	resp := &eval.EvalTestResponse{
		GenerationID: "gen-1",
		Scores: []eval.EvalTestScore{
			{Key: "sentiment", Type: "string", Value: "positive", Passed: nil, Explanation: ""},
		},
	}

	codec := &evaluators.TestTableCodec{}
	var buf bytes.Buffer
	require.NoError(t, codec.Encode(&buf, resp))

	output := buf.String()
	assert.Contains(t, output, "sentiment")
	// nil passed shows "-", empty explanation shows "-"
	lines := bytes.Split(buf.Bytes(), []byte("\n"))
	found := false
	for _, line := range lines {
		if bytes.Contains(line, []byte("sentiment")) {
			found = true
			break
		}
	}
	assert.True(t, found)
}

func TestTestTableCodec_WrongType(t *testing.T) {
	codec := &evaluators.TestTableCodec{}
	var buf bytes.Buffer
	err := codec.Encode(&buf, "not-a-response")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "expected *EvalTestResponse")
}

func TestTestTableCodec_DecodeUnsupported(t *testing.T) {
	codec := &evaluators.TestTableCodec{}
	err := codec.Decode(nil, nil)
	require.Error(t, err)
}
