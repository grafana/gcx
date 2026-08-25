package config

import (
	"fmt"
	"strings"
)

// cloudEntryFields are the field names that can appear under a cloud entry,
// used to give bare legacy paths ("cloud.token") a pointed error instead of
// silently creating an entry named after the field.
//
//nolint:gochecknoglobals // constant-like lookup list; never mutated.
var cloudEntryFields = map[string]bool{
	"token":                  true,
	"oauth-token":            true,
	"oauth-token-expires-at": true,
	"oauth-url":              true,
	"api-url":                true,
}

// legacyDatasourceFieldHints maps removed legacy per-context fields to their
// datasources-map key, for clear errors on old muscle memory.
//
//nolint:gochecknoglobals // constant-like lookup list; never mutated.
var legacyDatasourceFieldHints = map[string]string{
	"default-prometheus-datasource": "prometheus",
	"default-loki-datasource":       "loki",
	"default-pyroscope-datasource":  "pyroscope",
	"default-tempo-datasource":      "tempo",
}

// ValidateConfigPath checks a `gcx config set`/`unset` path. Paths are
// literal: they name the exact location in the config file, starting from a
// top-level section ("stacks.", "cloud.", "contexts.", ...). Bare paths are
// not routed anywhere — they error with the absolute path spelled out,
// computed from the current context where possible so the fix is
// copy-pasteable. Returns the path unchanged when valid.
func ValidateConfigPath(cfg Config, path string) (string, error) {
	first, rest, _ := strings.Cut(path, ".")
	switch first {
	case "contexts", "current-context", "stacks", "resources", "diagnostics", "version":
		return path, nil
	case "cloud":
		if sub, _, _ := strings.Cut(rest, "."); rest == "" || cloudEntryFields[sub] {
			return "", fmt.Errorf("invalid path %q: cloud credentials live in named entries; use cloud.<entry>.%s%s", path, rest, cloudEntryHint(cfg))
		}
		return path, nil
	}

	// Bare paths: name the absolute location instead of guessing. Every error
	// below follows the same "<problem>: <guidance>" shape — the CLI error
	// renderer splits on the first colon into a summary line and a details
	// block, so a stray or missing colon changes how the error presents.
	ctxName, stackName := currentNames(cfg)

	if kind, ok := legacyDatasourceFieldHints[first]; ok {
		return "", fmt.Errorf("legacy path %q was removed: use contexts.%s.datasources.%s", path, ctxName, kind)
	}

	switch first {
	case "grafana", "providers", "slug":
		return "", fmt.Errorf("invalid path %q: paths are literal and this field lives on a stack entry; use stacks.%s.%s", path, stackName, path)
	case "datasources", "stack":
		return "", fmt.Errorf("invalid path %q: paths are literal and this field lives on a context; use contexts.%s.%s", path, ctxName, path)
	}

	return "", fmt.Errorf("unknown path %q: paths start from a top-level section (stacks.<name>., cloud.<entry>., contexts.<name>., resources., current-context)", path)
}

// currentNames returns the current context name and its stack name for use in
// path suggestions, falling back to placeholders when unset.
func currentNames(cfg Config) (string, string) {
	ctxName, stackName := "<name>", "<name>"
	if cfg.CurrentContext != "" {
		ctxName = cfg.CurrentContext
		if cur := cfg.Contexts[cfg.CurrentContext]; cur != nil && cur.Stack != "" {
			stackName = cur.Stack
		}
	}
	return ctxName, stackName
}

// cloudEntryHint names an example cloud entry for error messages: the current
// context's entry when bound, else the sole entry when exactly one exists.
func cloudEntryHint(cfg Config) string {
	if cur := cfg.Contexts[cfg.CurrentContext]; cur != nil && cur.Cloud != "" {
		return fmt.Sprintf(" (current context uses cloud.%s)", cur.Cloud)
	}
	if len(cfg.Cloud) == 1 {
		for name := range cfg.Cloud {
			return fmt.Sprintf(" (e.g. cloud.%s)", name)
		}
	}
	return ""
}
