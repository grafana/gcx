package graph

import (
	"image/color"
	"regexp"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/grafana/gcx/internal/query/loki"
	"github.com/grafana/gcx/internal/style"
)

// LogLevel is a detected log severity level, matching Grafana's own
// LogLevel enum (public/app/features/logs/logsModel.ts).
type LogLevel string

// Log levels, matching Grafana core's LogLevel enum exactly.
const (
	LogLevelCritical LogLevel = "critical"
	LogLevelError    LogLevel = "error"
	LogLevelWarning  LogLevel = "warning"
	LogLevelInfo     LogLevel = "info"
	LogLevelDebug    LogLevel = "debug"
	LogLevelTrace    LogLevel = "trace"
	LogLevelUnknown  LogLevel = "unknown"
)

// Colors for levels that aren't part of gcx's shared style.ChartPalette.
// Values match Grafana's dark-theme LogLevelColor entries (logsModel.ts),
// which are the right choice for gcx's typically-dark terminal background.
//
//nolint:gochecknoglobals
var (
	colorLevelDebug   = lipgloss.Color("#9e9e9e")
	colorLevelUnknown = lipgloss.Color("#8e8e8e")
)

// LevelColor returns the color Grafana/Logs Drilldown uses for a given log
// level, verified against packages/grafana-ui/src/utils/colors.ts and
// public/app/features/logs/logsModel.ts's LogLevelColor in grafana/grafana.
// Five of the seven levels reuse gcx's existing style.ChartPalette, which is
// the same classic 10-color array Grafana's LogLevelColor indexes into.
func LevelColor(level LogLevel) color.Color {
	switch level {
	case LogLevelCritical:
		return style.ChartPalette[7] // #705DA0 violet
	case LogLevelError:
		return style.ChartPalette[4] // #E24D42 red
	case LogLevelWarning:
		return style.ChartPalette[1] // #EAB839 mustard
	case LogLevelInfo:
		return style.ChartPalette[5] // #1F78C1 ocean blue
	case LogLevelTrace:
		return style.ChartPalette[2] // #6ED0E0 light blue
	case LogLevelDebug:
		return colorLevelDebug
	default:
		return colorLevelUnknown
	}
}

// levelBodyPattern scans a log line body for a level keyword when no
// structured level metadata is available. Ordered longest-match-first
// (CRITICAL/WARNING before their abbreviations) isn't needed since word
// boundaries disambiguate.
var levelBodyPattern = regexp.MustCompile(`(?i)\b(critical|fatal|error|warn(?:ing)?|info|debug|trace)\b`)

// DetectLevel determines a LogEntry's level, layering a free-text regex
// fallback on top of loki.DetectedLevel (structured metadata, parsed
// fields, and body-JSON/logfmt parsing) for lines that carry no structure
// at all — e.g. a plain "ERROR: something broke" line.
func DetectLevel(stream map[string]string, entry loki.LogEntry) LogLevel {
	if lvl := loki.DetectedLevel(stream, entry); lvl != "" {
		if level := NormalizeLevel(lvl); level != LogLevelUnknown {
			return level
		}
	}
	if match := levelBodyPattern.FindString(entry.Line); match != "" {
		return NormalizeLevel(match)
	}
	return LogLevelUnknown
}

// NormalizeLevel maps a raw level string (from a label, parsed field, or
// regex match) onto one of the canonical LogLevel values.
func NormalizeLevel(raw string) LogLevel {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "critical", "crit":
		return LogLevelCritical
	case "fatal", "error", "err":
		return LogLevelError
	case "warning", "warn":
		return LogLevelWarning
	case "info", "information":
		return LogLevelInfo
	case "debug", "dbg":
		return LogLevelDebug
	case "trace":
		return LogLevelTrace
	default:
		return LogLevelUnknown
	}
}
