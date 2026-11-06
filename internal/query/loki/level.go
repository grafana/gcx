package loki

import "strings"

// DetectedLevel returns the best available level string for a log entry,
// checking (in priority order) Loki's structured-metadata detection, a
// query-time parsed field, and a level/lvl/severity key in a JSON/logfmt
// body. It deliberately trusts Loki's detection over the body, so it can
// differ from the table's LEVEL column, which prefers the body. Returns ""
// when none carry a level; callers layer a free-text fallback on top.
func DetectedLevel(stream map[string]string, e LogEntry) string {
	if lvl := e.StructuredMetadata["detected_level"]; lvl != "" {
		return lvl
	}
	if lvl := e.Parsed["level"]; lvl != "" {
		return lvl
	}
	if lvl := e.Parsed["detected_level"]; lvl != "" {
		return lvl
	}
	if fields, ok := parseStructuredLogBody(e.Line); ok {
		if _, lvl := pickFirst(fields, "level", "lvl", "severity", "detected_level"); lvl != "" {
			return lvl
		}
	}
	if lvl := stream["detected_level"]; lvl != "" {
		return lvl
	}
	return ""
}

// CanonicalLevel mirrors how Loki spells a level in detected_level (trace,
// debug, info, warn, error, critical, fatal, unknown): case-insensitively,
// trc, dbg, inf/information, wrn/warning and err fold onto those spellings, and
// anything else is returned lowercased and trimmed, as Loki leaves it. The set
// is not closed: Loki passes any other detected_level a client supplies through
// unchanged, so callers must keep accepting unrecognized values. It does not
// collapse levels the way graph.NormalizeLevel does (fatal stays fatal,
// unrecognized stays itself).
func CanonicalLevel(raw string) string {
	lvl := strings.ToLower(strings.TrimSpace(raw))
	switch lvl {
	case "trc":
		return "trace"
	case "dbg":
		return "debug"
	case "inf", "information":
		return "info"
	case "wrn", "warning":
		return "warn"
	case "err":
		return "error"
	default:
		return lvl
	}
}
