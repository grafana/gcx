package graph_test

import (
	"image/color"
	"testing"

	"github.com/grafana/gcx/internal/graph"
	"github.com/grafana/gcx/internal/query/loki"
	"github.com/stretchr/testify/assert"
)

func TestDetectLevel(t *testing.T) {
	tests := []struct {
		name   string
		stream map[string]string
		entry  loki.LogEntry
		want   graph.LogLevel
	}{
		{
			name:  "structured metadata detected_level takes priority",
			entry: loki.LogEntry{Line: "info: all good", StructuredMetadata: map[string]string{"detected_level": "error"}},
			want:  graph.LogLevelError,
		},
		{
			name:  "parsed level field",
			entry: loki.LogEntry{Line: "some line", Parsed: map[string]string{"level": "warn"}},
			want:  graph.LogLevelWarning,
		},
		{
			name:   "stream label detected_level",
			stream: map[string]string{"detected_level": "debug"},
			entry:  loki.LogEntry{Line: "some line"},
			want:   graph.LogLevelDebug,
		},
		{
			name:  "regex fallback on line body",
			entry: loki.LogEntry{Line: "2024-01-01 ERROR something broke"},
			want:  graph.LogLevelError,
		},
		{
			name:  "no signal at all is unknown",
			entry: loki.LogEntry{Line: "just some text"},
			want:  graph.LogLevelUnknown,
		},
		{
			name:  "critical alias",
			entry: loki.LogEntry{Line: "CRITICAL failure"},
			want:  graph.LogLevelCritical,
		},
		{
			// The line body is JSON but Loki never populated Parsed (no
			// `| json` parser stage in the query) — this must be caught by
			// parsing the body directly (loki.DetectedLevel), not by luck
			// via the regex fallback matching the level word wherever it
			// appears. The message text embeds "warning" before the real
			// "level":"error" field to prove this: a naive regex scan of
			// the raw line would match "warning" first and get it wrong.
			name:  "structured JSON body level beats a misleading word earlier in the raw line",
			entry: loki.LogEntry{Line: `{"message":"retrying after transient warning","level":"error"}`},
			want:  graph.LogLevelError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, graph.DetectLevel(tt.stream, tt.entry))
		})
	}
}

func TestLevelColor_MatchesGrafanaLogLevelColor(t *testing.T) {
	// Expected values verified directly against Grafana's own LogLevelColor
	// (public/app/features/logs/logsModel.ts) and its classic colors[] array
	// (packages/grafana-ui/src/utils/colors.ts) — see internal/graph/loglevel.go.
	tests := []struct {
		level graph.LogLevel
		want  color.RGBA
	}{
		{graph.LogLevelCritical, color.RGBA{R: 0x70, G: 0x5D, B: 0xA0, A: 0xff}},
		{graph.LogLevelError, color.RGBA{R: 0xE2, G: 0x4D, B: 0x42, A: 0xff}},
		{graph.LogLevelWarning, color.RGBA{R: 0xEA, G: 0xB8, B: 0x39, A: 0xff}},
		{graph.LogLevelInfo, color.RGBA{R: 0x1F, G: 0x78, B: 0xC1, A: 0xff}},
		{graph.LogLevelTrace, color.RGBA{R: 0x6E, G: 0xD0, B: 0xE0, A: 0xff}},
		{graph.LogLevelDebug, color.RGBA{R: 0x9e, G: 0x9e, B: 0x9e, A: 0xff}},
		{graph.LogLevelUnknown, color.RGBA{R: 0x8e, G: 0x8e, B: 0x8e, A: 0xff}},
	}
	for _, tt := range tests {
		t.Run(string(tt.level), func(t *testing.T) {
			assert.Equal(t, tt.want, graph.LevelColor(tt.level))
		})
	}
}
