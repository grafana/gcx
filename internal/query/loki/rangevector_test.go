package loki_test

import (
	"testing"
	"time"

	"github.com/grafana/gcx/internal/query/loki"
)

func TestMaxLookback(t *testing.T) {
	tests := []struct {
		name string
		expr string
		want time.Duration
	}{
		{
			name: "no range vector or offset",
			expr: `{app="x"}`,
			want: 0,
		},
		{
			name: "range vector alone",
			// The exact case from review: an instant count_over_time([24h])
			// must be recognized as reaching back 24h, not just the CLI's
			// own now-1m..now instant-query default.
			expr: `count_over_time({job="x"}[24h])`,
			want: 24 * time.Hour,
		},
		{
			name: "compound duration",
			expr: `rate({app="x"}[1h30m])`,
			want: time.Hour + 30*time.Minute,
		},
		{
			name: "offset alone",
			expr: `count_over_time({app="x"}[5m] offset 1h)`,
			want: 5*time.Minute + time.Hour,
		},
		{
			name: "offset is case-insensitive per LogQL grammar tolerance",
			expr: `count_over_time({app="x"}[5m] OFFSET 1h)`,
			want: 5*time.Minute + time.Hour,
		},
		{
			name: "multiple range vectors takes the max",
			expr: `count_over_time({a="1"}[5m]) + count_over_time({b="2"}[1h])`,
			want: time.Hour,
		},
		{
			name: "multiple offsets takes the max",
			expr: `count_over_time({a="1"}[1m] offset 10m) + count_over_time({b="2"}[1m] offset 2h)`,
			want: time.Minute + 2*time.Hour,
		},
		{
			// A regex character class inside a quoted matcher value also
			// uses square brackets; it must not be mistaken for a range
			// vector duration.
			name: "non-duration bracket content is ignored",
			expr: `{app=~"[a-z]+"}`,
			want: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := loki.MaxLookback(tt.expr)
			if got != tt.want {
				t.Errorf("MaxLookback(%q) = %v, want %v", tt.expr, got, tt.want)
			}
		})
	}
}
