package loki_test

import (
	"testing"
	"time"

	"github.com/grafana/gcx/internal/query/loki"
)

func TestMaxLookback(t *testing.T) {
	tests := []struct {
		name        string
		expr        string
		wantBack    time.Duration
		wantForward time.Duration
	}{
		{
			name: "no range vector or offset",
			expr: `{app="x"}`,
		},
		{
			name: "range vector alone",
			// The exact case from review: an instant count_over_time([24h])
			// must be recognized as reaching back 24h, not just the CLI's
			// own now-1m..now instant-query default.
			expr:     `count_over_time({job="x"}[24h])`,
			wantBack: 24 * time.Hour,
		},
		{
			name:     "compound duration",
			expr:     `rate({app="x"}[1h30m])`,
			wantBack: time.Hour + 30*time.Minute,
		},
		{
			name:     "offset alone",
			expr:     `count_over_time({app="x"}[5m] offset 1h)`,
			wantBack: 5*time.Minute + time.Hour,
		},
		{
			name:     "offset is case-insensitive per LogQL grammar tolerance",
			expr:     `count_over_time({app="x"}[5m] OFFSET 1h)`,
			wantBack: 5*time.Minute + time.Hour,
		},
		{
			name:     "multiple range vectors takes the max",
			expr:     `count_over_time({a="1"}[5m]) + count_over_time({b="2"}[1h])`,
			wantBack: time.Hour,
		},
		{
			name:     "multiple offsets takes the max",
			expr:     `count_over_time({a="1"}[1m] offset 10m) + count_over_time({b="2"}[1m] offset 2h)`,
			wantBack: time.Minute + 2*time.Hour,
		},
		{
			// LogQL accepts a negative offset (grafana/loki#7545), which
			// shifts the evaluated window forward (toward/past "now")
			// rather than backward — it must widen the end of the checked
			// window, not the start.
			name:        "negative offset widens forward, not backward",
			expr:        `count_over_time({app="x"}[5m] offset -5m)`,
			wantBack:    5 * time.Minute, // from the range vector
			wantForward: 5 * time.Minute,
		},
		{
			name:        "negative offset alone",
			expr:        `{app="x"} offset -10m`,
			wantForward: 10 * time.Minute,
		},
		{
			name:        "mix of positive and negative offsets takes the max in each direction",
			expr:        `count_over_time({a="1"}[1m] offset 10m) + count_over_time({b="2"}[1m] offset -20m)`,
			wantBack:    time.Minute + 10*time.Minute,
			wantForward: 20 * time.Minute,
		},
		{
			// A regex character class inside a quoted matcher value also
			// uses square brackets; it must not be mistaken for a range
			// vector duration.
			name: "non-duration bracket content is ignored",
			expr: `{app=~"[a-z]+"}`,
		},
		{
			// Regression: a line filter's quoted string can contain text
			// that looks like an offset/range-vector token; it must not
			// falsely widen the window.
			name: "quoted offset-looking text in a line filter is ignored",
			expr: `{app="x"} |= "offset 24h"`,
		},
		{
			name: "quoted bracket-looking text in a line filter is ignored",
			expr: "{app=\"x\"} |= `value[5m]`",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			back, forward := loki.MaxLookback(tt.expr)
			if back != tt.wantBack || forward != tt.wantForward {
				t.Errorf("MaxLookback(%q) = (back=%v, forward=%v), want (back=%v, forward=%v)",
					tt.expr, back, forward, tt.wantBack, tt.wantForward)
			}
		})
	}
}
