package loki

import (
	"regexp"
	"strings"
	"time"

	"github.com/grafana/gcx/internal/shared"
)

// ScanStatsRecoveryHint applies to unavailable or malformed index statistics,
// not to expressions that the estimator cannot understand.
const ScanStatsRecoveryHint = "Resolve the index-statistics failure and retry with --estimate-scan, or obtain approval for unknown volume."

var offsetKeyword = regexp.MustCompile(`\boffset\b`)

// scanSelector extracts scan inputs, not a complete LogQL AST. Exactly one
// selector and one literal range may occur outside strings. Outer aggregations
// and pipeline stages cannot introduce another log source or time window.
// Ambiguous syntax stays unknown rather than estimating a subset of the query.
func scanSelector(expr string) (string, time.Duration, time.Duration, bool) {
	if selector, ok := logSelector(expr); ok {
		return selector, 0, 0, true
	}
	masked, ok := maskScanStrings(expr)
	if !ok || strings.ContainsAny(masked, "#$@") {
		return "", 0, 0, false
	}
	var stack []byte
	selectorStart, selectorEnd, rangeStart, rangeEnd := -1, -1, -1, -1
	for i := range len(masked) {
		switch ch := masked[i]; ch {
		case '{', '[':
			if ch == '{' {
				if selectorStart != -1 || rangeStart != -1 {
					return "", 0, 0, false
				}
				selectorStart = i
			} else {
				if rangeStart != -1 || selectorEnd == -1 {
					return "", 0, 0, false
				}
				rangeStart = i
			}
			stack = append(stack, ch)
		case '(':
			stack = append(stack, ch)
		case '}', ']', ')':
			if len(stack) == 0 || !scanDelimiterPair(stack[len(stack)-1], ch) {
				return "", 0, 0, false
			}
			stack = stack[:len(stack)-1]
			switch ch {
			case '}':
				selectorEnd = i
			case ']':
				rangeEnd = i
			}
		}
	}
	if len(stack) != 0 || selectorStart < 0 || rangeEnd < 0 || !strings.Contains(masked[:selectorStart], "(") {
		return "", 0, 0, false
	}
	lookback, err := shared.ParseDuration(strings.TrimSpace(expr[rangeStart+1 : rangeEnd]))
	if err != nil || lookback <= 0 {
		return "", 0, 0, false
	}
	offset, ok := scanOffset(expr, masked, selectorStart, selectorEnd, rangeEnd)
	if !ok {
		return "", 0, 0, false
	}
	return expr[selectorStart : selectorEnd+1], lookback, offset, true
}

// Ignore label names inside the selector, but reject offset tokens anywhere
// except immediately after the range. Never silently drop a time modifier.
func scanOffset(expr, masked string, selectorStart, selectorEnd, rangeEnd int) (time.Duration, bool) {
	outsideSelector := masked[:selectorStart] + strings.Repeat(" ", selectorEnd-selectorStart+1) + masked[selectorEnd+1:]
	offsets := offsetKeyword.FindAllStringIndex(outsideSelector, -1)
	if len(offsets) == 0 {
		return 0, true
	}
	if len(offsets) != 1 {
		return 0, false
	}
	pos := offsets[0]
	if pos[0] <= rangeEnd || strings.TrimSpace(masked[rangeEnd+1:pos[0]]) != "" {
		return 0, false
	}
	tail := expr[pos[1]:]
	if len(tail) == 0 || !strings.ContainsRune(" \t\r\n", rune(tail[0])) {
		return 0, false
	}
	tail = strings.TrimLeft(tail, " \t\r\n")
	end := strings.IndexAny(tail, " \t\r\n)")
	if end < 0 {
		end = len(tail)
	}
	offset, err := shared.ParseDuration(tail[:end])
	return offset, err == nil && offset > 0
}

func scanDelimiterPair(left, right byte) bool {
	return left == '(' && right == ')' || left == '{' && right == '}' || left == '[' && right == ']'
}

// Preserve byte positions while hiding quoted syntax, including escaped quotes.
func maskScanStrings(expr string) (string, bool) {
	masked := []byte(expr)
	var quote byte
	escaped := false
	for i, ch := range masked {
		if quote != 0 {
			masked[i] = ' '
			switch {
			case escaped:
				escaped = false
			case ch == '\\' && quote != '`':
				escaped = true
			case ch == quote:
				quote = 0
			}
		} else if ch == '"' || ch == '`' {
			quote = ch
			masked[i] = ' '
		}
	}
	return string(masked), quote == 0
}
