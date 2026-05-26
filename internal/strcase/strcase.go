// Package strcase provides string case conversion (snake_case, kebab-case,
// PascalCase). It replaces the github.com/huandu/xstrings dependency, covering
// the three conversion functions used by the dev scaffold/import/generate commands.
package strcase

import (
	"strings"
	"unicode"
)

// splitWords splits s into words by detecting transitions between
// lowercase/uppercase, letter/digit, or delimiter characters (-_. and space).
func splitWords(s string) []string {
	var words []string
	var current []rune

	flush := func() {
		if len(current) > 0 {
			words = append(words, string(current))
			current = current[:0]
		}
	}

	runes := []rune(s)
	for i, r := range runes {
		if r == '-' || r == '_' || r == '.' || r == ' ' {
			flush()
			continue
		}

		if i > 0 {
			prev := runes[i-1]
			// Transition: lower -> upper starts a new word.
			if unicode.IsLower(prev) && unicode.IsUpper(r) {
				flush()
			}
			// Transition: upper -> upper+lower (e.g. "HTMLParser" -> "HTML", "Parser").
			if unicode.IsUpper(prev) && unicode.IsUpper(r) && i+1 < len(runes) && unicode.IsLower(runes[i+1]) {
				flush()
			}
			// Transition: letter <-> digit.
			if unicode.IsLetter(prev) != unicode.IsLetter(r) && unicode.IsDigit(prev) != unicode.IsDigit(r) {
				flush()
			}
		}

		current = append(current, r)
	}

	flush()

	return words
}

func toLowerDelimited(s, sep string) string {
	words := splitWords(s)
	for i, w := range words {
		words[i] = strings.ToLower(w)
	}
	return strings.Join(words, sep)
}

// ToSnakeCase converts s to snake_case.
func ToSnakeCase(s string) string { return toLowerDelimited(s, "_") }

// ToKebabCase converts s to kebab-case.
func ToKebabCase(s string) string { return toLowerDelimited(s, "-") }

// ToPascalCase converts s to PascalCase.
func ToPascalCase(s string) string {
	words := splitWords(s)
	for i, w := range words {
		words[i] = strings.ToUpper(w[:1]) + strings.ToLower(w[1:])
	}

	return strings.Join(words, "")
}
