package login

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/grafana/gcx/internal/xdg"
)

// sigilConfigPath returns the default path to the shared sigil dotenv config:
// $XDG_CONFIG_HOME/sigil/config.env, falling back to ~/.config/sigil/config.env.
// This matches the path the sigil binary itself resolves.
func sigilConfigPath() (string, error) {
	home := xdg.ConfigHome()
	if home == "" {
		return "", errors.New("cannot resolve config home directory (set XDG_CONFIG_HOME or HOME)")
	}
	return filepath.Join(home, "sigil", "config.env"), nil
}

// WriteSigilConfig merges updates into the dotenv file at path, preserving
// unrelated lines, comments, and ordering. A key whose value is empty is
// removed; a key not already present is appended. The write is atomic
// (temp file + rename) and the file is created with 0600 permissions because
// it holds credentials.
func WriteSigilConfig(path string, updates map[string]string) error {
	var existing []string
	if data, err := os.ReadFile(path); err == nil {
		existing = strings.Split(string(data), "\n")
		// Drop a single trailing empty element produced by a trailing newline
		// so we don't accumulate blank lines on repeated writes.
		if n := len(existing); n > 0 && existing[n-1] == "" {
			existing = existing[:n-1]
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("read %s: %w", path, err)
	}

	applied := make(map[string]bool, len(updates))
	out := make([]string, 0, len(existing)+len(updates))

	for _, line := range existing {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			out = append(out, line)
			continue
		}
		key := lineKey(trimmed)
		val, ok := updates[key]
		if !ok {
			out = append(out, line)
			continue
		}
		applied[key] = true
		if strings.TrimSpace(val) == "" {
			continue // empty value removes the key
		}
		out = append(out, key+"="+val)
	}

	for _, key := range sortedKeys(updates) {
		if applied[key] || strings.TrimSpace(updates[key]) == "" {
			continue
		}
		out = append(out, key+"="+updates[key])
	}

	content := strings.Join(out, "\n") + "\n"

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create %s: %w", dir, err)
	}

	tmp, err := os.CreateTemp(dir, ".config.env-*")
	if err != nil {
		return fmt.Errorf("create temp file in %s: %w", dir, err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }() // no-op after successful rename

	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("chmod temp file: %w", err)
	}
	if _, err := tmp.WriteString(content); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp file: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("rename temp file to %s: %w", path, err)
	}
	return nil
}

// lineKey extracts the KEY from a dotenv line of the form `KEY=value` or
// `export KEY=value`. Returns "" when the line has no key.
func lineKey(line string) string {
	line = strings.TrimSpace(line)
	if rest, ok := strings.CutPrefix(line, "export "); ok {
		line = strings.TrimSpace(rest)
	}
	key, _, ok := strings.Cut(line, "=")
	if !ok {
		return ""
	}
	return strings.TrimSpace(key)
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
