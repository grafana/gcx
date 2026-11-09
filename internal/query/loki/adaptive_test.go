package loki_test

import (
	"testing"
	"time"

	"github.com/grafana/gcx/internal/query/loki"
	"github.com/stretchr/testify/assert"
)

func sampledEntry(value string) loki.LogEntry {
	return loki.LogEntry{
		Timestamp:          "1",
		Line:               "line",
		StructuredMetadata: map[string]string{loki.SampledMetadataKey: value},
	}
}

func TestSampledLines(t *testing.T) {
	plain := loki.LogEntry{Timestamp: "1", Line: "line", StructuredMetadata: map[string]string{"stream": "stderr"}}
	notice := loki.FrameNotice{Severity: "warning", Text: "Some logs may have been dropped by Adaptive Logs sampling"}

	tests := []struct {
		name    string
		resp    *loki.QueryResponse
		want    loki.SampledSummary
		wantAny bool
	}{
		{name: "nil response", resp: nil},
		{
			name: "no marker",
			resp: &loki.QueryResponse{Data: loki.QueryResultData{Result: []loki.StreamEntry{{Values: []loki.LogEntry{plain, {Line: "bare"}}}}}},
		},
		{
			name:    "numeric rates across streams keep the maximum",
			resp:    &loki.QueryResponse{Data: loki.QueryResultData{Result: []loki.StreamEntry{{Values: []loki.LogEntry{sampledEntry("10.00"), plain}}, {Values: []loki.LogEntry{sampledEntry("99.99")}}}}},
			want:    loki.SampledSummary{Lines: 2, MaxDropPercent: 99.99},
			wantAny: true,
		},
		{
			name:    "legacy true marker has no rate",
			resp:    &loki.QueryResponse{Data: loki.QueryResultData{Result: []loki.StreamEntry{{Values: []loki.LogEntry{sampledEntry("true")}}}}},
			want:    loki.SampledSummary{Lines: 1},
			wantAny: true,
		},
		{
			name:    "unparseable marker still counts",
			resp:    &loki.QueryResponse{Data: loki.QueryResultData{Result: []loki.StreamEntry{{Values: []loki.LogEntry{sampledEntry("garbage"), sampledEntry("50")}}}}},
			want:    loki.SampledSummary{Lines: 2, MaxDropPercent: 50},
			wantAny: true,
		},
		{
			name:    "notice only",
			resp:    &loki.QueryResponse{Data: loki.QueryResultData{Result: []loki.StreamEntry{{Values: []loki.LogEntry{plain}}}, Notices: []loki.FrameNotice{notice}}},
			want:    loki.SampledSummary{Notice: true},
			wantAny: true,
		},
		{
			name: "unrelated notice",
			resp: &loki.QueryResponse{Data: loki.QueryResultData{Notices: []loki.FrameNotice{{Severity: "warning", Text: "something else"}}}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := loki.SampledLines(tt.resp)
			assert.Equal(t, tt.want, got)
			assert.Equal(t, tt.wantAny, got.Any())
		})
	}
}

func TestQueryRequestEffectiveRange(t *testing.T) {
	now := time.Date(2026, 1, 2, 15, 0, 0, 0, time.UTC)
	start := now.Add(-time.Hour)

	gotStart, gotEnd := loki.QueryRequest{Start: start, End: now}.EffectiveRange(now.Add(time.Hour))
	assert.Equal(t, start, gotStart)
	assert.Equal(t, now, gotEnd)

	gotStart, gotEnd = loki.QueryRequest{}.EffectiveRange(now)
	assert.Equal(t, now.Add(-time.Minute), gotStart)
	assert.Equal(t, now, gotEnd)
}
