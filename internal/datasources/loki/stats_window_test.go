package loki //nolint:testpackage // white-box: exercises unexported resolveStatsWindow

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestResolveStatsWindow(t *testing.T) {
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	start, end := now.Add(-time.Hour), now

	tests := map[string]struct {
		expr      string
		isRange   bool
		wantStart time.Time
		wantEnd   time.Time
	}{
		"range query keeps its own window":     {`{job="x"}`, true, start, end},
		"instant query defaults to one minute": {`{job="x"}`, false, now.Add(-time.Minute), now},
		"range vector widens the start":        {`count_over_time({job="x"}[24h])`, true, start.Add(-24 * time.Hour), end},
		"offset widens the start":              {`count_over_time({job="x"}[5m] offset 1h)`, true, start.Add(-65 * time.Minute), end},
		"negative offset extends the end":      {`count_over_time({job="x"}[5m] offset -10m)`, true, start.Add(-5 * time.Minute), end.Add(10 * time.Minute)},
		"quoted offset text is ignored":        {`{job="x"} |= "offset 24h"`, true, start, end},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			gotStart, gotEnd := resolveStatsWindow(tt.expr, tt.isRange, start, end, now)
			assert.True(t, tt.wantStart.Equal(gotStart), "start: want %v, got %v", tt.wantStart, gotStart)
			assert.True(t, tt.wantEnd.Equal(gotEnd), "end: want %v, got %v", tt.wantEnd, gotEnd)
		})
	}
}
