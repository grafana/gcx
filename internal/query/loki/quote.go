package loki

import "strings"

// maskQuoted returns expr with the contents of every double-quoted or
// backtick-quoted string blanked out to spaces (delimiters kept, same
// length/byte-positions preserved), so a scan for LogQL syntax — braces,
// range-vector brackets, "offset" — never matches text that was inside a
// string literal, e.g. a line filter like `|= "offset 24h"`. Backtick
// strings are raw (no escape processing); double-quoted strings support `\`
// escapes.
func maskQuoted(expr string) string {
	var b strings.Builder
	b.Grow(len(expr))

	inQuotes := false
	inBacktick := false
	escaped := false

	for i := range len(expr) {
		c := expr[i]
		switch {
		case inBacktick:
			if c == '`' {
				inBacktick = false
				b.WriteByte(c)
			} else {
				b.WriteByte(' ')
			}
		case escaped:
			escaped = false
			b.WriteByte(' ')
		case c == '\\':
			escaped = true
			b.WriteByte(' ')
		case c == '"':
			inQuotes = !inQuotes
			b.WriteByte(c)
		case inQuotes:
			b.WriteByte(' ')
		case c == '`':
			inBacktick = true
			b.WriteByte(c)
		default:
			b.WriteByte(c)
		}
	}

	return b.String()
}
