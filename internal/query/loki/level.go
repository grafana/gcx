package loki

// DetectedLevel returns the best available level string for a log entry,
// checking (in priority order) Loki's automatic structured-metadata
// detection, a query-time parsed field, and a level/lvl/severity key
// embedded in a JSON/logfmt-structured line body (the same body-parsing
// FormatQueryTable's LEVEL column uses, so callers stay consistent with
// table output even for a bare selector query with no `| json`/`| logfmt`
// stage, where Loki never populates Parsed itself). Returns "" when none of
// these carry a level; callers wanting a free-text regex fallback (e.g. a
// plain "ERROR: ..." line with no structure at all) layer that on top.
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
