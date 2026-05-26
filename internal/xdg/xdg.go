package xdg

import (
	"os"
	"path/filepath"
	"strings"
)

// ConfigHome returns the XDG config home directory.
// Reads $XDG_CONFIG_HOME at call time; defaults to $HOME/.config.
func ConfigHome() string {
	if v := os.Getenv("XDG_CONFIG_HOME"); v != "" {
		return v
	}
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, ".config")
	}
	return ""
}

// StateHome returns the XDG state home directory.
// Reads $XDG_STATE_HOME at call time; defaults to $HOME/.local/state.
func StateHome() string {
	if v := os.Getenv("XDG_STATE_HOME"); v != "" {
		return v
	}
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, ".local", "state")
	}
	return ""
}

// ConfigDirs returns the list of XDG system config directories.
// Reads $XDG_CONFIG_DIRS at call time; defaults to ["/etc/xdg"].
func ConfigDirs() []string {
	if v := os.Getenv("XDG_CONFIG_DIRS"); v != "" {
		return strings.Split(v, string(os.PathListSeparator))
	}
	return []string{"/etc/xdg"}
}

// ConfigFile returns the full path for a config file relative to ConfigHome,
// creating intermediate directories as needed.
func ConfigFile(relPath string) (string, error) {
	p := filepath.Join(ConfigHome(), relPath)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return "", err
	}
	return p, nil
}
