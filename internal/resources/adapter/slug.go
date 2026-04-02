package adapter

import (
	"regexp"
	"strconv"
	"strings"
)

// Compiled regexes for slug generation (RFC 1123 subdomain-safe names).
var (
	nonAlphanumHyphen = regexp.MustCompile(`[^a-z0-9-]+`)
	multiHyphen       = regexp.MustCompile(`-+`)
)

// SlugifyName converts a human-readable name to a K8s-safe slug (RFC 1123 subdomain).
// Non-alphanumeric characters become hyphens, consecutive hyphens collapse,
// and leading/trailing hyphens are trimmed. Returns "resource" for empty input.
func SlugifyName(name string) string {
	s := strings.ToLower(name)
	s = nonAlphanumHyphen.ReplaceAllString(s, "-")
	s = multiHyphen.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if s == "" {
		return "resource"
	}
	return s
}

// ComposeName builds a "slug-id" composite resource name.
func ComposeName(slug, id string) string {
	if slug == "" {
		return id
	}
	return slug + "-" + id
}

// ExtractIDFromSlug recovers a numeric ID from a composite "slug-id" resource name.
// Handles three formats:
//   - "slug-<id>" (e.g. "web-check-8127")  — returns ("8127", true)
//   - "<id>"      (e.g. "8127")            — returns ("8127", true)
//   - "<name>"    (e.g. "web-check")       — returns ("", false)
func ExtractIDFromSlug(name string) (string, bool) {
	// Pure numeric — treat as ID directly.
	if _, err := strconv.Atoi(name); err == nil {
		return name, true
	}
	// "slug-<id>" — extract numeric suffix after the last hyphen.
	if idx := strings.LastIndex(name, "-"); idx >= 0 {
		suffix := name[idx+1:]
		if _, err := strconv.Atoi(suffix); err == nil {
			return suffix, true
		}
	}
	return "", false
}

// ExtractInt64IDFromSlug is like ExtractIDFromSlug but parses the result as int64.
// Convenient for providers whose APIs use numeric IDs (e.g. Synthetic Monitoring).
func ExtractInt64IDFromSlug(name string) (int64, bool) {
	s, ok := ExtractIDFromSlug(name)
	if !ok {
		return 0, false
	}
	id, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, false
	}
	return id, true
}
