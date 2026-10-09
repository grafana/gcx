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

// numericTimestampPattern matches a bare (optionally signed) integer Unix
// timestamp, with an optional fractional-seconds component.
var numericTimestampPattern = regexp.MustCompile(`^(-?\d+)(?:\.(\d+))?$`)

// ParseTime parses a time string that can be either:
// - RFC3339 format (e.g., "2024-01-15T10:30:00Z").
// - A bare Unix timestamp, with digit count deciding the unit: seconds
// (<=10 digits), milliseconds (11-13), microseconds (14-16), or nanoseconds
// (17-19) — matching how a value copied from a Drilldown/Explore permalink's
// startNs/endNs param (a bare nanosecond-epoch integer) or a dashboard URL's
// from/to param (milliseconds) actually looks. An integer with a
// fractional-seconds part (e.g. "1705315800.123456789") is always seconds
// plus that many fractional digits, regardless of length.
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

	if numericTimestampPattern.MatchString(s) {
		return parseNumericTimestamp(s)
	}

	return time.Time{}, fmt.Errorf("unable to parse time: %s", s)
}

// parseNumericTimestamp parses a bare Unix timestamp matched by
// numericTimestampPattern, using exact integer arithmetic throughout —
// unlike a float64 round trip, this never loses precision on a
// nanosecond-epoch value (19 significant digits, beyond float64's ~15-17).
func parseNumericTimestamp(s string) (time.Time, error) {
	matches := numericTimestampPattern.FindStringSubmatch(s)

	sec, err := strconv.ParseInt(matches[1], 10, 64)
	if err != nil {
		return time.Time{}, fmt.Errorf("unable to parse time: %s", s)
	}

	if matches[2] != "" {
		nsec, err := fractionToNanos(matches[2])
		if err != nil {
			return time.Time{}, fmt.Errorf("unable to parse time: %s", s)
		}
		if strings.HasPrefix(matches[1], "-") {
			nsec = -nsec
		}
		return time.Unix(sec, nsec), nil
	}

	// Decompose into (seconds, nanosecond remainder) in the value's own
	// unit rather than first scaling to a single nanosecond int64 — a
	// 10-digit seconds value times 1e9, or a 13-digit milliseconds value
	// times 1e6, can itself overflow int64 for timestamps a couple
	// centuries out, even though the final (sec, nsec) pair never would.
	switch digits := len(strings.TrimPrefix(matches[1], "-")); {
	case digits <= 10: // seconds
		return time.Unix(sec, 0), nil
	case digits <= 13: // milliseconds
		return time.Unix(sec/1e3, (sec%1e3)*1e6), nil
	case digits <= 16: // microseconds
		return time.Unix(sec/1e6, (sec%1e6)*1e3), nil
	case digits <= 19: // nanoseconds
		return time.Unix(0, sec), nil
	default:
		return time.Time{}, fmt.Errorf("unable to parse time: %s", s)
	}
}

// HasSubMillisecondPrecision reports whether s, as parsed by ParseTime,
// carries meaningful precision finer than a millisecond: a bare
// microsecond- or nanosecond-magnitude timestamp (14+ digits), or a
// fractional-seconds value with more than 3 significant fractional digits.
// Callers use this to decide whether a query needs Loki's own nanosecond-
// capable API instead of Grafana's millisecond-capped query proxy.
func HasSubMillisecondPrecision(s string) bool {
	matches := numericTimestampPattern.FindStringSubmatch(strings.TrimSpace(s))
	if matches == nil {
		return false
	}
	if matches[2] != "" {
		return len(strings.TrimRight(matches[2], "0")) > 3
	}
	return len(strings.TrimPrefix(matches[1], "-")) >= 14
}

// fractionToNanos converts a fractional-seconds digit string (the part
// after the decimal point) to nanoseconds, padding or truncating to exactly
// 9 digits — time.Time itself can't represent anything finer.
func fractionToNanos(digits string) (int64, error) {
	switch {
	case len(digits) > 9:
		digits = digits[:9]
	case len(digits) < 9:
		digits += strings.Repeat("0", 9-len(digits))
	}
	return strconv.ParseInt(digits, 10, 64)
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

	var duration time.Duration
	switch unit {
	case "s":
		duration = time.Duration(value) * time.Second
	case "m":
		duration = time.Duration(value) * time.Minute
	case "h":
		duration = time.Duration(value) * time.Hour
	case "d":
		duration = time.Duration(value) * 24 * time.Hour
	case "w":
		duration = time.Duration(value) * 7 * 24 * time.Hour
	case "M":
		duration = time.Duration(value) * 30 * 24 * time.Hour
	case "y":
		duration = time.Duration(value) * 365 * 24 * time.Hour
	default:
		return time.Time{}, fmt.Errorf("unknown time unit: %s", unit)
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
