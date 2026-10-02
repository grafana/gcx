package shared

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/prometheus/common/model"
)

var relativeTimeRegex = regexp.MustCompile(`^now(?:([+-])(\d+)([smhdwMy]))?$`)

// ParseTime parses a time string that can be either:
// - RFC3339 format (e.g., "2024-01-15T10:30:00Z").
// - Unix timestamp (e.g., "1705315800").
// - Relative time (e.g., "now", "now-1h", "now-30m", "now-7d").
func ParseTime(s string, now time.Time) (time.Time, error) {
	if s == "" {
		return time.Time{}, nil
	}

	s = strings.TrimSpace(s)

	if strings.HasPrefix(s, "now") {
		return parseRelativeTime(s, now)
	}

	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, nil
	}

	if ts, err := strconv.ParseFloat(s, 64); err == nil {
		sec := int64(ts)
		nsec := int64((ts - float64(sec)) * 1e9)
		return time.Unix(sec, nsec), nil
	}

	return time.Time{}, fmt.Errorf("unable to parse time: %s", s)
}

func parseRelativeTime(s string, now time.Time) (time.Time, error) {
	if s == "now" {
		return now, nil
	}

	matches := relativeTimeRegex.FindStringSubmatch(s)
	if matches == nil {
		return time.Time{}, fmt.Errorf("invalid relative time format: %s", s)
	}

	if len(matches) < 4 {
		return now, nil
	}

	sign := matches[1]
	value, err := strconv.ParseInt(matches[2], 10, 64)
	if err != nil {
		return time.Time{}, fmt.Errorf("relative time value %q is out of range: %w", matches[2], err)
	}
	unit := matches[3]

	var duration time.Duration
	var unitDuration time.Duration
	switch unit {
	case "s":
		unitDuration = time.Second
	case "m":
		unitDuration = time.Minute
	case "h":
		unitDuration = time.Hour
	case "d":
		unitDuration = 24 * time.Hour
	case "w":
		unitDuration = 7 * 24 * time.Hour
	case "M":
		unitDuration = 30 * 24 * time.Hour
	case "y":
		unitDuration = 365 * 24 * time.Hour
	default:
		return time.Time{}, fmt.Errorf("unknown time unit: %s", unit)
	}
	if value > int64((time.Duration(1<<63-1))/unitDuration) {
		return time.Time{}, fmt.Errorf("relative time %q is out of range", s)
	}
	duration = time.Duration(value) * unitDuration
	if sign == "-" {
		duration = -duration
	}

	return now.Add(duration), nil
}

// ParseDuration parses a Prometheus-style duration string. It accepts the units
// ms, s, m, h, d, w, and y, optionally compounded from larger to smaller units
// (e.g. "30m", "1h30m", "7d", "2w", "1y"), matching Prometheus and the Grafana
// Explore UI. Unlike Go's time.ParseDuration it understands d/w/y, but it does
// not accept fractional values ("1.5h") or units in reversed order ("30m1h").
// An empty string parses to 0.
func ParseDuration(s string) (time.Duration, error) {
	if s == "" {
		return 0, nil
	}

	d, err := model.ParseDurationAllowNegative(s)
	if err != nil {
		return 0, fmt.Errorf("%w (valid units: s, m, h, d, w, y; e.g. 30m, 1h30m, 7d)", err)
	}
	return time.Duration(d), nil
}
