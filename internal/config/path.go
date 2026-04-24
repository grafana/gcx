package config

import (
	"fmt"
	"strings"
)

// ResolveContextPath rewrites a bare config path (e.g. "cloud.token") to a
// context-qualified path (e.g. "contexts.dev.cloud.token") by prefixing the
// current context. Paths whose first segment already targets a top-level
// Config field (see types.go) are returned unchanged.
//
// Returns an error if the path is bare but no current context is set.
func ResolveContextPath(cfg Config, path string) (string, error) {
	first, _, _ := strings.Cut(path, ".")
	switch first {
	case "contexts", "current-context":
		return path, nil
	}
	if cfg.CurrentContext == "" {
		return "", fmt.Errorf("no current context set; use a fully qualified path (e.g. contexts.<name>.%s) or set one with: gcx config use-context <name>", path)
	}
	return "contexts." + cfg.CurrentContext + "." + path, nil
}
