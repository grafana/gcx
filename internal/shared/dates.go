package shared

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var relativeTimeRegex = regexp.MustCompile(`^now(?:([+-])(\d+)([smhdwMy]))?$`)
var simpleDurationRegex = regexp.MustCompile(`^(\d+)([smhdwMy])$`)

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
	value, _ := strconv.Atoi(matches[2])
	unit := matches[3]

	if sign == "-" {
		value = -value
	}

	duration, err := durationFromUnit(value, unit)
	if err != nil {
		return time.Time{}, err
	}

	return now.Add(duration), nil
}

func durationFromUnit(value int, unit string) (time.Duration, error) {
	switch unit {
	case "s":
		return time.Duration(value) * time.Second, nil
	case "m":
		return time.Duration(value) * time.Minute, nil
	case "h":
		return time.Duration(value) * time.Hour, nil
	case "d":
		return time.Duration(value) * 24 * time.Hour, nil
	case "w":
		return time.Duration(value) * 7 * 24 * time.Hour, nil
	case "M":
		return time.Duration(value) * 30 * 24 * time.Hour, nil
	case "y":
		return time.Duration(value) * 365 * 24 * time.Hour, nil
	default:
		return 0, fmt.Errorf("unknown time unit: %s", unit)
	}
}

// ParseDuration parses a duration string. Accepted formats:
// - Go duration (e.g., "1h30m", "5m", "30s").
// - Simple durations with extended units: Ns, Nm, Nh, Nd, Nw, NM, Ny.
func ParseDuration(s string) (time.Duration, error) {
	if s == "" {
		return 0, nil
	}

	if m := simpleDurationRegex.FindStringSubmatch(s); m != nil {
		value, _ := strconv.Atoi(m[1])
		return durationFromUnit(value, m[2])
	}

	return time.ParseDuration(s)
}
