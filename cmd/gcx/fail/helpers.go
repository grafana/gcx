package fail

import "strings"

// sameRenderedMessage reports whether details and parent render the same
// message, used to suppress redundant output in error formatting.
func sameRenderedMessage(details string, parent string) bool {
	normalize := func(s string) string {
		s = strings.ReplaceAll(s, "\r\n", "\n")
		return strings.TrimSpace(s)
	}

	normalizedDetails := normalize(details)
	if normalizedDetails == "" {
		return false
	}

	return normalizedDetails == normalize(parent)
}
