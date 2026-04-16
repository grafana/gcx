package query

import (
	"time"

	"github.com/grafana/gcx/internal/shared"
)

// ParseTime delegates to dates.ParseTime.
// Kept for backward compatibility with existing callers.
func ParseTime(s string, now time.Time) (time.Time, error) {
	return shared.ParseTime(s, now)
}

// ParseDuration delegates to dates.ParseDuration.
// Kept for backward compatibility with existing callers.
func ParseDuration(s string) (time.Duration, error) {
	return shared.ParseDuration(s)
}
