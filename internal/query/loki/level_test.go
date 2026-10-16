package loki_test

import (
	"testing"

	"github.com/grafana/gcx/internal/query/loki"
	"github.com/stretchr/testify/assert"
)

func TestDetectedLevel(t *testing.T) {
	tests := []struct {
		name   string
		stream map[string]string
		entry  loki.LogEntry
		want   string
	}{
		{
			name:  "structured metadata takes priority",
			entry: loki.LogEntry{Line: `{"level":"info"}`, StructuredMetadata: map[string]string{"detected_level": "error"}},
			want:  "error",
		},
		{
			name:  "parsed level field",
			entry: loki.LogEntry{Line: "some line", Parsed: map[string]string{"level": "warn"}},
			want:  "warn",
		},
		{
			name:  "parsed detected_level field",
			entry: loki.LogEntry{Line: "some line", Parsed: map[string]string{"detected_level": "debug"}},
			want:  "debug",
		},
		{
			name:  "body-parsed JSON level with no parser stage in the query",
			entry: loki.LogEntry{Line: `{"level":"error","message":"boom"}`},
			want:  "error",
		},
		{
			name:  "body-parsed logfmt level",
			entry: loki.LogEntry{Line: `level=warn msg="disk almost full"`},
			want:  "warn",
		},
		{
			name:   "stream label fallback",
			stream: map[string]string{"detected_level": "trace"},
			entry:  loki.LogEntry{Line: "some line"},
			want:   "trace",
		},
		{
			name:  "no signal at all",
			entry: loki.LogEntry{Line: "just some unstructured text"},
			want:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, loki.DetectedLevel(tt.stream, tt.entry))
		})
	}
}
