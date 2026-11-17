package loki

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
