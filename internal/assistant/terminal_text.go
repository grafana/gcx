package assistant

import (
	"fmt"
	"strings"
	"unicode"
)

// EscapeTerminalText preserves content without emitting terminal controls.
func EscapeTerminalText(value string) string {
	var escaped strings.Builder
	for _, r := range value {
		if !unicode.IsControl(r) {
			escaped.WriteRune(r)
			continue
		}
		switch r {
		case '\n':
			escaped.WriteString(`\n`)
		case '\r':
			escaped.WriteString(`\r`)
		case '\t':
			escaped.WriteString(`\t`)
		default:
			fmt.Fprintf(&escaped, `\u%04x`, r)
		}
	}
	return escaped.String()
}

// singleLineTerminalText removes controls and normalizes whitespace.
func singleLineTerminalText(value string) string {
	value = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, value)
	return strings.Join(strings.Fields(value), " ")
}
