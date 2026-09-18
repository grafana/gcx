package logs_test

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/grafana/gcx/internal/query/loki"
	tuilogs "github.com/grafana/gcx/internal/tui/logs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNew_FlattensAndSortsAcrossStreams(t *testing.T) {
	resp := &loki.QueryResponse{
		Data: loki.QueryResultData{
			Result: []loki.StreamEntry{
				{
					Stream: map[string]string{"app": "b"},
					Values: []loki.LogEntry{
						{Timestamp: "3000000000", Line: "third"},
					},
				},
				{
					Stream: map[string]string{"app": "a"},
					Values: []loki.LogEntry{
						{Timestamp: "1000000000", Line: "first"},
						{Timestamp: "2000000000", Line: "second"},
					},
				},
			},
		},
	}

	m := tuilogs.New(resp)
	rendered := m.RenderLinesForTest(200, false)
	firstIdx := indexOf(rendered, "first")
	secondIdx := indexOf(rendered, "second")
	thirdIdx := indexOf(rendered, "third")

	require.NotEqual(t, -1, firstIdx)
	require.NotEqual(t, -1, secondIdx)
	require.NotEqual(t, -1, thirdIdx)
	assert.Less(t, firstIdx, secondIdx)
	assert.Less(t, secondIdx, thirdIdx)
}

func TestNew_ExtractsCleanMessageAndLevelFromJSONBody(t *testing.T) {
	resp := &loki.QueryResponse{
		Data: loki.QueryResultData{
			Result: []loki.StreamEntry{
				{
					Stream: map[string]string{"app": "backstage"},
					Values: []loki.LogEntry{
						{Timestamp: "1000000000", Line: `{"level":"info","message":"collating documents"}`},
					},
				},
			},
		},
	}

	rendered := tuilogs.New(resp).RenderLinesForTest(200, false)
	assert.Contains(t, rendered, "INFO")
	assert.Contains(t, rendered, "collating documents")
	// The raw JSON body shouldn't leak into the rendered line verbatim.
	assert.NotContains(t, rendered, `{"level"`)
}

func TestNew_NoLevelLeavesBlankLevelColumn(t *testing.T) {
	resp := &loki.QueryResponse{
		Data: loki.QueryResultData{
			Result: []loki.StreamEntry{
				{
					Stream: map[string]string{"app": "backstage"},
					Values: []loki.LogEntry{
						{Timestamp: "1000000000", Line: "plain unstructured line"},
					},
				},
			},
		},
	}

	rendered := tuilogs.New(resp).RenderLinesForTest(200, false)
	assert.Contains(t, rendered, "plain unstructured line")
	assert.NotContains(t, rendered, "UNKNOWN")
}

func TestNew_EmptyResponse(t *testing.T) {
	assert.Equal(t, "No log lines", tuilogs.New(nil).RenderLinesForTest(200, false))
	assert.Equal(t, "No log lines", tuilogs.New(&loki.QueryResponse{}).RenderLinesForTest(200, false))
}

func TestNew_SkipsUnparsableTimestamps(t *testing.T) {
	resp := &loki.QueryResponse{
		Data: loki.QueryResultData{
			Result: []loki.StreamEntry{
				{
					Stream: map[string]string{"app": "foo"},
					Values: []loki.LogEntry{
						{Timestamp: "not-a-number", Line: "bad entry"},
						{Timestamp: "1000000000", Line: "good entry"},
					},
				},
			},
		},
	}

	rendered := tuilogs.New(resp).RenderLinesForTest(200, false)
	assert.NotContains(t, rendered, "bad entry")
	assert.Contains(t, rendered, "good entry")
}

func TestNew_StripsRedundantLeadingTimestampFromMessage(t *testing.T) {
	resp := &loki.QueryResponse{
		Data: loki.QueryResultData{
			Result: []loki.StreamEntry{
				{
					Stream: map[string]string{"app": "backstage"},
					Values: []loki.LogEntry{
						{Timestamp: "1000000000", Line: `[2026-09-17T21:11:08.106Z] "GET /healthz HTTP/1.1" 200 15`},
					},
				},
			},
		},
	}

	rendered := tuilogs.New(resp).RenderLinesForTest(200, false)
	assert.Contains(t, rendered, `"GET /healthz HTTP/1.1" 200 15`)
	assert.NotContains(t, rendered, "[2026-09-17T21:11:08.106Z]")
}

func TestNew_KeepsBracketedNonTimestampContent(t *testing.T) {
	resp := &loki.QueryResponse{
		Data: loki.QueryResultData{
			Result: []loki.StreamEntry{
				{
					Stream: map[string]string{"app": "backstage"},
					Values: []loki.LogEntry{
						{Timestamp: "1000000000", Line: "[worker-1] processing job 42"},
					},
				},
			},
		},
	}

	rendered := tuilogs.New(resp).RenderLinesForTest(200, false)
	assert.Contains(t, rendered, "[worker-1] processing job 42")
}

func TestModel_WrapOptionAndLiveToggle(t *testing.T) {
	resp := &loki.QueryResponse{
		Data: loki.QueryResultData{
			Result: []loki.StreamEntry{
				{Stream: map[string]string{"app": "foo"}, Values: []loki.LogEntry{{Timestamp: "1000000000", Line: "line"}}},
			},
		},
	}

	m := tuilogs.New(resp, tuilogs.WithWrap(true))
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m, ok := updated.(tuilogs.Model)
	require.True(t, ok)
	assert.True(t, m.SoftWrapForTest(), "WithWrap(true) should start with wrapping enabled")

	updated, _ = m.Update(tea.KeyPressMsg{Text: "w", Code: 'w'})
	m, ok = updated.(tuilogs.Model)
	require.True(t, ok)
	assert.False(t, m.SoftWrapForTest(), "'w' should toggle wrap off")

	updated, _ = m.Update(tea.KeyPressMsg{Text: "w", Code: 'w'})
	m, ok = updated.(tuilogs.Model)
	require.True(t, ok)
	assert.True(t, m.SoftWrapForTest(), "'w' should toggle wrap back on")
}

func TestModel_WrapDefaultsOff(t *testing.T) {
	m := tuilogs.New(&loki.QueryResponse{})
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m, ok := updated.(tuilogs.Model)
	require.True(t, ok)
	assert.False(t, m.SoftWrapForTest())
}

func TestNew_WrapIndentsContinuationLinesUnderMessageColumn(t *testing.T) {
	resp := &loki.QueryResponse{
		Data: loki.QueryResultData{
			Result: []loki.StreamEntry{
				{
					Stream: map[string]string{"app": "foo"},
					Values: []loki.LogEntry{
						{Timestamp: "1000000000", Line: "this is a fairly long message body that should wrap across more than one line when the width is narrow"},
					},
				},
			},
		},
	}

	rendered := tuilogs.New(resp).RenderLinesForTest(60, true)
	lines := strings.Split(strings.TrimRight(rendered, "\n"), "\n")
	require.Greater(t, len(lines), 1, "message should have wrapped onto multiple lines")

	// The first line starts with the timestamp; every continuation line
	// must be indented with EXACTLY the timestamp+level prefix width of
	// blank space — not merely "at least that much" (HasPrefix alone would
	// still pass if the indent were off by one in either direction) — and
	// then continue directly into wrapped text, not more blank space.
	assert.Regexp(t, `^\d{4}-\d{2}-\d{2}T`, lines[0])
	prefixWidth := tuilogs.PrefixWidthForTest()
	indent := strings.Repeat(" ", prefixWidth)
	for _, line := range lines[1:] {
		require.True(t, strings.HasPrefix(line, indent),
			"continuation line %q should be indented under the message column", line)
		require.Greater(t, len(line), prefixWidth, "continuation line %q should carry wrapped text after the indent", line)
		assert.NotEqual(t, byte(' '), line[prefixWidth],
			"continuation line %q should have exactly %d indent spaces, not more", line, prefixWidth)
	}
}

func TestNew_NoWrapKeepsMessageOnOneLine(t *testing.T) {
	resp := &loki.QueryResponse{
		Data: loki.QueryResultData{
			Result: []loki.StreamEntry{
				{
					Stream: map[string]string{"app": "foo"},
					Values: []loki.LogEntry{
						{Timestamp: "1000000000", Line: "this is a fairly long message body that should NOT wrap when wrap mode is off"},
					},
				},
			},
		},
	}

	rendered := tuilogs.New(resp).RenderLinesForTest(60, false)
	lines := strings.Split(strings.TrimRight(rendered, "\n"), "\n")
	assert.Len(t, lines, 1)
}

func indexOf(s, substr string) int {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}
