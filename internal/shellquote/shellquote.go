// Package shellquote quotes argv tokens so they stay safe to paste into a
// POSIX shell. It backs every gcx feature that echoes back a runnable
// command line (did-you-mean corrections, pagination follow-up commands,
// the instrumentation setup helm formatter).
package shellquote

import "strings"

// safeChars are the characters a POSIX shell passes through a bareword
// unquoted, without special interpretation.
const safeChars = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_-./=:@%+,"

// Quote returns s unmodified if every rune is shell-safe (see safeChars), or
// single-quoted otherwise (see Escape). Use this for argv tokens that should
// stay readable when they don't need quoting, e.g. building a corrected
// command line to display back to a user.
func Quote(s string) string {
	if s != "" && strings.Trim(s, safeChars) == "" {
		return s
	}
	return Escape(s)
}

// Escape always wraps s in single quotes, escaping any embedded single
// quotes using the canonical POSIX form (end-quote, backslash-escaped quote,
// re-open-quote). Use this when a value should always be visibly quoted,
// e.g. helm --set key=value pairs, even when it contains no characters a
// shell would otherwise treat specially.
func Escape(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// Join joins argv tokens into a shell command string, quoting each token
// with Quote.
func Join(tokens []string) string {
	quoted := make([]string, len(tokens))
	for i, token := range tokens {
		quoted[i] = Quote(token)
	}
	return strings.Join(quoted, " ")
}
