package conversations_test

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/grafana/gcx/internal/providers/sigil/conversations"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTableCodec_Encode(t *testing.T) {
	now := time.Date(2026, 4, 2, 18, 30, 0, 0, time.UTC)

	convs := []conversations.Conversation{
		{ID: "conv-1", Title: "Debug latency", GenerationCount: 5, LastGenerationAt: now},
		{ID: "conv-2", Title: "", GenerationCount: 1, LastGenerationAt: time.Time{}},
	}

	tests := []struct {
		name    string
		wide    bool
		want    []string
		notWant []string
	}{
		{
			name: "table format",
			wide: false,
			want: []string{"ID", "TITLE", "GENERATIONS", "LAST ACTIVITY", "conv-1", "Debug latency", "5", "2026-04-02 18:30", "conv-2", "-"},
		},
		{
			name: "wide includes CREATED",
			wide: true,
			want: []string{"CREATED", "conv-1"},
		},
		{
			name:    "empty title shows dash",
			wide:    false,
			want:    []string{"-"},
			notWant: []string{"\t\t"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			codec := &conversations.TableCodec{Wide: tc.wide}
			var buf bytes.Buffer
			err := codec.Encode(&buf, convs)
			require.NoError(t, err)

			output := buf.String()
			for _, s := range tc.want {
				assert.Contains(t, output, s)
			}
			for _, s := range tc.notWant {
				assert.NotContains(t, output, s)
			}
		})
	}
}

func TestTableCodec_WrongType(t *testing.T) {
	codec := &conversations.TableCodec{}
	var buf bytes.Buffer
	err := codec.Encode(&buf, "not-a-slice")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "expected []Conversation")
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
		codec := &conversations.TableCodec{Wide: tc.wide}
		assert.Equal(t, tc.expect, string(codec.Format()))
	}
}

func TestTableCodec_DecodeUnsupported(t *testing.T) {
	codec := &conversations.TableCodec{}
	err := codec.Decode(nil, nil)
	require.Error(t, err)
}

func TestTableCodec_TitleTruncation(t *testing.T) {
	convs := []conversations.Conversation{
		{ID: "c1", Title: strings.Repeat("A", 50), GenerationCount: 1},
	}

	codec := &conversations.TableCodec{}
	var buf bytes.Buffer
	require.NoError(t, codec.Encode(&buf, convs))
	assert.Contains(t, buf.String(), "...")
	assert.NotContains(t, buf.String(), strings.Repeat("A", 50))
}

func TestSearchTableCodec_Encode(t *testing.T) {
	now := time.Date(2026, 4, 2, 18, 30, 0, 0, time.UTC)

	results := []conversations.SearchResult{
		{
			ConversationID: "conv-1", ConversationTitle: "Error debug",
			GenerationCount: 10, Models: []string{"gpt-4", "claude-3"},
			Agents: []string{"my-agent"}, ErrorCount: 3, LastGenerationAt: now,
		},
		{
			ConversationID: "conv-2", ConversationTitle: "",
			GenerationCount: 1, Models: nil,
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
			want: []string{"conv-1", "Error debug", "gpt-4, claude-3", "10"},
		},
		{
			name: "wide shows agents and errors",
			wide: true,
			want: []string{"AGENTS", "ERRORS", "my-agent", "3"},
		},
		{
			name: "nil models shows dash",
			wide: false,
			want: []string{"conv-2", "-"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			codec := &conversations.SearchTableCodec{Wide: tc.wide}
			var buf bytes.Buffer
			require.NoError(t, codec.Encode(&buf, results))

			output := buf.String()
			for _, s := range tc.want {
				assert.Contains(t, output, s)
			}
		})
	}
}

func TestSearchTableCodec_WrongType(t *testing.T) {
	codec := &conversations.SearchTableCodec{}
	var buf bytes.Buffer
	err := codec.Encode(&buf, "not-a-slice")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "expected []SearchResult")
}
