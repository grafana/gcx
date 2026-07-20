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
	"orgs":                   true,
	"stacks":                 true,
}

// legacyContextFieldHints maps removed legacy per-context paths to their new
// homes, for clear errors on old muscle memory.
//
//nolint:gochecknoglobals // constant-like lookup list; never mutated.
var legacyContextFieldHints = map[string]string{
	"default-prometheus-datasource": "datasources.prometheus",
	"default-loki-datasource":       "datasources.loki",
	"default-pyroscope-datasource":  "datasources.pyroscope",
	"default-tempo-datasource":      "datasources.tempo",
}

// ResolveContextPath rewrites a bare config path to a fully qualified one:
// paths that already target a top-level Config field are returned unchanged,
// stack-owned fields ("grafana.server", "providers.slo.x", "slug") resolve
// through the current context's stack reference, and the remaining bare paths
// ("datasources.prometheus", "stack", "cloud") qualify against the current
// context. Legacy paths that no longer exist get an error naming the new path.
func ResolveContextPath(cfg Config, path string) (string, error) {
	first, rest, _ := strings.Cut(path, ".")
	switch first {
	case "contexts", "current-context", "stacks", "resources", "diagnostics", "version":
		return path, nil
	case "cloud":
		if rest == "" {
			// Bare `cloud` sets the current context's cloud reference.
			break
		}
		if sub, _, _ := strings.Cut(rest, "."); cloudEntryFields[sub] {
			return "", fmt.Errorf("legacy path %q: cloud credentials now live in named entries; use cloud.<entry>.%s%s", path, rest, cloudEntryHint(cfg))
		}
		return path, nil
	}

	if hint, ok := legacyContextFieldHints[first]; ok {
		return "", fmt.Errorf("legacy path %q was removed; use %s", path, hint)
	}

	if cfg.CurrentContext == "" {
		return "", fmt.Errorf("no current context set; use a fully qualified path (e.g. contexts.<name>.%s) or set one with: gcx config use-context <name>", path)
	}

	switch first {
	case "grafana", "providers", "slug":
		cur := cfg.Contexts[cfg.CurrentContext]
		if cur == nil || cur.Stack == "" {
			return "", fmt.Errorf("current context references no stack; use a fully qualified path (stacks.<name>.%s) or run `gcx login`", path)
		}
		return "stacks." + cur.Stack + "." + path, nil
	}

	return "contexts." + cfg.CurrentContext + "." + path, nil
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
