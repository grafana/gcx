package output

import (
	"os"
	"strings"
)

// Status symbols prefix the lines written by Success, Warning, Error and Info.
// They are Unicode by default and fall back to ASCII when the locale says the
// terminal is not UTF-8, so a terminal or font without the glyphs does not
// print question marks.
const (
	unicodeSuccess = "✔"
	unicodeWarning = "⚠"
	unicodeError   = "✘"
	unicodeInfo    = "ℹ"

	asciiSuccess = "[ok]"
	asciiWarning = "[!]"
	asciiError   = "[x]"
	asciiInfo    = "[i]"
)

// SuccessSymbol returns the symbol that marks a successful step.
func SuccessSymbol() string { return pick(unicodeSuccess, asciiSuccess) }

// WarningSymbol returns the symbol that marks a warning.
func WarningSymbol() string { return pick(unicodeWarning, asciiWarning) }

// ErrorSymbol returns the symbol that marks an error.
func ErrorSymbol() string { return pick(unicodeError, asciiError) }

// InfoSymbol returns the symbol that marks an informational line.
func InfoSymbol() string { return pick(unicodeInfo, asciiInfo) }

func pick(unicode, ascii string) string {
	if localeIsUTF8() {
		return unicode
	}
	return ascii
}

// localeIsUTF8 reports whether the locale charset is UTF-8. The first non-empty
// of LC_ALL, LC_CTYPE and LANG decides, as in POSIX. With none set, it assumes
// UTF-8: that is the default on current systems, and it keeps the existing
// output unchanged unless the environment explicitly asks for something else.
// The environment is read on every call so it can be changed at runtime.
func localeIsUTF8() bool {
	for _, name := range []string{"LC_ALL", "LC_CTYPE", "LANG"} {
		if value := os.Getenv(name); value != "" {
			value = strings.ToLower(value)
			return strings.Contains(value, "utf-8") || strings.Contains(value, "utf8")
		}
	}
	return true
}
