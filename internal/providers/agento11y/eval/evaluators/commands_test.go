package evaluators_test

import (
	"bytes"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/grafana/gcx/internal/providers/agento11y/eval"
	"github.com/grafana/gcx/internal/providers/agento11y/eval/evaluators"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
